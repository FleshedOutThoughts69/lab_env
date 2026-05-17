// Package signals manages the service's signal files under /run/app/.
//
// Signal files are the service's self-reporting interface to the control plane.
// Application Runtime Contract §3.
//
// File vocabulary:
//
//	/run/app/app.pid    — decimal PID; written before accepting requests
//	/run/app/loading    — empty file; present during initialization
//	/run/app/healthy    — empty file; present when service is ready
//	/run/app/status     — single word: Starting|Running|Degraded|Unhealthy|ShuttingDown
//
// Status file vocabulary (Application Runtime Contract §3.4):
//
//	Starting     — startup sequence in progress
//	Running      — fully initialized, all checks passed
//	Degraded     — serving but with reduced functionality (chaos active)
//	Unhealthy    — state directory write fails; / returns 500
//	ShuttingDown — SIGTERM received, graceful shutdown in progress
//
// IMPORTANT: these are NOT canonical state names. The control plane maps them
// to canonical states (CONFORMANT, DEGRADED, BROKEN, etc.). The service must
// never use canonical state names in signal files per Runtime Contract §2.
//
// Startup sequence (must follow this exact order):
//  1. Remove stale /run/app/loading from previous crash (if present)
//  2. Write status = Starting
//  3. Create /run/app/loading
//  4. Write /run/app/app.pid
//  5. Parse config, open log, bind socket
//  6. Optional: probe /var/lib/app writability
//  7. Create /run/app/healthy
//  8. Delete /run/app/loading
//  9. Write status = Running
// 10. Begin accepting requests
//
// Shutdown sequence:
//  1. Write status = ShuttingDown
//  2. Remove /run/app/healthy
//  3. Call http.Server.Shutdown (drains in-flight requests)
//  4. Remove /run/app/app.pid
//  5. Exit 0
//
// Unhealthy is set only when the state directory write fails.
// Degraded is set only when any chaos mode is active.
// This distinction is intentional — see comment in SetStatus.
package signals

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const (
	// Dir is the signal file directory. Must be a tmpfs owned by appuser:appuser 755.
	// Application Runtime Contract §3.
	Dir = "/run/app"

	// PIDFile is the path of the PID file.
	PIDFile = Dir + "/app.pid"

	// HealthyFile is the path of the healthy marker.
	HealthyFile = Dir + "/healthy"

	// LoadingFile is the path of the loading marker.
	LoadingFile = Dir + "/loading"

	// StatusFile is the path of the status string file.
	StatusFile = Dir + "/status"

	// StatusStarting is written at the beginning of the startup sequence.
	StatusStarting = "Starting"

	// StatusRunning is written when the service is fully ready.
	StatusRunning = "Running"

	// StatusDegraded is written when any chaos mode is active.
	// Degraded means: serving, but with reduced functionality.
	// Distinct from Unhealthy — see package doc.
	StatusDegraded = "Degraded"

	// StatusUnhealthy is written when the state directory write fails.
	// Unhealthy means: running but GET / returns 500.
	// Distinct from Degraded — see package doc.
	StatusUnhealthy = "Unhealthy"

	// StatusShuttingDown is written when SIGTERM is received.
	StatusShuttingDown = "ShuttingDown"
)

// Init prepares the signal directory for a fresh startup.
// It removes any stale files from a previous crash and writes the initial status.
// Must be called before any other signal file operation.
func Init() error {
	// Remove stale loading file from previous crash.
	// If the process crashed after writing loading but before deleting it,
	// the control plane would see both healthy absent and loading present —
	// which it interprets as RECOVERING. Cleaning it here ensures a fresh start.
	_ = os.Remove(LoadingFile)

	// Remove stale healthy marker.
	_ = os.Remove(HealthyFile)

	// Write initial status.
	return SetStatus(StatusStarting)
}

// CreateLoading creates the loading marker file.
// Called immediately after Init, before any initialization.
func CreateLoading() error {
	return writeEmpty(LoadingFile)
}

// WritePID writes the current process PID to the PID file.
// Application Runtime Contract §3.1: written before accepting requests.
func WritePID() error {
	return writeAtomic(PIDFile, []byte(strconv.Itoa(os.Getpid())+"\n"))
}

// CreateHealthy creates the healthy marker file.
// Called only after all initialization passes and socket is bound.
// Application Runtime Contract §3.2.
func CreateHealthy() error {
	return writeEmpty(HealthyFile)
}

// RemoveLoading removes the loading marker.
// Called immediately after CreateHealthy — the two must not coexist.
func RemoveLoading() error {
	err := os.Remove(LoadingFile)
	if os.IsNotExist(err) {
		return nil // idempotent
	}
	return err
}

// SetStatus atomically writes the status string to StatusFile.
// Application Runtime Contract §3.4: must be written atomically (write-temp-rename).
//
// Valid values: StatusStarting, StatusRunning, StatusDegraded,
// StatusUnhealthy, StatusShuttingDown.
//
// Unhealthy is set only when the state directory write fails.
// Degraded is set only when any chaos mode is active.
// These are mutually exclusive in normal operation; if both conditions exist
// simultaneously, Unhealthy takes precedence (service is not serving correctly).
func SetStatus(status string) error {
	return writeAtomic(StatusFile, []byte(status+"\n"))
}

// BeginShutdown writes ShuttingDown status then removes the healthy marker.
// Order is critical: status is written FIRST so the control plane sees
// ShuttingDown before healthy disappears. If healthy were removed first,
// the control plane would briefly see healthy=absent with status=Running —
// a false crash signal. With status-first ordering, the transition is:
//   status=Running,  healthy=present  → service up
//   status=ShuttingDown, healthy=present → shutdown in progress (brief window)
//   status=ShuttingDown, healthy=absent → shutdown confirmed
// The brief window with both set is benign; the final state is unambiguous.
func BeginShutdown() {
	_ = SetStatus(StatusShuttingDown)
	_ = os.Remove(HealthyFile)
}

// RemovePID removes the PID file.
// Called as the last step of the shutdown sequence, after http.Server.Shutdown returns.
func RemovePID() {
	_ = os.Remove(PIDFile)
}

// writeEmpty writes an empty file atomically.
func writeEmpty(path string) error {
	return writeAtomic(path, []byte{})
}

// writeAtomic writes data to path using a temp-file + rename pattern.
// This is the required write mechanism for all signal files and telemetry:
// it prevents partial reads by the control plane during writes.
// The temp file is created in the same directory as the target to ensure
// the rename is atomic (same filesystem).
//
// All signal files are mode 0644: world-readable so the control plane
// (which may run as a different user) can read them without privilege.
// Application Runtime Contract §3: all files under /run/app/ are mode 644.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp file for %s: %w", path, err)
	}
	// Set mode before rename so the file is never visible with wrong permissions.
	// os.CreateTemp creates with 0600; we need 0644 for control plane readability.
	if err := os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file to %s: %w", path, err)
	}
	return nil
}