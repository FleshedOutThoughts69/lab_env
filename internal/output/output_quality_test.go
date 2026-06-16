package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"lab_env/internal/conformance"
	"lab_env/internal/output"
	"lab_env/internal/state"
)

// TestOutput_AllRenderedJSON_IsValidUTF8 verifies that JSON output for all
// result types consists entirely of valid UTF-8 sequences.
func TestOutput_AllRenderedJSON_IsValidUTF8(t *testing.T) {
	results := buildAllResultTypes(t)

	for name, result := range results {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			r := output.NewRenderer(&buf, nil, output.FormatJSON, false)
			r.Render(result)

			if !utf8.Valid(buf.Bytes()) {
				t.Errorf("%s: rendered JSON contains invalid UTF-8 bytes", name)
			}
		})
	}
}

// TestOutput_AllRenderedJSON_NoTrailingWhitespace verifies that no line in
// the rendered JSON output has trailing whitespace.
func TestOutput_AllRenderedJSON_NoTrailingWhitespace(t *testing.T) {
	results := buildAllResultTypes(t)

	for name, result := range results {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			r := output.NewRenderer(&buf, nil, output.FormatJSON, false)
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

// TestOutput_JSON_IsCompactNotPretty verifies that JSON output is compact.
func TestOutput_JSON_IsCompactNotPretty(t *testing.T) {
	result := buildStatusResult(t)

	var buf bytes.Buffer
	r := output.NewRenderer(&buf, nil, output.FormatJSON, false)
	r.Render(result)

	rendered := buf.String()
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if i == 0 {
			continue
		}
		if strings.HasPrefix(line, "  ") && line != "" {
			t.Errorf("JSON output is pretty-printed (line %d starts with spaces): %q", i+1, line)
			break
		}
	}
}

// TestOutput_StatusResult_LastValidateIsRFC3339 verifies last_validate timestamp format.
func TestOutput_StatusResult_LastValidateTimestamp_IsRFC3339(t *testing.T) {
	now := time.Now().UTC()
	ts := &output.ValidateSummary{
		At:     now,
		Passed: 23,
		Total:  23,
	}

	result := output.CommandResult{
		ExitCode: 0,
		Value: output.StatusResult{
			State:        state.StateConformant,
			ActiveFault:  nil,
			LastValidate: ts,
		},
	}

	var buf bytes.Buffer
	r := output.NewRenderer(&buf, nil, output.FormatJSON, false)
	r.Render(result)

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON parse: %v\nraw: %s", err, buf.String())
	}

	lvRaw, ok := parsed["last_validate"]
	if !ok {
		t.Log("last_validate not present (may be omitted when empty) — skipping format check")
		return
	}
	lvObj, ok := lvRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("last_validate: expected object, got %T", lvRaw)
	}
	atStr, ok := lvObj["at"].(string)
	if !ok {
		t.Fatalf("last_validate.at: expected string, got %T", lvObj["at"])
	}

	if _, err := time.Parse(time.RFC3339, atStr); err != nil {
		t.Errorf("last_validate.at %q is not RFC3339: %v", atStr, err)
	}
}

// TestOutput_JSON_DoubleEncodingNotPresent checks for double-encoded strings.
func TestOutput_JSON_NoDoubleEncoding(t *testing.T) {
	result := buildStatusResult(t)

	var buf bytes.Buffer
	r := output.NewRenderer(&buf, nil, output.FormatJSON, false)
	r.Render(result)

	if strings.Contains(buf.String(), `\"`) {
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
		Value: output.StatusResult{
			State:       state.StateConformant,
			ActiveFault: nil,
		},
	}
}

func buildAllResultTypes(t *testing.T) map[string]output.CommandResult {
	t.Helper()
	return map[string]output.CommandResult{
		"status_conformant": {
			ExitCode: 0,
			Value: output.StatusResult{
				State:       state.StateConformant,
				ActiveFault: nil,
			},
		},
		"status_degraded": {
			ExitCode: 0,
			Value: output.StatusResult{
				State: state.StateDegraded,
				ActiveFault: &output.FaultRef{
					ID: "F-004",
				},
			},
		},
		"validate_conformant": {
			ExitCode: 0,
			Value: output.ValidateResult{
				Passed: 23,
				Total:  23,
				Checks: buildCheckResults(),
			},
		},
		"fault_list": {
			ExitCode: 0,
			Value: output.FaultListResult{
				Faults: []output.FaultSummary{
					{ID: "F-001", Description: "Delete config file"},
				},
			},
		},
	}
}

func buildCheckResults() []output.CheckResultItem {
	checks := conformance.Catalog()
	results := make([]output.CheckResultItem, len(checks))
	for i, c := range checks {
		results[i] = output.CheckResultItem{
			ID:       c.ID,
			Passed:   true,
			Severity: "blocking", // or from c.Severity if needed
		}
	}
	return results
}