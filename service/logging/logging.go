// Package logging provides the diagnostic log writer for /var/log/app/app.log.
//
// # Why this package exists
//
// The log file contract has one critical requirement that is easy to violate:
// the file must be opened with O_APPEND. Without O_APPEND, logrotate's
// copytruncate mechanism leaves the file offset past the new end-of-file,
// causing the next write to produce null bytes and corrupting the log.
// Conformance checks L-001 (log non-empty) and L-002 (last line valid JSON)
// would fail intermittently with no obvious cause. This package makes O_APPEND
// non-negotiable by encoding it in the constructor.
//
// # What this package is NOT
//
// This is not a replacement for the telemetry system (/run/app/telemetry.json).
// Telemetry is machine-readable metrics; this log is diagnostic text for operators
// and the conformance suite. The two must remain separate.
//
// # Format contract
//
// Every line is a single JSON object with exactly these fields:
//
//	{"ts":"<RFC3339Nano UTC>","level":"<info|warn|error>","msg":"<message>"[,"<key>":"<value>"...]}
//
// The conformance suite checks:
//
//	L-001: file exists and is non-empty (guaranteed after first write)
//	L-002: last line is valid JSON (guaranteed by json.Marshal + newline)
//	L-003: file contains {"msg":"server started"} (written by main.go step 6)
//
// # Thread safety
//
// All methods are safe for concurrent use. A sync.Mutex serialises writes,
// preventing interleaved bytes from concurrent goroutines.
//
// # Buffering
//
// Writes are unbuffered: each call produces one os.File.Write syscall.
// This satisfies the "unbuffered, newline-delimited JSON" requirement from
// the Runtime Contract §6.1. No bufio.Writer is used.
package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	// LogPath is the canonical diagnostic log file location.
	// canonical-environment.md §2.3, internal/config/config.go LogPath.
	LogPath = "/var/log/app/app.log"

	levelInfo  = "info"
	levelWarn  = "warn"
	levelError = "error"
)

// Logger writes newline-delimited JSON to the diagnostic log file.
// Created once at startup and shared across all packages.
type Logger struct {
	mu sync.Mutex
	f  *os.File
}

// New opens the log file at path for append-only writing.
// The file is created if it does not exist, with mode 0640.
// O_APPEND is required: without it, logrotate's copytruncate leaves
// the file offset past the truncated end, causing null-byte corruption.
func New(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return nil, fmt.Errorf("opening log file %s: %w", path, err)
	}
	return &Logger{f: f}, nil
}

// Info writes a log entry at level "info".
// Accepts optional key-value pairs: Info("msg", "key1", "val1", "key2", "val2").
// Keys must be strings. Values are formatted with fmt.Sprint.
func (l *Logger) Info(msg string, kvs ...interface{}) {
	l.write(levelInfo, msg, kvs)
}

// Warn writes a log entry at level "warn".
func (l *Logger) Warn(msg string, kvs ...interface{}) {
	l.write(levelWarn, msg, kvs)
}

// Error writes a log entry at level "error".
func (l *Logger) Error(msg string, kvs ...interface{}) {
	l.write(levelError, msg, kvs)
}

// Close flushes any pending writes and closes the file descriptor.
// Must be called during graceful shutdown, after the final log entry.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

// write serialises a log entry to JSON and writes it as a single line.
// The mutex guarantees atomicity: concurrent calls never produce interleaved bytes.
func (l *Logger) write(level, msg string, kvs []interface{}) {
	entry := buildEntry(level, msg, kvs)
	line, err := json.Marshal(entry)
	if err != nil {
		// json.Marshal should never fail on map[string]interface{} with string keys.
		// If it does, write a minimal error line rather than silently dropping.
		line = []byte(fmt.Sprintf(
			`{"ts":%q,"level":"error","msg":"log marshal failed","error":%q}`+"\n",
			time.Now().UTC().Format(time.RFC3339Nano), err.Error(),
		))
	} else {
		line = append(line, '\n')
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	// Single Write call per entry: satisfies the unbuffered contract.
	// os.File.Write is a direct syscall on Linux — no userspace buffering.
	_, _ = l.f.Write(line)
}

// buildEntry constructs the log entry map from level, msg, and key-value pairs.
// Keys at odd positions in kvs are used as field names; values follow.
// Unpaired keys get value "<missing>".
func buildEntry(level, msg string, kvs []interface{}) map[string]interface{} {
	entry := map[string]interface{}{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"level": level,
		"msg":   msg,
	}
	for i := 0; i+1 < len(kvs); i += 2 {
		key, ok := kvs[i].(string)
		if !ok {
			key = fmt.Sprint(kvs[i])
		}
		entry[key] = kvs[i+1]
	}
	if len(kvs)%2 != 0 {
		entry["_unpaired"] = kvs[len(kvs)-1]
	}
	return entry
}

// Stderr writes a plain-text line to os.Stderr.
// Used before the log file is open (early startup errors) and after it is
// closed (post-shutdown). Not JSON — these messages are captured by systemd
// journald separately, per Runtime Contract §6.3.
func Stderr(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}