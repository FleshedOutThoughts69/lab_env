//go:build integration

package cmd

import (
	"path/filepath"
	"testing"
	"time"
	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

func TestFaultApplyCmd_PreconditionFails_NotConformant(t *testing.T) {
	// State is BROKEN — fault apply must reject without mutation
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateBroken))

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-004")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-004", false, false) // no --force

	if result.ExitCode != 3 {
		t.Errorf("ExitCode = %d for BROKEN precondition, want 3 (precondition not met)", result.ExitCode)
	}
	if len(exec.mutationCalls) > 0 {
		t.Errorf("no mutations should occur when precondition fails, got: %v", exec.mutationCalls)
	}
}

func TestFaultApplyCmd_PreconditionFails_FaultAlreadyActive(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))

	sf := state.Fresh(state.StateDegraded)
	sf.ActiveFault = &state.ActiveFault{ID: "F-001", AppliedAt: time.Now().UTC()}
	store.Write(sf)

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-004")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-004", false, false) // no --force

	if result.ExitCode != 3 {
		t.Errorf("ExitCode = %d when fault already active, want 3", result.ExitCode)
	}
	if len(exec.mutationCalls) > 0 {
		t.Errorf("no mutations should occur when fault already active, got: %v", exec.mutationCalls)
	}
}

func TestFaultApplyCmd_Success_UpdatesStateToDegraded(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-004")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-004", false, false)

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d for successful Apply, want 0; err: %v", result.ExitCode, result.Err)
	}

	ar, ok := result.Value.(output.FaultApplyResult)
	if !ok {
		t.Fatalf("Value is not FaultApplyResult: %T", result.Value)
	}
	if !ar.Applied {
		t.Error("Applied should be true")
	}
	if ar.ToState != state.StateDegraded {
		t.Errorf("ToState = %q, want DEGRADED", ar.ToState)
	}

	sf2, _ := store.Read()
	if sf2 == nil {
		t.Fatal("state file should exist")
	}
	if sf2.State != state.StateDegraded {
		t.Errorf("State = %q after Apply, want DEGRADED", sf2.State)
	}
	if sf2.ActiveFault == nil || sf2.ActiveFault.ID != "F-004" {
		t.Errorf("ActiveFault = %+v, want F-004", sf2.ActiveFault)
	}
}

func TestFaultApplyCmd_RequiresConfirmation_WithoutYes_Aborts(t *testing.T) {
	// F-008 requires confirmation — must abort without --yes
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-008")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-008", false, false) // no --yes

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d for confirmation abort, want 0", result.ExitCode)
	}
	ar, ok := result.Value.(output.FaultApplyResult)
	if !ok {
		t.Fatalf("Value is not FaultApplyResult: %T", result.Value)
	}
	if !ar.Aborted {
		t.Error("Aborted should be true when RequiresConfirmation without --yes")
	}
	if len(exec.mutationCalls) > 0 {
		t.Errorf("no mutations should occur when aborted, got: %v", exec.mutationCalls)
	}
}

func TestFaultApplyCmd_HistoryUpdated_OnSuccess(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-004")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)
	cmd.Run("F-004", false, false)

	sf2, _ := store.Read()
	if sf2 == nil || len(sf2.History) == 0 {
		t.Error("History should have an entry after successful fault apply")
	}
	if len(sf2.History) > 0 && sf2.History[len(sf2.History)-1].Fault != "F-004" {
		t.Errorf("last history entry fault = %q, want F-004", sf2.History[len(sf2.History)-1].Fault)
	}
}

func TestFaultApplyCmd_PreconditionCheckFails_F010(t *testing.T) {
	// F-010 requires PreconditionChecks: [P-001] — app process must be running.
	// When the observer reports the app process is NOT running, Apply must be
	// rejected with ErrFaultPreconditionFailed (exit 3) before any mutation.
	// control-plane-contract §4.5 step 5.
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	// Observer reports app process NOT running (P-001 fails).
	obs := &stubObserver{}
	obs.serviceActive = map[string]bool{"app.service": false, "nginx": true}
	obs.portListening = map[string]bool{"127.0.0.1:8080": false}
	obs.endpointStatus = map[string]int{}

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-010")
	cmd := NewFaultApplyCmd(obs, conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-010", false, false)

	if result.ExitCode != 3 {
		t.Errorf("ExitCode = %d for failing PreconditionCheck, want 3 (ErrFaultPreconditionFailed)", result.ExitCode)
	}
	if len(exec.mutationCalls) > 0 {
		t.Errorf("no mutations should occur when PreconditionCheck fails, got: %v", exec.mutationCalls)
	}
	// State must remain CONFORMANT
	sf2, _ := store.Read()
	if sf2 != nil && sf2.State != state.StateConformant {
		t.Errorf("State = %q after precondition check failure, want CONFORMANT", sf2.State)
	}
}
