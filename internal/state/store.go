package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lab_env/internal/config"
)

const (
	// HistoryMaxEntries is the ring buffer limit for the history field.
	// Defined in control-plane-contract §6.1.
	HistoryMaxEntries = 50

	// SpecVersion is the version of the control-plane-contract this binary implements.
	SpecVersion = "1.0.0"
)

// Path aliases — all paths come from internal/config, never hardcoded here.
// These vars allow store_test.go and other callers to reference paths
// without importing config directly.
var (
	StatePath = config.StatePath
	AuditPath = config.AuditPath
	LockPath  = config.LockPath
)

// File is the schema for /var/lib/lab/state.json.
// Defined in control-plane-contract §6.1.
type File struct {
	SpecVersion         string         `json:"spec_version"`
	State               State          `json:"state"`
	ClassificationValid bool           `json:"classification_valid"`
	ActiveFault         *ActiveFault   `json:"active_fault"`
	LastValidate        *ValidateRecord `json:"last_validate"`
	LastReset           *ResetRecord   `json:"last_reset"`
	LastProvision       *ProvisionRecord `json:"last_provision"`
	LastStatusAt        *time.Time     `json:"last_status_at"`
	History             []HistoryEntry `json:"history"`
}

// ActiveFault records the currently active fault. Non-nil only when
// State == StateDegraded. Invariant I-2 from system-state-model §5.2.
type ActiveFault struct {
	ID        string    `json:"id"`
	AppliedAt time.Time `json:"applied_at"`
	Forced    bool      `json:"forced"`
}

// ValidateRecord is the result of the most recent lab validate run.
type ValidateRecord struct {
	At             time.Time `json:"at"`
	Passed         int       `json:"passed"`
	Total          int       `json:"total"`
	FailingChecks  []string  `json:"failing_checks"`
}

// ResetRecord records the most recent lab reset operation.
type ResetRecord struct {
	At           time.Time `json:"at"`
	Tier         string    `json:"tier"`
	FromState    State     `json:"from_state"`
	FaultCleared string    `json:"fault_cleared,omitempty"`
}

// ProvisionRecord records the most recent lab provision operation.
type ProvisionRecord struct {
	At     time.Time `json:"at"`
	Result string    `json:"result"` // "CONFORMANT" or "BROKEN"
}

// HistoryEntry is one entry in the bounded state transition history ring.
type HistoryEntry struct {
	Ts      time.Time `json:"ts"`
	From    State     `json:"from"`
	To      State     `json:"to"`
	Command string    `json:"command"`
	Fault   string    `json:"fault,omitempty"`
	Forced  bool      `json:"forced,omitempty"`
}

// Store reads and writes state.json with atomic write semantics.
type Store struct {
	path string
}

// NewStore returns a Store that operates on the canonical state file path.
func NewStore() *Store {
	return &Store{path: StatePath}
}

// NewStoreAt returns a Store that operates on a custom path.
// Used in tests to avoid touching the real state file.
func NewStoreAt(path string) *Store {
	return &Store{path: path}
}

// Read reads and parses the state file.
// Returns ErrStateFileNotFound if the file does not exist.
// Returns ErrStateFileCorrupt if the file cannot be parsed.
func (s *Store) Read() (*File, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrStateFileNotFound{Path: s.path}
		}
		return nil, fmt.Errorf("reading state file %s: %w", s.path, err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, ErrStateFileCorrupt{Path: s.path, Cause: err}
	}

	if !IsValid(f.State) {
		return nil, ErrStateFileCorrupt{
			Path:  s.path,
			Cause: ErrInvalidState{Value: string(f.State)},
		}
	}

	return &f, nil
}

// Write atomically writes the state file. Uses temp file + rename to
// guarantee that readers either see the complete old content or the
// complete new content, never a partial write.
// Defined in control-plane-contract §6.2.
func (s *Store) Write(f *File) error {
	// Ensure classification_valid is always explicitly set.
	// New writes are always valid unless the caller explicitly sets false.

	f.SpecVersion = SpecVersion

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state directory %s: %w", dir, err)
	}

	// Write to a temp file in the same directory (same filesystem ensures
	// rename is atomic on POSIX systems).
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpPath := tmp.Name()

	// Clean up temp file on any failure path.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		return fmt.Errorf("setting temp state file permissions: %w", err)
	}

	// Atomic rename — either old or new exists, never partial.
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("renaming state file: %w", err)
	}

	success = true
	return nil
}

// InvalidateClassification marks the state file's classification as invalid,
// indicating that control-plane certainty has been lost (e.g., after an
// interrupted operation). Does NOT set state to BROKEN.
// Defined in control-plane-contract §3.6.
func (s *Store) InvalidateClassification() error {
	f, err := s.Read()
	if err != nil {
		// If we can't read the state file, we can't invalidate it either.
		// This is acceptable — the next status run will detect UNKNOWN.
		return fmt.Errorf("reading state file for invalidation: %w", err)
	}
	f.ClassificationValid = false
	return s.Write(f)
}

// AppendHistory adds a history entry to the ring buffer and writes the
// updated state file. Entries beyond HistoryMaxEntries are dropped FIFO.
func (s *Store) AppendHistory(entry HistoryEntry, f *File) {
	f.History = append(f.History, entry)
	if len(f.History) > HistoryMaxEntries {
		f.History = f.History[len(f.History)-HistoryMaxEntries:]
	}
}

// Fresh returns a new File with sensible zero values for initial write
// after provision.
func Fresh(st State) *File {
	now := time.Now().UTC()
	return &File{
		SpecVersion:         SpecVersion,
		State:               st,
		ClassificationValid: true,
		LastStatusAt:        &now,
		History:             []HistoryEntry{},
	}
}

// ErrStateFileNotFound is returned when state.json does not exist.
type ErrStateFileNotFound struct {
	Path string
}

func (e ErrStateFileNotFound) Error() string {
	return fmt.Sprintf("state file not found: %s", e.Path)
}

// ErrStateFileCorrupt is returned when state.json cannot be parsed.
type ErrStateFileCorrupt struct {
	Path  string
	Cause error
}

func (e ErrStateFileCorrupt) Error() string {
	return fmt.Sprintf("state file corrupt (%s): %v", e.Path, e.Cause)
}

func (e ErrStateFileCorrupt) Unwrap() error {
	return e.Cause
}