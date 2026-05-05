package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Format selects the output renderer.
type Format int

const (
	FormatHuman Format = iota
	FormatJSON
)

// Renderer writes command results to stdout.
// Human-readable phrasing and formatting are implementation-defined
// (not frozen by the spec). JSON schemas are stable.
type Renderer struct {
	stdout io.Writer
	stderr io.Writer
	format Format
	quiet  bool
}

// NewRenderer returns a Renderer writing to the provided writers.
func NewRenderer(stdout, stderr io.Writer, format Format, quiet bool) *Renderer {
	return &Renderer{stdout: stdout, stderr: stderr, format: format, quiet: quiet}
}

// Render writes the result value to stdout. If format is JSON, the value
// is marshaled directly. If format is human, a formatted view is produced.
func (r *Renderer) Render(value interface{}) {
	if r.quiet {
		return
	}
	switch r.format {
	case FormatJSON:
		r.renderJSON(value)
	default:
		r.renderHuman(value)
	}
}

// Error writes an error message to stderr.
func (r *Renderer) Error(msg string) {
	fmt.Fprintln(r.stderr, msg)
}

// Errorf writes a formatted error message to stderr.
func (r *Renderer) Errorf(format string, args ...interface{}) {
	fmt.Fprintf(r.stderr, format+"\n", args...)
}

func (r *Renderer) renderJSON(value interface{}) {
	enc := json.NewEncoder(r.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintf(r.stderr, "error encoding JSON output: %v\n", err)
	}
}

func (r *Renderer) renderHuman(value interface{}) {
	switch v := value.(type) {
	case StatusResult:
		r.renderStatus(v)
	case ValidateResult:
		r.renderValidate(v)
	case FaultListResult:
		r.renderFaultList(v)
	case FaultInfoResult:
		r.renderFaultInfo(v)
	case FaultApplyResult:
		r.renderFaultApply(v)
	case ResetResult:
		r.renderReset(v)
	case HistoryResult:
		r.renderHistory(v)
	default:
		// Fallback: JSON even in human mode for unknown types.
		r.renderJSON(value)
	}
}

const sep = "──────────────────────────────────────────"

func (r *Renderer) renderStatus(v StatusResult) {
	fmt.Fprintln(r.stdout, "Lab Environment Status")
	fmt.Fprintln(r.stdout, sep)

	if v.Unknown {
		fmt.Fprintln(r.stdout, "State:  UNKNOWN (classification failed — run lab validate)")
		return
	}

	stateStr := string(v.State)
	fmt.Fprintf(r.stdout, "State:         %s\n", stateStr)

	if v.ActiveFault != nil {
		fmt.Fprintf(r.stdout, "Active fault:  %s (applied %s)\n",
			v.ActiveFault.ID, humanTime(v.ActiveFault.AppliedAt))
	} else {
		fmt.Fprintln(r.stdout, "Active fault:  none")
	}

	if v.LastValidate != nil {
		fmt.Fprintf(r.stdout, "Last validate: %s (%d/%d)\n",
			v.LastValidate.At.UTC().Format(time.RFC3339),
			v.LastValidate.Passed, v.LastValidate.Total)
	}
	if v.LastReset != nil {
		fmt.Fprintf(r.stdout, "Last reset:    %s (%s)\n",
			v.LastReset.At.UTC().Format(time.RFC3339), v.LastReset.Tier)
	}

	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Services")
	for name, svc := range v.Services {
		status := "● active"
		if !svc.Active {
			status = "✗ inactive"
		}
		if svc.PID > 0 {
			fmt.Fprintf(r.stdout, "  %-14s %s  pid=%d\n", name, status, svc.PID)
		} else {
			fmt.Fprintf(r.stdout, "  %-14s %s\n", name, status)
		}
	}

	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Ports")
	for _, p := range v.Ports {
		fmt.Fprintf(r.stdout, "  %-22s %s\n", p.Addr, p.Owner)
	}

	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Endpoints")
	for url, code := range v.Endpoints {
		icon := "✓"
		if code != 200 {
			icon = "✗"
		}
		fmt.Fprintf(r.stdout, "  %s %-36s %d\n", icon, url, code)
	}
	fmt.Fprintln(r.stdout, sep)
}

func (r *Renderer) renderValidate(v ValidateResult) {
	fmt.Fprintf(r.stdout, "Running conformance suite (%d checks)...\n\n", v.Total)
	for _, c := range v.Checks {
		fmt.Fprintln(r.stdout, r.formatCheckLine(c))
	}
	fmt.Fprintln(r.stdout)
	fmt.Fprintf(r.stdout, "%s  %d/%d checks passed\n", v.Classification, v.Passed, v.Total)
}

