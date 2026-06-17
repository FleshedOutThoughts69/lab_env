package invariants

//go:generate go run ../../cmd/generate_spec_index/main.go

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ModuleRoot is the absolute path to the repository root.
var ModuleRoot string

func init() {
	_, file, _, _ := runtime.Caller(0)
	ModuleRoot = filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// SpecSection identifies one section of a semantic model document.
type SpecSection struct {
	Doc     string
	Section string
	Title   string
}

// SpecMapping maps one SpecSection to the files that implement it.
type SpecMapping struct {
	Spec        SpecSection
	ImplFiles   []string
	TestFiles   []string
	TestOnly    bool
	CrossRef    bool
	Constraints string
	Note        string
}

// SpecIndex is the complete reverse mapping.
var SpecIndex = []SpecMapping{

	// ── conformance-model.md ─────────────────────────────────────────────────

	{
		Spec: SpecSection{"conformance-model.md", "§3", "Check Catalog"},
		ImplFiles: []string{
			"internal/conformance/check.go",
			"internal/conformance/catalog.go",
		},
		TestFiles: []string{
			"internal/conformance/runner_test.go",
			"internal/catalog/catalog_test.go",
		},
	},
	{
		Spec: SpecSection{"conformance-model.md", "§4", "Validation Semantics"},
		ImplFiles: []string{
			"internal/conformance/result.go",
			"internal/conformance/runner.go",
		},
		TestFiles: []string{
			"internal/conformance/runner_test.go",
			"internal/conformance/runner_edge_cases_test.go",
		},
	},
	{
		Spec: SpecSection{"conformance-model.md", "§4.7", "Validation Output Schema"},
		ImplFiles: []string{
			"internal/output/model.go",
			"internal/output/render.go",
		},
		TestFiles: []string{
			"internal/output/golden_test.go",
			"internal/output/render_test.go",
		},
	},
	{
		Spec:      SpecSection{"conformance-model.md", "§5", "Model Completeness Condition"},
		TestFiles: []string{"internal/invariants/invariants_test.go"},
		TestOnly:  true,
	},

	// ── system-state-model.md ────────────────────────────────────────────────

	{
		Spec: SpecSection{"system-state-model.md", "§2", "State Definitions"},
		ImplFiles: []string{
			"internal/state/state.go",
		},
		TestFiles: []string{"internal/state/state_test.go"},
	},
	{
		Spec: SpecSection{"system-state-model.md", "§3", "Transition Model"},
		ImplFiles: []string{
			"cmd/fault.go",
			"cmd/reset.go",
			"internal/state/store.go",
			"internal/state/state.go",
		},
		TestFiles: []string{
			"cmd/fault_test.go",
			"cmd/interrupt_test.go",
			"internal/state/state_test.go",
		},
	},
	{
		Spec: SpecSection{"system-state-model.md", "§4", "State Detection"},
		ImplFiles: []string{
			"internal/state/detect.go",
			"cmd/status.go",
		},
		TestFiles: []string{
			"internal/state/detect_test.go",
			"internal/state/store_test.go",
			"cmd/status_test.go",
		},
	},
	{
		Spec: SpecSection{"system-state-model.md", "§5", "Constraint Graph"},
		ImplFiles: []string{
			"internal/state/state.go",
			"internal/state/store.go",
		},
		TestFiles: []string{
			"internal/state/state_test.go",
			"internal/state/store_test.go",
			"internal/invariants/invariants_test.go",
		},
	},

	// ── fault-model.md ───────────────────────────────────────────────────────

	{
		Spec: SpecSection{"fault-model.md", "§3", "Fault Schema"},
		ImplFiles: []string{
			"internal/catalog/fault.go",
		},
		TestFiles: []string{
			"internal/catalog/catalog_test.go",
			"internal/catalog/content_integrity_test.go",
		},
	},
	{
		Spec: SpecSection{"fault-model.md", "§4", "Mutation Rules"},
		ImplFiles: []string{
			"cmd/fault.go",
			"internal/catalog/faults.go",
			"internal/executor/executor.go",
		},
		TestFiles: []string{
			"cmd/fault_test.go",
			"internal/executor/trace_test.go",
			"internal/executor/embed_test.go",
		},
	},
	{
		Spec: SpecSection{"fault-model.md", "§5", "Pre/Post Conditions"},
		ImplFiles: []string{
			"cmd/fault.go",
			"internal/conformance/catalog.go",
		},
		TestFiles: []string{
			"cmd/fault_test.go",
			"internal/invariants/invariants_test.go",
		},
	},
	{
		Spec: SpecSection{"fault-model.md", "§6", "Reversibility Semantics"},
		ImplFiles: []string{
			"internal/catalog/faults.go",
			"cmd/reset.go",
		},
		TestFiles: []string{
			"internal/catalog/catalog_test.go",
			"internal/catalog/content_integrity_test.go",
		},
	},
	{
		Spec: SpecSection{"fault-model.md", "§7", "Fault Catalog"},
		ImplFiles: []string{
			"internal/catalog/faults.go",
		},
		TestFiles: []string{
			"internal/catalog/catalog_test.go",
			"internal/catalog/content_integrity_test.go",
			"internal/invariants/invariants_test.go",
		},
	},
	{
		Spec:      SpecSection{"fault-model.md", "§10", "Baseline Network Behaviours"},
		TestFiles: []string{"internal/invariants/invariants_test.go"},
		TestOnly:  true,
	},

	// ── control-plane-contract.md ────────────────────────────────────────────

	{
		Spec: SpecSection{"control-plane-contract.md", "§3", "Global Contract"},
		ImplFiles: []string{
			"app.go",
			"internal/output/render.go",
			"internal/executor/lock.go",
		},
		TestFiles: []string{
			"internal/executor/lock_test.go",
			"internal/executor/lock_stale_system_process_test.go",
			"internal/output/render_test.go",
		},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.1", "lab status"},
		ImplFiles: []string{"cmd/status.go"},
		TestFiles: []string{"cmd/status_test.go"},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.2", "lab validate"},
		ImplFiles: []string{"cmd/validate.go"},
		TestFiles: []string{"cmd/validate_test.go"},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.3", "lab fault list"},
		ImplFiles: []string{"cmd/fault.go"},
		TestFiles: []string{"cmd/fault_test.go"},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.4", "lab fault info"},
		ImplFiles: []string{"cmd/fault.go"},
		TestFiles: []string{"cmd/fault_test.go"},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.5", "lab fault apply"},
		ImplFiles: []string{"cmd/fault.go"},
		TestFiles: []string{
			"cmd/fault_test.go",
			"cmd/interrupt_test.go",
			"cmd/live_fault_matrix_test.go",
		},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.6", "lab reset"},
		ImplFiles: []string{"cmd/reset.go"},
		TestFiles: []string{"cmd/live_fault_matrix_test.go"},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.7", "lab provision"},
		ImplFiles: []string{"cmd/reset_provision_history.go"},
		TestFiles: []string{},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.8", "lab history"},
		ImplFiles: []string{"cmd/reset_provision_history.go"},
		TestFiles: []string{},
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§5", "Executor Behavioral Contract"},
		ImplFiles: []string{
			"internal/executor/executor.go",
			"internal/executor/real.go",
		},
		TestFiles: []string{
			"internal/executor/audit_test.go",
			"internal/executor/trace_test.go",
			"internal/executor/embed_test.go",
			"internal/executor/restore_test.go",
		},
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§6", "State File Contract"},
		ImplFiles: []string{
			"internal/state/store.go",
		},
		TestFiles: []string{
			"internal/state/store_test.go",
			"internal/state/store_edge_cases_test.go",
		},
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§7", "Audit Log Contract"},
		ImplFiles: []string{
			"internal/executor/audit.go",
		},
		TestFiles: []string{
			"internal/executor/audit_test.go",
			"internal/executor/mutation_failure_test.go",
		},
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§8", "Error Catalog"},
		ImplFiles: []string{"cmd/fault.go", "internal/executor/lock.go"},
		TestFiles: []string{"cmd/fault_test.go"},
	},
	{
		Spec:     SpecSection{"control-plane-contract.md", "§9", "Conformance with Semantic Models"},
		CrossRef: true,
	},

	// ── canonical-enviornment-lab.md ─────────────────────────────────────────

	{
		Spec: SpecSection{"canonical-enviornment-lab.md", "§2", "Canonical Environment Contract"},
		ImplFiles: []string{
			"internal/config/config.go",
		},
		TestFiles: []string{
			"internal/executor/embed_test.go",
		},
		Constraints: "constants (internal/config/config.go) + provisioning (scripts/bootstrap.sh) + verification (conformance checks)",
	},
	{
		Spec: SpecSection{"canonical-enviornment-lab.md", "§3", "Go Service Interface Contract"},
		ImplFiles: []string{
			"service/main.go",
			"service/server/server.go",
			"service/logging/logging.go",
			"service/signals/signals.go",
		},
		TestFiles: []string{
			"service/server/server_test.go",
			"service/server/server_edge_cases_test.go",
			"service/signals/signals_test.go",
			"service/logging/logging_test.go",
		},
	},
	{
		Spec: SpecSection{"canonical-enviornment-lab.md", "§4", "Canonical Artifact Contents"},
		ImplFiles: []string{
			"internal/config/app.service",
			"internal/config/config.yaml",
			"internal/config/nginx.conf",
			"internal/config/logrotate.conf",
			"service/config/config.go",
		},
		TestFiles: []string{
			"internal/executor/embed_test.go",
			"service/config/config_test.go",
		},
		Constraints: "embedded content (internal/config/*) + parser enforcement (service/config/config.go) + R2 restore (internal/executor/canonical_files.go)",
	},
	{
		Spec: SpecSection{"canonical-enviornment-lab.md", "§5", "Provisioning Contract"},
		ImplFiles: []string{
			"scripts/bootstrap.sh",
		},
		TestFiles:   []string{"cmd/live_fault_matrix_test.go"},
		Constraints: "script (scripts/bootstrap.sh) + idempotency strategy (docs/provisioning-blueprint.md) + final gate (lab validate)",
	},
	{
		Spec: SpecSection{"canonical-enviornment-lab.md", "§8", "State Control"},
		ImplFiles: []string{
			"cmd/reset.go",
			"scripts/reset.sh",
		},
		TestFiles: []string{"cmd/live_fault_matrix_test.go"},
	},
}

// DocOrder defines the canonical display order of the five semantic documents.
var DocOrder = []string{
	"conformance-model.md",
	"system-state-model.md",
	"fault-model.md",
	"control-plane-contract.md",
	"canonical-enviornment-lab.md",
}

// GenerateMarkdown produces the canonical markdown for the index section.
func GenerateMarkdown() string {
	var sb strings.Builder

	sb.WriteString("<!-- BEGIN GENERATED: Specification → Implementation Index -->\n")
	sb.WriteString("## Specification → Implementation Index\n\n")
	sb.WriteString("> **Source of truth:** `internal/invariants/spec_index.go` — the Go data structure that backs this table.\n")

	byDoc := make(map[string][]SpecMapping)
	for _, m := range SpecIndex {
		byDoc[m.Spec.Doc] = append(byDoc[m.Spec.Doc], m)
	}

	for _, doc := range DocOrder {
		entries, ok := byDoc[doc]
		if !ok {
			continue
		}
		sb.WriteString("---\n\n### `" + doc + "`\n\n")
		sb.WriteString("| Section | Title | Primary implementation | Enforcing tests | Notes |\n")
		sb.WriteString("|---|---|---|---|---|\n")
		for _, m := range entries {
			impl := renderImpl(m)
			tests := renderTests(m)
			note := m.Note
			sb.WriteString("| " + m.Spec.Section + " | " + m.Spec.Title + " | " + impl + " | " + tests + " | " + note + " |\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("<!-- END GENERATED: Specification → Implementation Index -->\n")
	return sb.String()
}

func renderImpl(m SpecMapping) string {
	if m.CrossRef { return "→ (cross-reference)" }
	if m.TestOnly { return "→ (test-only)" }
	if m.Constraints != "" { return "constraints: " + m.Constraints }
	if len(m.ImplFiles) == 0 { return "—" }
	parts := make([]string, len(m.ImplFiles))
	for i, f := range m.ImplFiles { parts[i] = "`" + f + "`" }
	return strings.Join(parts, " · ")
}

func renderTests(m SpecMapping) string {
	if len(m.TestFiles) == 0 { return "—" }
	parts := make([]string, len(m.TestFiles))
	for i, f := range m.TestFiles {
		base := f
		if idx := strings.LastIndex(f, "/"); idx >= 0 { base = f[idx+1:] }
		parts[i] = "`" + base + "`"
	}
	return strings.Join(parts, " · ")
}

// GenerateSpecIndex rewrites the Specification → Implementation Index section
// in docs/codebase-reference.md.
func GenerateSpecIndex() error {
	docPath := filepath.Join(ModuleRoot, "docs", "codebase-reference.md")
	data, err := os.ReadFile(docPath)
	if err != nil { return fmt.Errorf("read %s: %w", docPath, err) }
	content := string(data)

	beginGuard := "<!-- BEGIN GENERATED: Specification → Implementation Index -->"
	endGuard   := "<!-- END GENERATED: Specification → Implementation Index -->"

	beginIdx := strings.Index(content, beginGuard)
	endIdx   := strings.Index(content, endGuard)

	var before, after string
	if beginIdx >= 0 && endIdx >= 0 && endIdx > beginIdx {
		before = strings.TrimRight(content[:beginIdx], "\n")
		after  = strings.TrimLeft(content[endIdx+len(endGuard):], "\n")
	} else {
		marker := "\n---\n\n## Specification → Implementation Index\n"
		idx := strings.Index(content, marker)
		if idx < 0 {
			marker2 := "## Specification → Implementation Index\n"
			idx = strings.Index(content, marker2)
			if idx < 0 {
				return fmt.Errorf("neither guard markers nor fallback heading found")
			}
			before = strings.TrimRight(content[:idx], "\n")
		} else {
			before = strings.TrimRight(content[:idx], "\n")
		}
		after = ""
	}

	generated := GenerateMarkdown()
	var newContent string
	if after != "" {
		newContent = before + "\n\n" + generated + "\n\n" + after
	} else {
		newContent = before + "\n\n" + generated + "\n"
	}
	if err := os.WriteFile(docPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("write %s: %w", docPath, err)
	}
	fmt.Printf("regenerated spec index in %s\n", docPath)
	return nil
}