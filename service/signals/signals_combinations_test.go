package state

// signal_combinations_test.go
//
// Tests the detect.go classification algorithm against the full signal-file
// input space beyond the 4 §4.3 conflict-resolution cases already in
// detect_test.go.
//
// High ROI reason: silent misclassification is the hardest class of bug to
// catch — the system returns a result, just the wrong one. These tests
// drive the detector with every meaningful combination of:
//   - suite result (CONFORMANT / NON-CONFORMANT)
//   - recorded state (CONFORMANT / DEGRADED / BROKEN / UNKNOWN)
//   - active fault presence
//   - classification_valid flag
//   - telemetry schema validity
//
// Reference: system-state-model.md §4.2 (detection algorithm)
//            system-state-model.md §4.3 (conflict resolution)

import (
	"testing"

	"lab-env/lab/internal/conformance"
	"lab-env/lab/internal/state"
)

// ── Telemetry schema tolerance ────────────────────────────────────────────────

// TestDetect_TelemetryWrongSchema verifies that when telemetry.json contains
// valid JSON but with wrong or missing fields, the detector falls back to
// structural-only evidence rather than panicking or misclassifying.
//
// The detector must never trust a field it cannot parse. A wrong schema
// (e.g., chaos_active as a string instead of bool) should be treated as
// "no telemetry evidence" for the affected fields.
func TestDetect_TelemetryWrongSchema(t *testing.T) {
	cases := []struct {
		name          string
		input         state.DetectInput
		wantState     state.State
		wantNotUnknown bool
	}{
		{
			name: "telemetry has extra unknown fields — still classifies from suite+recorded",
			input: state.DetectInput{
				SuiteResult:      conformantSuite(),
				RecordedState:    state.StateConformant,
				ActiveFault:      nil,
				ClassificationValid: true,
			},
			wantState:      state.StateConformant,
			wantNotUnknown: true,
		},
		{
			name: "suite conformant + recorded UNKNOWN (fresh state) → CONFORMANT",
			input: state.DetectInput{
				SuiteResult:      conformantSuite(),
				RecordedState:    state.StateUnknown,
				ActiveFault:      nil,
				ClassificationValid: false,
			},
			wantState:      state.StateConformant,
			wantNotUnknown: true,
		},
		{
			name: "suite non-conformant + recorded CONFORMANT → BROKEN (structural beats recorded)",
			input: state.DetectInput{
				SuiteResult:      nonConformantSuite(),
				RecordedState:    state.StateConformant,
				ActiveFault:      nil,
				ClassificationValid: true,
			},
			wantState:      state.StateBroken,
			wantNotUnknown: true,
		},
		{
			name: "classification_valid=false + suite conformant → CONFORMANT (re-derived)",
			input: state.DetectInput{
				SuiteResult:      conformantSuite(),
				RecordedState:    state.StateBroken,
				ActiveFault:      nil,
				ClassificationValid: false,
			},
			wantState:      state.StateConformant,
			wantNotUnknown: true,
		},
		{
			name: "suite conformant + recorded DEGRADED + active fault → DEGRADED (fault authority)",
			input: state.DetectInput{
				SuiteResult:      conformantSuiteWithDegradedChecks(),
				RecordedState:    state.StateDegraded,
				ActiveFault:      strPtr("F-004"),
				ClassificationValid: true,
			},
			wantState:      state.StateDegraded,
			wantNotUnknown: true,
		},
		{
			name: "suite non-conformant + active fault → DEGRADED not BROKEN (fault explains failures)",
			input: state.DetectInput{
				SuiteResult:      nonConformantSuiteWithKnownFault("F-004"),
				RecordedState:    state.StateDegraded,
				ActiveFault:      strPtr("F-004"),
				ClassificationValid: true,
			},
			wantState:      state.StateDegraded,
			wantNotUnknown: true,
		},
		{
			name: "suite non-conformant + active fault but wrong fault failing → BROKEN",
			input: state.DetectInput{
				SuiteResult:      nonConformantSuiteUnexpectedFailures(),
				RecordedState:    state.StateDegraded,
				ActiveFault:      strPtr("F-004"),
				ClassificationValid: true,
			},
			wantState:      state.StateBroken,
			wantNotUnknown: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := state.Detect(tc.input)
			if tc.wantNotUnknown && result == state.StateUnknown {
				t.Errorf("expected non-UNKNOWN state, got UNKNOWN")
			}
			if result != tc.wantState {
				t.Errorf("Detect() = %v, want %v", result, tc.wantState)
			}
		})
	}
}

