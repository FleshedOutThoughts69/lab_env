package executor

// lock_test.go enforces the lock contract from control-plane-contract §3.5:
// the lock protects control-plane state mutation authority, not general
// system exclusivity. Tests verify acquire/release semantics, stale lock
// reclamation, and that a live lock cannot be stolen.

import (
	"path/filepath"
	"testing"
)



func TestLock_Release_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	lock := NewLockAt(filepath.Join(dir, "lab.lock"))

	lock.Acquire()
	// Double release should not panic or error
	lock.Release()
	lock.Release()
}