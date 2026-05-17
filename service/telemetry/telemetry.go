// Package telemetry manages the /run/app/telemetry.json file.
//
// Application Runtime Contract §3.5: the telemetry file must be updated every
// 2 seconds and written atomically. The control plane and learners poll it at
// any time.
//
// Schema (12 fields — contract says "10-field schema" but lists 12 including ts
// and the additive inode_usage_percent field):
//
//	{
//	  "ts":                   "2026-01-01T12:00:00Z",   // RFC3339 UTC timestamp
//	  "pid":                  1234,                       // process ID
//	  "uptime_seconds":       120,                        // seconds since startup
//	  "cpu_percent":          12.3,                       // % of one CPU core (0-100+)
//	  "memory_rss_mb":        45.2,                       // resident set size, MiB
//	  "open_fds":             18,                         // open file descriptors
//	  "disk_usage_percent":   67,                         // /var/lib/app block usage %
//	  "inode_usage_percent":  12,                         // /var/lib/app inode usage %
//	  "requests_total":       1042,                       // cumulative since start
//	  "errors_total":         3,                          // cumulative 5xx + panics
//	  "chaos_active":         false,                      // any chaos mode active
//	  "chaos_modes":          []                          // active mode names
//	}
//
// Note: disk_usage_percent reflects block usage only. Inode exhaustion (fault F-018)
// fills inodes while leaving blocks available. inode_usage_percent was added to make
// F-018 observable without requiring learners to run df -i manually.
//
// cpu_percent is computed as a two-sample delta between consecutive telemetry
// writes. The first write after startup will show 0.0 cpu_percent.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// TelemetryFile is the canonical path for the telemetry JSON file.
	// Application Runtime Contract §3.5.
	TelemetryFile = "/run/app/telemetry.json"

	// StatePath is the runtime state directory whose usage is reported.
	StatePath = "/var/lib/app"

	// UpdateInterval is how often telemetry is refreshed.
	// Application Runtime Contract §3.5: MUST be refreshable every 2 seconds.
	UpdateInterval = 2 * time.Second
)

// Snapshot is the telemetry JSON schema.
// All fields are present in every write, even when zero.
type Snapshot struct {
	Ts                string   `json:"ts"`
	PID               int      `json:"pid"`
	UptimeSeconds     int64    `json:"uptime_seconds"`
	CPUPercent        float64  `json:"cpu_percent"`
	MemoryRSSMB       float64  `json:"memory_rss_mb"`
	OpenFDs           int      `json:"open_fds"`
	DiskUsagePercent  float64  `json:"disk_usage_percent"`
	InodeUsagePercent float64  `json:"inode_usage_percent"`
	RequestsTotal     int64    `json:"requests_total"`
	ErrorsTotal       int64    `json:"errors_total"`
	ChaosActive       bool     `json:"chaos_active"`
	ChaosModes        []string `json:"chaos_modes"`
}

// Metrics holds the shared atomic counters incremented by HTTP handlers.
// All fields are safe for concurrent access from multiple goroutines.
type Metrics struct {
	RequestsTotal atomic.Int64
	ErrorsTotal   atomic.Int64
}

// cpuSample holds a point-in-time reading of process CPU time from /proc/self/stat.
type cpuSample struct {
	at       time.Time
	userMs   int64 // user-mode CPU time in milliseconds
	systemMs int64 // kernel-mode CPU time in milliseconds
}

// Collector manages telemetry state and the update goroutine.
type Collector struct {
	startTime   time.Time
	pid         int
	metrics     *Metrics
	chaosActive func() bool
	chaosModes  func() []string
	prevSample  atomic.Value // stores *cpuSample
	logger      logWriter    // for panic recovery; may write to stderr if nil
}

// logWriter is a minimal interface satisfied by *logging.Logger.
// Using an interface avoids an import cycle between telemetry and logging.
type logWriter interface {
	Error(msg string, kvs ...interface{})
}

// New creates a Collector. chaosActive and chaosModes are callbacks that return
// current chaos state — this avoids importing the chaos package (no import cycle).
// logger is used only for panic recovery; pass nil to fall back to stderr.
func New(metrics *Metrics, chaosActive func() bool, chaosModes func() []string, logger logWriter) *Collector {
	return &Collector{
		startTime:   time.Now(),
		pid:         os.Getpid(),
		metrics:     metrics,
		chaosActive: chaosActive,
		chaosModes:  chaosModes,
		logger:      logger,
	}
}

// Run starts the telemetry update loop. It writes telemetry immediately on start,
// then every UpdateInterval. It returns when ctx is cancelled.
// Panics in write() are caught and logged — telemetry must not die silently.
func (c *Collector) Run(ctx context.Context) {
	// Write once immediately so the file exists before the first poll.
	c.safeWrite()

	ticker := time.NewTicker(UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.safeWrite()
		}
	}
}

// safeWrite calls write() with panic recovery.
// If write() panics, the error is logged and the goroutine continues.
// Telemetry observability must not die due to a transient implementation bug.
func (c *Collector) safeWrite() {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("telemetry write panic: %v", r)
			if c.logger != nil {
				c.logger.Error(msg)
			} else {
				fmt.Fprintln(os.Stderr, msg)
			}
		}
	}()
	c.write()
}

