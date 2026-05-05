package state

import (
	"errors"

	"lab_env/internal/conformance"
)

// DetectionResult is the output of the state detection algorithm.
// It carries the detected state, its source of authority, and any
// reconciliation action taken.
type DetectionResult struct {
	// Detected is the state the algorithm determined from evidence.
	Detected State

	// Confidence indicates how the detected state was established.
	Confidence DetectionConfidence

	// Reconciled is true when the detected state differs from the
	// recorded state and the state file has been updated.
	Reconciled bool

	// PriorState is the state recorded before reconciliation, if any.
	PriorState State
}

// DetectionConfidence indicates the evidentiary basis for the detected state.
type DetectionConfidence int

const (
	// ConfidenceRuntime: detected via live runtime observation.
	// Highest authority per system-state-model §4.1.
	ConfidenceRuntime DetectionConfidence = iota

	// ConfidenceConformance: detected via full conformance suite result.
	ConfidenceConformance

	// ConfidenceStateFile: detected from state.json without contradicting evidence.
	ConfidenceStateFile

	// ConfidenceUnknown: contradictory or insufficient evidence.
	// This is not a state — it is a classification failure.
	// See system-state-model §4.4.
	ConfidenceUnknown
)

// Detector implements the state detection algorithm defined in
// system-state-model §4.2. It uses the Observer for lightweight runtime
// checks and a conformance SuiteResult when one is provided.
//
// The Detector does not run the conformance suite itself — it receives
// results from the Runner. This keeps detection logic separate from
// check execution.
type Detector struct {
	obs   conformance.Observer
	store *Store
}

// NewDetector returns a Detector using the given Observer and Store.
func NewDetector(obs conformance.Observer, store *Store) *Detector {
	return &Detector{obs: obs, store: store}
}

// DetectInput is the input to Detect. All fields are optional; the
// algorithm applies the authority precedence rules from §4.1 based on
// which inputs are provided.
type DetectInput struct {
	// StateFile is the current state.json content. Nil if unreadable.
	StateFile *File

	// SuiteResult is the result of a recent conformance suite run.
	// Nil if no suite run has occurred in this invocation.
	SuiteResult *conformance.SuiteResult

	// LightweightResult is the result of the lightweight checks run by
	// lab status. Nil if not available.
	LightweightResult *conformance.SuiteResult
}

// Detect applies the state detection algorithm from system-state-model §4.2.
// It returns the detected state and whether a reconciliation occurred.
// It does NOT write to the state file — reconciliation writes are the
// responsibility of the caller (lab status).
func Detect(input DetectInput) DetectionResult {
	// Step 1: establish the recorded state from the state file.
	var recorded State
	var activeFault *ActiveFault
	var classificationValid bool

	if input.StateFile != nil {
		recorded = input.StateFile.State
		activeFault = input.StateFile.ActiveFault
		classificationValid = input.StateFile.ClassificationValid
	}

	// Step 2: if classification was invalidated by an interrupt,
	// we cannot trust the cached state. We must use runtime evidence.
	if !classificationValid && input.StateFile != nil {
		// Force re-detection from runtime/suite evidence.
		recorded = ""
	}

	// Step 3: apply runtime observation evidence (highest authority).
	// The lightweight checks P-001, P-002, S-003, E-001 provide the
	// fastest signal on the most common failure modes.
	if input.LightweightResult != nil {
		return reconcileFromRuntime(input.LightweightResult, activeFault, recorded)
	}

	// Step 4: apply conformance suite result if available.
	if input.SuiteResult != nil {
		return reconcileFromSuite(input.SuiteResult, activeFault, recorded)
	}

	// Step 5: fall back to state file if no runtime evidence.
	if recorded != "" {
		return DetectionResult{
			Detected:   recorded,
			Confidence: ConfidenceStateFile,
		}
	}

	// Step 6: insufficient evidence — UNKNOWN classification failure.
	return DetectionResult{
		Detected:   "",
		Confidence: ConfidenceUnknown,
	}
}

