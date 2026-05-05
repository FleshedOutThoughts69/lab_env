// Package catalog implements the fault catalog defined in fault-model.md §7.
// Each fault is a deterministic, documented mutation operator over the
// environment state machine.
//
// The catalog uses a two-type model:
//   - FaultDef: pure static metadata — serializable, inspectable, no behavior
//   - FaultImpl: adds Apply and Recover functions to a FaultDef
//
// lab fault list and lab fault info operate on FaultDef only.
// lab fault apply uses FaultImpl.
// This split makes the catalog introspectable without executor dependencies.
package catalog

import (
	"lab_env/lab/internal/executor"
	"lab_env/lab/internal/state"
)

// FaultDef is the static, serializable definition of a fault.
// It contains all metadata needed for display, documentation, and
// postcondition verification. It carries no executable behavior.
// Defined in fault-model.md §3.1.
type FaultDef struct {
	// Identity
	ID     string   `json:"id"`
	Layer  string   `json:"layer"`
	Domain []string `json:"domain"`

	// Execution model metadata
	RequiresConfirmation bool   `json:"requires_confirmation"`
	IsReversible         bool   `json:"is_reversible"`
	IsBaselineBehavior   bool   `json:"is_baseline_behavior"`
	ResetTier            string `json:"reset_tier"` // empty for baseline behaviors

	// Preconditions
	Preconditions []state.State `json:"preconditions"`

	// Postcondition
	Postcondition PostconditionSpec `json:"postcondition"`

	// Human-readable content (used by lab fault info)
	Symptom             string `json:"symptom"`
	AuthoritativeSignal string `json:"authoritative_signal"`
	Observable          string `json:"observable"`
	MutationDisplay     string `json:"mutation"`
	ResetAction         string `json:"reset_action"`
}

// PostconditionSpec defines the exact conformance state after a fault is applied.
// Defined in fault-model.md §3.2.
type PostconditionSpec struct {
	Behavioral    string   `json:"behavioral"`
	FailingChecks []string `json:"failing_checks"`
	PassingChecks []string `json:"passing_checks"`
}

// FaultImpl pairs a FaultDef with executable Apply and Recover functions.
// Only commands that perform mutations (lab fault apply, lab reset) need FaultImpl.
// All introspection and display commands use FaultDef only.
//
// FaultImpl uses a Def *FaultDef pointer (not embedding) so that fault constructors
// can use Go composite literals naturally without the embedded-struct field promotion
// restriction.
type FaultImpl struct {
	// Def holds all static metadata. Access via f.Def.ID, f.Def.Layer, etc.
	Def *FaultDef

	// Apply executes the fault mutation through the Executor.
	// MUST route all system operations through the Executor.
	// Returns nil on success. If Apply returns an error, the state file
	// MUST NOT be updated to DEGRADED.
	Apply func(exec executor.Executor) error

	// Recover reverses the Apply mutation through the Executor.
	// MUST be idempotent.
	// For non-reversible faults (IsReversible: false), MUST return an error
	// directing the operator to R3 reset without attempting partial recovery.
	Recover func(exec executor.Executor) error
}