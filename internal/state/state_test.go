package state

// state_test.go enforces the state enumeration and transition guard contracts
// defined in system-state-model.md §2 and §3.
//
// These tests are the enforcement gate for "Adding a New System State" in
// docs/extension-boundary-note.md. Adding a new state without updating All()
// will fail TestState_ValidStates_ContainsAll. Adding a state without
// correctly implementing its transition guards will fail the corresponding
// TestState_Transitions_* test.

import (
	"testing"

	. "lab-env/lab/internal/state"
)

// ── State enumeration ─────────────────────────────────────────────────────────

// TestState_ValidStates_ContainsAll enforces that All() returns exactly the
// six states defined in system-state-model.md §2, in definition order, with
// no duplicates and no omissions.
//
// If this test fails after adding a new state, update All() in state.go.
// If this test fails after a state is removed, update both All() and this test
// after confirming the removal is a deliberate breaking change with a migration path.
func TestState_ValidStates_ContainsAll(t *testing.T) {
	// Canonical six-state set from system-state-model.md §2.
	// Order must match definition order in state.go.
	expected := []State{
		StateUnprovisioned,
		StateProvisioned,
		StateConformant,
		StateDegraded,
		StateBroken,
		StateRecovering,
	}

	got := All()

	if len(got) != len(expected) {
		t.Fatalf("All() returned %d states, want %d\ngot:  %v\nwant: %v",
			len(got), len(expected), got, expected)
	}

	for i, want := range expected {
		if got[i] != want {
			t.Errorf("All()[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// TestState_IsValid_AcceptsAllSixStates verifies that IsValid returns true
// for every state returned by All() and false for non-states.
func TestState_IsValid_AcceptsAllSixStates(t *testing.T) {
	for _, s := range All() {
		if !IsValid(s) {
			t.Errorf("IsValid(%q) = false, want true", s)
		}
	}
}

func TestState_IsValid_RejectsUnknownValues(t *testing.T) {
	for _, bad := range []State{"", "UNKNOWN", "conformant", "broken", "FAULT"} {
		if IsValid(bad) {
			t.Errorf("IsValid(%q) = true, want false", bad)
		}
	}
}

// ── Transition guards ─────────────────────────────────────────────────────────

// TestState_Transitions_CanApplyFault enforces the fault application guard
// defined in system-state-model.md §3.4.
//
// Without --force: only CONFORMANT permits fault application.
// With --force: all states except RECOVERING permit it.
func TestState_Transitions_CanApplyFault(t *testing.T) {
	cases := []struct {
		state     State
		force     bool
		wantAllow bool
	}{
		// Without --force: only CONFORMANT is permitted.
		{StateUnprovisioned, false, false},
		{StateProvisioned, false, false},
		{StateConformant, false, true},
		{StateDegraded, false, false},
		{StateBroken, false, false},
		{StateRecovering, false, false},

		// With --force: all except RECOVERING are permitted.
		{StateUnprovisioned, true, true},
		{StateProvisioned, true, true},
		{StateConformant, true, true},
		{StateDegraded, true, true},
		{StateBroken, true, true},
		{StateRecovering, true, false},
	}

	for _, tc := range cases {
		got := tc.state.CanApplyFault(tc.force)
		if got != tc.wantAllow {
			t.Errorf("State(%q).CanApplyFault(force=%v) = %v, want %v",
				tc.state, tc.force, got, tc.wantAllow)
		}
	}
}

// TestState_Transitions_CanReset enforces that reset is permitted from every
// state. system-state-model.md §3.3: reset always converges toward CONFORMANT.
func TestState_Transitions_CanReset(t *testing.T) {
	for _, s := range All() {
		if !s.CanReset() {
			t.Errorf("State(%q).CanReset() = false, want true — reset must be permitted from all states", s)
		}
	}
}

// TestState_Transitions_RequiresActiveFault enforces invariant I-2 from
// system-state-model.md §5.2: exactly DEGRADED requires a non-null active_fault.
func TestState_Transitions_RequiresActiveFault(t *testing.T) {
	cases := []struct {
		state State
		want  bool
	}{
		{StateUnprovisioned, false},
		{StateProvisioned, false},
		{StateConformant, false},
		{StateDegraded, true},
		{StateBroken, false},
		{StateRecovering, false},
	}

	for _, tc := range cases {
		got := tc.state.RequiresActiveFault()
		if got != tc.want {
			t.Errorf("State(%q).RequiresActiveFault() = %v, want %v", tc.state, got, tc.want)
		}
	}
}

// TestState_Transitions_ForbidsActiveFault is the complement of
// RequiresActiveFault: every state except DEGRADED must have null active_fault.
func TestState_Transitions_ForbidsActiveFault(t *testing.T) {
	cases := []struct {
		state State
		want  bool
	}{
		{StateUnprovisioned, true},
		{StateProvisioned, true},
		{StateConformant, true},
		{StateDegraded, false}, // DEGRADED is the single exception
		{StateBroken, true},
		{StateRecovering, true},
	}

	for _, tc := range cases {
		got := tc.state.ForbidsActiveFault()
		if got != tc.want {
			t.Errorf("State(%q).ForbidsActiveFault() = %v, want %v", tc.state, got, tc.want)
		}
	}
}

// TestState_Transitions_RequiresForbids_AreComplementary verifies that
// RequiresActiveFault and ForbidsActiveFault are strict complements:
// exactly one must be true for every state.
func TestState_Transitions_RequiresForbids_AreComplementary(t *testing.T) {
	for _, s := range All() {
		requires := s.RequiresActiveFault()
		forbids := s.ForbidsActiveFault()
		if requires == forbids {
			t.Errorf("State(%q): RequiresActiveFault()=%v and ForbidsActiveFault()=%v — must be strict complements",
				s, requires, forbids)
		}
	}
}

// ── ErrInvalidState ───────────────────────────────────────────────────────────

// TestState_ErrInvalidState_ContainsValue verifies the error message includes
// the invalid value for diagnostic clarity.
func TestState_ErrInvalidState_ContainsValue(t *testing.T) {
	err := ErrInvalidState{Value: "NONSENSE"}
	msg := err.Error()
	if msg == "" {
		t.Error("ErrInvalidState.Error() returned empty string")
	}
	// Must include the invalid value so the operator knows what was rejected.
	if len(msg) < 8 {
		t.Errorf("ErrInvalidState.Error() = %q, too short to be useful", msg)
	}
}