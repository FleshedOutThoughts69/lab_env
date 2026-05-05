package executor

// lock_test.go enforces the lock contract from control-plane-contract §3.5:
// the lock protects control-plane state mutation authority, not general
// system exclusivity. Tests verify acquire/release semantics, stale lock
// reclamation, and that a live lock cannot be stolen.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

)

func TestLock_Acquire_Succeeds_WhenAbsent(t *testing.T) {
	dir := t.TempDir()
	lock := NewLockAt(filepath.Join(dir, "lab.lock"))

	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire should succeed when lock is absent, got: %v", err)
	}
	defer lock.Release()

	if !lock.Held() {
		t.Error("Held() should be true after successful Acquire")
	}
}

func TestLock_Acquire_Fails_WhenHeldByLiveProcess(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")

	// Write a lock file with the current process's PID (guaranteed live)
	currentPID := os.Getpid()
	os.WriteFile(lockPath, []byte(strconv.Itoa(currentPID)+"\n"), 0600)

	lock := NewLockAt(lockPath)
	err := lock.Acquire()
	if err == nil {
		lock.Release()
		t.Fatal("Acquire should fail when lock is held by a live process")
	}
	if !IsErrLockHeld(err) {
		t.Errorf("expected ErrLockHeld, got: %T %v", err, err)
	}
}

func TestLock_Acquire_Reclaims_StaleLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")

	// Write a lock file with PID 99999999 — guaranteed not running
	os.WriteFile(lockPath, []byte("99999999\n"), 0600)

	lock := NewLockAt(lockPath)
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire should reclaim stale lock (dead PID), got: %v", err)
	}
	defer lock.Release()

	if !lock.Held() {
		t.Error("Held() should be true after reclaiming stale lock")
	}
}

func TestLock_Acquire_Reclaims_MalformedLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")

	// Malformed lock file (not a valid PID)
	os.WriteFile(lockPath, []byte("not-a-pid\n"), 0600)

	lock := NewLockAt(lockPath)
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire should reclaim malformed lock file, got: %v", err)
	}
	defer lock.Release()
}

func TestLock_Release_RemovesLockFile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")
	lock := NewLockAt(lockPath)

	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire error: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release error: %v", err)
	}

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file should be removed after Release")
	}
	if lock.Held() {
		t.Error("Held() should be false after Release")
	}
}

func TestLock_Release_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	lock := NewLockAt(filepath.Join(dir, "lab.lock"))

	lock.Acquire()
	// Double release should not panic or error
	lock.Release()
	lock.Release()
}

func TestLock_AcquireAfterRelease_Succeeds(t *testing.T) {
	dir := t.TempDir()
	lock := NewLockAt(filepath.Join(dir, "lab.lock"))

	if err := lock.Acquire(); err != nil {
		t.Fatalf("first Acquire error: %v", err)
	}
	lock.Release()

	if err := lock.Acquire(); err != nil {
		t.Fatalf("re-Acquire after Release error: %v", err)
	}
	defer lock.Release()
}

func TestLock_SecondInstance_Fails_WhileFirstHeld(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")

	lock1 := NewLockAt(lockPath)
	lock2 := NewLockAt(lockPath)

	if err := lock1.Acquire(); err != nil {
		t.Fatalf("lock1.Acquire error: %v", err)
	}
	defer lock1.Release()

	err := lock2.Acquire()
	if err == nil {
		lock2.Release()
		t.Fatal("lock2.Acquire should fail while lock1 holds the lock")
	}
	if !IsErrLockHeld(err) {
		t.Errorf("expected ErrLockHeld, got: %T %v", err, err)
	}
}

func TestErrLockHeld_ContainsPID(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")

	currentPID := os.Getpid()
	os.WriteFile(lockPath, []byte(strconv.Itoa(currentPID)+"\n"), 0600)

	lock := NewLockAt(lockPath)
	err := lock.Acquire()
	if err == nil {
		lock.Release()
		t.Fatal("expected ErrLockHeld")
	}

	errMsg := err.Error()
	pidStr := strconv.Itoa(currentPID)
	if !containsStr(errMsg, pidStr) {
		t.Errorf("ErrLockHeld message should contain PID %s, got: %q", pidStr, errMsg)
	}
}