// reconcileFromRuntime applies conflict resolution rules from
// system-state-model §4.3 using lightweight runtime check results.
func reconcileFromRuntime(lr *conformance.SuiteResult, activeFault *ActiveFault, recorded State) DetectionResult {
	runtimeHealthy := lr.Classification.IsConformant()

	if runtimeHealthy {
		if activeFault != nil {
			// Suite passes + active fault recorded = DEGRADED
			// (expected failures for active fault are present).
			return DetectionResult{
				Detected:   StateDegraded,
				Confidence: ConfidenceRuntime,
				Reconciled: recorded != StateDegraded,
				PriorState: recorded,
			}
		}
		// Suite passes + no active fault = CONFORMANT.
		detected := StateConformant
		return DetectionResult{
			Detected:   detected,
			Confidence: ConfidenceRuntime,
			Reconciled: recorded != detected,
			PriorState: recorded,
		}
	}

	// Runtime shows unhealthy.
	if activeFault != nil {
		// Check if the failures are consistent with the active fault's postcondition.
		// We cannot do full postcondition verification in detect.go (that requires
		// the fault catalog). We optimistically trust the recorded DEGRADED state
		// when runtime shows failures and an active fault is recorded.
		return DetectionResult{
			Detected:   StateDegraded,
			Confidence: ConfidenceRuntime,
		}
	}

	// Runtime unhealthy + no active fault = BROKEN.
	return DetectionResult{
		Detected:   StateBroken,
		Confidence: ConfidenceRuntime,
		Reconciled: recorded != StateBroken,
		PriorState: recorded,
	}
}

// reconcileFromSuite applies conflict resolution rules from
// system-state-model §4.3 using full conformance suite results.
func reconcileFromSuite(sr *conformance.SuiteResult, activeFault *ActiveFault, recorded State) DetectionResult {
	suiteConformant := sr.Classification.IsConformant()

	// Case: conformance suite passes but state file records DEGRADED.
	// Resolution: CONFORMANT wins (system-state-model §4.3, case 1).
	if suiteConformant && recorded == StateDegraded {
		return DetectionResult{
			Detected:   StateConformant,
			Confidence: ConfidenceConformance,
			Reconciled: true,
			PriorState: recorded,
		}
	}

	// Case: conformance suite fails but state file records CONFORMANT.
	// Resolution: BROKEN (system-state-model §4.3, case 2).
	if !suiteConformant && recorded == StateConformant {
		return DetectionResult{
			Detected:   StateBroken,
			Confidence: ConfidenceConformance,
			Reconciled: true,
			PriorState: recorded,
		}
	}

	// Case: suite shows fault-consistent failures, no fault recorded.
	// Resolution: BROKEN — manual mutation mimics a fault but was not
	// applied through the control plane (system-state-model §4.3, case 3).
	if !suiteConformant && activeFault == nil {
		return DetectionResult{
			Detected:   StateBroken,
			Confidence: ConfidenceConformance,
			Reconciled: recorded != StateBroken,
			PriorState: recorded,
		}
	}

	// Case: multiple blocking checks fail in any pattern.
	// Resolution: BROKEN (system-state-model §4.3, case 4).
	if len(sr.FailingBlockingIDs) > 0 {
		return DetectionResult{
			Detected:   StateBroken,
			Confidence: ConfidenceConformance,
			Reconciled: recorded != StateBroken,
			PriorState: recorded,
		}
	}

	// Suite passes — use the recorded state, updated if DEGRADED without
	// an active fault (invariant I-2 repair).
	if activeFault == nil && recorded == StateDegraded {
		return DetectionResult{
			Detected:   StateConformant,
			Confidence: ConfidenceConformance,
			Reconciled: true,
			PriorState: recorded,
		}
	}

	detected := recorded
	if detected == "" {
		detected = StateConformant
	}
	return DetectionResult{
		Detected:   detected,
		Confidence: ConfidenceConformance,
	}
}

// IsUnknown returns true when the detection algorithm could not determine
// the state due to contradictory or insufficient evidence.
func IsUnknown(r DetectionResult) bool {
	return r.Confidence == ConfidenceUnknown || r.Detected == ""
}

// StateFileError returns true if the error represents a state file problem
// (not found or corrupt).
func StateFileError(err error) bool {
	var notFound ErrStateFileNotFound
	var corrupt ErrStateFileCorrupt
	return errors.As(err, &notFound) || errors.As(err, &corrupt)
}