// write collects all metrics and atomically writes telemetry.json.
func (c *Collector) write() {
	now := time.Now().UTC()
	snap := Snapshot{
		Ts:            now.Format(time.RFC3339),
		PID:           c.pid,
		UptimeSeconds: int64(now.Sub(c.startTime).Seconds()),
		CPUPercent:    c.cpuPercent(now),
		MemoryRSSMB:   memoryRSSMB(),
		OpenFDs:       openFDCount(),
		RequestsTotal: c.metrics.RequestsTotal.Load(),
		ErrorsTotal:   c.metrics.ErrorsTotal.Load(),
		ChaosActive:   c.chaosActive(),
		ChaosModes:    c.chaosModes(),
	}

	// Filesystem stats for /var/lib/app.
	diskPct, inodePct := filesystemUsage(StatePath)
	snap.DiskUsagePercent = diskPct
	snap.InodeUsagePercent = inodePct

	// Ensure chaos_modes is always an array, never null in JSON.
	if snap.ChaosModes == nil {
		snap.ChaosModes = []string{}
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return // should never happen with a well-typed struct
	}

	// Atomic write: temp file + rename. Required by Application Runtime Contract §3.5.
	writeAtomic(TelemetryFile, data)
}

// cpuPercent computes CPU usage as a percentage of one core since the last sample.
// Returns 0.0 on the first call (no previous sample exists yet).
func (c *Collector) cpuPercent(now time.Time) float64 {
	current, err := readCPUSample(now)
	if err != nil {
		return 0.0
	}

	prev, ok := c.prevSample.Load().(*cpuSample)
	c.prevSample.Store(current)

	if !ok || prev == nil {
		return 0.0
	}

	elapsed := current.at.Sub(prev.at).Milliseconds()
	if elapsed <= 0 {
		return 0.0
	}

	cpuMs := (current.userMs - prev.userMs) + (current.systemMs - prev.systemMs)
	return float64(cpuMs) / float64(elapsed) * 100.0
}

// readCPUSample reads CPU time from /proc/self/stat.
// Fields 14 (utime) and 15 (stime) are in clock ticks; divided by CLK_TCK (usually 100).
func readCPUSample(at time.Time) (*cpuSample, error) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return nil, err
	}

	// Format: pid (comm) state ppid ... utime stime ...
	// The comm field may contain spaces and is wrapped in parens.
	// Find the last ')' to skip it reliably.
	s := string(data)
	idx := strings.LastIndex(s, ")")
	if idx < 0 {
		return nil, fmt.Errorf("malformed /proc/self/stat")
	}

	fields := strings.Fields(s[idx+1:])
	// After ')': state(0) ppid(1) pgrp(2) session(3) tty(4) tpgid(5)
	// flags(6) minflt(7) cminflt(8) majflt(9) cmajflt(10)
	// utime(11) stime(12)
	if len(fields) < 13 {
		return nil, fmt.Errorf("insufficient fields in /proc/self/stat")
	}

	utime, err := strconv.ParseInt(fields[11], 10, 64)
	if err != nil {
		return nil, err
	}
	stime, err := strconv.ParseInt(fields[12], 10, 64)
	if err != nil {
		return nil, err
	}

	// Convert ticks to milliseconds. CLK_TCK is typically 100 on Linux.
	clkTck := int64(100)
	return &cpuSample{
		at:       at,
		userMs:   utime * 1000 / clkTck,
		systemMs: stime * 1000 / clkTck,
	}, nil
}

// memoryRSSMB reads resident set size from /proc/self/status.
// Returns 0.0 on error.
func memoryRSSMB() float64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0.0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseFloat(fields[1], 64)
				if err == nil {
					return kb / 1024.0 // kB → MiB
				}
			}
		}
	}
	return 0.0
}

// openFDCount counts open file descriptors by reading /proc/self/fd.
// Returns 0 on error.
func openFDCount() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0
	}
	return len(entries)
}

// filesystemUsage returns block and inode usage percentages for the given path.
// Uses syscall.Statfs which works for both regular filesystems and loopback mounts.
//
// disk_usage_percent: block usage (relevant for F-004, normal disk fills)
// inode_usage_percent: inode usage (relevant for F-018 which exhausts inodes
// while leaving block usage near 0 — the key diagnostic distinction)
//
// Returns (0, 0) on error (e.g., mount not present).
func filesystemUsage(path string) (diskPct, inodePct float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0.0, 0.0
	}

	// Block usage: (total - free) / total * 100
	if stat.Blocks > 0 {
		used := stat.Blocks - stat.Bfree
		diskPct = float64(used) / float64(stat.Blocks) * 100.0
	}

	// Inode usage: (total - free) / total * 100
	if stat.Files > 0 {
		usedInodes := stat.Files - stat.Ffree
		inodePct = float64(usedInodes) / float64(stat.Files) * 100.0
	}

	return diskPct, inodePct
}

// writeAtomic writes data to path via temp file + rename.
// Duplicated from signals package to avoid import cycle.
// All signal and telemetry files use this pattern.
// Files are created mode 0644 so the control plane can read them.
func writeAtomic(path string, data []byte) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	// Set mode before rename: 0644 for control plane readability.
	if err := os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName)
		return
	}
	_ = os.Rename(tmpName, path)
}

// Ensure runtime import is used (for GOMAXPROCS if needed in future).
var _ = runtime.GOMAXPROCS