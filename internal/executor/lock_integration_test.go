//go:build integration

package executor

import (
	"os"
	"path/filepath"
	"testing"
	"strconv"
	"fmt"
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


// TestLock_StaleLockWithForeignPID_IsReclaimed verifies that a lock file
// containing the PID of a running system process (not a lab CLI) is treated
// as stale and can be reclaimed.
func TestLock_StaleLockWithForeignPID_IsReclaimed(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")

	// Write a lock file with PID 1 (always a live system process)
	if err := os.WriteFile(lockPath, []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Attempt to acquire the lock — should succeed because PID 1 is not
	// a lab CLI instance
	lock := NewLock()
	if err := lock.Acquire(); err != nil {
		t.Errorf("lock.Acquire() with foreign PID 1: got error %v\n"+
			"This means a PID collision with a system process would permanently block lab operations.\n"+
			"The lock implementation must distinguish between foreign and lab-CLI PIDs, or\n"+
			"always treat any PID > current as stale if the process name does not match.", err)
		return
	}
	defer lock.Release()

	// Verify we now hold the lock
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	ourPID := fmt.Sprintf("%d\n", os.Getpid())
	if string(data) != ourPID {
		t.Errorf("lock file after acquisition: got %q, want %q (our PID)", string(data), ourPID)
	}
}
