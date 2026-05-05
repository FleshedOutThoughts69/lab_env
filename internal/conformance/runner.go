package conformance

import (
	"fmt"
	"time"
)

// Runner executes the conformance suite against the system via an Observer.
// It enforces the check dependency order defined in conformance-model.md §4.2:
//
//   S-series → P-series → E-series (dependent on S-001 passing)
//   F-series and L-series are independent.
//
// All checks run even when earlier checks fail — the full picture is required.
// The single exception: when S-001 fails, E-series checks still execute but
// their failures are marked as Dependent (expected consequence of S-001 failure,
// not independent signal).
type Runner struct {
	checks []*Check
}

// NewRunner returns a Runner loaded with the full check catalog.
func NewRunner() *Runner {
	return &Runner{checks: Catalog()}
}

// NewRunnerWith returns a Runner loaded with the provided checks.
// Used in tests to run a subset of the catalog.
func NewRunnerWith(checks []*Check) *Runner {
	return &Runner{checks: checks}
}

// Run executes all checks and returns a SuiteResult with Classification set.
// The Observer provides all system access; the runner makes no direct
// system calls.
func (r *Runner) Run(o Observer) *SuiteResult {
	result := &SuiteResult{
		At:    time.Now().UTC(),
		Total: len(r.checks),
	}

	// Track whether S-001 passed — used to mark E-series as dependent.
	s001Passed := true

	for _, check := range r.checks {
		res := r.runOne(check, o, s001Passed)
		result.Results = append(result.Results, res)

		if check.ID == "S-001" && !res.Passed {
			s001Passed = false
		}
	}

	result.Classify()
	return result
}

// RunSingle executes a single check by ID and returns its result.
// Used by lab validate --check <ID>.
// Does not update any state file and does not write audit entries.
// Returns an error if the check ID is not found in the catalog.
func (r *Runner) RunSingle(id string, o Observer) (*CheckResult, error) {
	for _, check := range r.checks {
		if check.ID == id {
			res := r.runOne(check, o, true) // s001Passed=true: no dependent marking for single runs
			return &res, nil
		}
	}
	return nil, fmt.Errorf("unknown check ID: %q", id)
}

// RunIDs executes only the checks with the given IDs.
// Used for fault postcondition verification — running only the checks
// listed in a fault's FailingChecks to confirm the fault is active.
// Returns results in catalog order regardless of the order of ids.
func (r *Runner) RunIDs(ids []string, o Observer) []*CheckResult {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}

	var results []*CheckResult
	for _, check := range r.checks {
		if !want[check.ID] {
			continue
		}
		res := r.runOne(check, o, true)
		results = append(results, &res)
	}
	return results
}

// LightweightRun executes the minimal subset of checks used by lab status
// to determine whether the environment appears healthy without running the
// full suite. It runs P-001, P-002, S-003, and E-001 — the four checks
// that provide the fastest signal on the most common failure modes.
//
// The result is a partial SuiteResult; it should not be used to update
// the authoritative conformance classification. It is only for lab status's
// lightweight display.
func (r *Runner) LightweightRun(o Observer) *SuiteResult {
	lightweightIDs := []string{"P-001", "P-002", "S-003", "E-001"}
	results := r.RunIDs(lightweightIDs, o)

	sr := &SuiteResult{
		At:    time.Now().UTC(),
		Total: len(results),
	}
	for _, res := range results {
		sr.Results = append(sr.Results, *res)
	}
	sr.Classify()
	return sr
}

func (r *Runner) runOne(check *Check, o Observer, s001Passed bool) CheckResult {
	res := CheckResult{Check: check}

	// Mark E-series checks as dependent when S-001 failed.
	// They still run (per conformance-model §4.2) but their failure
	// is noted as a consequence, not independent signal.
	if check.Category == CategoryEndpoint && !s001Passed {
		res.Dependent = true
	}

	err := check.Execute(o)
	if err != nil {
		res.Passed = false
		res.Err = err
	} else {
		res.Passed = true
	}

	return res
}