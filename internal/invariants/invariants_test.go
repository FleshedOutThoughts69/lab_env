package invariants_test

// invariants_test.go enforces cross-document invariants that span multiple
// packages. These tests make architectural rules explicit and catch
// desynchronization between the model documents and the implementation.
//
// Each test names the document and section it enforces.

import (
	"testing"

	"lab-env/lab/internal/catalog"
	"lab-env/lab/internal/conformance"
	"lab-env/lab/internal/state"
)

// ── fault-model.md × conformance-model.md ────────────────────────────────────

// TestInvariant_FaultFailingChecks_ExistInCatalog enforces the reverse
// direction of the bidirectional completeness condition:
// every check ID in fault postconditions must exist in the conformance catalog.
// fault-model.md §8 + conformance-model.md §5.
func TestInvariant_FaultFailingChecks_ExistInCatalog(t *testing.T) {
	knownChecks := map[string]bool{}
	for _, c := range conformance.Catalog() {
		knownChecks[c.ID] = true
	}

	for _, f := range catalog.AllDefs() {
		for _, checkID := range f.Postcondition.FailingChecks {
			if !knownChecks[checkID] {
				t.Errorf("fault %s postcondition references non-existent check %q", f.ID, checkID)
			}
		}
		for _, checkID := range f.Postcondition.PassingChecks {
			if !knownChecks[checkID] {
				t.Errorf("fault %s passing checks references non-existent check %q", f.ID, checkID)
			}
		}
	}
}

// TestInvariant_DegradedChecks_AreNonBlocking enforces that the four degraded
// checks (F-006, L-001, L-002, L-003) are all SeverityDegraded.
// conformance-model.md §3.1 and §4.5.
func TestInvariant_DegradedChecks_AreNonBlocking(t *testing.T) {
	degradedExpected := map[string]bool{
		"F-006": true, "L-001": true, "L-002": true, "L-003": true,
	}
	for _, c := range conformance.Catalog() {
		if degradedExpected[c.ID] {
			if c.Severity != conformance.SeverityDegraded {
				t.Errorf("check %s should be SeverityDegraded (conformance-model §3.1)", c.ID)
			}
		}
	}
}

// TestInvariant_NoBaselineFaultsInCatalog enforces that F-011 and F-012
// (reclassified as baseline network behaviours in fault-model.md §10)
// are absent from the fault catalog.
func TestInvariant_NoBaselineFaultsInCatalog(t *testing.T) {
	for _, f := range catalog.AllDefs() {
		if f.ID == "F-011" || f.ID == "F-012" {
			t.Errorf("fault %s must not be in the catalog — it is a baseline network behaviour (fault-model.md §10)", f.ID)
		}
	}
}

// TestInvariant_NonReversible_RequiresR3 enforces that non-reversible,
// non-baseline faults always require R3 reset tier.
// fault-model.md §6.3.
func TestInvariant_NonReversible_RequiresR3(t *testing.T) {
	for _, f := range catalog.AllDefs() {
		if !f.IsReversible && f.ResetTier != "R3" {
			t.Errorf("non-reversible fault %s must have ResetTier R3, got %q", f.ID, f.ResetTier)
		}
	}
}

// TestInvariant_NonReversible_RequiresConfirmation enforces that non-reversible
// faults always require operator confirmation.
// fault-model.md §4.4.
func TestInvariant_NonReversible_RequiresConfirmation(t *testing.T) {
	for _, f := range catalog.AllDefs() {
		if !f.IsReversible && !f.RequiresConfirmation {
			t.Errorf("non-reversible fault %s must require confirmation", f.ID)
		}
	}
}

