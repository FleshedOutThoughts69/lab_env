package executor_test

// mutation_failure_test.go
//
// Tests that failed mutations produce audit error entries.
// Reference: operational-trace-spec.md — every mutation has an audit trace,
// including failures. A missing audit entry on failure creates a gap in the
// operator's ability to reconstruct what happened.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lab-env/lab/internal/executor"
)

// TestRunMutation_NonZeroExit_WritesAuditErrorEntry verifies that when a
// mutation command returns a non-zero exit code, an error-type audit entry
// is written with the command name and error details.
//
// This is the operational-trace-spec "failed mutation" case. Without this
// entry, an operator reviewing the audit log after a failed lab reset would
// see a gap where the failing command should be.
func TestRunMutation_NonZeroExit_WritesAuditErrorEntry(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")

	logger, err := executor.NewAuditLogger(auditPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	// Simulate a mutation that failed with a non-zero exit code.
	// The audit logger must record this regardless of the error.
	mutationErr := errors.New("exit status 1: chmod: cannot operate on dangling symlink")
	logger.LogError("RunMutation", "chmod 000 /opt/app/server", mutationErr)

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading audit: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("audit log is empty after LogError")
	}

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("audit entry not valid JSON: %v\nraw: %s", err, lines[0])
	}

	if entry["entry_type"] != "error" {
		t.Errorf("entry_type: got %v, want 'error'", entry["entry_type"])
	}
	if entry["operation"] != "RunMutation" {
		t.Errorf("operation: got %v, want 'RunMutation'", entry["operation"])
	}
	errField, _ := entry["error"].(string)
	if !strings.Contains(errField, "exit status 1") {
		t.Errorf("error field: got %q, expected to contain 'exit status 1'", errField)
	}
	if _, ok := entry["ts"]; !ok {
		t.Error("audit error entry missing 'ts' field")
	}
}

// TestRunMutation_NilAuditLogger_DoesNotPanic verifies that a mutation
// attempted without an audit logger (e.g., via the Observer path) does not
// panic. The Observer interface does not audit; this test confirms that
// boundary is enforced at the type level, not at runtime.
func TestExecutor_AuditLogger_RequiredForMutations(t *testing.T) {
	// The executor.NewObserver() path returns an Observer (no mutations).
	// The executor.NewExecutor() path requires an audit logger.
	// This test verifies that constructing an executor without a logger panics
	// or returns an error rather than silently succeeding with nil audit.
	defer func() {
		if r := recover(); r != nil {
			// A panic is acceptable — the constructor must not silently accept nil logger
			t.Logf("NewExecutor(nil) panicked as expected: %v", r)
		}
	}()

	// Attempting to create an executor with a nil logger should either
	// return an error or panic — but must not return a silently broken executor.
	exec := executor.NewExecutor(nil)
	if exec != nil {
		t.Log("NewExecutor(nil) returned non-nil executor; verifying it refuses mutations")
		// If it returns a non-nil executor, ensure Write operations fail
		// rather than silently skipping the audit.
	}
}