package logging

// logging_edge_test.go
//
// Edge case tests for the logging package.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lab-env/service/logging"
)

// TestLogger_SpecialChars_ProperlyEscaped verifies that log messages and
// key-value pairs containing JSON special characters are properly escaped.
//
// A log line like {"msg":"error: "bad" value"} is invalid JSON and would
// fail L-002 (last line valid JSON). The json.Marshal in logging.go handles
// this, but this test confirms it explicitly.
func TestLogger_SpecialChars_ProperlyEscaped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	defer logger.Close()

	cases := []struct {
		msg  string
		key  string
		val  string
	}{
		{`error: "bad" value`, "path", `/var/lib/app`},
		{`backslash: `, "cmd", `chmod 000 /opt/app/server`},
		{`unicode: ñoño`, "env", `prod`},
		{`newline in \n msg`, "detail", "multi\nline"},
		{`tab\there`, "tag", "value\twith\ttabs"},
	}

	for _, tc := range cases {
		logger.Error(tc.msg, tc.key, tc.val)
	}
	logger.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("expected %d lines, got %d", len(cases), len(lines))
	}

	for i, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON after special chars: %v\nline: %s",
				i+1, err, line)
		}
	}
}

// TestLogger_Close_Idempotent verifies that calling Close() multiple times
// does not panic or produce an error beyond the first call.
//
// The shutdown sequence may call Close from main() defer and from a signal
// handler; the second call must be safe.
func TestLogger_Close_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Close() panicked on second call: %v", r)
		}
	}()

	logger.Close() // first close
	logger.Close() // second close — must not panic
}

// TestLogger_WriteAfterClose_DoesNotPanic verifies that writing to a closed
// logger does not panic. The service may receive a signal and write a log
// entry during cleanup after Close() has been called.
func TestLogger_WriteAfterClose_DoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	logger.Close()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Write after Close panicked: %v", r)
		}
	}()

	// Should not panic — may silently fail or log to stderr
	logger.Info("post-close write")
}

// TestLogger_Levels_ProduceCorrectLevelField verifies that Info, Warn, and
// Error produce entries with level "info", "warn", and "error" respectively.
func TestLogger_Levels_ProduceCorrectLevelField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	logger, err := logging.New(path)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
	logger.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	expectedLevels := []string{"info", "warn", "error"}
	for i, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d not valid JSON: %v", i+1, err)
		}
		if entry["level"] != expectedLevels[i] {
			t.Errorf("line %d: level=%v, want %q", i+1, entry["level"], expectedLevels[i])
		}
	}
}