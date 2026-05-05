package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"lab_env/internal/config"
	"lab_env/internal/state"
)

// AuditEntry is a single entry in the audit log.
// Schema defined in control-plane-contract §7.2.
type AuditEntry struct {
	Ts         time.Time `json:"ts"`
	EntryType  string    `json:"entry_type"`
	Command    string    `json:"command"`
	FaultID    string    `json:"fault_id,omitempty"`
	Op         string    `json:"op,omitempty"`
	OpArgs     string    `json:"op_args,omitempty"`
	ExitCode   *int      `json:"exit_code,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}

// Entry types defined in control-plane-contract §7.3.
const (
	EntryTypeExecutorOp     = "executor_op"
	EntryTypeStateTransition = "state_transition"
	EntryTypeValidationRun  = "validation_run"
	EntryTypeReconciliation = "reconciliation"
	EntryTypeInterrupt      = "interrupt"
	EntryTypeError          = "error"
)

// AuditLogger appends entries to the audit log at AuditPath.
// It is the only component that writes to the audit log.
// The log is append-only and must never be truncated.
type AuditLogger struct {
	path    string
	command string // current lab command being executed
	mu      sync.Mutex
}

// NewAuditLogger returns a logger writing to the canonical audit log path.
// command is the full lab command invocation (e.g., "lab fault apply F-004").
func NewAuditLogger(command string) *AuditLogger {
	return &AuditLogger{path: config.AuditPath, command: command}
}

// NewAuditLoggerAt returns a logger writing to a custom path.
// Used in tests.
func NewAuditLoggerAt(path, command string) *AuditLogger {
	return &AuditLogger{path: path, command: command}
}

// LogOp records an executor operation entry.
// Must be called before the operation begins (ordering guarantee from §7.4).
func (l *AuditLogger) LogOp(op, args string, durationMs int64, exitCode int, err error) {
	code := exitCode
	entry := AuditEntry{
		Ts:         time.Now().UTC(),
		EntryType:  EntryTypeExecutorOp,
		Command:    l.command,
		Op:         op,
		OpArgs:     args,
		ExitCode:   &code,
		DurationMs: durationMs,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	l.append(entry)
}

// LogTransition records a state transition entry.
func (l *AuditLogger) LogTransition(from, to state.State, faultID string) {
	entry := AuditEntry{
		Ts:        time.Now().UTC(),
		EntryType: EntryTypeStateTransition,
		Command:   l.command,
		FaultID:   faultID,
		Op:        fmt.Sprintf("%s → %s", from, to),
	}
	l.append(entry)
}

// LogReconciliation records a state reconciliation entry.
// Called by lab status when detected state differs from recorded state.
func (l *AuditLogger) LogReconciliation(from, to state.State) {
	entry := AuditEntry{
		Ts:        time.Now().UTC(),
		EntryType: EntryTypeReconciliation,
		Command:   l.command,
		Op:        fmt.Sprintf("%s → %s", from, to),
	}
	l.append(entry)
}

// LogInterrupt records a signal interrupt entry.
// Called by the signal handler when an operation is interrupted.
func (l *AuditLogger) LogInterrupt(op string, gracePeriodExceeded bool) {
	args := "signal received"
	if gracePeriodExceeded {
		args = "signal received; grace period exceeded; operation abandoned"
	}
	entry := AuditEntry{
		Ts:        time.Now().UTC(),
		EntryType: EntryTypeInterrupt,
		Command:   l.command,
		Op:        op,
		OpArgs:    args,
	}
	l.append(entry)
}

// LogError records a command-level error entry.
func (l *AuditLogger) LogError(errName, detail string) {
	entry := AuditEntry{
		Ts:        time.Now().UTC(),
		EntryType: EntryTypeError,
		Command:   l.command,
		Op:        errName,
		Error:     detail,
	}
	l.append(entry)
}

func (l *AuditLogger) append(entry AuditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		// Audit failure is logged to stderr but must not block the operation.
		fmt.Fprintf(os.Stderr, "audit log marshal error: %v\n", err)
		return
	}
	data = append(data, '\n')

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit log open error: %v\n", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "audit log write error: %v\n", err)
	}
}