func (r *Renderer) formatCheckLine(c CheckResultItem) string {
	if c.Passed {
		return fmt.Sprintf("  [PASS] %-6s %s", c.ID, c.Assertion)
	}
	if c.Dependent {
		return fmt.Sprintf("  [SKIP] %-6s %s (dependent)", c.ID, c.Assertion)
	}
	return fmt.Sprintf("  [FAIL] %-6s %s — %s", c.ID, c.Assertion, c.Error)
}

func (r *Renderer) renderFaultList(v FaultListResult) {
	fmt.Fprintf(r.stdout, "Fault Catalog  (%d faults)\n", v.Total)
	fmt.Fprintln(r.stdout, strings.Repeat("─", 74))
	fmt.Fprintf(r.stdout, "%-6s %-13s %-20s %s\n", "ID", "Layer", "Domain", "Symptom")
	fmt.Fprintln(r.stdout, strings.Repeat("─", 74))
	for _, f := range v.Faults {
		domain := strings.Join(f.Domain, ", ")
		fmt.Fprintf(r.stdout, "%-6s %-13s %-20s %s\n", f.ID, f.Layer, domain, f.Symptom)
	}
	fmt.Fprintln(r.stdout, strings.Repeat("─", 74))
	fmt.Fprintln(r.stdout, "Apply a fault:   lab fault apply <ID>")
	fmt.Fprintln(r.stdout, "Fault details:   lab fault info <ID>")
}

func (r *Renderer) renderFaultInfo(v FaultInfoResult) {
	fmt.Fprintf(r.stdout, "Fault %s — %s\n", v.ID, v.Symptom)
	fmt.Fprintln(r.stdout, strings.Repeat("─", 74))
	fmt.Fprintf(r.stdout, "Layer:      %s\n", v.Layer)
	fmt.Fprintf(r.stdout, "Domain:     %s\n", strings.Join(v.Domain, ", "))
	fmt.Fprintf(r.stdout, "Reset tier: %s\n", v.ResetTier)
	fmt.Fprintln(r.stdout)
	fmt.Fprintf(r.stdout, "Mutation:\n  %s\n\n", v.MutationDisplay)
	fmt.Fprintf(r.stdout, "Symptom:\n  %s\n\n", v.Symptom)
	fmt.Fprintf(r.stdout, "Authoritative signal:\n  %s\n\n", v.AuthoritativeSignal)
	fmt.Fprintf(r.stdout, "Observable:\n  %s\n\n", v.Observable)
	fmt.Fprintf(r.stdout, "Reset action:\n  %s\n\n", v.ResetAction)
	fmt.Fprintf(r.stdout, "Confirmation required: %v\n", v.RequiresConfirmation)
	fmt.Fprintf(r.stdout, "Reversible:           %v\n", v.IsReversible)
}

func (r *Renderer) renderFaultApply(v FaultApplyResult) {
	if v.Aborted {
		fmt.Fprintf(r.stdout, "Aborted: %s\n", v.AbortReason)
		return
	}
	if !v.Applied {
		return
	}
	fmt.Fprintf(r.stdout, "State: %s → %s\n", v.FromState, v.ToState)
	fmt.Fprintf(r.stdout, "Active fault: %s\n", v.FaultID)
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Reset when done:  lab reset")
}

func (r *Renderer) renderReset(v ResetResult) {
	fmt.Fprintf(r.stdout, "Resetting environment (tier %s)...\n\n", v.Tier)
	if v.FaultCleared != "" {
		fmt.Fprintf(r.stdout, "  Fault cleared: %s\n", v.FaultCleared)
	}
	if v.Suite != nil {
		passing := v.Suite.Passed
		total := v.Suite.Total
		fmt.Fprintf(r.stdout, "  [%d/%d] conformance suite\n\n", passing, total)
	}
	fmt.Fprintf(r.stdout, "State: %s → %s\n", v.FromState, v.ToState)
	if v.FaultCleared != "" {
		fmt.Fprintln(r.stdout, "Active fault: none")
	}
	fmt.Fprintf(r.stdout, "Reset duration: %dms\n", v.DurationMs)
}

func (r *Renderer) renderHistory(v HistoryResult) {
	fmt.Fprintf(r.stdout, "State Transition History (last %d)\n", len(v.Entries))
	fmt.Fprintln(r.stdout, strings.Repeat("─", 74))
	fmt.Fprintf(r.stdout, "%-21s %-15s %-15s %s\n", "Timestamp", "From", "To", "Command")
	fmt.Fprintln(r.stdout, strings.Repeat("─", 74))
	for _, e := range v.Entries {
		ts := e.Ts.UTC().Format(time.RFC3339)
		fmt.Fprintf(r.stdout, "%-21s %-15s %-15s %s\n",
			ts, string(e.From), string(e.To), e.Command)
	}
	fmt.Fprintln(r.stdout, strings.Repeat("─", 74))
}

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return t.UTC().Format(time.RFC3339)
	}
}