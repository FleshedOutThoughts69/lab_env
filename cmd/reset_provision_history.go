package cmd

import (
    "fmt"
    "time"

    "lab_env/internal/conformance"
    "lab_env/internal/executor"
    "lab_env/internal/output"
    "lab_env/internal/state"
    cfg "lab_env/internal/config"
)

// ── lab provision ──

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

// ── lab history ──

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