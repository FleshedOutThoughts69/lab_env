package signals

// signals_edge_test.go
//
// Edge cases for the signal file package not covered by signals_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lab-env/service/signals"
)

// TestBeginShutdown_WhenHealthyAlreadyRemoved verifies that calling
// BeginShutdown when the healthy file has already been removed does not
// return an error and still sets status=ShuttingDown.
//
// External cleanup scripts (e.g., a lab reset that removes signal files)
// may remove healthy before the service's own shutdown sequence runs.
// The service must handle this gracefully.
func TestBeginShutdown_WhenHealthyAlreadyRemoved(t *testing.T) {
	dir := t.TempDir()
	signals.SetDirForTest(dir)
	defer signals.ResetDir()

	// Set up running state but don't create healthy
	if err := signals.Init(); err != nil {
		t.Fatal(err)
	}
	if err := signals.SetStatus(signals.StatusRunning); err != nil {
		t.Fatal(err)
	}
	// healthy is intentionally absent

	// BeginShutdown must not error even when healthy is already absent
	signals.BeginShutdown() // returns void — must not panic

	assertStatus(t, dir, signals.StatusShuttingDown)
	assertFileAbsent(t, dir, "healthy")
}

// TestBeginShutdown_AlsoRemovesPID verifies that RemovePID (called after
// BeginShutdown in the shutdown sequence) removes the PID file.
func TestShutdownSequence_RemovesPID(t *testing.T) {
	dir := t.TempDir()
	signals.SetDirForTest(dir)
	defer signals.ResetDir()

	if err := signals.Init(); err != nil {
		t.Fatal(err)
	}
	if err := signals.WritePID(); err != nil {
		t.Fatal(err)
	}
	assertFileExists(t, dir, "app.pid")

	signals.BeginShutdown()
	signals.RemovePID()

	assertFileAbsent(t, dir, "app.pid")
}

// TestSetStatus_ContentIsExactStringPlusNewline verifies that the status file
// contains exactly the status string followed by a single newline character.
//
// The control plane reads this file and may use exact string matching.
// Extra whitespace, trailing spaces, or missing newlines could cause
// misclassification.
func TestSetStatus_ContentIsExactStringPlusNewline(t *testing.T) {
	dir := t.TempDir()
	signals.SetDirForTest(dir)
	defer signals.ResetDir()

	cases := []string{
		signals.StatusStarting,
		signals.StatusRunning,
		signals.StatusDegraded,
		signals.StatusUnhealthy,
		signals.StatusShuttingDown,
	}

	for _, status := range cases {
		t.Run(status, func(t *testing.T) {
			if err := signals.SetStatus(status); err != nil {
				t.Fatalf("SetStatus(%q): %v", status, err)
			}

			path := filepath.Join(dir, "status")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading status file: %v", err)
			}

			content := string(raw)
			expected := status + "\n"
			if content != expected {
				t.Errorf("status file content: got %q, want %q", content, expected)
			}

			// Verify no extra whitespace beyond the trailing newline
			trimmed := strings.TrimRight(content, "\n")
			if trimmed != status {
				t.Errorf("status file has extra content beyond status string: %q", content)
			}
		})
	}
}

// TestWritePID_ContainsOnlyDecimalPIDAndNewline verifies the PID file format.
// The conformance check P-005 reads this file; format must be exact.
func TestWritePID_ContainsDecimalPIDAndNewline(t *testing.T) {
	dir := t.TempDir()
	signals.SetDirForTest(dir)
	defer signals.ResetDir()

	if err := signals.Init(); err != nil {
		t.Fatal(err)
	}
	if err := signals.WritePID(); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	path := filepath.Join(dir, "app.pid")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading PID file: %v", err)
	}

	content := string(raw)

	// Must end with exactly one newline
	if !strings.HasSuffix(content, "\n") {
		t.Errorf("PID file does not end with newline: %q", content)
	}
	if strings.HasSuffix(content, "\n\n") {
		t.Errorf("PID file has double newline: %q", content)
	}

	// Content before newline must be a valid decimal integer (the PID)
	pidStr := strings.TrimSuffix(content, "\n")
	if len(pidStr) == 0 {
		t.Error("PID file is empty before newline")
	}
	for _, ch := range pidStr {
		if ch < '0' || ch > '9' {
			t.Errorf("PID file contains non-digit character %q in %q", ch, pidStr)
			break
		}
	}
}

// TestRemoveLoading_IdempotentWhenAbsent verifies that calling RemoveLoading
// when the loading file is already absent returns nil (not an error).
func TestRemoveLoading_IdempotentWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	signals.SetDirForTest(dir)
	defer signals.ResetDir()

	// loading file was never created
	if err := signals.RemoveLoading(); err != nil {
		t.Errorf("RemoveLoading when absent: expected nil, got %v", err)
	}
}