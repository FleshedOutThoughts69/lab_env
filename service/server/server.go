// Package server implements the HTTP service.
//
// ═══════════════════════════════════════════════════════════════════════════
// CONFORMANCE SUITE CONTRACT
// ═══════════════════════════════════════════════════════════════════════════
//
// This package is the primary target of the conformance suite checks.
// Each handler documents which checks it satisfies and which fault targets it.
//
// Handler → Check mapping:
//
//	GET /health
//	  Satisfies: E-001 (returns 200), E-003 (/health body contains "status":"ok")
//	  Also satisfies: E-005 (https://app.local/health via nginx TLS proxy)
//	  MUST NOT: touch /var/lib/app — health must survive F-004
//	  Fault target: none directly (F-004 does NOT break /health)
//
//	GET /
//	  Satisfies: E-002 (returns 200 when healthy)
//	  Fault target: F-004 (chmod 000 /var/lib/app → write fails → returns 500)
//	  Fault target: F-018 (inode exhaustion → write fails → returns 500)
//	  Diagnostic pattern: E-001 passes, E-002 fails → F-004 or F-018
//
//	GET /slow
//	  Satisfies: F-011 baseline behavior demo (nginx timeout < app response time)
//	  Not tested by conformance suite; used in fault matrix runbook Demo 5
//
// The nginx proxy adds X-Proxy: nginx to all upstream responses.
// E-004 (Response includes X-Proxy: nginx header) is satisfied by nginx,
// not by this service. The service does not set this header.
//
// ═══════════════════════════════════════════════════════════════════════════
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/lab-env/service/logging"
	"github.com/lab-env/service/signals"
	"github.com/lab-env/service/telemetry"
)

const (
	// StateTouchPath is touched on every GET / request.
	// canonical-environment.md §2.3, internal/config/config.go StateTouchPath.
	// Fault F-004 sets this directory to mode 000, breaking writes here.
	// Fault F-018 exhausts inodes in this directory, also breaking writes.
	StateTouchPath = "/var/lib/app/state"

	// SlowHandlerDelay is the fixed response delay for GET /slow.
	// Used in F-011 baseline behavior demo: nginx proxy_read_timeout (~3s)
	// fires before this completes, producing a 504. Direct access succeeds.
	SlowHandlerDelay = 5 * time.Second
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	http    *http.Server
	metrics *telemetry.Metrics
	appEnv  string
	logger  *logging.Logger
}

// New creates a Server bound to addr.
// appEnv is the APP_ENV value from config (e.g., "prod").
// metrics is the shared atomic counter set incremented by handlers.
// The handler chain is: chaos middleware → mux → handlers.
// The chaos middleware is applied by the caller (main.go) wrapping the mux.
func New(addr string, appEnv string, metrics *telemetry.Metrics, logger *logging.Logger) *Server {
	s := &Server{
		metrics: metrics,
		appEnv:  appEnv,
		logger:  logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /", s.handleRoot)
	mux.HandleFunc("GET /slow", s.handleSlow)

	s.http = &http.Server{
		Addr:    addr,
		Handler: mux,
		// Timeouts: generous enough for the slow endpoint (5s + margin).
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s
}

// HTTPServer returns the underlying *http.Server for use in Shutdown calls.
func (s *Server) HTTPServer() *http.Server {
	return s.http
}

// handleHealth handles GET /health.
//
// Conformance checks satisfied: E-001, E-003, E-005 (via nginx TLS proxy).
//
// Contract:
//   - MUST return 200 with body {"status":"ok"}
//   - MUST NOT touch /var/lib/app — this endpoint must survive fault F-004
//   - MUST NOT return anything other than 200 in a conformant environment
//
// This is the readiness/liveness probe. Its independence from the state
// directory is the load-bearing design decision for the F-004 diagnostic pattern.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.metrics.RequestsTotal.Add(1)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

// handleRoot handles GET /.
//
// Conformance check satisfied: E-002 (returns 200 when healthy).
// Fault targets: F-004 (state dir mode 000), F-018 (inode exhaustion).
//
// Contract:
//   - MUST touch /var/lib/app/state on every request
//   - If touch succeeds: return 200 with body {"status":"ok","env":"<APP_ENV>"}
//   - If touch fails: return 500 with body {"status":"error","msg":"state write failed"}
//     AND set service status to Unhealthy
//     AND log at level error with msg "state write failed"
//   - The 500 response produces the E-001-passes / E-002-fails diagnostic pattern
//     that identifies F-004 and F-018
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	s.metrics.RequestsTotal.Add(1)
	w.Header().Set("Content-Type", "application/json")

	if err := touchStatePath(); err != nil {
		s.metrics.ErrorsTotal.Add(1)
		s.logger.Error("state write failed",
			"path", StateTouchPath,
			"error", err,
		)
		// Set Unhealthy status. Unhealthy is distinct from Degraded:
		// Unhealthy = state directory unwritable (structural fault)
		// Degraded  = chaos mode active (injected fault)
		_ = signals.SetStatus(signals.StatusUnhealthy)

		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"status":"error","msg":"state write failed"}`)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","env":"%s"}`, s.appEnv)
}

// handleSlow handles GET /slow.
//
// Not part of the conformance suite. Used in fault matrix runbook Demo 5 and
// F-011 baseline behavior documentation.
//
// Contract:
//   - Sleep for SlowHandlerDelay (5 seconds) before responding
//   - Return 200 with body {"status":"ok"}
//   - nginx proxy_read_timeout (~3 seconds) fires before this completes
//     when accessed via localhost (nginx), producing 504
//   - Direct access via 127.0.0.1:8080/slow succeeds after 5 seconds
//   - This demonstrates the proxy timeout layer in isolation
func (s *Server) handleSlow(w http.ResponseWriter, r *http.Request) {
	s.metrics.RequestsTotal.Add(1)
	time.Sleep(SlowHandlerDelay)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

// touchStatePath updates the mtime of StateTouchPath, creating it if absent.
// This write is the state directory access that fault F-004 breaks.
// Using os.WriteFile rather than os.Chtimes ensures both permissions and
// inode availability are tested (inode exhaustion blocks file creation).
func touchStatePath() error {
	f, err := os.OpenFile(StateTouchPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

// writeJSON is a helper for writing JSON responses.
// Used when the response body is more complex than an inline literal.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Encoding error after WriteHeader — can't change status code.
		// Log internally but nothing more can be done.
		return
	}
}