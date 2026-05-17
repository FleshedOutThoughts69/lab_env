package catalog_test

// catalog_test.go validates the fault catalog against the completeness
// conditions from fault-model.md §8 and the FaultDef/FaultImpl split
// contract from the boundary audit.

import (
	"testing"

	"lab-env/lab/internal/conformance"
	. "lab-env/lab/internal/catalog"
	"lab-env/lab/internal/state"
)

// ── Catalog completeness ──────────────────────────────────────────────────────

func TestAllImpls_Has16Faults(t *testing.T) {
	faults := AllImpls()
	if len(faults) != 16 {
		t.Errorf("AllImpls() has %d faults, want 16", len(faults))
	}
}

func TestAllDefs_Has16Defs(t *testing.T) {
	defs := AllDefs()
	if len(defs) != 16 {
		t.Errorf("AllDefs() has %d defs, want 16", len(defs))
	}
}

func TestFaultIDs_AreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range AllImpls() {
		if seen[f.Def.ID] {
			t.Errorf("duplicate fault ID: %s", f.Def.ID)
		}
		seen[f.Def.ID] = true
	}
}

func TestFaultIDs_SequentialWithGap(t *testing.T) {
	// F-011 and F-012 are baseline network behaviours documented in
	// fault-model.md §10. They are not faults and are not in this catalog.
	// The catalog jumps from F-010 to F-013.
	faults := AllImpls()
	expected := []string{
		"F-001", "F-002", "F-003", "F-004", "F-005", "F-006",
		"F-007", "F-008", "F-009", "F-010",
		"F-013", "F-014", "F-015", "F-016", "F-017", "F-018",
	}
	if len(faults) != len(expected) {
		t.Fatalf("AllImpls() has %d faults, want %d", len(faults), len(expected))
	}
	for i, f := range faults {
		if f.Def.ID != expected[i] {
			t.Errorf("fault[%d].ID = %q, want %q", i, f.Def.ID, expected[i])
		}
	}
}

func TestFaultIDs_F011AndF012_NotInCatalog(t *testing.T) {
	// F-011 and F-012 were reclassified as baseline network behaviours
	// (fault-model.md §10) and removed from the fault catalog.
	// ImplByID must return nil for both.
	for _, id := range []string{"F-011", "F-012"} {
		if ImplByID(id) != nil {
			t.Errorf("ImplByID(%s) should return nil — not a fault", id)
		}
		if DefByID(id) != nil {
			t.Errorf("DefByID(%s) should return nil — not a fault", id)
		}
	}
}

// ── FaultDef completeness: every def must have required fields ────────────────

func TestAllDefs_RequiredFieldsPresent(t *testing.T) {
	for _, f := range AllDefs() {
		if f.ID == "" {
			t.Errorf("fault has empty ID")
		}
		if f.Layer == "" {
			t.Errorf("fault %s has empty Layer", f.ID)
		}
		if len(f.Domain) == 0 {
			t.Errorf("fault %s has empty Domain", f.ID)
		}
		if f.Symptom == "" {
			t.Errorf("fault %s has empty Symptom", f.ID)
		}
		if f.Observable == "" {
			t.Errorf("fault %s has empty Observable", f.ID)
		}
		if f.MutationDisplay == "" {
			t.Errorf("fault %s has empty MutationDisplay", f.ID)
		}
		if f.AuthoritativeSignal == "" {
			t.Errorf("fault %s has empty AuthoritativeSignal", f.ID)
		}
	}
}

// ── FaultImpl completeness: every impl must have Apply and Recover ────────────

func TestAllImpls_HaveApplyAndRecover(t *testing.T) {
	for _, f := range AllImpls() {
		if f.Apply == nil {
			t.Errorf("fault %s has nil Apply", f.Def.ID)
		}
		if f.Recover == nil {
			t.Errorf("fault %s has nil Recover", f.Def.ID)
		}
	}
}

// ── Preconditions ─────────────────────────────────────────────────────────────

func TestAllFaults_HavePreconditions(t *testing.T) {
	for _, f := range AllDefs() {
		if len(f.Preconditions) == 0 {
			t.Errorf("fault %s has no Preconditions", f.ID)
		}
	}
}

func TestNonBaseline_StandardPrecondition(t *testing.T) {
	// All faults must require CONFORMANT as a precondition
	for _, f := range AllDefs() {
		hasConformant := false
		for _, p := range f.Preconditions {
			if p == state.StateConformant {
				hasConformant = true
				break
			}
		}
		if !hasConformant {
			t.Errorf("fault %s does not require CONFORMANT precondition", f.ID)
		}
	}
}

