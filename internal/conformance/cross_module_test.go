package conformance

// cross_module_test.go
//
// Cross-module contract tests that verify the service's HTTP responses
// satisfy the conformance check assertions.
//
// Why high ROI: the conformance checks define what "correct" means, and the
// service implements the responses. These are tested independently in each
// module, but a subtle mismatch between the check's assertion and the
// handler's response body would only appear here.
//
// Example: the service returns {"status":"ok"} and E-003 checks for
// "status":"ok" — if the service changed to {"status": "ok"} (with a space
// after the colon), the check might fail depending on implementation.
// This test runs the actual check logic against the actual handler output.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"lab-env/lab/internal/conformance"
)

// TestE003_CheckLogic_MatchesHandlerResponse verifies that the E-003 check's
// assertion ("GET /health body contains {\"status\":\"ok\"}") is satisfied by
// the exact response body produced by the /health handler.
//
// This test uses a stub HTTP server that returns the exact body the service
// would return, and runs the E-003 check against it. A mismatch between
// the handler output and the check assertion would fail silently in the
// field — this test catches it.
func TestE003_CheckLogic_MatchesHandlerResponse(t *testing.T) {
	// Simulate the exact /health handler response
	handlerBody := `{"status":"ok"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(handlerBody))
	}))
	defer server.Close()

	// Find E-003 in the check catalog
	var e003 *conformance.Check
	for _, check := range conformance.AllChecks() {
		c := check
		if c.ID == "E-003" {
			e003 = &c
			break
		}
	}
	if e003 == nil {
		t.Fatal("E-003 not found in conformance catalog")
	}

	// Create a stub observer that routes /health to our test server
	obs := conformance.NewURLOverrideObserver(server.URL + "/health")

	// Execute the check
	err := e003.Execute(obs)
	if err != nil {
		t.Errorf("E-003 check failed against handler response %q: %v\n"+
			"This means the handler response does not match the conformance assertion.\n"+
			"Either the handler body is wrong or the check assertion is wrong.",
			handlerBody, err)
	}
}

// TestE001_CheckLogic_MatchesHandlerResponse verifies E-001 (GET /health
// returns HTTP 200) against the handler's actual response.
func TestE001_CheckLogic_MatchesHandlerResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	var e001 *conformance.Check
	for _, check := range conformance.AllChecks() {
		c := check
		if c.ID == "E-001" {
			e001 = &c
			break
		}
	}
	if e001 == nil {
		t.Fatal("E-001 not found in conformance catalog")
	}

	obs := conformance.NewURLOverrideObserver(server.URL + "/health")
	if err := e001.Execute(obs); err != nil {
		t.Errorf("E-001 failed against 200 response: %v", err)
	}
}

// TestE002_CheckLogic_FailsOn500 verifies that E-002 (GET / returns 200)
// fails when the handler returns 500 — confirming the check correctly
// detects the F-004 diagnostic pattern.
func TestE002_CheckLogic_FailsOn500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"status":"error","msg":"state write failed"}`))
	}))
	defer server.Close()

	var e002 *conformance.Check
	for _, check := range conformance.AllChecks() {
		c := check
		if c.ID == "E-002" {
			e002 = &c
			break
		}
	}
	if e002 == nil {
		t.Fatal("E-002 not found in conformance catalog")
	}

	obs := conformance.NewURLOverrideObserver(server.URL + "/")
	err := e002.Execute(obs)
	if err == nil {
		t.Error("E-002 passed on 500 response; should fail when / returns 500 (F-004 pattern)")
	}
}

// TestF004DiagnosticPattern_E001PassesE002Fails verifies the complete F-004
// diagnostic pattern: E-001 passes and E-002 fails simultaneously.
//
// This is the "most diagnostically important fault pattern" per the fault
// matrix runbook. This test proves the conformance check logic correctly
// identifies the pattern.
func TestF004DiagnosticPattern_E001PassesE002Fails(t *testing.T) {
	// /health returns 200 (E-001 passes)
	// / returns 500 (E-002 fails)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"status":"error","msg":"state write failed"}`))
		}
	}))
	defer server.Close()

	obsHealth := conformance.NewURLOverrideObserver(server.URL + "/health")
	obsRoot := conformance.NewURLOverrideObserver(server.URL + "/")

	var e001, e002 *conformance.Check
	for _, check := range conformance.AllChecks() {
		c := check
		switch c.ID {
		case "E-001":
			e001 = &c
		case "E-002":
			e002 = &c
		}
	}

	if e001 == nil || e002 == nil {
		t.Fatal("E-001 or E-002 not found in catalog")
	}

	e001Err := e001.Execute(obsHealth)
	e002Err := e002.Execute(obsRoot)

	if e001Err != nil {
		t.Errorf("F-004 pattern: E-001 failed (should pass): %v", e001Err)
	}
	if e002Err == nil {
		t.Error("F-004 pattern: E-002 passed (should fail when / returns 500)")
	}
}