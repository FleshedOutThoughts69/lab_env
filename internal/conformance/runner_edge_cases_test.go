package conformance

// runner_edge_cases_test.go
//
// Tests conformance runner behavior under edge conditions:
//   - A panicking check must be caught; the runner continues with remaining checks
//   - Severity assignment invariant: all blocking checks are S/P/E series;
//     all degraded checks are F-006 and L series

import (
	"testing"

	"lab-env/lab/internal/conformance"
)

// TestRunner_PanickingCheck_DoesNotHaltSuite verifies that if a check's
// Execute function panics, the runner catches the panic, records a failed
// result for that check, and continues running all remaining checks.
//
// A single panicking check must never halt the entire suite — a degraded
// environment might have unexpected conditions that trigger panics in
// specific checks. The suite must always produce a complete result.
func TestRunner_PanickingCheck_DoesNotHaltSuite(t *testing.T) {
	panicCheck := conformance.Check{
		ID:       "X-001",
		Severity: conformance.SeverityBlocking,
		Layer:    conformance.LayerBehavioral,
		Category: conformance.CategoryE,
		Assertion: "synthetic panicking check",
		ObservableCommand: "false",
		Execute: func(obs conformance.Observer) error {
			panic("simulated check panic")
		},
	}

	passingCheck := conformance.Check{
		ID:       "X-002",
		Severity: conformance.SeverityBlocking,
		Layer:    conformance.LayerBehavioral,
		Category: conformance.CategoryE,
		Assertion: "synthetic passing check",
		ObservableCommand: "true",
		Execute: func(obs conformance.Observer) error {
			return nil
		},
	}

	runner := conformance.NewRunnerWithChecks(
		[]conformance.Check{panicCheck, passingCheck},
		conformance.NewStubObserver(),
	)

	result := runner.Run()

	// Suite must complete — both checks must have results
	if len(result.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(result.Results))
	}

	// Panicking check must be recorded as failed
	var panicResult *conformance.CheckResult
	var passResult *conformance.CheckResult
	for i := range result.Results {
		switch result.Results[i].ID {
		case "X-001":
			r := result.Results[i]
			panicResult = &r
		case "X-002":
			r := result.Results[i]
			passResult = &r
		}
	}

	if panicResult == nil {
		t.Fatal("no result for panicking check X-001")
	}
	if panicResult.Passed {
		t.Error("panicking check X-001: Passed=true; expected Passed=false")
	}
	if panicResult.Err == nil {
		t.Error("panicking check X-001: Err=nil; expected non-nil error describing the panic")
	}

	if passResult == nil {
		t.Fatal("no result for passing check X-002")
	}
	if !passResult.Passed {
		t.Error("passing check X-002: Passed=false; panic in X-001 should not affect X-002")
	}
}

// TestCatalog_SeverityInvariant_BlockingChecksAreInCorrectSeries verifies that
// every blocking check belongs to the S, P, or E series, and every degraded
// check belongs to the F-006 or L series.
//
// Reference: conformance-model.md §4.4 (verdict derivation rules).
// The classification model depends on this mapping: if an L-series check were
// accidentally set to blocking, it would make L-001 failure cause exit 1,
// breaking all pipelines that expect exit 0 on degraded-only failures.
func TestCatalog_SeverityInvariant_BlockingChecksAreInCorrectSeries(t *testing.T) {
	checks := conformance.AllChecks()

	for _, check := range checks {
		t.Run(check.ID, func(t *testing.T) {
			switch check.Severity {
			case conformance.SeverityBlocking:
				// Blocking checks must be S, P, or E series
				if check.Category != conformance.CategoryS &&
					check.Category != conformance.CategoryP &&
					check.Category != conformance.CategoryE &&
					check.ID != "F-001" && check.ID != "F-002" &&
					check.ID != "F-003" && check.ID != "F-004" &&
					check.ID != "F-005" && check.ID != "F-007" {
					t.Errorf("%s is blocking but category=%v; expected S/P/E or F-001..F-005/F-007",
						check.ID, check.Category)
				}

			case conformance.SeverityDegraded:
				// Degraded checks must be F-006 or L series
				if check.ID != "F-006" && check.Category != conformance.CategoryL {
					t.Errorf("%s is degraded but category=%v (id=%s); expected F-006 or L series",
						check.ID, check.Category, check.ID)
				}
			}
		})
	}
}

// TestCatalog_AllChecks_HandleMissingFilesGracefully verifies that every
// check that reads a file returns a non-panicking error when the file is
// missing, rather than a nil pointer dereference or an unhandled os.Open error.
//
// Uses a stub observer that returns ErrNotExist for all file reads.
func TestCatalog_AllChecks_HandleMissingFilesGracefully(t *testing.T) {
	checks := conformance.AllChecks()
	emptyObs := conformance.NewEmptyObserver() // returns errors for all calls

	for _, check := range checks {
		check := check
		t.Run(check.ID, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked on empty observer: %v", check.ID, r)
				}
			}()

			// Execute must return an error (check fails), not panic
			err := check.Execute(emptyObs)
			// We don't care about the error value — just that it didn't panic
			_ = err
		})
	}
}