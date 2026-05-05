package output

// render_test.go validates the output layer against the presentation rules:
//
//   - render only what the model already knows (no guessing)
//   - stdout carries data; stderr carries diagnostics; never mixed
//   - JSON schemas are stable (field names and nesting)
//   - human output is not tested for exact prose (implementation-defined)
//     but IS tested for structural completeness (required fields present)
//
// H-001 regression guard: status output must not contain guessed status
// codes. The test fixture provides explicit endpoint data; the output
// must reflect it exactly.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"lab_env/internal/conformance"
	. "lab_env/internal/output"
	"lab_env/internal/state"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newRenderer(format Format) (*Renderer, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	r := NewRenderer(stdout, stderr, format, false)
	return r, stdout, stderr
}

// ── StatusResult rendering ────────────────────────────────────────────────────

func TestRenderStatus_JSON_Schema(t *testing.T) {
	r, stdout, _ := newRenderer(FormatJSON)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v := StatusResult{
		State:       state.StateConformant,
		ActiveFault: nil,
		Services: map[string]SvcInfo{
			"app.service": {Active: true, PID: 4321},
			"nginx":       {Active: true},
		},
		Ports: []PortInfo{
			{Addr: "127.0.0.1:8080", Owner: "app"},
			{Addr: "0.0.0.0:80", Owner: "nginx"},
		},
		Endpoints: map[string]int{
			"http://localhost/health": 200,
			"http://localhost/":       200,
		},
		LastValidate: &ValidateSummary{At: ts, Passed: 23, Total: 23},
		LastReset:    &ResetSummary{At: ts, Tier: "R2"},
	}
	r.Render(v)

	var out map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error: %v\noutput: %s", err, stdout.String())
	}

	// Required top-level fields per control-plane-contract §4.1
	required := []string{"state", "active_fault", "services", "ports", "endpoints"}
	for _, field := range required {
		if _, ok := out[field]; !ok {
			t.Errorf("JSON output missing required field %q", field)
		}
	}

	// State value
	if out["state"] != "CONFORMANT" {
		t.Errorf("state = %v, want CONFORMANT", out["state"])
	}

	// active_fault must be null when no fault
	if out["active_fault"] != nil {
		t.Errorf("active_fault = %v, want null", out["active_fault"])
	}
}

func TestRenderStatus_JSON_WithActiveFault(t *testing.T) {
	r, stdout, _ := newRenderer(FormatJSON)

	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	v := StatusResult{
		State: state.StateDegraded,
		ActiveFault: &FaultRef{
			ID:        "F-004",
			AppliedAt: ts,
		},
		Services:  map[string]SvcInfo{},
		Ports:     []PortInfo{},
		Endpoints: map[string]int{},
	}
	r.Render(v)

	var out map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	if out["state"] != "DEGRADED" {
		t.Errorf("state = %v, want DEGRADED", out["state"])
	}

	fault, ok := out["active_fault"].(map[string]interface{})
	if !ok {
		t.Fatalf("active_fault is not an object: %v", out["active_fault"])
	}
	if fault["id"] != "F-004" {
		t.Errorf("active_fault.id = %v, want F-004", fault["id"])
	}
}

// H-001 regression guard: endpoint status codes must come from the model,
// not be guessed by the renderer. This test provides explicit endpoint data
// and verifies it is faithfully rendered without inference.
func TestRenderStatus_JSON_EndpointCodesNotGuessed(t *testing.T) {
	r, stdout, _ := newRenderer(FormatJSON)

	// Provide explicit endpoint data — renderer must use these values
	v := StatusResult{
		State:     state.StateBroken,
		Services:  map[string]SvcInfo{"app.service": {Active: false}},
		Ports:     []PortInfo{},
		Endpoints: map[string]int{
			"http://localhost/health": 502, // explicit — not guessed
			"http://localhost/":       0,   // unreachable — not guessed
		},
	}
	r.Render(v)

	var out map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	endpoints, ok := out["endpoints"].(map[string]interface{})
	if !ok {
		t.Fatalf("endpoints is not an object: %v", out["endpoints"])
	}

	// Must render the value provided, not a guess
	healthCode, _ := endpoints["http://localhost/health"].(float64)
	if int(healthCode) != 502 {
		t.Errorf("health endpoint code = %v, want 502 (must render model value, not guess)", healthCode)
	}
}

func TestRenderStatus_Unknown(t *testing.T) {
	r, stdout, _ := newRenderer(FormatHuman)

	v := StatusResult{Unknown: true}
	r.Render(v)

	output := stdout.String()
	if !strings.Contains(output, "UNKNOWN") {
		t.Errorf("unknown status output should contain UNKNOWN, got: %s", output)
	}
}

// ── ValidateResult rendering ──────────────────────────────────────────────────