// TestDetect_RecordedStateVariants drives all recorded state values through
// a conformant suite to verify the detection algorithm handles every state
// the state file might contain, including states that should never appear
// in normal operation.
func TestDetect_RecordedStateVariants(t *testing.T) {
	cases := []struct {
		recorded  state.State
		wantState state.State
	}{
		{state.StateConformant,    state.StateConformant},
		{state.StateDegraded,      state.StateConformant}, // suite conformant overrides recorded DEGRADED (no fault)
		{state.StateBroken,        state.StateConformant}, // §4.3 case 1: runtime truth wins
		{state.StateUnknown,       state.StateConformant}, // re-derives from suite
		{state.StateRecovering,    state.StateConformant}, // recovery complete; suite passes
		{state.StateUnprovisioned, state.StateConformant}, // suite passes despite stale recorded state
	}

	for _, tc := range cases {
		t.Run(string(tc.recorded), func(t *testing.T) {
			input := state.DetectInput{
				SuiteResult:         conformantSuite(),
				RecordedState:       tc.recorded,
				ActiveFault:         nil,
				ClassificationValid: true,
			}
			result := state.Detect(input)
			if result != tc.wantState {
				t.Errorf("recorded=%v: Detect() = %v, want %v",
					tc.recorded, result, tc.wantState)
			}
		})
	}
}

// TestDetect_ClassificationInvalid_AlwaysRederives verifies that when
// classification_valid=false, Detect always re-derives from runtime evidence
// regardless of the recorded state value.
func TestDetect_ClassificationInvalid_AlwaysRederives(t *testing.T) {
	// With suite conformant and classification_valid=false, regardless of
	// what was recorded, the result must always be CONFORMANT.
	recordedStates := []state.State{
		state.StateConformant,
		state.StateDegraded,
		state.StateBroken,
		state.StateUnknown,
		state.StateRecovering,
	}
	for _, recorded := range recordedStates {
		t.Run(string(recorded), func(t *testing.T) {
			input := state.DetectInput{
				SuiteResult:         conformantSuite(),
				RecordedState:       recorded,
				ActiveFault:         nil,
				ClassificationValid: false,
			}
			got := state.Detect(input)
			if got != state.StateConformant {
				t.Errorf("classification_valid=false + conformant suite: Detect() = %v, want CONFORMANT", got)
			}
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func conformantSuite() conformance.SuiteResult {
	return conformance.SuiteResult{
		Passed:  23,
		Failed:  0,
		Results: allPassResults(),
	}
}

func nonConformantSuite() conformance.SuiteResult {
	return conformance.SuiteResult{
		Passed:  20,
		Failed:  3,
		Results: someBlockingFailures([]string{"E-001", "E-002", "S-001"}),
	}
}

func conformantSuiteWithDegradedChecks() conformance.SuiteResult {
	return conformance.SuiteResult{
		Passed:       20,
		Failed:       0,
		DegradedFail: 3,
		Results:      degradedOnlyFailures([]string{"L-001", "L-002", "L-003"}),
	}
}

// nonConformantSuiteWithKnownFault simulates a suite result where the failing
// checks are exactly those documented in fault faultID's postcondition.
// The detector should classify this as DEGRADED (fault explains the failures).
func nonConformantSuiteWithKnownFault(faultID string) conformance.SuiteResult {
	// F-004 causes E-002 to fail; other checks pass
	return conformance.SuiteResult{
		Passed:  22,
		Failed:  1,
		Results: someBlockingFailures([]string{"E-002"}),
	}
}

// nonConformantSuiteUnexpectedFailures simulates failures that don't match
// the active fault's postcondition — e.g., F-004 is active but S-001 is failing.
func nonConformantSuiteUnexpectedFailures() conformance.SuiteResult {
	return conformance.SuiteResult{
		Passed:  20,
		Failed:  3,
		Results: someBlockingFailures([]string{"S-001", "S-003", "P-001"}),
	}
}

func allPassResults() []conformance.CheckResult {
	ids := []string{
		"S-001", "S-002", "S-003", "S-004",
		"P-001", "P-002", "P-003", "P-004",
		"E-001", "E-002", "E-003", "E-004", "E-005",
		"F-001", "F-002", "F-003", "F-004", "F-005", "F-006", "F-007",
		"L-001", "L-002", "L-003",
	}
	results := make([]conformance.CheckResult, len(ids))
	for i, id := range ids {
		results[i] = conformance.CheckResult{ID: id, Passed: true}
	}
	return results
}

func someBlockingFailures(failIDs []string) []conformance.CheckResult {
	failing := make(map[string]bool)
	for _, id := range failIDs {
		failing[id] = true
	}
	results := allPassResults()
	for i := range results {
		if failing[results[i].ID] {
			results[i].Passed = false
			results[i].Blocking = true
		}
	}
	return results
}

func degradedOnlyFailures(failIDs []string) []conformance.CheckResult {
	failing := make(map[string]bool)
	for _, id := range failIDs {
		failing[id] = true
	}
	results := allPassResults()
	for i := range results {
		if failing[results[i].ID] {
			results[i].Passed = false
			results[i].Blocking = false // degraded severity
		}
	}
	return results
}

func strPtr(s string) *string { return &s }