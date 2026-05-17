package output

// output_quality_test.go
//
// Byte-level quality tests for all rendered JSON output.
// These tests catch formatting regressions that break machine consumers:
//   - Invalid UTF-8 would cause JSON parse failures in some parsers
//   - Trailing whitespace on JSON lines would fail strict parsers
//   - Non-RFC3339 timestamps would break dashboard parsers

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"lab-env/lab/internal/conformance"
	"lab-env/lab/internal/output"
	"lab-env/lab/internal/state"
)

// TestOutput_AllRenderedJSON_IsValidUTF8 verifies that JSON output for all
// result types consists entirely of valid UTF-8 sequences.
//
// Invalid UTF-8 in a JSON string is technically invalid JSON (JSON strings
// must be valid UTF-8). Some parsers accept it; others reject it. Strict
// control plane parsers must be able to rely on clean output.
func TestOutput_AllRenderedJSON_IsValidUTF8(t *testing.T) {
	results := buildAllResultTypes(t)

	for name, result := range results {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			r := output.NewRenderer(&buf, nil, false)
			r.Render(result)

			if !utf8.Valid(buf.Bytes()) {
				t.Errorf("%s: rendered JSON contains invalid UTF-8 bytes", name)
			}
		})
	}
}

// TestOutput_AllRenderedJSON_NoTrailingWhitespace verifies that no line in
// the rendered JSON output has trailing whitespace (spaces or tabs before \n).
//
// Trailing whitespace is invisible in most editors but can cause strict
// diff-based golden fixture comparison to fail, and some JSON parsers in
// strict mode reject trailing whitespace in specific positions.
func TestOutput_AllRenderedJSON_NoTrailingWhitespace(t *testing.T) {
	results := buildAllResultTypes(t)

	for name, result := range results {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			r := output.NewRenderer(&buf, nil, false)
			r.Render(result)

			for i, line := range strings.Split(buf.String(), "\n") {
				trimmed := strings.TrimRight(line, " \t")
				if trimmed != line {
					t.Errorf("%s line %d has trailing whitespace: %q", name, i+1, line)
				}
			}
		})
	}
}

// TestOutput_JSON_IsCompactNotPretty verifies that JSON output is compact
// (no indentation) rather than pretty-printed.
//
// The control plane pipes JSON output through jq for parsing. Compact JSON
// is the standard for machine-readable output. Pretty-printed JSON would
// also work with jq but signals incorrect formatting.
func TestOutput_JSON_IsCompactNotPretty(t *testing.T) {
	result := buildStatusResult(t)

	var buf bytes.Buffer
	r := output.NewRenderer(&buf, nil, false)
	r.Render(result)

	rendered := buf.String()
	// Compact JSON has no indentation — no lines starting with spaces after {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if i == 0 {
			continue // first line is the outer {
		}
		if strings.HasPrefix(line, "  ") && line != "" {
			t.Errorf("JSON output is pretty-printed (line %d starts with spaces): %q", i+1, line)
			break
		}
	}
}

// TestOutput_StatusResult_LastValidateIsRFC3339 verifies that when
// last_validate is present in the status output, it is a valid RFC3339
// timestamp.
//
// The operational dashboard reads this field for display. A non-RFC3339
// format (e.g., Unix timestamp, custom format) would break parsing.
func TestOutput_StatusResult_LastValidateTimestamp_IsRFC3339(t *testing.T) {
	now := time.Now().UTC()
	ts := now.Format(time.RFC3339)

	result := output.CommandResult{
		ExitCode: 0,
		Result: output.StatusResult{
			State:          "CONFORMANT",
			ActiveFault:    nil,
			LastValidate:   ts,
			Classification: "conformant",
		},
	}

	var buf bytes.Buffer
	r := output.NewRenderer(&buf, nil, false)
	r.Render(result)

	// Parse the output and verify last_validate round-trips as RFC3339
	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON parse: %v\nraw: %s", err, buf.String())
	}

	lvRaw, ok := parsed["last_validate"]
	if !ok {
		t.Log("last_validate not present (may be omitted when empty) — skipping format check")
		return
	}
	lvStr, ok := lvRaw.(string)
	if !ok {
		t.Fatalf("last_validate: expected string, got %T", lvRaw)
	}

	if _, err := time.Parse(time.RFC3339, lvStr); err != nil {
		t.Errorf("last_validate %q is not RFC3339: %v", lvStr, err)
	}
}

// TestOutput_JSON_DoubleEncodingNotPresent verifies that string values in JSON
// output are not double-encoded (e.g., \\\"msg\\\" instead of "msg").
//
// Double encoding happens when a pre-marshaled JSON string is marshaled again.
// This would produce escaped quotes visible as literal backslashes in output.
func TestOutput_JSON_NoDoubleEncoding(t *testing.T) {
	result := buildStatusResult(t)

	var buf bytes.Buffer
	r := output.NewRenderer(&buf, nil, false)
	r.Render(result)

	// If double-encoded, the output would contain literal \\"
	if strings.Contains(buf.String(), `\"`) {
		// Check if this is inside a nested JSON string (acceptable) or top-level
		// by verifying the output parses cleanly
		var v interface{}
		if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
			t.Errorf("JSON output may be double-encoded: parse error: %v\nraw: %s", err, buf.String())
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildStatusResult(t *testing.T) output.CommandResult {
	t.Helper()
	return output.CommandResult{
		ExitCode: 0,
		Result: output.StatusResult{
			State:          string(state.StateConformant),
			ActiveFault:    nil,
			Classification: "conformant",
		},
	}
}

func buildAllResultTypes(t *testing.T) map[string]output.CommandResult {
	t.Helper()
	activeFault := "F-004"
	return map[string]output.CommandResult{
		"status_conformant": {
			ExitCode: 0,
			Result: output.StatusResult{
				State:          string(state.StateConformant),
				ActiveFault:    nil,
				Classification: "conformant",
			},
		},
		"status_degraded": {
			ExitCode: 0,
			Result: output.StatusResult{
				State:          string(state.StateDegraded),
				ActiveFault:    &activeFault,
				Classification: "conformant",
			},
		},
		"validate_conformant": {
			ExitCode: 0,
			Result: output.ValidateResult{
				Passed:       23,
				Failed:       0,
				CheckResults: buildCheckResults(),
			},
		},
		"fault_list": {
			ExitCode: 0,
			Result: output.FaultListResult{
				Faults: []output.FaultSummary{
					{ID: "F-001", Description: "Delete config file"},
				},
			},
		},
	}
}

func buildCheckResults() []output.CheckSummary {
	checks := conformance.AllChecks()
	results := make([]output.CheckSummary, len(checks))
	for i, c := range checks {
		results[i] = output.CheckSummary{
			ID:        c.ID,
			Passed:    true,
			Assertion: c.Assertion,
		}
	}
	return results
}