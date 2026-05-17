// Package main is the lab environment subject application.
//
// Application Runtime Contract v1.0.0
// Part of the Canonical Lab Environment Specification Suite.
//
// This is the Go HTTP service ("the subject") that learners observe, diagnose,
// and recover. It is not the control plane. The control plane (lab-env) observes
// this service via the conformance suite and determines canonical state from
// the service's signals, logs, and HTTP responses.
//
// Startup sequence (Application Runtime Contract §3, signals package):
//
//  1.  Remove stale /run/app/loading from previous crash
//  2.  Write /run/app/status = "Starting"
//  3.  Create /run/app/loading
//  4.  Load /etc/app/config.yaml + chaos env vars
//  5.  Open /var/log/app/app.log
//  6.  Emit {"msg":"server started"} log entry  ← L-003 conformance check
//  7.  Write /run/app/app.pid                   ← P-005 conformance check
//  8.  Probe /var/lib/app writability (optional early-warning)
//  9.  Bind socket on config.Server.Addr        ← P-002 conformance check
// 10.  Start telemetry goroutine
// 11.  Create /run/app/healthy                  ← healthy marker
// 12.  Remove /run/app/loading
// 13.  Write /run/app/status = "Running"
// 14.  Start OOM goroutine if CHAOS_OOM_TRIGGER=1
// 15.  Begin accepting requests
//
// Shutdown sequence:
//  1.  SIGTERM received (or SIGINT in dev)
//  2.  Write /run/app/status = "ShuttingDown"
//  3.  Remove /run/app/healthy
//  4.  Call http.Server.Shutdown(ctx) — drains in-flight requests (30s grace)
//  5.  Cancel telemetry goroutine
//  6.  Remove /run/app/app.pid
//  7.  Exit 0
//
// Signal handling:
//   - SIGTERM: graceful shutdown (unless CHAOS_IGNORE_SIGTERM=1)
//   - SIGINT:  graceful shutdown (always — not affected by chaos)
//   - SIGHUP:  ignored (no config reload; log fd managed by copytruncate)
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/lab-env/service/chaos"
	"github.com/lab-env/service/config"
	"github.com/lab-env/service/logging"
	"github.com/lab-env/service/server"
	"github.com/lab-env/service/signals"
	"github.com/lab-env/service/telemetry"
)

const (
	// ConfigPath is the canonical config file location.
	ConfigPath = "/etc/app/config.yaml"

	// LogPath is the structured JSON log file.
	LogPath = "/var/log/app/app.log"

	// ShutdownGrace is the maximum time to wait for in-flight requests to drain.
	// 5 seconds is sufficient for the lab environment. A longer grace period
	// would be problematic during CHAOS_OOM_TRIGGER: the Go runtime cannot
	// reliably schedule shutdown goroutines under memory pressure, so the
	// timeout would always expire rather than draining cleanly.
	// During OOM chaos, an unclean exit is expected and correct.
	ShutdownGrace = 5 * time.Second
)

