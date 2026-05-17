// Package chaos implements the chaos injection interface.
//
// Application Runtime Contract §5: the service reads /etc/app/chaos.env via
// systemd EnvironmentFile and must implement four chaos modes.
//
// All chaos modes are initialized once at startup from the loaded config.
// Runtime re-activation is not supported (no SIGHUP reload).
//
// Modes:
//
//	CHAOS_LATENCY_MS    — add fixed delay to every request (fault F-020)
//	CHAOS_DROP_PERCENT  — randomly drop N% of requests with 503
//	CHAOS_OOM_TRIGGER   — allocate until OOM killed (equivalent to F-014)
//	CHAOS_IGNORE_SIGTERM — mask SIGTERM (equivalent to F-008)
//
// OOM isolation: the OOM goroutine is protected by sync.Once to ensure it
// fires exactly once per process lifetime, regardless of how many times
// Middleware is called. The goroutine allocates in isolation from request
// handlers to avoid deadlocking the server before the OOM reaper acts.
// The process will be terminated by the kernel; no telemetry will be written
// after that point. The diagnostic signal for the operator is:
//   - sudden halt of /run/app/telemetry.json updates
//   - systemctl status app.service showing failed
//   - journalctl -u app.service showing OOM kill
//   - dmesg showing OOM kill entry
package chaos

import (
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/lab-env/service/logging"
)

// Handler wraps an http.Handler with chaos middleware.
// It applies chaos effects in this order:
//  1. Drop check (returns 503 immediately, before any processing)
//  2. Latency injection (sleep before dispatching to next handler)
//  3. Next handler
type Handler struct {
	next        http.Handler
	latencyMS   int
	dropPercent int
	reqCounter  func() // increments requests_total on drop (drop is still a request arrival)
	errCounter  func() // increments errors_total on drop
	logger      *logging.Logger
}

// New creates a chaos Handler wrapping next.
// latencyMS and dropPercent come from the loaded config.
// reqCounter is called on every dropped request to increment requests_total.
// errCounter is called on every dropped request to increment errors_total.
// Both must be non-nil if dropPercent > 0; safe to pass nil if drop is disabled.
// logger is used to write chaos drop events; may be nil (drops are silently counted).
func New(next http.Handler, latencyMS, dropPercent int, reqCounter, errCounter func(), logger *logging.Logger) *Handler {
	return &Handler{
		next:        next,
		latencyMS:   latencyMS,
		dropPercent: dropPercent,
		reqCounter:  reqCounter,
		errCounter:  errCounter,
		logger:      logger,
	}
}

// ServeHTTP applies chaos effects then calls the wrapped handler.
//
// Drop percent applies to ALL routes including /health — a dropped request
// returns 503 regardless of path. This matches "at the earliest possible point"
// from Application Runtime Contract §5.1.
//
// Latency injection is EXEMPTED for /health. Rationale: /health is the
// conformance check E-001 target. Adding latency there would cause E-001 to
// fail if the check's HTTP timeout is shorter than CHAOS_LATENCY_MS, producing
// the wrong diagnostic pattern (looks like a service crash rather than F-020).
// Fault F-020 (chaos latency) is observable via E-002 and /slow timing.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Drop check: apply to all routes before any request-specific processing.
	// Application Runtime Contract §5.1: "at the earliest possible point".
	if h.dropPercent > 0 {
		if rand.IntN(100) < h.dropPercent {
			if h.logger != nil {
				h.logger.Warn("chaos drop", "path", r.URL.Path, "drop_percent", h.dropPercent)
			}
			// A dropped request is still a request arrival — increment both counters.
			if h.reqCounter != nil {
				h.reqCounter()
			}
			if h.errCounter != nil {
				h.errCounter()
			}
			http.Error(w, `{"status":"error","msg":"chaos drop"}`, http.StatusServiceUnavailable)
			return
		}
	}

	// Latency injection: exempt /health to preserve E-001 diagnostic integrity.
	// Applied to all other routes (/, /slow, any future routes).
	if h.latencyMS > 0 && r.URL.Path != "/health" {
		time.Sleep(time.Duration(h.latencyMS) * time.Millisecond)
	}

	h.next.ServeHTTP(w, r)
}

// oomOnce ensures the OOM goroutine fires exactly once per process lifetime.
// A second call to StartOOM (e.g., from a misconfigured test) is silently ignored.
var oomOnce sync.Once

// StartOOM launches the OOM trigger goroutine if CHAOS_OOM_TRIGGER is active.
// This function is a no-op if called more than once.
func StartOOM(logger *logging.Logger) {
	oomOnce.Do(func() {
		logger.Warn("chaos oom started — process will be terminated by OOM killer",
			"mode", "oom",
			"action", "allocating until OOM kill",
		)
		go func() {
			// Allocate in large chunks. Each iteration allocates 64 MiB.
			// The cgroup MemoryMax=256M limit means the process will be killed
			// after ~4 iterations (accounting for existing RSS).
			// We touch the memory to ensure it is actually resident (not just
			// virtually allocated), forcing the cgroup limit to be enforced.
			var sinks [][]byte
			for {
				chunk := make([]byte, 64*1024*1024) // 64 MiB
				for i := range chunk {
					chunk[i] = byte(i) // touch every page
				}
				sinks = append(sinks, chunk)
				// Brief pause to allow telemetry to record the rising memory.
				time.Sleep(200 * time.Millisecond)
			}
		}()
	})
}