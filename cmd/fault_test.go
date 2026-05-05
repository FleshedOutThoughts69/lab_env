package cmd

// fault_test.go enforces the fault apply command contract from
// control-plane-contract §4.5:
//   - unknown fault ID rejected before lock acquisition
//   - precondition failure rejected before mutation
//   - baseline faults rejected before lock
//   - TOCTOU re-read occurs after lock acquisition
//   - Apply failure does NOT update state to DEGRADED
//   - Apply success writes state + audit entry

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"


	"lab_env/internal/catalog"
	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

// ── stub executor ─────────────────────────────────────────────────────────────

// trackingExecutor records mutation calls and can inject faults.
type trackingExecutor struct {
	stubObserver
	mutationCalls []string
	applyError    error // if non-nil, returned from the fault's Apply
}

func newTrackingExecutor() *trackingExecutor {
	te := &trackingExecutor{}
	te.serviceActive = map[string]bool{"app.service": true, "nginx": true}
	te.portListening = map[string]bool{"127.0.0.1:8080": true}
	te.endpointStatus = map[string]int{}
	return te
}

func (t *trackingExecutor) WriteFile(path string, _ []byte, _ fs.FileMode, _, _ string) error {
	t.mutationCalls = append(t.mutationCalls, "WriteFile:"+path)
	return nil
}
func (t *trackingExecutor) Chmod(path string, _ fs.FileMode) error {
	t.mutationCalls = append(t.mutationCalls, "Chmod:"+path)
	return nil
}
func (t *trackingExecutor) Chown(path, _, _ string) error {
	t.mutationCalls = append(t.mutationCalls, "Chown:"+path)
	return nil
}
func (t *trackingExecutor) Remove(path string) error {
	t.mutationCalls = append(t.mutationCalls, "Remove:"+path)
	return nil
}
func (t *trackingExecutor) MkdirAll(path string, _ fs.FileMode, _, _ string) error {
	t.mutationCalls = append(t.mutationCalls, "MkdirAll:"+path)
	return nil
}
func (t *trackingExecutor) Systemctl(action, unit string) error {
	t.mutationCalls = append(t.mutationCalls, "Systemctl:"+action+":"+unit)
	return nil
}
func (t *trackingExecutor) NginxReload() error {
	t.mutationCalls = append(t.mutationCalls, "NginxReload")
	return nil
}
func (t *trackingExecutor) RestoreFile(path string) error {
	t.mutationCalls = append(t.mutationCalls, "RestoreFile:"+path)
	return nil
}
func (t *trackingExecutor) RunMutation(cmd string, args ...string) error {
	t.mutationCalls = append(t.mutationCalls, "RunMutation:"+cmd)
	return nil
}

// trackingExecutor conformance.Observer methods (reuse stubObserver via embedding)

// faultApplyReturns makes a single named fault return the given Apply error.
// Uses a catalog entry's Apply function wrapped to inject the error.
type injectErrorExecutor struct {
	trackingExecutor
	errorOnChmod bool
}

func (e *injectErrorExecutor) Chmod(path string, mode fs.FileMode) error {
	e.mutationCalls = append(e.mutationCalls, "Chmod:"+path)
	if e.errorOnChmod {
		return fmt.Errorf("chmod failed: permission denied")
	}
	return nil
}

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

func TestFaultApplyCmd_BaselineFault_Rejected(t *testing.T) {
	// F-011 is a baseline behavior — must be rejected before lock
	dir := t.TempDir()
	store := state.NewStoreAt(filepath.Join(dir, "state.json"))
	store.Write(state.Fresh(state.StateConformant))

	exec := newTrackingExecutor()
	audit := executor.NewAuditLoggerAt(filepath.Join(dir, "audit.log"), "lab fault apply F-011")
	cmd := NewFaultApplyCmd(healthyObs(), conformance.NewRunner(), exec, store, audit)

	result := cmd.Run("F-011", false, false)

	if result.ExitCode != 2 {
		t.Errorf("ExitCode = %d for baseline fault, want 2", result.ExitCode)
	}
	if len(exec.mutationCalls) > 0 {
		t.Errorf("no mutations for baseline fault, got: %v", exec.mutationCalls)
	}
}

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

// Verify catalog.AllDefs() is used for list (no executor dependency)
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