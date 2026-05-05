package cmd

import (
	"fmt"
	"time"

	"lab_env/internal/catalog"
	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

// ── lab fault list ────────────────────────────────────────────────────────────

// FaultListCmd implements lab fault list.
type FaultListCmd struct{}

func NewFaultListCmd() *FaultListCmd { return &FaultListCmd{} }

func (c *FaultListCmd) Run() output.CommandResult {
	defs := catalog.AllDefs()
	result := output.FaultListResult{Total: len(defs)}
	for _, f := range defs {
		result.Faults = append(result.Faults, output.FaultSummary{
			ID:      f.ID,
			Layer:   f.Layer,
			Domain:  f.Domain,
			Symptom: f.Symptom,
		})
	}
	return output.CommandResult{Value: result, ExitCode: 0}
}

// ── lab fault info ────────────────────────────────────────────────────────────

// FaultInfoCmd implements lab fault info <ID>.
type FaultInfoCmd struct{}

func NewFaultInfoCmd() *FaultInfoCmd { return &FaultInfoCmd{} }

func (c *FaultInfoCmd) Run(id string) output.CommandResult {
	// Use DefByID — display commands never need Apply/Recover.
	f := catalog.DefByID(id)
	if f == nil {
		return output.CommandResult{
			ExitCode: 1,
			Err:      fmt.Errorf("ErrUnknownFaultID: unknown fault ID %q — run 'lab fault list' to see available faults", id),
		}
	}
	result := output.FaultInfoResult{
		ID:                   f.ID,
		Layer:                f.Layer,
		Domain:               f.Domain,
		ResetTier:            f.ResetTier,
		RequiresConfirmation: f.RequiresConfirmation,
		IsReversible:         f.IsReversible,
		MutationDisplay:      f.MutationDisplay,
		Symptom:              f.Symptom,
		AuthoritativeSignal:  f.AuthoritativeSignal,
		Observable:           f.Observable,
		ResetAction:          f.ResetAction,
	}
	return output.CommandResult{Value: result, ExitCode: 0}
}

// ── lab fault apply ───────────────────────────────────────────────────────────

// FaultApplyCmd implements lab fault apply <ID>.
// Defined in control-plane-contract §4.5.
type FaultApplyCmd struct {
	obs    conformance.Observer
	runner *conformance.Runner
	exec   executor.Executor
	store  *state.Store
	audit  *executor.AuditLogger
	lock   *executor.Lock
}

func NewFaultApplyCmd(obs conformance.Observer, runner *conformance.Runner, exec executor.Executor, store *state.Store, audit *executor.AuditLogger) *FaultApplyCmd {
	return &FaultApplyCmd{
		obs:    obs,
		runner: runner,
		exec:   exec,
		store:  store,
		audit:  audit,
		lock:   executor.NewLock(),
	}
}

// Run applies the named fault. Preconditions checked in order per §4.5.
func (c *FaultApplyCmd) Run(id string, force, yes bool) output.CommandResult {
	// Precondition 1: fault ID must exist.
	fault := catalog.ByID(id)
	if fault == nil {
		if c.audit != nil {
			c.audit.LogError("ErrUnknownFaultID", id)
		}
		return output.CommandResult{
			ExitCode: 2,
			Err:      fmt.Errorf("ErrUnknownFaultID: unknown fault ID %q — run 'lab fault list'", id),
		}
	}

	// Baseline behavior faults cannot be applied.
	if fault.Def.IsBaselineBehavior {
		return output.CommandResult{
			ExitCode: 2,
			Err:      fmt.Errorf("%s is a baseline behavior entry and cannot be applied via lab fault apply", id),
		}
	}

	// Acquire mutation lock.
	if err := c.lock.Acquire(); err != nil {
		if c.audit != nil {
			c.audit.LogError("ErrLockHeld", err.Error())
		}
		return output.CommandResult{ExitCode: 1, Err: err}
	}
	defer c.lock.Release()

	// Re-read state after lock acquisition (TOCTOU guard).
	sf, err := c.store.Read()
	if err != nil {
		if c.audit != nil {
			c.audit.LogError("ErrStateFileUnreadable", err.Error())
		}
		return output.CommandResult{ExitCode: 1, Err: fmt.Errorf("reading state file: %w", err)}
	}

	fromState := sf.State

	// Precondition 2: state must be CONFORMANT (unless --force).
	if !force && sf.State != state.StateConformant {
		msg := fmt.Sprintf("ErrPreconditionNotMet: current state is %s; lab fault apply requires CONFORMANT\n\nReset first:   lab reset\nThen apply:    lab fault apply %s\n\nTo override (not recommended):  lab fault apply %s --force",
			sf.State, id, id)
		if c.audit != nil {
			c.audit.LogError("ErrPreconditionNotMet", string(sf.State))
		}
		return output.CommandResult{ExitCode: 3, Err: fmt.Errorf("%s", msg)}
	}

	// Precondition 3: no fault currently active (unless --force).
	if !force && sf.ActiveFault != nil {
		msg := fmt.Sprintf("ErrFaultAlreadyActive: fault %s is currently active\n\nReset first:   lab reset\nThen apply:    lab fault apply %s",
			sf.ActiveFault.ID, id)
		if c.audit != nil {
			c.audit.LogError("ErrFaultAlreadyActive", sf.ActiveFault.ID)
		}
		return output.CommandResult{ExitCode: 3, Err: fmt.Errorf("%s", msg)}
	}

	// Precondition 4: RequiresConfirmation prompt.
	if fault.Def.RequiresConfirmation && !yes {
		return output.CommandResult{
			Value: output.FaultApplyResult{
				FaultID:     id,
				Aborted:     true,
				AbortReason: fmt.Sprintf("fault %s requires confirmation; re-run with --yes to confirm:\n\n  Mutation: %s", id, fault.Def.MutationDisplay),
			},
			ExitCode: 0,
		}
	}

	// Execute Apply through executor.
	applyErr := fault.Apply(c.exec)
	if applyErr != nil {
		// Apply failed — state file MUST NOT be updated to DEGRADED.
		// Per control-plane-contract §4.5 atomicity guarantee.
		if c.audit != nil {
			c.audit.LogError("ErrApplyFailed", applyErr.Error())
		}
		return output.CommandResult{
			ExitCode: 1,
			Err:      fmt.Errorf("ErrApplyFailed: fault application failed: %w\n\nRun 'lab status' to determine current state.", applyErr),
		}
	}

	// Apply succeeded — atomically update state to DEGRADED.
	now := time.Now().UTC()
	sf.State = state.StateDegraded
	sf.ClassificationValid = true
	sf.ActiveFault = &state.ActiveFault{
		ID:        id,
		AppliedAt: now,
		Forced:    force,
	}
	c.store.AppendHistory(state.HistoryEntry{
		Ts:      now,
		From:    fromState,
		To:      state.StateDegraded,
		Command: fmt.Sprintf("lab fault apply %s", id),
		Fault:   id,
		Forced:  force,
	}, sf)

	if err := c.store.Write(sf); err != nil {
		// State recording failed after successful Apply.
		// Per control-plane-contract §4.5: exit 4, classification invalidated.
		if c.audit != nil {
			c.audit.LogError("StateRecordingFailed", err.Error())
		}
		_ = c.store.InvalidateClassification()
		return output.CommandResult{
			ExitCode: 4,
			Err:      fmt.Errorf("fault applied but state recording failed — run 'lab status' to re-establish state: %w", err),
		}
	}

	if c.audit != nil {
		c.audit.LogTransition(fromState, state.StateDegraded, id)
	}

	return output.CommandResult{
		Value: output.FaultApplyResult{
			FaultID:   id,
			Applied:   true,
			FromState: fromState,
			ToState:   state.StateDegraded,
			Forced:    force,
		},
		ExitCode: 0,
	}
}