// ── Non-reversible faults ─────────────────────────────────────────────────────

func TestNonReversibleFaults_AreF008AndF014(t *testing.T) {
	nonReversibleIDs := map[string]bool{}
	for _, f := range AllDefs() {
		if !f.IsReversible {
			nonReversibleIDs[f.ID] = true
		}
	}

	if !nonReversibleIDs["F-008"] {
		t.Error("F-008 should be non-reversible")
	}
	if !nonReversibleIDs["F-014"] {
		t.Error("F-014 should be non-reversible")
	}
	if len(nonReversibleIDs) != 2 {
		t.Errorf("expected exactly 2 non-reversible faults, got: %v", nonReversibleIDs)
	}
}

func TestNonReversibleFaults_RequireConfirmation(t *testing.T) {
	// Non-reversible faults must require confirmation (binary rebuild)
	for _, f := range AllDefs() {
		if !f.IsReversible && !f.RequiresConfirmation {
			t.Errorf("non-reversible fault %s must require confirmation", f.ID)
		}
	}
}

func TestNonReversibleFaults_HaveR3ResetTier(t *testing.T) {
	for _, f := range AllDefs() {
		if !f.IsReversible && f.ResetTier != "R3" {
			t.Errorf("non-reversible fault %s must have R3 ResetTier, got %q", f.ID, f.ResetTier)
		}
	}
}

// ── Postcondition completeness ────────────────────────────────────────────────

func TestPostconditions_FailingChecksAreKnownIDs(t *testing.T) {
	// All check IDs in postcondition FailingChecks must exist in the conformance catalog
	knownChecks := map[string]bool{}
	for _, c := range conformance.Catalog() {
		knownChecks[c.ID] = true
	}

	for _, f := range AllDefs() {
		for _, checkID := range f.Postcondition.FailingChecks {
			if !knownChecks[checkID] {
				t.Errorf("fault %s postcondition references unknown check ID %q", f.ID, checkID)
			}
		}
		for _, checkID := range f.Postcondition.PassingChecks {
			if !knownChecks[checkID] {
				t.Errorf("fault %s passing checks references unknown check ID %q", f.ID, checkID)
			}
		}
	}
}

func TestNonBaseline_NonBaselineHasPostcondition(t *testing.T) {
	// All faults except F-008 and F-014 (which manifest at shutdown or over time)
	// must have at least one failing check or a behavioral description.
	exceptIDs := map[string]bool{"F-008": true, "F-014": true}

	for _, f := range AllDefs() {
		if exceptIDs[f.ID] {
			continue
		}
		if f.Postcondition.Behavioral == "" && len(f.Postcondition.FailingChecks) == 0 {
			t.Errorf("fault %s has no postcondition (behavioral or conformance)", f.ID)
		}
	}
}

// ── DefByID / ImplByID lookup ─────────────────────────────────────────────────

func TestDefByID_KnownFault(t *testing.T) {
	d := DefByID("F-004")
	if d == nil {
		t.Fatal("DefByID(F-004) returned nil")
	}
	if d.ID != "F-004" {
		t.Errorf("ID = %q, want F-004", d.ID)
	}
	// FaultDef must not have function fields — verified by type
	// (compile-time: FaultDef has no func fields)
}

func TestDefByID_UnknownFault(t *testing.T) {
	d := DefByID("F-999")
	if d != nil {
		t.Errorf("DefByID(F-999) should return nil, got %+v", d)
	}
}

func TestImplByID_KnownFault(t *testing.T) {
	f := ImplByID("F-001")
	if f == nil {
		t.Fatal("ImplByID(F-001) returned nil")
	}
	if f.Def == nil {
		t.Fatal("ImplByID(F-001).Def is nil")
	}
	if f.Def.ID != "F-001" {
		t.Errorf("Def.ID = %q, want F-001", f.Def.ID)
	}
	if f.Apply == nil {
		t.Error("ImplByID(F-001).Apply is nil")
	}
	if f.Recover == nil {
		t.Error("ImplByID(F-001).Recover is nil")
	}
}

func TestImplByID_UnknownFault(t *testing.T) {
	f := ImplByID("F-000")
	if f != nil {
		t.Errorf("ImplByID(F-000) should return nil, got %+v", f)
	}
}

// ── AllDefs returns copies (not aliasing) ─────────────────────────────────────

func TestAllDefs_ReturnsCopies(t *testing.T) {
	defs1 := AllDefs()
	defs2 := AllDefs()

	// Mutating a def from one call must not affect the other
	defs1[0].ID = "MUTATED"
	if defs2[0].ID == "MUTATED" {
		t.Error("AllDefs should return independent copies, not shared pointers")
	}
}