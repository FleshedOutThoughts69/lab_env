package state

// store_edge_cases_test.go
//
// Tests state store behavior under edge cases not covered by store_test.go:
//   - 0-byte state file treated as corruption (not "missing")
//   - concurrent InvalidateClassification + Write race safety

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestStore_Read_EmptyFile_ReturnsCorrupt verifies that a 0-byte state file
// is treated as ErrStateFileCorrupt, not ErrStateFileNotFound.
//
// A full disk or crash mid-write can leave a 0-byte file. This is distinct
// from a missing file: the file exists but contains no parseable JSON.
// The store must not silently succeed with zero-value defaults — it must
// signal corruption so the caller can re-derive state from runtime evidence.
func TestStore_Read_EmptyFile_ReturnsCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Create a 0-byte file — simulates a truncated write or disk-full crash
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewStoreAt(path)
	_, err := store.Read()
	if err == nil {
		t.Fatal("Read() on 0-byte file: expected error, got nil")
	}
	var corruptErr ErrStateFileCorrupt
	if !errors.As(err, &corruptErr) {
		t.Errorf("error type: got %T (%v), want ErrStateFileCorrupt", err, err)
	}
}

// TestStore_Read_WhitespaceOnly_ReturnsCorrupt verifies that a file
// containing only whitespace (e.g., newline) is also treated as corrupt.
func TestStore_Read_WhitespaceOnly_ReturnsCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := os.WriteFile(path, []byte("\n\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewStoreAt(path)
	_, err := store.Read()
	var corruptErr ErrStateFileCorrupt
	if !errors.As(err, &corruptErr) {
		t.Errorf("whitespace-only file: got %T (%v), want ErrStateFileCorrupt", err, err)
	}
}

// TestStore_Concurrent_InvalidateAndSave verifies that concurrent calls to
// InvalidateClassification and Write do not produce a corrupt state file
// or a state where classification_valid is incorrectly true after invalidation.
//
// Scenario: lab status is running (reads + writes) while a SIGINT arrives
// (invalidates). The final state must have classification_valid = false,
// not stale true from a Write that raced with invalidation.
//
// This test is a data-race detector test: run with -race to detect races.
// Even without -race, it verifies the final semantic state.
func TestStore_Concurrent_InvalidateAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	store := NewStoreAt(path)

	// Initialize with a valid conformant state
	initial := Fresh(StateConformant)
	initial.ClassificationValid = true
	if err := store.Write(initial); err != nil {
		t.Fatalf("initial Write: %v", err)
	}

	const iterations = 100
	var wg sync.WaitGroup

	// Goroutine 1: repeatedly call InvalidateClassification (interrupt simulator)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := store.InvalidateClassification(); err != nil {
				t.Errorf("InvalidateClassification: %v", err)
			}
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 2: repeatedly write classification_valid=true (status simulator)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			f, err := store.Read()
			if err != nil {
				var corruptErr ErrStateFileCorrupt
				var notFoundErr ErrStateFileNotFound
				if !errors.As(err, &corruptErr) && !errors.As(err, &notFoundErr) {
					t.Errorf("Read: %v", err)
				}
				continue
			}
			if f == nil {
				f = Fresh(StateConformant)
			}
			f.State = StateConformant
			f.ClassificationValid = true
			if err := store.Write(f); err != nil {
				t.Errorf("Write: %v", err)
			}
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()

	// After all operations, call InvalidateClassification one final time
	// and verify the resulting state file has classification_valid = false
	if err := store.InvalidateClassification(); err != nil {
		t.Fatalf("final InvalidateClassification: %v", err)
	}

	final, err := store.Read()
	if err != nil {
		t.Fatalf("final Read: %v", err)
	}
	if final.ClassificationValid {
		t.Error("final state has ClassificationValid=true after InvalidateClassification; expected false")
	}
}

// TestStore_Save_ReadOnlyDir_ReturnsError verifies that Write returns an error
// when the underlying write fails, and the original state file is not corrupted.
//
// Simulated by making the directory read-only.
func TestStore_Save_ReadOnlyDir_ReturnsError(t *testing.T) {
	dir := t.TempDir()

	// Write an initial valid state
	path := filepath.Join(dir, "state.json")
	store := NewStoreAt(path)
	initial := Fresh(StateConformant)
	if err := store.Write(initial); err != nil {
		t.Fatalf("initial Write: %v", err)
	}

	// Make the directory read-only — temp file creation will fail
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755)

	// Attempt to write a new state
	updated := Fresh(StateBroken)
	err := store.Write(updated)
	if err == nil {
		t.Fatal("Write to read-only dir: expected error, got nil")
	}

	// Original file must be unchanged (still conformant)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	readBack, err := store.Read()
	if err != nil {
		t.Fatalf("Read after failed Write: %v", err)
	}
	if readBack.State != StateConformant {
		t.Errorf("state after failed Write: got %v, want CONFORMANT", readBack.State)
	}
}