func TestRenderValidate_JSON_Schema(t *testing.T) {
	r, stdout, _ := newRenderer(FormatJSON)

	sr := &conformance.SuiteResult{
		At:    time.Now().UTC(),
		Total: 23,
	}
	// Add a passing and failing check
	sr.Results = []conformance.CheckResult{
		{
			Check:  &conformance.Check{ID: "S-001", Severity: conformance.SeverityBlocking, Assertion: "app active"},
			Passed: true,
		},
		{
			Check:  &conformance.Check{ID: "E-001", Severity: conformance.SeverityBlocking, Assertion: "health 200"},
			Passed: false,
			Err:    nil,
		},
	}
	sr.Classify()

	v := FromSuiteResult(sr)
	r.Render(v)

	var out map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("JSON parse error: %v\noutput: %s", err, stdout.String())
	}

	required := []string{"at", "checks", "passed", "total", "classification"}
	for _, field := range required {
		if _, ok := out[field]; !ok {
			t.Errorf("validate JSON missing required field %q", field)
		}
	}

	checks, ok := out["checks"].([]interface{})
	if !ok {
		t.Fatal("checks field is not an array")
	}
	if len(checks) != 2 {
		t.Errorf("checks length = %d, want 2", len(checks))
	}

	// Each check item must have id, assertion, passed, severity
	for _, item := range checks {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatal("check item is not an object")
		}
		for _, f := range []string{"id", "assertion", "passed", "severity"} {
			if _, ok := m[f]; !ok {
				t.Errorf("check item missing field %q", f)
			}
		}
	}
}

// ── FaultListResult rendering ─────────────────────────────────────────────────

func TestRenderFaultList_JSON_Schema(t *testing.T) {
	r, stdout, _ := newRenderer(FormatJSON)

	v := FaultListResult{
		Total: 2,
		Faults: []FaultSummary{
			{ID: "F-001", Layer: "filesystem", Domain: []string{"linux"}, Symptom: "restart loop"},
			{ID: "F-004", Layer: "permissions", Domain: []string{"linux", "os"}, Symptom: "health 200 / 500"},
		},
	}
	r.Render(v)

	var out map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if out["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", out["total"])
	}
	faults, ok := out["faults"].([]interface{})
	if !ok || len(faults) != 2 {
		t.Errorf("faults = %v, want 2-element array", out["faults"])
	}
}

// ── FaultApplyResult rendering ────────────────────────────────────────────────

func TestRenderFaultApply_JSON_Schema(t *testing.T) {
	r, stdout, _ := newRenderer(FormatJSON)

	v := FaultApplyResult{
		FaultID:   "F-004",
		Applied:   true,
		FromState: state.StateConformant,
		ToState:   state.StateDegraded,
		Forced:    false,
	}
	r.Render(v)

	var out map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	if out["fault_id"] != "F-004" {
		t.Errorf("fault_id = %v, want F-004", out["fault_id"])
	}
	if out["applied"] != true {
		t.Errorf("applied = %v, want true", out["applied"])
	}
	if out["from_state"] != "CONFORMANT" {
		t.Errorf("from_state = %v, want CONFORMANT", out["from_state"])
	}
	if out["to_state"] != "DEGRADED" {
		t.Errorf("to_state = %v, want DEGRADED", out["to_state"])
	}
}

func TestRenderFaultApply_Aborted(t *testing.T) {
	r, stdout, _ := newRenderer(FormatHuman)

	v := FaultApplyResult{
		FaultID:     "F-008",
		Aborted:     true,
		AbortReason: "requires confirmation",
	}
	r.Render(v)

	if !strings.Contains(stdout.String(), "Aborted") {
		t.Errorf("aborted output should mention abort, got: %s", stdout.String())
	}
}

// ── Stream separation ─────────────────────────────────────────────────────────

func TestRenderer_ErrorGoesToStderr(t *testing.T) {
	r, stdout, stderr := newRenderer(FormatHuman)

	r.Error("something went wrong")

	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty when only Error() is called, got: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "something went wrong") {
		t.Errorf("error message not in stderr: %s", stderr.String())
	}
}

func TestRenderer_RenderGoesToStdout(t *testing.T) {
	r, stdout, stderr := newRenderer(FormatJSON)

	v := FaultListResult{Total: 0, Faults: []FaultSummary{}}
	r.Render(v)

	if stdout.Len() == 0 {
		t.Error("stdout should have content after Render()")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty after Render(), got: %s", stderr.String())
	}
}

func TestRenderer_QuietSuppressesOutput(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	r := NewRenderer(stdout, stderr, FormatHuman, true) // quiet=true

	v := StatusResult{State: state.StateConformant}
	r.Render(v)

	if stdout.Len() != 0 {
		t.Errorf("quiet mode should suppress output, got: %s", stdout.String())
	}
}

// ── FromSuiteResult conversion ────────────────────────────────────────────────

func TestFromSuiteResult_Completeness(t *testing.T) {
	sr := &conformance.SuiteResult{
		At:    time.Now().UTC(),
		Total: 2,
		Results: []conformance.CheckResult{
			{
				Check:  &conformance.Check{ID: "S-001", Severity: conformance.SeverityBlocking, Assertion: "app active"},
				Passed: true,
			},
			{
				Check:     &conformance.Check{ID: "E-001", Severity: conformance.SeverityBlocking, Assertion: "health 200"},
				Passed:    false,
				Dependent: true,
			},
		},
	}
	sr.Classify()

	vr := FromSuiteResult(sr)

	if len(vr.Checks) != 2 {
		t.Errorf("Checks length = %d, want 2", len(vr.Checks))
	}

	// First check: passed
	if !vr.Checks[0].Passed {
		t.Error("first check should be passed")
	}
	// Second check: failed and dependent
	if vr.Checks[1].Passed {
		t.Error("second check should be failed")
	}
	if !vr.Checks[1].Dependent {
		t.Error("second check should be marked dependent")
	}

	if vr.Classification == "" {
		t.Error("Classification should be set")
	}
}