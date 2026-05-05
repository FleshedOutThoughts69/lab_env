package cmd

import (
	"fmt"
	"os"
	"time"

	"lab_env/internal/conformance"
	"lab_env/internal/executor"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

// StatusCmd implements lab status.
// Defined in control-plane-contract §4.1.
//
// lab status is the canonical reconciliation point — the only command
// authorized to reconcile observed runtime reality with recorded
// control-plane classification.
//
// It does NOT acquire the mutation lock (read-path with reconciliation authority).
// It DOES update state.json if the detected state differs from the recorded state.
type StatusCmd struct {
	obs    conformance.Observer
	runner *conformance.Runner
	store  *state.Store
	audit  *executor.AuditLogger
}

// NewStatusCmd returns a StatusCmd wired to the provided dependencies.
func NewStatusCmd(obs conformance.Observer, runner *conformance.Runner, store *state.Store, audit *executor.AuditLogger) *StatusCmd {
	return &StatusCmd{obs: obs, runner: runner, store: store, audit: audit}
}

// Run executes the status command and returns a structured result.
// The exit code is embedded in the CommandResult.
func (c *StatusCmd) Run() output.CommandResult {
	// Step 1: read current state file.
	sf, readErr := c.store.Read()

	// Step 2: run lightweight checks (not the full suite).
	lwResult := c.runner.LightweightRun(c.obs)

	// Step 3: apply state detection algorithm.
	input := state.DetectInput{
		LightweightResult: lwResult,
	}
	if sf != nil {
		input.StateFile = sf
	}
	detection := state.Detect(input)

	// Step 4: reconcile state file if detected state differs.
	if detection.Reconciled && sf != nil {
		sf.State = detection.Detected
		sf.ClassificationValid = true
		now := time.Now().UTC()
		sf.LastStatusAt = &now

		if c.audit != nil {
			c.audit.LogReconciliation(detection.PriorState, detection.Detected)
		}
		if err := c.store.Write(sf); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write reconciled state: %v\n", err)
		}
	}

	// Step 5: build result.
	if state.IsUnknown(detection) {
		return output.CommandResult{
			Value:    output.StatusResult{Unknown: true},
			ExitCode: 5,
		}
	}

	result := buildStatusResult(sf, detection.Detected, lwResult, readErr)
	return output.CommandResult{Value: result, ExitCode: 0}
}

func buildStatusResult(sf *state.File, detected state.State, lw *conformance.SuiteResult, readErr error) output.StatusResult {
	r := output.StatusResult{
		State:    detected,
		Services: map[string]output.SvcInfo{},
		Endpoints: map[string]int{},
	}

	// Active fault from state file.
	if sf != nil && sf.ActiveFault != nil {
		r.ActiveFault = &output.FaultRef{
			ID:        sf.ActiveFault.ID,
			AppliedAt: sf.ActiveFault.AppliedAt,
		}
	}

	// Last validate / last reset from state file.
	if sf != nil {
		if sf.LastValidate != nil {
			r.LastValidate = &output.ValidateSummary{
				At:     sf.LastValidate.At,
				Passed: sf.LastValidate.Passed,
				Total:  sf.LastValidate.Total,
			}
		}
		if sf.LastReset != nil {
			r.LastReset = &output.ResetSummary{
				At:   sf.LastReset.At,
				Tier: sf.LastReset.Tier,
			}
		}
	}

	// Service and port info from lightweight check results.
	for _, res := range lw.Results {
		switch res.Check.ID {
		case "P-001":
			r.Services["app.service"] = output.SvcInfo{Active: res.Passed}
		case "S-003":
			r.Services["nginx"] = output.SvcInfo{Active: res.Passed}
		case "P-002":
			if res.Passed {
				r.Ports = append(r.Ports, output.PortInfo{Addr: "127.0.0.1:8080", Owner: "app"})
			}
		case "E-001":
			code := 0
			if !res.Passed {
				code = 502 // best guess without exact status
			} else {
				code = 200
			}
			r.Endpoints["http://localhost/health"] = code
		}
	}

	// Add nginx ports (known from canonical environment — not from lightweight run).
	r.Ports = append(r.Ports,
		output.PortInfo{Addr: "0.0.0.0:80", Owner: "nginx"},
		output.PortInfo{Addr: "0.0.0.0:443", Owner: "nginx (TLS)"},
	)

	return r
}