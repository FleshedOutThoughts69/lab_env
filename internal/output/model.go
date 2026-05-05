// Package output defines the structured result types returned by command
// handlers and the renderers that convert them to human-readable or JSON output.
//
// Commands return data. Renderers decide presentation. main connects them.
// This separation means command logic never contains formatting decisions.
package output

import (
	"time"

	"lab_env/internal/conformance"
	"lab_env/internal/state"
)

// StatusResult is the structured result of lab status.
// Defined in control-plane-contract §4.1.
type StatusResult struct {
	State       state.State        `json:"state"`
	ActiveFault *FaultRef          `json:"active_fault"`
	Services    map[string]SvcInfo `json:"services"`
	Ports       []PortInfo         `json:"ports"`
	Endpoints   map[string]int     `json:"endpoints"`
	LastValidate *ValidateSummary  `json:"last_validate"`
	LastReset    *ResetSummary     `json:"last_reset"`
	Reconciled   bool              `json:"reconciled,omitempty"`
	Unknown      bool              `json:"unknown,omitempty"`
}

// ValidateResult is the structured result of lab validate.
// Defined in control-plane-contract §4.2.
type ValidateResult struct {
	At             time.Time              `json:"at"`
	Checks         []CheckResultItem      `json:"checks"`
	Passed         int                    `json:"passed"`
	Total          int                    `json:"total"`
	Classification string                 `json:"classification"`
	FailingChecks  []string               `json:"failing_checks,omitempty"`
}

// CheckResultItem is a single check result in ValidateResult.
type CheckResultItem struct {
	ID         string `json:"id"`
	Assertion  string `json:"assertion"`
	Passed     bool   `json:"passed"`
	Severity   string `json:"severity"`
	Dependent  bool   `json:"dependent,omitempty"`
	Error      string `json:"error,omitempty"`
}

// FaultListResult is the structured result of lab fault list.
type FaultListResult struct {
	Faults []FaultSummary `json:"faults"`
	Total  int            `json:"total"`
}

// FaultSummary is a condensed fault entry for the list view.
type FaultSummary struct {
	ID      string   `json:"id"`
	Layer   string   `json:"layer"`
	Domain  []string `json:"domain"`
	Symptom string   `json:"symptom"`
}

// FaultInfoResult is the structured result of lab fault info <ID>.
type FaultInfoResult struct {
	ID                   string   `json:"id"`
	Layer                string   `json:"layer"`
	Domain               []string `json:"domain"`
	ResetTier            string   `json:"reset_tier"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	IsReversible         bool     `json:"is_reversible"`
	MutationDisplay      string   `json:"mutation"`
	Symptom              string   `json:"symptom"`
	AuthoritativeSignal  string   `json:"authoritative_signal"`
	Observable           string   `json:"observable"`
	ResetAction          string   `json:"reset_action"`
}

// FaultApplyResult is the structured result of lab fault apply.
type FaultApplyResult struct {
	FaultID     string      `json:"fault_id"`
	Applied     bool        `json:"applied"`
	FromState   state.State `json:"from_state"`
	ToState     state.State `json:"to_state"`
	Forced      bool        `json:"forced,omitempty"`
	Aborted     bool        `json:"aborted,omitempty"`
	AbortReason string      `json:"abort_reason,omitempty"`
}

// ResetResult is the structured result of lab reset.
type ResetResult struct {
	Tier           string                    `json:"tier"`
	FromState      state.State               `json:"from_state"`
	ToState        state.State               `json:"to_state"`
	FaultCleared   string                    `json:"fault_cleared,omitempty"`
	ValidationRan  bool                      `json:"validation_ran"`
	Suite          *conformance.SuiteResult  `json:"suite,omitempty"`
	DurationMs     int64                     `json:"duration_ms"`
}

// ProvisionResult is the structured result of lab provision.
type ProvisionResult struct {
	ToState    state.State `json:"to_state"`
	Suite      *conformance.SuiteResult `json:"suite,omitempty"`
	DurationMs int64       `json:"duration_ms"`
}

// HistoryResult is the structured result of lab history.
type HistoryResult struct {
	Entries []HistoryItem `json:"entries"`
	Total   int           `json:"total"`
}

// HistoryItem is a single entry in the history result.
type HistoryItem struct {
	Ts      time.Time   `json:"ts"`
	From    state.State `json:"from"`
	To      state.State `json:"to"`
	Command string      `json:"command"`
	Fault   string      `json:"fault,omitempty"`
	Forced  bool        `json:"forced,omitempty"`
}

// Supporting types shared across result models.

// FaultRef is a reference to an active fault in status output.
type FaultRef struct {
	ID        string    `json:"id"`
	AppliedAt time.Time `json:"applied_at"`
}

// SvcInfo is the service status in status output.
type SvcInfo struct {
	Active bool `json:"active"`
	PID    int  `json:"pid,omitempty"`
}

// PortInfo is a listening port in status output.
type PortInfo struct {
	Addr  string `json:"addr"`
	Owner string `json:"owner"`
}

// ValidateSummary is the last-validate summary in status output.
type ValidateSummary struct {
	At     time.Time `json:"at"`
	Passed int       `json:"passed"`
	Total  int       `json:"total"`
}

// ResetSummary is the last-reset summary in status output.
type ResetSummary struct {
	At   time.Time `json:"at"`
	Tier string    `json:"tier"`
}

// CommandResult wraps any result type with the exit code returned to the OS.
type CommandResult struct {
	// Value is the command-specific result (StatusResult, ValidateResult, etc.)
	Value interface{}
	// ExitCode is the process exit code. Defined in control-plane-contract §3.2.
	ExitCode int
	// Err is a command-level error. When non-nil, ExitCode is non-zero.
	Err error
}

// FromSuiteResult converts a conformance.SuiteResult to the output model's
// ValidateResult. Used by both lab validate and post-reset verification.
func FromSuiteResult(sr *conformance.SuiteResult) ValidateResult {
	vr := ValidateResult{
		At:             sr.At,
		Passed:         sr.Passed,
		Total:          sr.Total,
		Classification: sr.Classification.String(),
		FailingChecks:  sr.FailingBlockingIDs,
	}
	for _, r := range sr.Results {
		item := CheckResultItem{
			ID:        r.Check.ID,
			Assertion: r.Check.Assertion,
			Passed:    r.Passed,
			Severity:  r.Check.Severity.String(),
			Dependent: r.Dependent,
		}
		if r.Err != nil {
			item.Error = r.Err.Error()
		}
		vr.Checks = append(vr.Checks, item)
	}
	return vr
}