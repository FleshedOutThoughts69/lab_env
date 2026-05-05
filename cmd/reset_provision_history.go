package cmd

import (
	"fmt"
	"time"

	"lab_env/internal/catalog"
	cfg "lab_env/internal/config"
	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

// ── lab reset ────────────────────────────────────────────────────────────────

// ResetCmd implements lab reset.
// Defined in control-plane-contract §4.6.
type ResetCmd struct {
	obs    conformance.Observer
	runner *conformance.Runner
	exec   executor.Executor
	store  *state.Store
	audit  *executor.AuditLogger
	lock   *executor.Lock
}

func NewResetCmd(obs conformance.Observer, runner *conformance.Runner, exec executor.Executor, store *state.Store, audit *executor.AuditLogger) *ResetCmd {
	return &ResetCmd{obs: obs, runner: runner, exec: exec, store: store, audit: audit, lock: executor.NewLock()}
}

// Run resets the environment to CONFORMANT. tier is "R1", "R2", "R3", or ""
// (auto-select). Always runs the conformance suite after tier operations.
func (c *ResetCmd) Run(tier string) output.CommandResult {
	start := time.Now()

	if err := c.lock.Acquire(); err != nil {
		return output.CommandResult{ExitCode: 1, Err: err}
	}
	defer c.lock.Release()

	sf, err := c.store.Read()
	if err != nil {
		sf = state.Fresh(state.StateBroken)
	}

	fromState := sf.State

	// Auto-select tier from §4.6 tier selection table.
	if tier == "" {
		tier = c.selectTier(sf)
	}

	// If environment is already CONFORMANT and no fault active, verify and return.
	if sf.State == state.StateConformant && sf.ActiveFault == nil {
		sr := c.runner.Run(c.obs)
		if sr.Classification.IsConformant() {
			return output.CommandResult{
				Value: output.ResetResult{
					Tier:          tier,
					FromState:     fromState,
					ToState:       state.StateConformant,
					ValidationRan: true,
					Suite:         sr,
					DurationMs:    time.Since(start).Milliseconds(),
				},
				ExitCode: 0,
			}
		}
	}

	// Execute tier operations.
	if err := c.executeTier(tier, sf); err != nil {
		if c.audit != nil {
			c.audit.LogError("ErrResetOperationFailed", err.Error())
		}
		return output.CommandResult{
			ExitCode: 1,
			Err:      fmt.Errorf("ErrResetOperationFailed: %w", err),
		}
	}

	// Post-reset validation (always runs after tier operations).
	sr := c.runner.Run(c.obs)

	faultCleared := ""
	if sf.ActiveFault != nil {
		faultCleared = sf.ActiveFault.ID
	}

	// Determine resulting state from validation.
	var toState state.State
	var exitCode int
	if sr.Classification.IsConformant() {
		toState = state.StateConformant
		exitCode = 0
	} else {
		toState = state.StateBroken
		exitCode = 1
	}

	// Update state file.
	now := time.Now().UTC()
	sf.State = toState
	sf.ClassificationValid = true
	sf.ActiveFault = nil
	sf.LastReset = &state.ResetRecord{
		At:           now,
		Tier:         tier,
		FromState:    fromState,
		FaultCleared: faultCleared,
	}
	sf.LastValidate = &state.ValidateRecord{
		At:            sr.At,
		Passed:        sr.Passed,
		Total:         sr.Total,
		FailingChecks: sr.FailingBlockingIDs,
	}
	c.store.AppendHistory(state.HistoryEntry{
		Ts:      now,
		From:    fromState,
		To:      toState,
		Command: fmt.Sprintf("lab reset --tier %s", tier),
		Fault:   faultCleared,
	}, sf)
	c.store.Write(sf) //nolint:errcheck

	if c.audit != nil {
		c.audit.LogTransition(fromState, toState, faultCleared)
	}

	if exitCode != 0 {
		return output.CommandResult{
			ExitCode: exitCode,
			Err: fmt.Errorf("ErrPostResetValidationFailed: reset completed but validation failed: %v",
				sr.FailingBlockingIDs),
			Value: output.ResetResult{
				Tier:          tier,
				FromState:     fromState,
				ToState:       toState,
				FaultCleared:  faultCleared,
				ValidationRan: true,
				Suite:         sr,
				DurationMs:    time.Since(start).Milliseconds(),
			},
		}
	}

	return output.CommandResult{
		Value: output.ResetResult{
			Tier:          tier,
			FromState:     fromState,
			ToState:       toState,
			FaultCleared:  faultCleared,
			ValidationRan: true,
			Suite:         sr,
			DurationMs:    time.Since(start).Milliseconds(),
		},
		ExitCode: 0,
	}
}

func (c *ResetCmd) selectTier(sf *state.File) string {
	if sf.ActiveFault != nil {
		f := catalog.ByID(sf.ActiveFault.ID)
		if f != nil {
			return f.Def.ResetTier
		}
	}
	return "R2" // safe default for BROKEN or unknown
}

func (c *ResetCmd) executeTier(tier string, sf *state.File) error {
	// Call Recover on the active fault before tier operations if reversible.
	if sf.ActiveFault != nil {
		f := catalog.ByID(sf.ActiveFault.ID)
		if f != nil && f.Def.IsReversible {
			if err := f.Recover(c.exec); err != nil {
				return fmt.Errorf("fault %s Recover: %w", f.Def.ID, err)
			}
		}
	}

	switch tier {
	case "R1":
		if err := c.exec.Systemctl("restart", cfg.AppServiceName); err != nil {
			return fmt.Errorf("R1 app restart: %w", err)
		}
		if err := c.exec.NginxReload(); err != nil {
			return fmt.Errorf("R1 nginx reload: %w", err)
		}

	case "R2":
		// Restore canonical config files from the embedded content map.
		for _, path := range cfg.R2RestoreFiles {
			if err := c.exec.RestoreFile(path); err != nil {
				return fmt.Errorf("R2 restore %s: %w", path, err)
			}
		}
		// Restore canonical mode bits.
		for _, entry := range cfg.R2RestoreModes {
			if err := c.exec.Chmod(entry.Path, entry.Mode); err != nil {
				return fmt.Errorf("R2 chmod %s: %w", entry.Path, err)
			}
		}
		if err := c.exec.Systemctl("daemon-reload", ""); err != nil {
			return fmt.Errorf("R2 daemon-reload: %w", err)
		}
		if err := c.exec.Systemctl("restart", cfg.AppServiceName); err != nil {
			return fmt.Errorf("R2 app restart: %w", err)
		}
		if err := c.exec.NginxReload(); err != nil {
			return fmt.Errorf("R2 nginx reload: %w", err)
		}
		if err := c.exec.NginxReload(); err != nil {
			return fmt.Errorf("R2 nginx reload: %w", err)
		}

	case "R3":
		// Full reprovision — bootstrap script.
		if err := c.exec.RunMutation("bash", cfg.BootstrapScript); err != nil {
			return fmt.Errorf("R3 bootstrap: %w", err)
		}

	default:
		return fmt.Errorf("unknown reset tier %q: must be R1, R2, or R3", tier)
	}

	return nil
}

// ── lab provision ────────────────────────────────────────────────────────────

// ProvisionCmd implements lab provision.
type ProvisionCmd struct {
	obs    conformance.Observer
	runner *conformance.Runner
	exec   executor.Executor
	store  *state.Store
	audit  *executor.AuditLogger
	lock   *executor.Lock
}

func NewProvisionCmd(obs conformance.Observer, runner *conformance.Runner, exec executor.Executor, store *state.Store, audit *executor.AuditLogger) *ProvisionCmd {
	return &ProvisionCmd{obs: obs, runner: runner, exec: exec, store: store, audit: audit, lock: executor.NewLock()}
}

func (c *ProvisionCmd) Run() output.CommandResult {
	start := time.Now()

	if err := c.lock.Acquire(); err != nil {
		return output.CommandResult{ExitCode: 1, Err: err}
	}
	defer c.lock.Release()

	if err := c.exec.RunMutation("bash", cfg.BootstrapScript); err != nil {
		return output.CommandResult{
			ExitCode: 1,
			Err:      fmt.Errorf("ErrProvisionStepFailed: bootstrap: %w", err),
		}
	}

	sr := c.runner.Run(c.obs)

	toState := state.StateConformant
	exitCode := 0
	if !sr.Classification.IsConformant() {
		toState = state.StateBroken
		exitCode = 1
	}

	sf := state.Fresh(toState)
	now := time.Now().UTC()
	sf.LastProvision = &state.ProvisionRecord{At: now, Result: string(toState)}
	sf.LastValidate = &state.ValidateRecord{At: sr.At, Passed: sr.Passed, Total: sr.Total}
	c.store.AppendHistory(state.HistoryEntry{
		Ts:      now,
		From:    state.StateUnprovisioned,
		To:      toState,
		Command: "lab provision",
	}, sf)
	c.store.Write(sf) //nolint:errcheck

	if exitCode != 0 {
		return output.CommandResult{
			ExitCode: exitCode,
			Err:      fmt.Errorf("ErrPostProvisionValidationFailed: provision completed but validation failed: %v", sr.FailingBlockingIDs),
		}
	}

	return output.CommandResult{
		Value:    output.ProvisionResult{ToState: toState, Suite: sr, DurationMs: time.Since(start).Milliseconds()},
		ExitCode: 0,
	}
}

// ── lab history ──────────────────────────────────────────────────────────────

// HistoryCmd implements lab history.
type HistoryCmd struct {
	store *state.Store
}

func NewHistoryCmd(store *state.Store) *HistoryCmd {
	return &HistoryCmd{store: store}
}

func (c *HistoryCmd) Run(last int) output.CommandResult {
	sf, err := c.store.Read()
	if err != nil {
		return output.CommandResult{
			ExitCode: 1,
			Err:      fmt.Errorf("ErrStateFileUnreadable: %w", err),
		}
	}

	entries := sf.History
	if last > 0 && last < len(entries) {
		entries = entries[len(entries)-last:]
	}

	// Return in reverse chronological order (most recent first).
	result := output.HistoryResult{Total: len(sf.History)}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		result.Entries = append(result.Entries, output.HistoryItem{
			Ts:      e.Ts,
			From:    e.From,
			To:      e.To,
			Command: e.Command,
			Fault:   e.Fault,
			Forced:  e.Forced,
		})
	}

	return output.CommandResult{Value: result, ExitCode: 0}
}