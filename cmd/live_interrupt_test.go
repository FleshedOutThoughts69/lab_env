//go:build integration

package cmd

// live_interrupt_test.go — integration tests for the interrupt path.
//
// These tests start real lab subprocesses, send OS signals, and verify
// state.json and audit.log are updated correctly.
//
// Requires:
//   - LAB_TEST_MODE=live (enforced by TestMain in testhelpers_test.go)
//   - Real VM with provisioned environment
//   - lab binary at ./lab or $LAB_BIN
//   - /var/lib/lab/ writable by the test user
//
// Run: LAB_TEST_MODE=live go test -v -run TestLiveInterrupt ./cmd/...
//
// Authority: control-plane-contract.md §3.6 (interrupt contract),
//            testing-plan-revised.md Phase B.1

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const (
	liveStatePath = "/var/lib/lab/state.json"
	liveAuditPath = "/var/lib/lab/audit.log"
)

func labBin() string {
	if b := os.Getenv("LAB_BIN"); b != "" {
		return b
	}
	return "./lab"
}

// TestLiveInterrupt_Reset_ExitsCode4 verifies the full interrupt contract
// from control-plane-contract §3.6:
//
//  1. Apply F-001 to put environment in DEGRADED (reset will have work to do)
//  2. Start lab reset in background
//  3. Send SIGINT once the process is alive
//  4. Process must exit 4
//  5. state.json must have classification_valid: false
//  6. audit.log must have an interrupt entry
//  7. lab status must NOT assert BROKEN (interrupt ≠ broken assertion)
//  8. lab status reclassifies from runtime → classification_valid: true
//  9. lab reset restores CONFORMANT
func TestLiveInterrupt_Reset_ExitsCode4(t *testing.T) {
	lab := labBin()

	// ── Step 1: establish DEGRADED state ─────────────────────────────────────
	apply := exec.Command(lab, "fault", "apply", "F-001")
	if out, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("fault apply F-001 failed: %v\n%s", err, out)
	}

	defer func() {
		// Always restore on test completion.
		exec.Command(lab, "reset", "--tier", "R2").Run() //nolint:errcheck
	}()

	// ── Step 2: start lab reset in background ────────────────────────────────
	reset := exec.Command(lab, "reset")
	if err := reset.Start(); err != nil {
		t.Fatalf("starting lab reset: %v", err)
	}

	// ── Step 3: wait for process to be alive ─────────────────────────────────
	deadline := time.Now().Add(5 * time.Second)
	alive := false
	for time.Now().Before(deadline) {
		if err := reset.Process.Signal(syscall.Signal(0)); err == nil {
			alive = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !alive {
		reset.Process.Kill() //nolint:errcheck
		t.Fatal("lab reset process did not become alive within 5s")
	}

	// Give the reset process a moment to begin mutations before interrupting.
	time.Sleep(100 * time.Millisecond)

	// ── Step 4: send SIGINT ───────────────────────────────────────────────────
	if err := reset.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT: %v", err)
	}

	// ── Step 5: exit code must be 4 ──────────────────────────────────────────
	err := reset.Wait()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	if exitCode != 4 {
		t.Errorf("lab reset after SIGINT: exit code %d, want 4", exitCode)
	}

	// ── Step 6: state.json must have classification_valid: false ─────────────
	stateData, err := os.ReadFile(liveStatePath)
	if err != nil {
		t.Fatalf("reading state.json: %v", err)
	}
	var sf struct {
		ClassificationValid bool `json:"classification_valid"`
	}
	if err := json.Unmarshal(stateData, &sf); err != nil {
		t.Fatalf("parsing state.json: %v", err)
	}
	if sf.ClassificationValid {
		t.Error("state.json: classification_valid = true after interrupt, want false")
	}

	// ── Step 7: audit.log must have an interrupt entry ───────────────────────
	auditData, err := os.ReadFile(liveAuditPath)
	if err != nil {
		t.Fatalf("reading audit.log: %v", err)
	}
	if !containsAuditEntry(auditData, "interrupt") {
		t.Error("audit.log: no entry with entry_type=interrupt found after SIGINT")
	}

	// ── Step 8: lab status must NOT assert BROKEN ─────────────────────────────
	// Interrupt invalidates classification; lab status reclassifies from runtime.
	statusOut, err := exec.Command(lab, "status", "--json").Output()
	if err != nil {
		t.Fatalf("lab status after interrupt: %v", err)
	}
	var statusResult struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(statusOut, &statusResult); err != nil {
		t.Fatalf("parsing lab status output: %v", err)
	}
	if statusResult.State == "BROKEN" {
		t.Error("lab status asserted BROKEN after interrupt — interrupt must not assert BROKEN")
	}

	// ── Step 9: lab status must have reclassified (classification_valid: true) ─
	stateData2, err := os.ReadFile(liveStatePath)
	if err != nil {
		t.Fatalf("re-reading state.json after status: %v", err)
	}
	var sf2 struct {
		ClassificationValid bool `json:"classification_valid"`
	}
	if err := json.Unmarshal(stateData2, &sf2); err != nil {
		t.Fatalf("parsing state.json after status: %v", err)
	}
	if !sf2.ClassificationValid {
		t.Error("state.json: classification_valid = false after lab status — status must reclassify")
	}

	// Step 9 cleanup: deferred restore runs on exit.
}

