// Package executor implements the mutation layer defined in
// control-plane-contract §5. It provides two interfaces:
//
//   - Observer: read-only system inspection (no lock, no audit)
//   - Executor: mutation authority (lock required, audit required)
//
// The Observer interface is defined in internal/conformance/observer.go.
// The Executor interface here embeds Observer, giving mutation commands
// access to all read operations through a single dependency.
//
// All system mutations MUST route through the Executor. No command,
// fault, or reset operation may call os/exec or os system calls directly.
package executor

import (
	"io/fs"

	"lab_env/internal/conformance"
)

// Executor extends Observer with mutation capabilities. Every method that
// changes system state lives here. Methods from Observer are read-only
// and produce no audit entries. Methods on Executor are audited.
//
// Implementations: Real in real.go. A mock for use in unit tests.
type Executor interface {
	conformance.Observer

	// WriteFile atomically writes data to path with the given mode.
	// Uses temp file + rename semantics (control-plane-contract §6.2).
	// Owner is set to the given owner:group strings.
	WriteFile(path string, data []byte, mode fs.FileMode, owner, group string) error

	// Chmod changes mode bits of path. Does not change content or ownership.
	Chmod(path string, mode fs.FileMode) error

	// Chown changes the owner and group of path.
	Chown(path, owner, group string) error

	// Remove removes the file at path.
	Remove(path string) error

	// MkdirAll creates the directory at path and all parents, with the
	// given mode.
	MkdirAll(path string, mode fs.FileMode, owner, group string) error

	// Systemctl executes a systemd action on a unit.
	// action must be one of: start, stop, restart, enable, disable,
	// daemon-reload, is-active, is-enabled.
	Systemctl(action, unit string) error

	// NginxReload reloads nginx configuration after verifying syntax.
	// Returns an error if nginx -t fails; does not reload in that case.
	NginxReload() error

	// RestoreFile restores a canonical file from the embedded content map.
	// Used by R2 reset to restore files to their canonical state.
	RestoreFile(path string) error

	// RunMutation executes a privileged command that mutates system state.
	// All calls are audited with an executor_op entry before execution.
	// Use this for operations that change system state but do not fit
	// the named mutation methods above (e.g., bootstrap, set-environment).
	// MUST NOT be used for read-only operations — use Observer.RunCommand.
	RunMutation(cmd string, args ...string) error
}