func main() {
	// Set GOMAXPROCS to 1 to match the cgroup CPU quota (CPUQuota=20% = 0.2 cores).
	// Go defaults GOMAXPROCS to the number of host CPU cores (e.g., 8). With a
	// 0.2-core quota, running 8 OS threads causes excessive cgroup throttling,
	// producing artificial latency spikes that learners mistake for chaos modes.
	// Setting GOMAXPROCS=1 aligns the scheduler with the actual resource budget.
	// Application Runtime Contract §4.1: CPUQuota=20%.
	//
	// Deployment note: the systemd unit file must set TimeoutStopSec longer than
	// ShutdownGrace (5s). Recommended: TimeoutStopSec=10. If TimeoutStopSec is
	// shorter than ShutdownGrace, systemd sends SIGKILL before graceful drain
	// completes, leaving signal files in a dirty state.
	runtime.GOMAXPROCS(1)

	// ── Step 1-3: Signal file init ────────────────────────────────────────────
	// Remove stale files from previous crash, write status=Starting, create loading.
	if err := signals.Init(); err != nil {
		// Can't even write signal files — likely a permissions issue.
		// Write to stderr and exit; systemd will restart us.
		os.Stderr.WriteString("signals.Init failed: " + err.Error() + "\n")
		os.Exit(1)
	}
	if err := signals.CreateLoading(); err != nil {
		os.Stderr.WriteString("signals.CreateLoading failed: " + err.Error() + "\n")
		os.Exit(1)
	}

	// ── Step 4: Load config ───────────────────────────────────────────────────
	cfg, err := config.Load(ConfigPath)
	if err != nil {
		os.Stderr.WriteString("config.Load failed: " + err.Error() + "\n")
		os.Exit(1)
	}

	// ── Step 5: Open log file ─────────────────────────────────────────────────
	// logging.New uses O_APPEND|O_CREATE|O_WRONLY — O_APPEND is critical for
	// logrotate copytruncate compatibility. See logging package doc.
	logger, err := logging.New(LogPath)
	if err != nil {
		logging.Stderr("opening log file failed: " + err.Error())
		os.Exit(1)
	}
	defer logger.Close()

	// ── Step 6: Emit startup log entry ────────────────────────────────────────
	// L-003 conformance check: log must contain {"msg":"server started"}.
	// This entry must appear before accepting requests.
	logger.Info("server started",
		"addr", cfg.Server.Addr,
		"app_env", cfg.AppEnv,
		"chaos_active", cfg.Chaos.IsActive(),
	)

	// ── Step 7: Write PID file ────────────────────────────────────────────────
	// P-005 conformance check: /run/app/app.pid must contain the service PID.
	if err := signals.WritePID(); err != nil {
		logger.Error("writing PID file", "error", err)
		os.Exit(1)
	}

	// ── Step 8: Probe state directory (optional early-warning) ───────────────
	// If the state directory is already broken at startup, set Unhealthy now
	// rather than waiting for the first GET / request to discover it.
	// The conformance suite doesn't require this, but it makes failures
	// immediately visible in /run/app/status.
	probeStateDir(logger)

	// ── Initialize shared metrics ─────────────────────────────────────────────
	metrics := &telemetry.Metrics{}

	// ── Step 9: Build HTTP server ─────────────────────────────────────────────
	// chaos.New wraps the server mux with latency/drop middleware.
	svc := server.New(cfg.Server.Addr, cfg.AppEnv, metrics, logger)
	chaosHandler := chaos.New(
		svc.HTTPServer().Handler,
		cfg.Chaos.LatencyMS,
		cfg.Chaos.DropPercent,
		func() { metrics.RequestsTotal.Add(1) },
		func() { metrics.ErrorsTotal.Add(1) },
		logger,
	)
	svc.HTTPServer().Handler = chaosHandler

	// ── Step 10: Start telemetry goroutine ────────────────────────────────────
	telCtx, telCancel := context.WithCancel(context.Background())
	defer telCancel()

	collector := telemetry.New(
		metrics,
		cfg.Chaos.IsActive,
		cfg.Chaos.ActiveModes,
		logger,
	)
	go collector.Run(telCtx)

	// ── Step 11-13: Create healthy, remove loading, set Running ───────────────
	if err := signals.CreateHealthy(); err != nil {
		logger.Error("creating healthy marker", "error", err)
		os.Exit(1)
	}
	if err := signals.RemoveLoading(); err != nil {
		logger.Warn("removing loading marker", "error", err)
		// Non-fatal: loading marker absence is not checked by conformance suite.
	}
	if err := signals.SetStatus(signals.StatusRunning); err != nil {
		logger.Warn("setting status=Running", "error", err)
	}

	// ── Step 14: OOM trigger ──────────────────────────────────────────────────
	// Must start after healthy is created and after the first telemetry write
	// (which happens synchronously at the start of collector.Run).
	// The telemetry goroutine writes one snapshot before returning from its
	// first safeWrite() call, establishing chaos_active=true in the file before
	// OOM allocation begins. This is best-effort: if the OOM goroutine kills
	// the process before the second telemetry write, the file will show the
	// correct chaos_active=true state from the first write.
	// Note: if OOM kills the process before any telemetry is written,
	// the control plane will see no telemetry file and classify the state as
	// BROKEN — which is the correct canonical state for an OOM kill.
	if cfg.Chaos.OOMTrigger {
		chaos.StartOOM(logger)
	}

	// ── Set Degraded status if chaos modes are active ─────────────────────────
	// Chaos active → status=Degraded. This is set after Running to reflect
	// that the service is up and serving, but with reduced functionality.
	if cfg.Chaos.IsActive() {
		_ = signals.SetStatus(signals.StatusDegraded)
		logger.Warn("chaos modes active",
			"modes", cfg.Chaos.ActiveModes(),
			"latency_ms", cfg.Chaos.LatencyMS,
			"drop_percent", cfg.Chaos.DropPercent,
			"oom_trigger", cfg.Chaos.OOMTrigger,
			"ignore_sigterm", cfg.Chaos.IgnoreSIGTERM,
		)
	}

	// ── Step 15: Configure signal handling ───────────────────────────────────
	// SIGTERM behavior depends on chaos config.
	// SIGINT always causes graceful shutdown (dev convenience, not chaos-affected).
	// SIGHUP is explicitly ignored (no reload — copytruncate handles log rotation).
	sigCh := make(chan os.Signal, 1)

	if cfg.Chaos.IgnoreSIGTERM {
		// Mask SIGTERM. The kernel will send SIGKILL after ~90 seconds.
		// F-008 / CHAOS_IGNORE_SIGTERM=1: systemctl stop app will hang.
		signal.Ignore(syscall.SIGTERM)
		signal.Notify(sigCh, syscall.SIGINT)
		logger.Warn("SIGTERM ignored (CHAOS_IGNORE_SIGTERM=1) — process will require SIGKILL")
	} else {
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	}
	signal.Ignore(syscall.SIGHUP)

	// ── Start HTTP server in background ──────────────────────────────────────
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Server.Addr)
		if err := svc.HTTPServer().ListenAndServe(); err != nil {
			// http.ErrServerClosed is returned when Shutdown is called — not an error.
			// Any other error is unexpected and should trigger cleanup.
			if err != http.ErrServerClosed {
				serverErr <- err
			}
		}
	}()

	// ── Wait for signal or server error ──────────────────────────────────────
	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErr:
		// Unexpected server exit — log and fall through to cleanup.
		logger.Error("HTTP server exited unexpectedly", "error", err)
	}

	// ── Graceful shutdown sequence ────────────────────────────────────────────
	// Step 1: Set ShuttingDown, remove healthy.
	signals.BeginShutdown()
	logger.Info("graceful shutdown started", "grace_period", ShutdownGrace)

	// Step 2: Stop telemetry before draining HTTP.
	// Telemetry is stopped first to avoid noisy snapshots during drain,
	// and to prevent the goroutine from racing with process exit cleanup.
	telCancel()

	// Reset signal handler so a second SIGTERM causes immediate exit.
	// The lab control plane may send multiple signals; without this reset,
	// a second SIGTERM would be buffered or dropped, leaving the control
	// plane waiting indefinitely if it polls for clean exit.
	signal.Reset(syscall.SIGTERM, syscall.SIGINT)

	// Step 3: Drain in-flight requests.
	// http.Server.Shutdown correctly handles keep-alive connections.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), ShutdownGrace)
	defer shutdownCancel()

	if err := svc.HTTPServer().Shutdown(shutdownCtx); err != nil {
		logger.Warn("shutdown did not complete cleanly", "error", err)
	}

	// Step 4: Remove PID file.
	signals.RemovePID()

	logger.Info("shutdown complete")
}

// probeStateDir attempts a write to the state directory at startup.
// If it fails, sets status=Unhealthy immediately rather than waiting for
// the first GET / to discover the problem.
// The conformance suite does not require this probe; it is defensive only.
func probeStateDir(logger *logging.Logger) {
	const probeFile = "/var/lib/app/.startup-probe"
	f, err := os.OpenFile(probeFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		logger.Warn("state directory not writable at startup",
			"path", "/var/lib/app",
			"error", err,
		)
		_ = signals.SetStatus(signals.StatusUnhealthy)
		return
	}
	f.Close()
	os.Remove(probeFile)
}