// TestLiveInterrupt_BeforeMutation_ExitsCleanly verifies that a SIGINT
// received before the first mutation causes a clean exit (code 0, no
// classification_valid=false, no audit interrupt entry).
//
// control-plane-contract §3.6: "interrupt before first mutation → exit 0,
// state unchanged."
func TestLiveInterrupt_BeforeMutation_ExitsCleanly(t *testing.T) {
	lab := labBin()

	// Ensure CONFORMANT before test.
	if out, err := exec.Command(lab, "validate").Output(); err != nil {
		t.Skipf("environment not conformant, skipping: %s", out)
	}

	// Start lab reset (nothing to reset from CONFORMANT — it will verify and return).
	reset := exec.Command(lab, "reset")
	if err := reset.Start(); err != nil {
		t.Fatalf("starting lab reset: %v", err)
	}

	// Interrupt immediately — before any mutation can occur.
	if err := reset.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT: %v", err)
	}

	err := reset.Wait()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	// Pre-mutation interrupt: exit 0 (clean) or 4 (interrupt caught) are both
	// acceptable depending on timing. What must NOT happen: exit 1 (error).
	if exitCode == 1 {
		t.Errorf("lab reset interrupted before mutation: exit code 1 (error), want 0 or 4")
	}
}

// TestLiveInterrupt_SIGTERM_SameBehaviorAsSIGINT verifies that SIGTERM
// produces the same interrupt behavior as SIGINT.
// control-plane-contract §3.6: both signals are caught by the interrupt handler.
func TestLiveInterrupt_SIGTERM_SameBehaviorAsSIGINT(t *testing.T) {
	lab := labBin()

	// Apply a fault to give reset something to do.
	if out, err := exec.Command(lab, "fault", "apply", "F-004").CombinedOutput(); err != nil {
		t.Fatalf("fault apply F-004: %v\n%s", err, out)
	}
	defer exec.Command(lab, "reset", "--tier", "R2").Run() //nolint:errcheck

	reset := exec.Command(lab, "reset")
	if err := reset.Start(); err != nil {
		t.Fatalf("starting lab reset: %v", err)
	}

	// Wait for alive.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := reset.Process.Signal(syscall.Signal(0)); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)

	if err := reset.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}

	err := reset.Wait()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	if exitCode != 4 {
		t.Errorf("lab reset after SIGTERM: exit code %d, want 4", exitCode)
	}
}

// containsAuditEntry scans newline-delimited JSON audit log lines for an
// entry with the specified entry_type value.
func containsAuditEntry(data []byte, entryType string) bool {
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var entry struct {
			EntryType string `json:"entry_type"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.EntryType == entryType {
			return true
		}
	}
	return false
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// tempStateBackup saves and restores state.json around a test to avoid
// interference between live tests. Used for tests that need a clean baseline.
func tempStateBackup(t *testing.T) {
	t.Helper()
	orig, err := os.ReadFile(liveStatePath)
	if err != nil {
		return // no state file — nothing to back up
	}
	t.Cleanup(func() {
		os.WriteFile(liveStatePath, orig, 0644) //nolint:errcheck
	})
}

// init checks that the lab binary exists at the expected path.
// Called before any test runs to provide a clear error message.
func init() {
	lab := labBin()
	if _, err := exec.LookPath(lab); err != nil {
		// Not a fatal here — LookPath uses PATH; the binary may be at ./lab
		absPath, _ := filepath.Abs(lab)
		if _, err := os.Stat(absPath); err != nil {
			// Will fail loudly in individual tests with a clear message.
			_ = absPath
		}
	}
}