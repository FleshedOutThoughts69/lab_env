package state

// detect_test.go exercises the state detection algorithm from
// system-state-model.md §4.2 and all four conflict resolution cases
// from §4.3. These tests are the primary harness pressure on the
// highest-value semantic drift watchpoint in the codebase.
//
// Test structure: table-driven, one case per named scenario.
// Each case names the invariant being tested, the evidence inputs,
// and the exact expected outcome. Comments identify the §4.3 case
// number each conflict resolution test maps to.

import (
	"testing"
	"time"

	"lab_env/internal/conformance"
	. "lab_env/internal/state"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// suiteResult builds a minimal SuiteResult for use in detect inputs.
func suiteResult(classification conformance.Classification, failingBlockingIDs []string) *conformance.SuiteResult {
	sr := &conformance.SuiteResult{
		At:                 time.Now().UTC(),
		Classification:     classification,
		FailingBlockingIDs: failingBlockingIDs,
	}
	if failingBlockingIDs == nil {
		sr.FailingBlockingIDs = []string{}
	}
	return sr
}

func conformantSuite() *conformance.SuiteResult {
	return suiteResult(conformance.ClassConformant, nil)
}

func degradedSuite() *conformance.SuiteResult {
	return suiteResult(conformance.ClassDegradedConformant, nil)
}

func nonConformantSuite(failingIDs ...string) *conformance.SuiteResult {
	return suiteResult(conformance.ClassNonConformant, failingIDs)
}

// stateFile builds a minimal state File for use in detect inputs.
func stateFile(s State, activeFault *ActiveFault, classificationValid bool) *File {
	return &File{
		SpecVersion:         "1.0.0",
		State:               s,
		ClassificationValid: classificationValid,
		ActiveFault:         activeFault,
	}
}

func activeFault(id string) *ActiveFault {
	return &ActiveFault{
		ID:        id,
		AppliedAt: time.Now().UTC(),
		Forced:    false,
	}
}

// ── Detect() adversarial matrix ──────────────────────────────────────────────

func TestDetect(t *testing.T) {
	tests := []struct {
		name              string
		input             DetectInput
		wantDetected      State
		wantConfidence    DetectionConfidence
		wantReconciled    bool
		wantUnknown       bool
	}{
		// ── No evidence cases ────────────────────────────────────────────────

		{
			name:           "no evidence at all produces UNKNOWN",
			input:          DetectInput{},
			wantUnknown:    true,
			wantConfidence: ConfidenceUnknown,
		},
		{
			name: "state file only — CONFORMANT no fault",
			input: DetectInput{
				StateFile: stateFile(StateConformant, nil, true),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceStateFile,
		},
		{
			name: "state file only — DEGRADED with fault",
			input: DetectInput{
				StateFile: stateFile(StateDegraded, activeFault("F-004"), true),
			},
			wantDetected:   StateDegraded,
			wantConfidence: ConfidenceStateFile,
		},
		{
			name: "state file only — BROKEN",
			input: DetectInput{
				StateFile: stateFile(StateBroken, nil, true),
			},
			wantDetected:   StateBroken,
			wantConfidence: ConfidenceStateFile,
		},

		// ── classification_valid: false (interrupt recovery) ─────────────────

		{
			name: "classification invalid — no suite — UNKNOWN",
			input: DetectInput{
				StateFile: stateFile(StateConformant, nil, false), // interrupted
			},
			wantUnknown:    true,
			wantConfidence: ConfidenceUnknown,
		},
		{
			name: "classification invalid — lightweight suite passes — CONFORMANT",
			input: DetectInput{
				StateFile:         stateFile(StateConformant, nil, false),
				LightweightResult: conformantSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceRuntime,
		},
		{
			name: "classification invalid — lightweight suite fails — BROKEN",
			input: DetectInput{
				StateFile:         stateFile(StateConformant, nil, false),
				LightweightResult: nonConformantSuite("P-002"),
			},
			wantDetected:   StateBroken,
			wantConfidence: ConfidenceRuntime,
		},

		// ── Lightweight result (runtime authority) ───────────────────────────

		{
			name: "lightweight passes — no fault — CONFORMANT",
			input: DetectInput{
				StateFile:         stateFile(StateConformant, nil, true),
				LightweightResult: conformantSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceRuntime,
		},
		{
			name: "lightweight passes — active fault recorded — DEGRADED",
			input: DetectInput{
				StateFile:         stateFile(StateDegraded, activeFault("F-004"), true),
				LightweightResult: conformantSuite(),
			},
			wantDetected:   StateDegraded,
			wantConfidence: ConfidenceRuntime,
		},
		{
			name: "lightweight fails — no fault — BROKEN",
			input: DetectInput{
				StateFile:         stateFile(StateConformant, nil, true),
				LightweightResult: nonConformantSuite("E-001"),
			},
			wantDetected:   StateBroken,
			wantConfidence: ConfidenceRuntime,
			wantReconciled: true,
		},
		{
			name: "lightweight fails — fault recorded — optimistic DEGRADED (system-state-model §4.3 case 3 watchpoint)",
			// This is the detect.go optimistic DEGRADED trust case.
			// Runtime shows failures AND a fault is recorded.
			// The algorithm optimistically trusts the recorded DEGRADED state.
			// This is the primary semantic drift watchpoint.
			input: DetectInput{
				StateFile:         stateFile(StateDegraded, activeFault("F-004"), true),
				LightweightResult: nonConformantSuite("E-002"),
			},
			wantDetected:   StateDegraded,
			wantConfidence: ConfidenceRuntime,
		},
		{
			name: "lightweight passes — state was BROKEN — reconcile to CONFORMANT",
			input: DetectInput{
				StateFile:         stateFile(StateBroken, nil, true),
				LightweightResult: conformantSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceRuntime,
			wantReconciled: true,
		},

		// ── Full suite (conformance authority) ───────────────────────────────

		{
			name: "§4.3 case 1: suite passes but state file records DEGRADED — CONFORMANT wins",
			// system-state-model §4.3: "suite passes but state file records DEGRADED"
			// Resolution: CONFORMANT wins; fault was likely cleared outside control plane.
			input: DetectInput{
				StateFile:   stateFile(StateDegraded, activeFault("F-001"), true),
				SuiteResult: conformantSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceConformance,
			wantReconciled: true,
		},
		{
			name: "§4.3 case 2: suite fails but state file records CONFORMANT — BROKEN",
			// system-state-model §4.3: "suite fails but state file records CONFORMANT"
			// Resolution: BROKEN — environment was modified outside control plane.
			input: DetectInput{
				StateFile:   stateFile(StateConformant, nil, true),
				SuiteResult: nonConformantSuite("S-001", "E-001"),
			},
			wantDetected:   StateBroken,
			wantConfidence: ConfidenceConformance,
			wantReconciled: true,
		},
		{
			name: "§4.3 case 3: suite fails — fault-pattern failures — no fault recorded — BROKEN",
			// system-state-model §4.3: "suite shows fault-consistent failures, no fault recorded"
			// Resolution: BROKEN — manual mutation mimics fault but not applied via control plane.
			input: DetectInput{
				StateFile:   stateFile(StateConformant, nil, true),
				SuiteResult: nonConformantSuite("E-002"), // matches F-004 postcondition
			},
			wantDetected:   StateBroken,
			wantConfidence: ConfidenceConformance,
			wantReconciled: true,
		},
		{
			name: "§4.3 case 4: multiple blocking checks fail — BROKEN",
			// system-state-model §4.3: "multiple blocking checks fail in any pattern"
			input: DetectInput{
				StateFile:   stateFile(StateConformant, nil, true),
				SuiteResult: nonConformantSuite("S-001", "P-001", "P-002", "E-001"),
			},
			wantDetected:   StateBroken,
			wantConfidence: ConfidenceConformance,
			wantReconciled: true,
		},
		{
			name: "suite passes — active fault null — CONFORMANT",
			input: DetectInput{
				StateFile:   stateFile(StateConformant, nil, true),
				SuiteResult: conformantSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceConformance,
		},
		{
			name: "suite passes — degraded-only failures — CONFORMANT",
			// Degraded classification counts as conformant for state detection.
			input: DetectInput{
				StateFile:   stateFile(StateConformant, nil, true),
				SuiteResult: degradedSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceConformance,
		},
		{
			name: "suite passes — invariant I-2 repair: DEGRADED state but no fault in suite result",
			// If suite passes but state was DEGRADED and active_fault is now null,
			// state should reconcile to CONFORMANT.
			input: DetectInput{
				StateFile:   stateFile(StateDegraded, nil, true), // fault was nil — invariant I-2 violation
				SuiteResult: conformantSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceConformance,
			wantReconciled: true,
		},

		// ── Adversarial: contradictory inputs ────────────────────────────────

		{
			name: "lightweight takes priority over full suite when both present",
			// Runtime observation is highest authority (§4.1).
			// When LightweightResult is provided, it is used; SuiteResult is ignored.
			input: DetectInput{
				StateFile:         stateFile(StateConformant, nil, true),
				LightweightResult: nonConformantSuite("E-001"), // runtime: failing
				SuiteResult:       conformantSuite(),            // suite: passing — ignored
			},
			wantDetected:   StateBroken,
			wantConfidence: ConfidenceRuntime,
			wantReconciled: true,
		},
		{
			name: "nil state file + conformant suite — CONFORMANT",
			// No state file, but suite passes. Should produce CONFORMANT.
			input: DetectInput{
				StateFile:   nil,
				SuiteResult: conformantSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceConformance,
		},
		{
			name: "nil state file + failing suite — BROKEN",
			input: DetectInput{
				StateFile:   nil,
				SuiteResult: nonConformantSuite("S-001"),
			},
			wantDetected:   StateBroken,
			wantConfidence: ConfidenceConformance,
		},
		{
			name: "active fault recorded — suite passes — reconcile to CONFORMANT (fault cleared externally)",
			// This is §4.3 case 1 applied via suite (not lightweight).
			// The fault was cleared outside the control plane.
			input: DetectInput{
				StateFile:   stateFile(StateDegraded, activeFault("F-007"), true),
				SuiteResult: conformantSuite(),
			},
			wantDetected:   StateConformant,
			wantConfidence: ConfidenceConformance,
			wantReconciled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.input)

			if tt.wantUnknown {
				if !IsUnknown(result) {
					t.Errorf("IsUnknown = false, want true; Detected=%q Confidence=%d",
						result.Detected, result.Confidence)
				}
				return
			}

			if result.Detected != tt.wantDetected {
				t.Errorf("Detected = %q, want %q", result.Detected, tt.wantDetected)
			}
			if result.Confidence != tt.wantConfidence {
				t.Errorf("Confidence = %d, want %d", result.Confidence, tt.wantConfidence)
			}
			if result.Reconciled != tt.wantReconciled {
				t.Errorf("Reconciled = %v, want %v", result.Reconciled, tt.wantReconciled)
			}
		})
	}
}

// TestDetectReconciliationPriorState verifies that when reconciliation occurs,
// the PriorState field correctly records the state before reconciliation.
func TestDetectReconciliationPriorState(t *testing.T) {
	// DEGRADED → CONFORMANT reconciliation: PriorState should be DEGRADED
	result := Detect(DetectInput{
		StateFile:   stateFile(StateDegraded, activeFault("F-001"), true),
		SuiteResult: conformantSuite(),
	})
	if !result.Reconciled {
		t.Fatal("expected reconciliation")
	}
	if result.PriorState != StateDegraded {
		t.Errorf("PriorState = %q, want DEGRADED", result.PriorState)
	}
	if result.Detected != StateConformant {
		t.Errorf("Detected = %q, want CONFORMANT", result.Detected)
	}
}

// TestIsUnknown verifies the UNKNOWN classification failure detector.
func TestIsUnknown(t *testing.T) {
	unknown := DetectionResult{Confidence: ConfidenceUnknown}
	if !IsUnknown(unknown) {
		t.Error("ConfidenceUnknown should produce IsUnknown=true")
	}

	emptyDetected := DetectionResult{Detected: "", Confidence: ConfidenceStateFile}
	if !IsUnknown(emptyDetected) {
		t.Error("empty Detected should produce IsUnknown=true")
	}

	known := DetectionResult{Detected: StateConformant, Confidence: ConfidenceRuntime}
	if IsUnknown(known) {
		t.Error("valid state with runtime confidence should not be UNKNOWN")
	}
}