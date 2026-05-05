package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "lab_env/internal/state"
)

// ── Atomic write ──────────────────────────────────────────────────────────────

func TestStore_Write_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "state.json"))

	f := Fresh(StateConformant)
	if err := store.Write(f); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatalf("state.json not created: %v", err)
	}
}

func TestStore_Write_NoTempFilesLeft(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "state.json"))

	if err := store.Write(Fresh(StateConformant)); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("unexpected file after atomic write: %s", e.Name())
		}
	}
}

func TestStore_Write_SetsSpecVersion(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "state.json"))

	f := Fresh(StateConformant)
	f.SpecVersion = "" // intentionally clear
	if err := store.Write(f); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	f2, err := store.Read()
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if f2.SpecVersion != SpecVersion {
		t.Errorf("SpecVersion = %q, want %q", f2.SpecVersion, SpecVersion)
	}
}

// ── Schema round-trip ─────────────────────────────────────────────────────────

func TestStore_RoundTrip_AllFields(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "state.json"))

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	f := &File{
		State:               StateDegraded,
		ClassificationValid: true,
		ActiveFault: &ActiveFault{
			ID:        "F-004",
			AppliedAt: now,
			Forced:    true,
		},
		LastValidate: &ValidateRecord{
			At:            now,
			Passed:        21,
			Total:         23,
			FailingChecks: []string{"E-002", "F-004"},
		},
		LastReset: &ResetRecord{
			At:           now,
			Tier:         "R2",
			FromState:    StateConformant,
			FaultCleared: "F-001",
		},
		LastProvision: &ProvisionRecord{At: now, Result: "CONFORMANT"},
		History: []HistoryEntry{
			{Ts: now, From: StateConformant, To: StateDegraded, Command: "lab fault apply F-004", Fault: "F-004"},
		},
	}

	if err := store.Write(f); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	f2, err := store.Read()
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	if f2.State != StateDegraded {
		t.Errorf("State = %q, want DEGRADED", f2.State)
	}
	if f2.ActiveFault == nil {
		t.Fatal("ActiveFault should not be nil")
	}
	if f2.ActiveFault.ID != "F-004" {
		t.Errorf("ActiveFault.ID = %q, want F-004", f2.ActiveFault.ID)
	}
	if !f2.ActiveFault.Forced {
		t.Error("ActiveFault.Forced should be true")
	}
	if f2.LastValidate == nil || f2.LastValidate.Passed != 21 {
		t.Errorf("LastValidate mismatch: %+v", f2.LastValidate)
	}
	if len(f2.History) != 1 || f2.History[0].Fault != "F-004" {
		t.Errorf("History mismatch: %+v", f2.History)
	}
}

// ── Invariant I-2: active_fault null iff not DEGRADED ─────────────────────────

func TestStore_ActiveFaultNullWhenNotDegraded(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "state.json"))

	f := Fresh(StateConformant)
	f.ActiveFault = nil // must be null for non-DEGRADED

	if err := store.Write(f); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	// active_fault must be present as null, not absent
	_, present := raw["active_fault"]
	if !present {
		t.Error("active_fault field must be present in JSON even when null")
	}
	if raw["active_fault"] != nil {
		t.Errorf("active_fault = %v, want null for CONFORMANT state", raw["active_fault"])
	}
}

// ── Corruption recovery ───────────────────────────────────────────────────────

func TestStore_Read_MissingFile_ReturnsNotFound(t *testing.T) {
	store := NewStoreAt("/tmp/does-not-exist-lab-state.json")
	_, err := store.Read()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !StateFileError(err) {
		t.Errorf("expected StateFileError, got: %T %v", err, err)
	}
}

func TestStore_Read_CorruptJSON_ReturnsCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte("{not valid json"), 0644)

	store := NewStoreAt(path)
	_, err := store.Read()
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if !StateFileError(err) {
		t.Errorf("expected StateFileError, got: %T %v", err, err)
	}
}

func TestStore_Read_InvalidState_ReturnsCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte(`{"spec_version":"1.0.0","state":"NOTASTATE","classification_valid":true}`), 0644)

	store := NewStoreAt(path)
	_, err := store.Read()
	if err == nil {
		t.Fatal("expected error for invalid state value")
	}
	if !StateFileError(err) {
		t.Errorf("expected StateFileError, got: %T %v", err, err)
	}
}

// ── History ring buffer ───────────────────────────────────────────────────────

func TestStore_AppendHistory_RingBuffer(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "state.json"))

	f := Fresh(StateConformant)

	// Add HistoryMaxEntries + 5 entries
	for i := 0; i < HistoryMaxEntries+5; i++ {
		store.AppendHistory(HistoryEntry{
			Ts:      time.Now().UTC(),
			From:    StateConformant,
			To:      StateDegraded,
			Command: "lab fault apply F-001",
		}, f)
	}

	if len(f.History) != HistoryMaxEntries {
		t.Errorf("History length = %d, want %d (ring buffer limit)", len(f.History), HistoryMaxEntries)
	}
}

// ── InvalidateClassification ──────────────────────────────────────────────────

func TestStore_InvalidateClassification(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "state.json"))

	f := Fresh(StateConformant)
	f.ClassificationValid = true
	if err := store.Write(f); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if err := store.InvalidateClassification(); err != nil {
		t.Fatalf("InvalidateClassification error: %v", err)
	}

	f2, err := store.Read()
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if f2.ClassificationValid {
		t.Error("ClassificationValid should be false after invalidation")
	}
	// State must NOT be changed to BROKEN — only classification_valid is set
	if f2.State != StateConformant {
		t.Errorf("State = %q, want CONFORMANT — InvalidateClassification must not change state", f2.State)
	}
}

// ── Fresh ─────────────────────────────────────────────────────────────────────

func TestFresh_Defaults(t *testing.T) {
	f := Fresh(StateProvisioned)
	if f.State != StateProvisioned {
		t.Errorf("State = %q, want PROVISIONED", f.State)
	}
	if !f.ClassificationValid {
		t.Error("ClassificationValid should default to true")
	}
	if f.ActiveFault != nil {
		t.Error("ActiveFault should be nil for fresh state")
	}
	if f.History == nil {
		t.Error("History should be initialized (not nil)")
	}
	if f.SpecVersion != SpecVersion {
		t.Errorf("SpecVersion = %q, want %q", f.SpecVersion, SpecVersion)
	}
}