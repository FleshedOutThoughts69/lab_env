package output

// golden_test.go validates JSON output schemas against frozen fixture files.
// These tests freeze the machine-consumable contract before client integration.
//
// What golden tests prove that schema tests do not:
//   - exact nullability (active_fault: null vs absent)
//   - exact nesting depth
//   - no accidental additive fields
//   - no structural rearrangement between releases
//
// Update fixture files when intentional schema changes are made.
// Unexpected test failures here indicate a breaking API change.
//
// Fixture files: testdata/golden/*.json
// Update mode: set UPDATE_GOLDEN=1 env var to regenerate fixtures.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lab_env/internal/conformance"
	. "lab_env/internal/output"
	"lab_env/internal/state"
)

const goldenDir = "../../testdata/golden"

// goldenTime is a fixed timestamp for deterministic fixture output.
var goldenTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// ── Fixture helpers ───────────────────────────────────────────────────────────

// loadGolden reads a golden fixture file and returns its content.
func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loading golden fixture %s: %v", name, err)
	}
	return data
}

// normalizeJSON round-trips JSON through marshal/unmarshal to normalize
// field ordering and whitespace for comparison.
func normalizeJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("normalizing JSON: %v\ndata: %s", err, data)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("re-marshaling JSON: %v", err)
	}
	return out
}

// compareGolden normalizes both actual and golden JSON and compares them.
// If UPDATE_GOLDEN=1, writes actual output to the fixture file.
func compareGolden(t *testing.T, fixtureName string, actual []byte) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		path := filepath.Join(goldenDir, fixtureName)
		normalized := normalizeJSON(t, actual)
		if err := os.WriteFile(path, normalized, 0644); err != nil {
			t.Fatalf("updating golden fixture %s: %v", fixtureName, err)
		}
		t.Logf("updated golden fixture: %s", fixtureName)
		return
	}

	expected := normalizeJSON(t, loadGolden(t, fixtureName))
	got := normalizeJSON(t, actual)

	if !bytes.Equal(expected, got) {
		t.Errorf("JSON output does not match golden fixture %s\n\nExpected:\n%s\n\nGot:\n%s",
			fixtureName, expected, got)
	}
}

// renderJSON renders a value to JSON using the Renderer.
func renderToJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	var buf bytes.Buffer
	var errBuf bytes.Buffer
	r := NewRenderer(&buf, &errBuf, FormatJSON, false)
	r.Render(value)
	if buf.Len() == 0 {
		t.Fatal("JSON renderer produced no output")
	}
	return buf.Bytes()
}

// ── Golden fixture tests ──────────────────────────────────────────────────────

func TestGolden_Status_Conformant(t *testing.T) {
	resetTime := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	v := StatusResult{
		State:       state.StateConformant,
		ActiveFault: nil,
		Services: map[string]SvcInfo{
			"app.service": {Active: true, PID: 4321},
			"nginx":       {Active: true, PID: 0},
		},
		Ports: []PortInfo{
			{Addr: "127.0.0.1:8080", Owner: "app"},
			{Addr: "0.0.0.0:80", Owner: "nginx"},
			{Addr: "0.0.0.0:443", Owner: "nginx (TLS)"},
		},
		Endpoints: map[string]int{
			"http://localhost/health": 200,
			"http://localhost/":       200,
		},
		LastValidate: &ValidateSummary{At: goldenTime, Passed: 23, Total: 23},
		LastReset:    &ResetSummary{At: resetTime, Tier: "R2"},
	}

	actual := renderToJSON(t, v)
	compareGolden(t, "status_conformant.json", actual)
}

func TestGolden_Status_Degraded(t *testing.T) {
	v := StatusResult{
		State: state.StateDegraded,
		ActiveFault: &FaultRef{
			ID:        "F-004",
			AppliedAt: goldenTime,
		},
		Services: map[string]SvcInfo{
			"app.service": {Active: true, PID: 4321},
			"nginx":       {Active: true, PID: 0},
		},
		Ports: []PortInfo{
			{Addr: "127.0.0.1:8080", Owner: "app"},
			{Addr: "0.0.0.0:80", Owner: "nginx"},
			{Addr: "0.0.0.0:443", Owner: "nginx (TLS)"},
		},
		Endpoints: map[string]int{
			"http://localhost/health": 200,
			"http://localhost/":       500,
		},
		LastValidate: nil,
		LastReset:    nil,
	}

	actual := renderToJSON(t, v)
	compareGolden(t, "status_degraded.json", actual)
}

