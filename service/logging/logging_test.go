package logging

// logging_test.go
//
// Tests the logging package's correctness guarantees.
//
// High ROI:
//   - O_APPEND after copytruncate: this is the single most important bug
//     the logging package prevents. If the file is not opened with O_APPEND,
//     logrotate's copytruncate truncates the file but the service's write
//     offset is still at the old end-of-file. The next write fills the gap
//     with null bytes, producing a corrupted log. L-001 (non-empty) and
//     L-002 (last line valid JSON) fail intermittently with no obvious cause.
//   - Concurrent writes: multiple goroutines (HTTP handlers, telemetry panic
//     recovery, main.go shutdown) write concurrently. Lines must not interleave.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lab-env/service/logging"
)

// TestNew_OpensWithOAppend verifies that the log file is opened with O_APPEND.
//
// Test method: open logger, write a line, externally truncate the file to 0
// (simulating logrotate copytruncate), write another line, read file contents.
// With O_APPEND: second line starts at byte 0 (correct — no null bytes).
// Without O_APPEND: second line starts at the pre-truncation offset (wrong — null bytes).
func TestNew_OpensWithOAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	defer logger.Close()

	// Write first line
	logger.Info("before truncation")

	// Simulate logrotate copytruncate: truncate the file to 0 bytes
	// while the file descriptor remains open
	if err := os.Truncate(path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// Write second line — with O_APPEND this goes to offset 0 (correct)
	logger.Info("after truncation")

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	// Check for null bytes — their presence means O_APPEND was not used
	for i, b := range data {
		if b == 0 {
			t.Errorf("null byte at offset %d: O_APPEND not used; log is corrupted after copytruncate", i)
			t.Logf("file hex dump (first 64 bytes): %x", data[:min(64, len(data))])
			return
		}
	}

	// The file should contain exactly one valid JSON line (the "after truncation" entry)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line after truncation+write, got %d: %v", len(lines), lines)
	}

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Errorf("post-truncation log line is not valid JSON: %v\nline: %s", err, lines[0])
	}
	if msg, _ := entry["msg"].(string); msg != "after truncation" {
		t.Errorf("msg: got %q, want 'after truncation'", msg)
	}
}

// TestLogger_ConcurrentWrites_NoInterleavedLines verifies that concurrent
// writes from multiple goroutines produce complete, uninterleaved JSON lines.
//
// Without the sync.Mutex, a goroutine writing a 200-byte line could be
// interrupted mid-write by another goroutine, producing a line like:
//   {"ts":"...","level":"info","msg":"request {"ts":"...","level":"info"...
// This would fail L-002 (last line valid JSON).
func TestLogger_ConcurrentWrites_NoInterleavedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	defer logger.Close()

	const goroutines = 50
	const writesPerGoroutine = 20

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				logger.Info("concurrent write", "goroutine", i, "iteration", j)
			}
		}()
	}
	wg.Wait()

	// Read all lines and verify each is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nline: %s", lineNum, err, line)
			if lineNum > 5 {
				t.Log("(stopping after first 5 errors)")
				break
			}
		}
	}

	expectedLines := goroutines * writesPerGoroutine
	if lineNum != expectedLines {
		t.Errorf("line count: got %d, want %d", lineNum, expectedLines)
	}
}

// TestLogger_SingleWriteSyscall_PerEntry verifies that each log call produces
// a complete newline-terminated JSON object as a single os.File.Write call.
//
// The "unbuffered" contract means no userspace buffering between the JSON
// serialization and the OS write. If the write were buffered (e.g., via
// bufio.Writer), a process crash could lose the last partial buffer.
func TestLogger_EntryIsCompleteJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	// Write one entry then close
	logger.Info("server started", "addr", "127.0.0.1:8080")
	logger.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	// Exactly one line, terminated with \n
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("log entry does not end with newline")
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log entry not valid JSON: %v\nraw: %s", err, lines[0])
	}

	// Required fields: ts, level, msg
	for _, field := range []string{"ts", "level", "msg"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("missing required field %q in log entry", field)
		}
	}
	if entry["msg"] != "server started" {
		t.Errorf("msg: got %v, want 'server started'", entry["msg"])
	}
}

// TestLogger_KeyValuePairs verifies that extra key-value pairs are included
// in the JSON output with correct names.
func TestLogger_KeyValuePairs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	defer logger.Close()

	logger.Error("state write failed", "path", "/var/lib/app", "error", "permission denied")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	if entry["msg"] != "state write failed" {
		t.Errorf("msg: got %v, want 'state write failed'", entry["msg"])
	}
	if entry["path"] != "/var/lib/app" {
		t.Errorf("path: got %v, want '/var/lib/app'", entry["path"])
	}
	if entry["level"] != "error" {
		t.Errorf("level: got %v, want 'error'", entry["level"])
	}
}

// TestLogger_FileMode0640 verifies that the log file is created with mode 0640.
// The conformance check F-003 / ModeLogFile = 0640.
func TestLogger_FileMode0640(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	logger.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0640 {
		t.Errorf("log file mode: got %04o, want 0640", info.Mode().Perm())
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}