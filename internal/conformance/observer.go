// Package conformance implements the conformance check engine defined in
// conformance-model.md. It depends only on the Observer interface for all
// system inspection — it never requires mutation authority.
package conformance

import (
	"io/fs"
	"time"
)

// Observer provides read-only access to the system state. It carries no
// mutation authority, acquires no locks, and produces no audit entries.
// Every conformance check, the state detection algorithm, and the
// reconciliation engine depend on this interface and nothing broader.
//
// Implementations: executor.Real implements Observer (and Executor, which
// embeds Observer). A mock implementation is used in unit tests.
type Observer interface {
	// Stat returns metadata for the path without reading its contents.
	// Returns os.ErrNotExist if the path does not exist.
	Stat(path string) (fs.FileInfo, error)

	// ReadFile returns the full contents of a file.
	ReadFile(path string) ([]byte, error)

	// CheckProcess reports whether a process matching name is running
	// as the specified user. Empty user means any user.
	CheckProcess(name, user string) (ProcessStatus, error)

	// CheckPort reports whether a TCP listening socket exists on the
	// given address (e.g., "127.0.0.1:8080" or "0.0.0.0:80").
	CheckPort(addr string) (PortStatus, error)

	// CheckEndpoint makes an HTTP GET to url and returns the status code
	// and whether the request succeeded. skipTLSVerify applies only to
	// HTTPS endpoints with self-signed certificates.
	CheckEndpoint(url string, skipTLSVerify bool) (EndpointStatus, error)

	// ResolveHost returns the IP address that name resolves to using
	// the system resolver (equivalent to getent hosts).
	ResolveHost(name string) (string, error)

	// ServiceActive reports whether the named systemd unit is active.
	ServiceActive(unit string) (bool, error)

	// ServiceEnabled reports whether the named systemd unit is enabled.
	ServiceEnabled(unit string) (bool, error)

	// RunCommand executes a read-only command and returns stdout.
	// Used for checks that require external tools (openssl, nginx -t).
	// MUST NOT be used for mutations.
	RunCommand(cmd string, args ...string) (stdout string, err error)
}

// ProcessStatus is the result of CheckProcess.
type ProcessStatus struct {
	Running bool
	PID     int
	User    string
}

// PortStatus is the result of CheckPort.
type PortStatus struct {
	Listening bool
	Addr      string
}

// EndpointStatus is the result of CheckEndpoint.
type EndpointStatus struct {
	StatusCode int
	Reachable  bool
	// Body is populated only when the check requires body inspection.
	Body []byte
}

// FileInfo extends fs.FileInfo with ownership information, which the
// standard library's fs.FileInfo does not expose portably.
type FileInfo struct {
	fs.FileInfo
	Owner string
	Group string
	// ModeString is the formatted permission string, e.g. "750".
	ModeString string
}

// DNSResult is the result of ResolveHost.
type DNSResult struct {
	Addr string
	// Via is the resolver that produced this result (for diagnostics).
	Via string
}

// Timestamps used in log and state checks.
type LogEntry struct {
	Raw       []byte
	Timestamp time.Time
	Level     string
	Msg       string
}