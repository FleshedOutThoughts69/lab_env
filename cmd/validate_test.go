package cmd

// validate_test.go enforces the observation-only contract for lab validate:
//   - full validate writes last_validate but NOT state
//   - --check writes nothing at all
//   - exit code derives from blocking checks only (not degraded)
//   - H-001 pre-fix: validate must not update state field

import (
	"path/filepath"
	"testing"


	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

func TestValidateCmd_WritesLastValidate_NotState(t *testing.T) {
	// The observation-only rule: validate records observations,
	// MUST NOT update the authoritative state field.
	// control-plane-contract §4.2.
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))

	sf := state.Fresh(state.StateBroken) // state is BROKEN
	store.Write(sf)

	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab validate")
	cmd := NewValidateCmd(unhealthyObs(), conformance.NewRunner(), store, audit)
	cmd.Run()

	sf2, err := store.Read()
	if err != nil {
		t.Fatalf("reading state after validate: %v", err)
	}

	// State MUST NOT have changed — observation-only rule
	if sf2.State != state.StateBroken {
		t.Errorf("state.json State = %q after validate, want BROKEN — validate must not update state", sf2.State)
	}

	// last_validate MUST be set
	if sf2.LastValidate == nil {
		t.Error("last_validate should be set after validate run")
	}
}

func TestValidateCmd_FullRun_ExitCode0_WhenOnlyDegradedFail(t *testing.T) {
	// Degraded failures must not affect exit code.
	// This is the core conformance-model §4.3 enforcement in the command layer.
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	// Observer where all blocking checks pass but degraded checks fail
	// (In practice, L-series checks will fail with the unhealthy observer
	// since log files don't exist, but blocking service checks pass)
	obs := healthyObs() // blocking checks pass; L-series degraded checks will fail

	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab validate")
	cmd := NewValidateCmd(obs, conformance.NewRunner(), store, audit)
	result := cmd.Run()

	// The exit code depends on the mock — with healthyObs(), the blocking
	// checks pass but the L-series and F-series checks may fail due to
	// missing files in the test environment. We verify the exit code
	// reflects blocking-only classification.
	vr, ok := result.Value.(output.ValidateResult)
	if !ok {
		t.Fatalf("Value is not ValidateResult: %T", result.Value)
	}

	// If there are failing blocking checks, exit should be 1
	// If all blocking pass (even with degraded failures), exit should be 0
	if len(vr.FailingChecks) > 0 && result.ExitCode != 1 {
		t.Errorf("ExitCode = %d with blocking failures, want 1", result.ExitCode)
	}
	if len(vr.FailingChecks) == 0 && result.ExitCode != 0 {
		t.Errorf("ExitCode = %d with no blocking failures, want 0", result.ExitCode)
	}
}

func TestValidateCmd_SingleCheck_WritesNothing(t *testing.T) {
	// --check <ID> must not update any state file fields
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	sf := state.Fresh(state.StateConformant)
	store.Write(sf)

	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab validate")
	cmd := NewValidateCmd(healthyObs(), conformance.NewRunner(), store, audit)
	cmd.RunSingle("S-001")

	sf2, _ := store.Read()
	if sf2 == nil {
		return // store not written is fine
	}
	// last_validate must NOT be updated by single-check run
	if sf2.LastValidate != nil {
		t.Error("RunSingle should not update last_validate in state.json")
	}
}

func TestValidateCmd_UnknownCheckID_ReturnsUsageError(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab validate")
	cmd := NewValidateCmd(healthyObs(), conformance.NewRunner(), store, audit)
	result := cmd.RunSingle("Z-999")

	if result.ExitCode != 2 {
		t.Errorf("ExitCode = %d for unknown check ID, want 2 (usage error)", result.ExitCode)
	}
}

func TestValidateCmd_DoesNotReconcileState(t *testing.T) {
	// Even if validate result implies a different state, it must not update State.
	// This directly tests the observation-only rule.
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))

	// Write BROKEN state
	sf := state.Fresh(state.StateBroken)
	store.Write(sf)

	// Run validate with a healthy observer (would imply CONFORMANT if it could mutate)
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab validate")
	cmd := NewValidateCmd(healthyObs(), conformance.NewRunner(), store, audit)
	cmd.Run()

	sf2, _ := store.Read()
	if sf2 == nil {
		t.Fatal("state file should exist")
	}
	if sf2.State != state.StateBroken {
		t.Errorf("State = %q after validate, want BROKEN — validate must NOT reconcile state", sf2.State)
	}
}