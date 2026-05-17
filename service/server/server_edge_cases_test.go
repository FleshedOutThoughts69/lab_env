package server

// server_edge_test.go
//
// Edge case HTTP handler tests.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lab-env/service/logging"
	"github.com/lab-env/service/server"
	"github.com/lab-env/service/telemetry"
)

// TestHandleRoot_EmptyAppEnv_ReturnsEmptyStringNotNull verifies that when
// app_env is an empty string, GET / returns {"status":"ok","env":""} not
// {"status":"ok","env":null}.
//
// The spec says the body is {"status":"ok","env":"<APP_ENV>"}. An empty
// string is valid; null would break JSON parsers that expect a string.
func TestHandleRoot_EmptyAppEnv_ReturnsEmptyStringNotNull(t *testing.T) {
	stateDir := t.TempDir()
	svc := newEdgeTestServer(t, "", stateDir) // empty app_env

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	svc.HTTPServer().Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d, want 200", w.Code)
	}

	// Parse response body
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v\nraw: %s", err, w.Body.String())
	}

	// env must be a string (possibly empty), not nil/null
	envVal, exists := body["env"]
	if !exists {
		t.Error("body missing 'env' field")
		return
	}
	if envVal == nil {
		t.Error("env is null; must be a string (empty string is acceptable)")
	}
	if _, ok := envVal.(string); !ok {
		t.Errorf("env type: got %T, want string", envVal)
	}
}

// TestHandlers_NoServerHeader verifies that responses do not include a Server
// header that reveals the Go version or framework.
//
// Security best practice and conformance suite may check for this.
// Go's net/http does not add a Server header by default; this test ensures
// no middleware accidentally adds one.
func TestHandlers_NoGoServerHeader(t *testing.T) {
	stateDir := t.TempDir()
	svc := newEdgeTestServer(t, "prod", stateDir)

	routes := []string{"/health", "/"}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			w := httptest.NewRecorder()
			svc.HTTPServer().Handler.ServeHTTP(w, req)

			if sv := w.Header().Get("Server"); sv != "" {
				t.Errorf("%s: Server header is set to %q; must not reveal server identity", route, sv)
			}
		})
	}
}

// TestConcurrent_HealthAndRoot_StateWriteFailure verifies that simultaneous
// GET /health and GET / requests during a state directory failure do not
// produce incorrect results or deadlocks.
//
// /health must return 200 and / must return 500, even under concurrent load.
// This rules out any global mutex that could cause /health to block waiting
// for a failed state write to complete.
func TestConcurrent_HealthAndRoot_StateWriteFailure(t *testing.T) {
	stateDir := t.TempDir()
	svc := newEdgeTestServer(t, "prod", stateDir)

	// Break state dir
	if err := os.Chmod(stateDir, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(stateDir, 0755)

	const goroutines = 20
	var wg sync.WaitGroup

	healthCodes := make([]int, goroutines)
	rootCodes := make([]int, goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			w := httptest.NewRecorder()
			svc.HTTPServer().Handler.ServeHTTP(w, req)
			healthCodes[i] = w.Code
		}()
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			svc.HTTPServer().Handler.ServeHTTP(w, req)
			rootCodes[i] = w.Code
		}()
	}

	wg.Wait()

	for i, code := range healthCodes {
		if code != http.StatusOK {
			t.Errorf("goroutine %d: GET /health returned %d; must be 200 even during state failure", i, code)
		}
	}
	for i, code := range rootCodes {
		if code != http.StatusInternalServerError {
			t.Errorf("goroutine %d: GET / returned %d; must be 500 when state dir unwritable", i, code)
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newEdgeTestServer(t *testing.T, appEnv, stateDir string) *server.Server {
	t.Helper()
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "app.log")
	logger, err := logging.New(logPath)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	t.Cleanup(func() { logger.Close() })

	server.SetStateTouchPathForTest(filepath.Join(stateDir, "state"))
	t.Cleanup(server.ResetStateTouchPath)

	return server.New("127.0.0.1:0", appEnv, &telemetry.Metrics{}, logger)
}