// TestInvariant_AllFaults_HaveConformantPrecondition enforces the standard
// precondition from fault-model.md §5.1: every fault requires CONFORMANT state.
func TestInvariant_AllFaults_HaveConformantPrecondition(t *testing.T) {
	for _, f := range catalog.AllDefs() {
		found := false
		for _, p := range f.Preconditions {
			if p == state.StateConformant {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("fault %s must have CONFORMANT in Preconditions (fault-model §5.1)", f.ID)
		}
	}
}

// TestInvariant_PreconditionChecks_AreValidCheckIDs enforces that every check ID
// declared in a fault's PreconditionChecks field exists in the conformance catalog.
// An unknown check ID would cause cmd/fault.go to return an internal error at
// runtime rather than a clean precondition failure.
// fault-model.md §5.2; control-plane-contract.md §4.5 step 5.
func TestInvariant_PreconditionChecks_AreValidCheckIDs(t *testing.T) {
	for _, f := range catalog.AllDefs() {
		for _, checkID := range f.PreconditionChecks {
			if conformance.CheckByID(checkID) == nil {
				t.Errorf("fault %s declares PreconditionCheck %q which does not exist in the conformance catalog (fault-model §5.2)", f.ID, checkID)
			}
		}
	}
}

// TestInvariant_F010_HasPreconditionCheck_P001 enforces that F-010 specifically
// declares P-001 in its PreconditionChecks. The fault's teaching value depends
// on the app process holding the deleted inode open; applying it to a stopped
// service produces incorrect observable behaviour.
// fault-model.md §5.2.
func TestInvariant_F010_HasPreconditionCheck_P001(t *testing.T) {
	f := catalog.DefByID("F-010")
	if f == nil {
		t.Fatal("F-010 not found in catalog")
	}
	for _, checkID := range f.PreconditionChecks {
		if checkID == "P-001" {
			return
		}
	}
	t.Error("F-010 must have P-001 in PreconditionChecks (fault-model §5.2)")
}

// ── system-state-model.md × conformance-model.md ─────────────────────────────

// TestInvariant_ConformantDefinition enforces that CONFORMANT is defined by
// conformance checks, not by state values. The state model must not
// redefine conformance. system-state-model.md §2.3.
func TestInvariant_ConformantState_IsValid(t *testing.T) {
	if !state.IsValid(state.StateConformant) {
		t.Error("StateConformant must be a valid state")
	}
}

// TestInvariant_AllStates_AreValid ensures no state value is empty or unknown.
func TestInvariant_AllStates_AreValid(t *testing.T) {
	for _, s := range state.All() {
		if !state.IsValid(s) {
			t.Errorf("state.All() contains invalid state: %q", s)
		}
		if string(s) == "" {
			t.Error("state value must not be empty")
		}
	}
}

// TestInvariant_SixStates enforces the six-state model from
// system-state-model.md §2.
func TestInvariant_ExactlySixStates(t *testing.T) {
	all := state.All()
	if len(all) != 6 {
		t.Errorf("state.All() has %d states, want 6 (system-state-model §2)", len(all))
	}
}

// TestInvariant_StateDegraded_RequiresActiveFault enforces invariant I-2:
// if state is DEGRADED, active_fault must be non-null.
// system-state-model.md §5.2.
func TestInvariant_Degraded_RequiresActiveFault(t *testing.T) {
	if !state.StateDegraded.RequiresActiveFault() {
		t.Error("StateDegraded must require active fault (invariant I-2)")
	}
}

// TestInvariant_ConformantState_ForbidsActiveFault enforces invariant I-2
// in the other direction.
func TestInvariant_Conformant_ForbidsActiveFault(t *testing.T) {
	if !state.StateConformant.ForbidsActiveFault() {
		t.Error("StateConformant must forbid active fault (invariant I-2)")
	}
}

// ── fault-model.md × catalog count ───────────────────────────────────────────

// TestInvariant_16FaultsInCatalog enforces the catalog count from
// fault-model.md §7.2. F-011 and F-012 are baseline network behaviours
// (fault-model.md §10) and are not in the fault catalog.
func TestInvariant_16FaultsInCatalog(t *testing.T) {
	if got := len(catalog.AllDefs()); got != 16 {
		t.Errorf("fault catalog has %d faults, want 16 (fault-model §7.2)", got)
	}
}

// TestInvariant_23ChecksInConformanceCatalog enforces the check count from
// conformance-model.md §3.
func TestInvariant_23ChecksInConformanceCatalog(t *testing.T) {
	if got := len(conformance.Catalog()); got != 23 {
		t.Errorf("conformance catalog has %d checks, want 23 (conformance-model §3)", got)
	}
}

// TestInvariant_ResetTierValues enforces that all faults use only R1, R2, or R3.
// Empty ResetTier is no longer valid — baseline behaviours are not in the catalog.
func TestInvariant_ResetTierValues(t *testing.T) {
	valid := map[string]bool{"R1": true, "R2": true, "R3": true}
	for _, f := range catalog.AllDefs() {
		if !valid[f.ResetTier] {
			t.Errorf("fault %s has invalid ResetTier %q, must be R1, R2, or R3", f.ID, f.ResetTier)
		}
	}
}