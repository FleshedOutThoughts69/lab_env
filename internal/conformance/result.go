package conformance

import "time"

// Classification is the conformance outcome derived from a suite run.
// Defined in conformance-model.md §4.3 and control-plane-contract §4.2.
type Classification int

const (
	// ClassConformant: all blocking checks pass. Suite exits 0.
	// May include degraded-severity failures (ClassDegradedConformant).
	ClassConformant Classification = iota

	// ClassDegradedConformant: all blocking checks pass; one or more
	// degraded-severity checks fail. Suite exits 0. The environment is
	// semantically correct but operationally degraded.
	ClassDegradedConformant

	// ClassNonConformant: one or more blocking checks fail. Suite exits 1.
	ClassNonConformant
)

func (c Classification) String() string {
	switch c {
	case ClassConformant:
		return "CONFORMANT"
	case ClassDegradedConformant:
		return "CONFORMANT (degraded)"
	case ClassNonConformant:
		return "NON-CONFORMANT"
	default:
		return "UNKNOWN"
	}
}

// IsConformant returns true for both full and degraded conformance.
// Only ClassNonConformant returns false.
func (c Classification) IsConformant() bool {
	return c == ClassConformant || c == ClassDegradedConformant
}

// SuiteResult is the complete output of a conformance suite run.
// This is the canonical result type produced by Runner.Run and consumed
// by lab validate, lab status (lightweight variant), and post-reset
// verification.
type SuiteResult struct {
	// At is the moment the suite run completed.
	At time.Time

	// Results contains one entry per check, in execution order.
	Results []CheckResult

	// Classification is derived from the results — never set directly.
	// Set by Classify() after all checks have run.
	Classification Classification

	// Passed is the count of checks that passed (blocking + degraded).
	Passed int

	// Total is the total number of checks executed.
	Total int

	// FailingBlockingIDs contains the IDs of all blocking checks that failed.
	// This is the machine-verifiable postcondition surface used by
	// fault postcondition verification.
	FailingBlockingIDs []string

	// FailingDegradedIDs contains the IDs of degraded checks that failed.
	FailingDegradedIDs []string
}

// Classify derives and sets the Classification field from the Results.
// Must be called after all checks have run.
func (r *SuiteResult) Classify() {
	r.Passed = 0
	r.FailingBlockingIDs = nil
	r.FailingDegradedIDs = nil

	for _, res := range r.Results {
		if res.Passed {
			r.Passed++
			continue
		}
		if res.Dependent {
			// Dependent failures do not contribute to classification.
			continue
		}
		switch res.Check.Severity {
		case SeverityBlocking:
			r.FailingBlockingIDs = append(r.FailingBlockingIDs, res.Check.ID)
		case SeverityDegraded:
			r.FailingDegradedIDs = append(r.FailingDegradedIDs, res.Check.ID)
		}
	}

	switch {
	case len(r.FailingBlockingIDs) > 0:
		r.Classification = ClassNonConformant
	case len(r.FailingDegradedIDs) > 0:
		r.Classification = ClassDegradedConformant
	default:
		r.Classification = ClassConformant
	}
}

// ExitCode returns the process exit code this result should produce.
// Derived from control-plane-contract §4.2:
//   - 0: all blocking checks pass (degraded failures do not affect exit code)
//   - 1: at least one blocking check fails
func (r *SuiteResult) ExitCode() int {
	if len(r.FailingBlockingIDs) > 0 {
		return 1
	}
	return 0
}

// CheckByID returns the result for the check with the given ID, or nil
// if no such check was run.
func (r *SuiteResult) CheckByID(id string) *CheckResult {
	for i := range r.Results {
		if r.Results[i].Check.ID == id {
			return &r.Results[i]
		}
	}
	return nil
}

// HasFailingCheck returns true if the check with the given ID failed.
// Used by fault postcondition verification.
func (r *SuiteResult) HasFailingCheck(id string) bool {
	for _, fid := range r.FailingBlockingIDs {
		if fid == id {
			return true
		}
	}
	for _, fid := range r.FailingDegradedIDs {
		if fid == id {
			return true
		}
	}
	return false
}