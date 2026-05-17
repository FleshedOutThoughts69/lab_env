package server

// server_test.go
//
// Tests the HTTP handler conformance contracts.
//
// High ROI:
//   - GET /health must never touch /var/lib/app — this is the load-bearing
//     property for the F-004 diagnostic pattern (E-001 passes, E-002 fails)
//   - GET / failure body must be exactly {"status":"error","msg":"state write failed"}
//     so the conformance suite log check L-003 and fault matrix match
//   - /slow returns 200 after 5s even when client disconnects early

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	slog "log/slog"

	"github.com/lab-env/service/logging"
	"github.com/lab-env/service/server"
	"github.com/lab-env/service/telemetry"
)

// TestHandleHealth_Returns200_WithOKBody verifies that GET /health returns
// HTTP 200 with exact body {"status":"ok"}.
// Reference: conformance checks E-001, E-003.
func TestHandleHealth_Returns200_WithOKBody(t *testing.T) {
	svc := newTestServer(t, "prod", newTempStateDir(t))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	svc.HTTPServer().Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	want := `{"status":"ok"}`
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Errorf("body: got %q, want %q", got, want)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
}

// TestHandleHealth_NeverTouchesStateDir verifies that GET /health makes
// no filesystem operations on /var/lib/app (or the test temp equivalent).
//
// This is the most important test in the server package. If /health touches
// the state directory, fault F-004 (chmod 000 /var/lib/app) would break
// /health too, making F-004 look identical to a service crash.
func TestHandleHealth_NeverTouchesStateDir(t *testing.T) {
	stateDir := t.TempDir()
	svc := newTestServer(t, "prod", stateDir)

	// Make the state dir unwritable — if /health touches it, it will error
	if err := os.Chmod(stateDir, 0000); err != nil {
		t.Fatalf("chmod state dir: %v", err)
	}
	defer os.Chmod(stateDir, 0755)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	svc.HTTPServer().Handler.ServeHTTP(w, req)

	// Even with unwritable state dir, /health must return 200
	if w.Code != http.StatusOK {
		t.Errorf("GET /health with unwritable state dir: status %d, want 200", w.Code)
	}
}

// TestHandleRoot_Success_Returns200_WithEnv verifies that GET / returns
// HTTP 200 with body {"status":"ok","env":"<APP_ENV>"} when state dir is writable.
func TestHandleRoot_Success_Returns200_WithEnv(t *testing.T) {
	stateDir := t.TempDir()
	svc := newTestServer(t, "prod", stateDir)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	svc.HTTPServer().Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v\nraw: %s", err, w.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf("body.status: got %q, want ok", body["status"])
	}
	if body["env"] != "prod" {
		t.Errorf("body.env: got %q, want prod", body["env"])
	}
}

// TestHandleRoot_StateWriteFailure_Returns500_WithExactBody verifies that
// when /var/lib/app is unwritable (fault F-004), GET / returns:
//   - HTTP 500
//   - exact body {"status":"error","msg":"state write failed"}
//   - Content-Type: application/json
//
// The exact body and log message are what the fault matrix runbook expects.
// Any variation ("state directory write failed", "write error", etc.) breaks
// the diagnostic pattern.
func TestHandleRoot_StateWriteFailure_Returns500_WithExactBody(t *testing.T) {
	stateDir := t.TempDir()
	svc := newTestServer(t, "prod", stateDir)

	// Make state dir unwritable — simulates F-004
	if err := os.Chmod(stateDir, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(stateDir, 0755)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	svc.HTTPServer().Handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}

	// Exact body contract — any deviation breaks fault matrix runbook
	wantBody := `{"status":"error","msg":"state write failed"}`
	if got := strings.TrimSpace(w.Body.String()); got != wantBody {
		t.Errorf("body: got %q, want %q", got, wantBody)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
}

// TestHandleRoot_StateWriteFailure_E001StillPasses verifies the core diagnostic
// pattern: when state dir is broken, GET /health still returns 200.
// This is the E-001-passes / E-002-fails pattern that identifies F-004/F-018.
func TestHandleRoot_StateWriteFailure_E001StillPasses(t *testing.T) {
	stateDir := t.TempDir()
	svc := newTestServer(t, "prod", stateDir)

	// Break state dir
	if err := os.Chmod(stateDir, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(stateDir, 0755)

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthW := httptest.NewRecorder()
	svc.HTTPServer().Handler.ServeHTTP(healthW, healthReq)

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootW := httptest.NewRecorder()
	svc.HTTPServer().Handler.ServeHTTP(rootW, rootReq)

	if healthW.Code != http.StatusOK {
		t.Errorf("E-001 (GET /health): %d, want 200 — health must survive F-004", healthW.Code)
	}
	if rootW.Code != http.StatusInternalServerError {
		t.Errorf("E-002 (GET /): %d, want 500 — root must fail when state dir broken", rootW.Code)
	}
}

// TestHandleSlow_Returns200_After5Seconds verifies the /slow endpoint
// returns 200 after exactly 5 seconds — the F-011 demo endpoint.
// nginx's proxy_read_timeout=3s will cut the proxied connection; direct
// access to the service completes.
func TestHandleSlow_Returns200_After5Seconds(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test: takes 5+ seconds")
	}

	stateDir := t.TempDir()
	svc := newTestServer(t, "prod", stateDir)

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	svc.HTTPServer().Handler.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	// Must take at least 4.8 seconds (allow 200ms tolerance)
	if elapsed < 4800*time.Millisecond {
		t.Errorf("/slow returned in %v; must sleep ≥5s (4.8s with tolerance)", elapsed)
	}
}

// TestHandleRoot_CountersIncrement verifies that RequestsTotal is incremented
// on each GET / request (success and failure paths).
func TestHandleRoot_CountersIncrement(t *testing.T) {
	stateDir := t.TempDir()
	metrics := &telemetry.Metrics{}
	svc := newTestServerWithMetrics(t, "prod", stateDir, metrics)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		svc.HTTPServer().Handler.ServeHTTP(w, req)
	}

	if got := metrics.RequestsTotal.Load(); got != 3 {
		t.Errorf("RequestsTotal: got %d, want 3", got)
	}
}

// TestHandleRoot_ErrorCounterIncrements verifies that ErrorsTotal is
// incremented on state write failure (not on success).
func TestHandleRoot_ErrorCounterIncrements(t *testing.T) {
	stateDir := t.TempDir()
	metrics := &telemetry.Metrics{}
	svc := newTestServerWithMetrics(t, "prod", stateDir, metrics)

	// Break state dir
	if err := os.Chmod(stateDir, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(stateDir, 0755)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	svc.HTTPServer().Handler.ServeHTTP(w, req)

	if metrics.ErrorsTotal.Load() != 1 {
		t.Errorf("ErrorsTotal: got %d, want 1 after state write failure", metrics.ErrorsTotal.Load())
	}
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func newTestServer(t *testing.T, appEnv, stateDir string) *server.Server {
	t.Helper()
	return newTestServerWithMetrics(t, appEnv, stateDir, &telemetry.Metrics{})
}

func newTestServerWithMetrics(t *testing.T, appEnv, stateDir string, metrics *telemetry.Metrics) *server.Server {
	t.Helper()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "app.log")
	logger, err := logging.New(logPath)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	t.Cleanup(func() { logger.Close() })

	// Point state touch path to our temp dir
	server.SetStateTouchPathForTest(filepath.Join(stateDir, "state"))
	t.Cleanup(server.ResetStateTouchPath)

	_ = slog.Default() // suppress unused import
	return server.New("127.0.0.1:0", appEnv, metrics, logger)
}

func newTempStateDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}