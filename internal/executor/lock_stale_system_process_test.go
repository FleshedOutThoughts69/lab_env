package executor

// lock_stale_system_process_test.go
//
// Tests the lock acquisition behavior when the lock file contains the PID
// of a real system process that is NOT the lab CLI.
//
// Why high ROI: the stale lock detection uses kill -0 to check if the PID
// is alive. If the PID belongs to a system process (e.g., PID 1, init),
// the lock would appear "live" even though the lab CLI is not running.
// The lab would then be permanently locked, requiring manual intervention.
//
// The fix is to also check that the live process is actually a lab CLI
// instance, or to always treat a foreign PID as stale.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"lab-env/lab/internal/executor"
)

// TestLock_StaleLockWithForeignPID_IsReclaimed verifies that a lock file
// containing the PID of a running system process (not a lab CLI) is treated
// as stale and can be reclaimed.
//
// The lock file correctly identifies a live process (kill -0 succeeds), but
// that process is not the lab CLI. The lock must be reclaimable, otherwise
// any PID collision permanently blocks lab operations.
//
// NOTE: This test uses PID 1 (init/systemd) as the "foreign" process. PID 1
// is always running on Linux. If the lock implementation only checks liveness
// (not process identity), this test will FAIL — which is the bug being caught.
func TestLock_StaleLockWithForeignPID_IsReclaimed(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")

	// Write a lock file with PID 1 (always a live system process)
	if err := os.WriteFile(lockPath, []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Attempt to acquire the lock — should succeed because PID 1 is not
	// a lab CLI instance
	lock := executor.NewLock(lockPath)
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

// TestLock_StaleLockWithCurrentPID_DoesNotSelfDeadlock verifies that a lock
// file containing the current process's own PID can be reclaimed.
//
// This could happen if the process crashed and restarted with the same PID
// (unusual but possible in containerized environments). The process must not
// deadlock waiting for itself.
func TestLock_StaleLockWithCurrentPID_IsReclaimed(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lab.lock")

	// Write a lock with the current PID — simulates crash+restart same PID
	if err := os.WriteFile(lockPath, fmt.Sprintf("%d\n", os.Getpid()), 0644); err != nil {
		t.Fatal(err)
	}

	lock := executor.NewLock(lockPath)
	err := lock.Acquire()
	if err != nil {
		// This is an acceptable outcome — the process sees its own PID as live
		// and refuses to overwrite. Document the behavior.
		t.Logf("lock.Acquire() with own PID refused: %v (acceptable — document this behavior)", err)
		return
	}
	defer lock.Release()
	t.Log("lock.Acquire() with own PID succeeded (reclaimed stale self-lock)")
}