package cmd

import (
	"fmt"
	"time"

	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

// ValidateCmd implements lab validate.
// Defined in control-plane-contract §4.2.
//
// lab validate is an observation primitive — it records conformance
// observations but does NOT update the authoritative state classification.
// Only lab status reconciles state. This rule is enforced here regardless
// of whether the validation result would change the classification.
type ValidateCmd struct {
	obs    conformance.Observer
	runner *conformance.Runner
	store  *state.Store
	audit  *executor.AuditLogger
}

// NewValidateCmd returns a ValidateCmd wired to the provided dependencies.
func NewValidateCmd(obs conformance.Observer, runner *conformance.Runner, store *state.Store, audit *executor.AuditLogger) *ValidateCmd {
	return &ValidateCmd{obs: obs, runner: runner, store: store, audit: audit}
}

// Run executes the full conformance suite and returns the result.
// Does NOT update the authoritative state classification.
// DOES update last_validate in state.json.
func (c *ValidateCmd) Run() output.CommandResult {
	sr := c.runner.Run(c.obs)
	return c.processResult(sr)
}

// RunSingle executes a single check by ID.
// Does NOT update state.json. Does NOT write an audit entry.
func (c *ValidateCmd) RunSingle(checkID string) output.CommandResult {
	res, err := c.runner.RunSingle(checkID, c.obs)
	if err != nil {
		return output.CommandResult{
			ExitCode: 2,
			Err:      fmt.Errorf("ErrUnknownCheckID: %w", err),
		}
	}

	// Wrap in a minimal SuiteResult for uniform output handling.
	sr := &conformance.SuiteResult{
		At:      time.Now().UTC(),
		Total:   1,
		Results: []conformance.CheckResult{*res},
	}
	sr.Classify()

	result := output.FromSuiteResult(sr)
	return output.CommandResult{
		Value:    result,
		ExitCode: sr.ExitCode(),
	}
}

func (c *ValidateCmd) processResult(sr *conformance.SuiteResult) output.CommandResult {
	// Record the observation in state.json (last_validate only).
	// MUST NOT update the state field — observation-only rule.
	c.recordObservation(sr)

	// Write audit entry for full suite run.
	if c.audit != nil {
		c.audit.LogOp("ValidateFullSuite",
			fmt.Sprintf("%d/%d checks passed", sr.Passed, sr.Total),
			0, sr.ExitCode(), nil)
	}

	result := output.FromSuiteResult(sr)
	return output.CommandResult{
		Value:    result,
		ExitCode: sr.ExitCode(),
	}
}

func (c *ValidateCmd) recordObservation(sr *conformance.SuiteResult) {
	sf, err := c.store.Read()
	if err != nil {
		// If we can't read the state file, we can't record the observation.
		// This is not fatal — the observation result is still returned.
		return
	}

	// Update ONLY last_validate. Never touch sf.State.
	sf.LastValidate = &state.ValidateRecord{
		At:            sr.At,
		Passed:        sr.Passed,
		Total:         sr.Total,
		FailingChecks: sr.FailingBlockingIDs,
	}

	// Intentional: we do NOT update sf.State here.
	// State reconciliation is exclusively lab status's responsibility.
	if err := c.store.Write(sf); err != nil {
		// Non-fatal: the observation result is still valid even if
		// the state file couldn't be updated.
		fmt.Printf("warning: failed to record validation observation: %v\n", err)
	}
}