func TestGolden_Status_Broken(t *testing.T) {
	v := StatusResult{
		State:       state.StateBroken,
		ActiveFault: nil,
		Services: map[string]SvcInfo{
			"app.service": {Active: false, PID: 0},
			"nginx":       {Active: true, PID: 0},
		},
		Ports: []PortInfo{
			{Addr: "0.0.0.0:80", Owner: "nginx"},
			{Addr: "0.0.0.0:443", Owner: "nginx (TLS)"},
		},
		Endpoints: map[string]int{
			"http://localhost/health": 502,
		},
		LastValidate: nil,
		LastReset:    nil,
	}

	actual := renderToJSON(t, v)
	compareGolden(t, "status_broken.json", actual)
}

func TestGolden_Validate_Conformant(t *testing.T) {
	checks := []CheckResultItem{
		{ID: "S-001", Assertion: "app.service is active",                      Passed: true, Severity: "blocking"},
		{ID: "S-002", Assertion: "app.service is enabled",                     Passed: true, Severity: "blocking"},
		{ID: "S-003", Assertion: "nginx is active",                            Passed: true, Severity: "blocking"},
		{ID: "S-004", Assertion: "nginx is enabled",                           Passed: true, Severity: "blocking"},
		{ID: "P-001", Assertion: "App process runs as appuser",                Passed: true, Severity: "blocking"},
		{ID: "P-002", Assertion: "App listens on 127.0.0.1:8080",             Passed: true, Severity: "blocking"},
		{ID: "P-003", Assertion: "nginx listens on 0.0.0.0:80",               Passed: true, Severity: "blocking"},
		{ID: "P-004", Assertion: "nginx listens on 0.0.0.0:443",              Passed: true, Severity: "blocking"},
		{ID: "E-001", Assertion: "GET /health returns HTTP 200",               Passed: true, Severity: "blocking"},
		{ID: "E-002", Assertion: "GET / returns HTTP 200",                     Passed: true, Severity: "blocking"},
		{ID: "E-003", Assertion: "/health body contains \"status\":\"ok\"",   Passed: true, Severity: "blocking"},
		{ID: "E-004", Assertion: "Response includes X-Proxy: nginx header",   Passed: true, Severity: "blocking"},
		{ID: "E-005", Assertion: "GET https://app.local/health returns 200 (skip verify)", Passed: true, Severity: "blocking"},
		{ID: "F-001", Assertion: "/opt/app/server exists, owned appuser:appuser, mode 750",         Passed: true, Severity: "blocking"},
		{ID: "F-002", Assertion: "/etc/app/config.yaml exists, owned appuser:appuser, mode 640",   Passed: true, Severity: "blocking"},
		{ID: "F-003", Assertion: "/var/log/app/ exists, owned appuser:appuser, mode 755",          Passed: true, Severity: "blocking"},
		{ID: "F-004", Assertion: "/var/lib/app/ exists, owned appuser:appuser, mode 755",          Passed: true, Severity: "blocking"},
		{ID: "F-005", Assertion: "nginx configuration passes syntax check",   Passed: true, Severity: "blocking"},
		{ID: "F-006", Assertion: "TLS certificate exists and has not expired", Passed: true, Severity: "degraded"},
		{ID: "F-007", Assertion: "app.local resolves to 127.0.0.1",           Passed: true, Severity: "blocking"},
		{ID: "L-001", Assertion: "/var/log/app/app.log exists and is non-empty", Passed: true, Severity: "degraded"},
		{ID: "L-002", Assertion: "Last line of app.log is valid JSON",        Passed: true, Severity: "degraded"},
		{ID: "L-003", Assertion: "app.log contains a startup entry",          Passed: true, Severity: "degraded"},
	}

	v := ValidateResult{
		At:             goldenTime,
		Checks:         checks,
		Passed:         23,
		Total:          23,
		Classification: "CONFORMANT",
		FailingChecks:  nil,
	}

	actual := renderToJSON(t, v)
	compareGolden(t, "validate_conformant.json", actual)
}

