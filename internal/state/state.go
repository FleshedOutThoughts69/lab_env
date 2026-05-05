// Package state implements the state machine defined in system-state-model.md.
// It provides the state type, state file persistence, the detection algorithm,
// and the conflict resolution rules.
//
// This package depends on conformance result types but NOT on the conformance
// runner — it receives SuiteResult values, it does not execute checks itself.
package state

import "fmt"

// State represents one of the six states defined in system-state-model.md §2.
// States are mutually exclusive and collectively exhaustive over all reachable
// environment conditions.
type State string

const (
	// StateUnprovisioned: VM exists but bootstrap has not been run.
	// No canonical files, users, services, or the lab binary present.
	StateUnprovisioned State = "UNPROVISIONED"

	// StateProvisioned: bootstrap completed, conformance not yet verified.
	// Transitional — should not persist beyond the bootstrap + validate sequence.
	StateProvisioned State = "PROVISIONED"

	// StateConformant: all blocking conformance checks pass; no active fault.
	// Required precondition for fault injection.
	// Defined by reference to conformance-model.md §3 — not by this package.
	StateConformant State = "CONFORMANT"

	// StateDegraded: exactly one catalog fault deliberately applied.
	// Active fault ID is recorded in state.json.
	StateDegraded State = "DEGRADED"

	// StateBroken: one or more blocking checks fail; no active fault.
	// Recoverable through a defined reset tier.
	StateBroken State = "BROKEN"

	// StateRecovering: a reset operation is in progress.
	// Transitional — bounded duration.
	StateRecovering State = "RECOVERING"
)

// All returns all valid operational states in definition order.
func All() []State {
	return []State{
		StateUnprovisioned,
		StateProvisioned,
		StateConformant,
		StateDegraded,
		StateBroken,
		StateRecovering,
	}
}

// IsValid returns true if s is one of the six defined states.
func IsValid(s State) bool {
	for _, v := range All() {
		if v == s {
			return true
		}
	}
	return false
}

// String implements fmt.Stringer.
func (s State) String() string {
	return string(s)
}

// IsOperational returns true for states that are reachable through normal
// control-plane operation. UNKNOWN is not a state (it is a classification
// failure) and is therefore not returned here.
func (s State) IsOperational() bool {
	return IsValid(s)
}

// RequiresActiveFault returns true for states that MUST have a non-null
// active_fault in state.json. Invariant I-2 from system-state-model §5.2.
func (s State) RequiresActiveFault() bool {
	return s == StateDegraded
}

// ForbidsActiveFault returns true for states that MUST have a null
// active_fault in state.json. All states except DEGRADED.
func (s State) ForbidsActiveFault() bool {
	return s != StateDegraded
}

// CanApplyFault returns true if fault injection is permitted from this state.
// Defined in system-state-model §3.4 (forbidden transitions).
func (s State) CanApplyFault(force bool) bool {
	if force {
		// --force bypasses the guard but the result is classified as ambiguous.
		return s != StateRecovering
	}
	return s == StateConformant
}

// CanReset returns true if lab reset is permitted from this state.
func (s State) CanReset() bool {
	// Reset is permitted from any state — it always converges toward CONFORMANT.
	return true
}

// ErrInvalidState is returned when a state value is not one of the six defined states.
type ErrInvalidState struct {
	Value string
}

func (e ErrInvalidState) Error() string {
	return fmt.Sprintf("invalid state %q: must be one of UNPROVISIONED, PROVISIONED, CONFORMANT, DEGRADED, BROKEN, RECOVERING", e.Value)
}