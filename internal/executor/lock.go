package executor

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"lab_env/internal/config"
)

// Lock manages the exclusive advisory mutation lock at config.LockPath.
// Defined in control-plane-contract §3.5.
//
// The lock protects control-plane state mutation authority — specifically,
// state.json writes and the sequence of executor operations that constitute
// a transition. It does NOT guarantee system-level exclusivity outside
// control-plane operations.
type Lock struct {
	path string
	pid  int
	file *os.File
}

// NewLock returns a Lock for the canonical lock path.
func NewLock() *Lock {
	return &Lock{path: config.LockPath}
}

// NewLockAt returns a Lock for a custom path. Used in tests.
func NewLockAt(path string) *Lock {
	return &Lock{path: path}
}

// Acquire attempts to acquire the lock. Returns ErrLockHeld if another
// lab process holds the lock. Returns ErrLockStale if a stale lock is
// detected and cleared (acquisition then proceeds).
//
// The lock file contains the PID of the holding process.
// control-plane-contract §3.5: "the command MUST NOT wait for the lock."
func (l *Lock) Acquire() error {
	dir := config.LockPath[:strings.LastIndex(config.LockPath, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}

	// Try to create the lock file exclusively.
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		// Success — we hold the lock.
		l.pid = os.Getpid()
		l.file = f
		fmt.Fprintf(f, "%d\n", l.pid)
		return nil
	}

	if !os.IsExist(err) {
		return fmt.Errorf("acquiring lock: %w", err)
	}

	// Lock file exists — check if it's stale.
	data, readErr := os.ReadFile(l.path)
	if readErr != nil {
		// Can't read lock file — try to remove and reacquire.
		os.Remove(l.path)
		return l.Acquire()
	}

	pidStr := strings.TrimSpace(string(data))
	holderPID, parseErr := strconv.Atoi(pidStr)
	if parseErr != nil {
		// Malformed lock file — remove and reacquire.
		os.Remove(l.path)
		return l.Acquire()
	}

	// Check if the holding process is still running.
	if !processRunning(holderPID) {
		// Stale lock — remove and reacquire.
		os.Remove(l.path)
		return l.Acquire()
	}

	// Live process holds the lock.
	return ErrLockHeld{HolderPID: holderPID}
}

// Release releases the lock. Safe to call if the lock is not held.
func (l *Lock) Release() error {
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
	if l.pid == os.Getpid() {
		os.Remove(l.path)
		l.pid = 0
	}
	return nil
}

// Held returns true if this instance currently holds the lock.
func (l *Lock) Held() bool {
	return l.pid == os.Getpid() && l.file != nil
}

// processRunning returns true if a process with the given PID is running.
func processRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check existence.
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ErrLockHeld is returned when the mutation lock is held by another process.
// Defined in control-plane-contract §8 (error catalog).
type ErrLockHeld struct {
	HolderPID int
}

func (e ErrLockHeld) Error() string {
	return fmt.Sprintf("another lab operation is in progress (PID %d)", e.HolderPID)
}

// IsErrLockHeld returns true if err is ErrLockHeld.
func IsErrLockHeld(err error) bool {
	var e ErrLockHeld
	return errors.As(err, &e)
}