func TestGolden_FaultApply_Success(t *testing.T) {
	v := FaultApplyResult{
		FaultID:   "F-004",
		Applied:   true,
		FromState: state.StateConformant,
		ToState:   state.StateDegraded,
		Forced:    false,
	}

	actual := renderToJSON(t, v)
	compareGolden(t, "fault_apply_success.json", actual)
}

func TestGolden_FaultInfo_F004(t *testing.T) {
	v := FaultInfoResult{
		ID:                   "F-004",
		Layer:                "permissions",
		Domain:               []string{"linux", "os"},
		ResetTier:            "R2",
		RequiresConfirmation: false,
		IsReversible:         true,
		MutationDisplay:      "sudo chmod 000 /var/lib/app/",
		Symptom:              "/health returns 200; / returns 500; app continues running",
		AuthoritativeSignal:  "app.log",
		Observable:           "curl localhost/health → 200; curl localhost/ → 500; tail -5 /var/log/app/app.log shows \"level\":\"error\",\"msg\":\"state write failed\"",
		ResetAction:          "sudo chmod 755 /var/lib/app/",
	}

	actual := renderToJSON(t, v)
	compareGolden(t, "fault_info_f004.json", actual)
}

// ── Schema stability: no unexpected additive fields ───────────────────────────

// TestGolden_StatusResult_NoExtraFields verifies that the StatusResult JSON
// does not contain fields beyond what the schema defines. This catches
// accidental field additions that would break clients.
func TestGolden_StatusResult_NoExtraFields(t *testing.T) {
	v := StatusResult{
		State:       state.StateConformant,
		ActiveFault: nil,
		Services:    map[string]SvcInfo{},
		Ports:       []PortInfo{},
		Endpoints:   map[string]int{},
	}
	actual := renderToJSON(t, v)

	var m map[string]interface{}
	if err := json.Unmarshal(actual, &m); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Defined schema fields from control-plane-contract §4.1
	defined := map[string]bool{
		"state": true, "active_fault": true, "services": true,
		"ports": true, "endpoints": true, "last_validate": true,
		"last_reset": true, "reconciled": true, "unknown": true,
	}

	for field := range m {
		if !defined[field] {
			t.Errorf("StatusResult JSON contains unexpected field %q — schema drift", field)
		}
	}
}

func TestGolden_ValidateResult_NoExtraFields(t *testing.T) {
	sr := &conformance.SuiteResult{At: goldenTime, Total: 0}
	sr.Classify()
	v := FromSuiteResult(sr)
	actual := renderToJSON(t, v)

	var m map[string]interface{}
	if err := json.Unmarshal(actual, &m); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	defined := map[string]bool{
		"at": true, "checks": true, "passed": true,
		"total": true, "classification": true, "failing_checks": true,
	}
	for field := range m {
		if !defined[field] {
			t.Errorf("ValidateResult JSON contains unexpected field %q — schema drift", field)
		}
	}
}

func TestGolden_FaultApplyResult_NoExtraFields(t *testing.T) {
	v := FaultApplyResult{FaultID: "F-001", Applied: true,
		FromState: state.StateConformant, ToState: state.StateDegraded}
	actual := renderToJSON(t, v)

	var m map[string]interface{}
	json.Unmarshal(actual, &m)

	defined := map[string]bool{
		"fault_id": true, "applied": true, "from_state": true,
		"to_state": true, "forced": true, "aborted": true, "abort_reason": true,
	}
	for field := range m {
		if !defined[field] {
			t.Errorf("FaultApplyResult JSON contains unexpected field %q — schema drift", field)
		}
	}
}

// ── Nullability contract ──────────────────────────────────────────────────────

// TestGolden_ActiveFault_NullNotAbsent verifies that active_fault is
// serialized as JSON null (not absent) when no fault is active.
// This is the explicit null requirement from control-plane-contract §6.1.
func TestGolden_ActiveFault_NullNotAbsent(t *testing.T) {
	v := StatusResult{
		State:       state.StateConformant,
		ActiveFault: nil, // must render as null, not omitted
		Services:    map[string]SvcInfo{},
		Ports:       []PortInfo{},
		Endpoints:   map[string]int{},
	}
	actual := renderToJSON(t, v)

	var m map[string]interface{}
	json.Unmarshal(actual, &m)

	val, present := m["active_fault"]
	if !present {
		t.Error("active_fault must be present in JSON output even when null")
	}
	if val != nil {
		t.Errorf("active_fault = %v, want null when no fault is active", val)
	}
}