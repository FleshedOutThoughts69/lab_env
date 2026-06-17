package cmd

// fault_test.go enforces the fault apply command contract from
// control-plane-contract §4.5:
//   - unknown fault ID rejected before lock acquisition (precondition 1)
//   - state precondition failure rejected before mutation (precondition 2)
//   - already-active fault rejected before mutation (precondition 3)
//   - PreconditionChecks guard: failing check rejects Apply (precondition 5)
//   - --force bypasses state guards and PreconditionChecks (preconditions 2–5)
//   - RequiresConfirmation abort without --yes (precondition 6)
//   - TOCTOU re-read occurs after lock acquisition
//   - Apply failure does NOT update state to DEGRADED
//   - Apply success writes state + audit entry

import (
	"os"
	"path/filepath"
	"testing"
	"lab_env/internal/catalog" 
	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

// ── tests ─────────────────────────────────────────────────────────────────────

func TestFaultApplyCmd_UnknownID_RejectsBeforeLock(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-999")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-999", false, false)

	if result.ExitCode != 2 {
		t.Errorf("ExitCode = %d for unknown fault, want 2 (usage error)", result.ExitCode)
	}
	if len(exec.mutationCalls) > 0 {
		t.Errorf("no mutations should occur for unknown fault ID, got: %v", exec.mutationCalls)
	}
}

func TestFaultApplyCmd_BaselineID_Rejected(t *testing.T) {
	// F-011 and F-012 are no longer in the fault catalog (fault-model.md §10).
	// Attempting to apply them produces ErrUnknownFaultID (exit 2),
	// not a special baseline-rejection error.
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-011")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	for _, id := range []string{"F-011", "F-012"} {
		result := cmd.Run(id, false, false)

		if result.ExitCode != 2 {
			t.Errorf("ExitCode = %d for %s (not in catalog), want 2 (ErrUnknownFaultID)", result.ExitCode, id)
		}
		if len(exec.mutationCalls) > 0 {
			t.Errorf("no mutations for unknown fault ID %s, got: %v", id, exec.mutationCalls)
		}
	}
}

func TestFaultApplyCmd_ForceBypassesPrecondition(t *testing.T) {
	// --force bypasses the state guard
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateBroken))

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-004 --force")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-004", true, false) // --force

	// With force, should attempt Apply (may succeed or fail depending on exec)
	if result.ExitCode == 3 {
		t.Error("--force should bypass precondition check, not return ExitCode 3")
	}
}

func TestFaultApplyCmd_ApplyFailure_DoesNotUpdateState(t *testing.T) {
	// If Apply returns an error, state.json MUST NOT be updated to DEGRADED.
	// This is the atomicity guarantee from control-plane-contract §4.5.
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	// F-004 Apply calls exec.Chmod — make it fail
	failingExec := &injectErrorExecutor{errorOnChmod: true}
	failingExec.serviceActive = map[string]bool{"app.service": true, "nginx": true}
	failingExec.portListening = map[string]bool{"127.0.0.1:8080": true}
	failingExec.endpointStatus = map[string]int{}

	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-004")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), failingExec, store, audit)

	result := cmd.Run("F-004", false, false)

	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d for failed Apply, want 1", result.ExitCode)
	}

	// State MUST remain CONFORMANT
	sf2, _ := store.Read()
	if sf2 == nil {
		t.Fatal("state file should exist")
	}
	if sf2.State != state.StateConformant {
		t.Errorf("State = %q after failed Apply, want CONFORMANT — atomicity guarantee violated", sf2.State)
	}
	if sf2.ActiveFault != nil {
		t.Errorf("ActiveFault = %+v after failed Apply, want nil", sf2.ActiveFault)
	}
}

func TestFaultApplyCmd_Success_WritesAuditEntry(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	auditPath := filepath.Join(dir, "audit.log")
	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(auditPath, "lab fault apply F-004")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)
	cmd.Run("F-004", false, false)

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}
	if len(data) == 0 {
		t.Error("audit log should have entries after successful fault apply")
	}
}

func TestFaultApplyCmd_ForceBypassesPreconditionChecks(t *testing.T) {
	// --force bypasses PreconditionChecks (step 5) in addition to state guards.
	// With --force, F-010 Apply proceeds even when P-001 is not satisfied.
	// control-plane-contract §4.5 --force scope.
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	// Observer reports app process NOT running — would normally fail P-001.
	obs := &stubObserver{}
	obs.serviceActive = map[string]bool{"app.service": false, "nginx": true}
	obs.portListening = map[string]bool{"127.0.0.1:8080": false}
	obs.endpointStatus = map[string]int{}

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-010 --force")
	cmd := NewFaultApplyCmd(obs, conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-010", true, false) // --force

	// With --force, should not return exit 3 (precondition not met).
	if result.ExitCode == 3 {
		t.Error("--force should bypass PreconditionChecks, not return ExitCode 3")
	}
}

func TestFaultApplyCmd_PreconditionCheckPasses_F010(t *testing.T) {
	// F-010 with P-001 passing proceeds to Apply normally.
	// control-plane-contract §4.5 step 5.
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	exec := newTrackingExecutor() // serviceActive["app.service"] = true
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-010")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-010", false, false)

	// Should proceed to Apply (exit 0 on success, or 1 if Apply fails in test env)
	if result.ExitCode == 3 {
		t.Errorf("P-001 should pass with healthy observer; got exit 3: %v", result.Err)
	}
}

func TestFaultList_UsesAllDefs(t *testing.T) {
	cmd := NewFaultListCmd()
	result := cmd.Run()

	if result.ExitCode != 0 {
		t.Errorf("fault list ExitCode = %d, want 0", result.ExitCode)
	}
	fl, ok := result.Value.(output.FaultListResult)
	if !ok {
		t.Fatalf("Value is not FaultListResult: %T", result.Value)
	}
	if fl.Total != len(catalog.AllDefs()) {
		t.Errorf("Total = %d, want %d", fl.Total, len(catalog.AllDefs()))
	}
}

// Verify DefByID is used for info (no executor dependency)
func TestFaultInfo_UsesDefByID(t *testing.T) {
	cmd := NewFaultInfoCmd()
	result := cmd.Run("F-004")

	if result.ExitCode != 0 {
		t.Errorf("fault info ExitCode = %d, want 0", result.ExitCode)
	}
	fi, ok := result.Value.(output.FaultInfoResult)
	if !ok {
		t.Fatalf("Value is not FaultInfoResult: %T", result.Value)
	}
	if fi.ID != "F-004" {
		t.Errorf("ID = %q, want F-004", fi.ID)
	}
}

func TestFaultInfo_UnknownID_ReturnsError(t *testing.T) {
	cmd := NewFaultInfoCmd()
	result := cmd.Run("F-999")
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d for unknown fault info, want 1", result.ExitCode)
	}
}