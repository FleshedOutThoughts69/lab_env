package invariants

//go:generate go run generate_spec_index.go

import "strings"

// spec_index.go declares the reverse mapping between semantic model document
// sections and the files that implement them.
//
// This file is the authoritative source of truth for the reverse index.
// The markdown document (docs/codebase-reference.md §Spec→Implementation index)
// is generated from these declarations. The test file spec_index_test.go
// verifies every file reference exists on disk.
//
// Maintenance contract:
//   - When a file is renamed: update the path here.
//   - When a new spec section is implemented: add a SpecMapping entry.
//   - When a section changes authority: update the doc + section fields.
//   - Run: go test ./internal/invariants/ -run TestSpecIndex to re-verify.
//   - Run: go generate ./internal/invariants/ to regenerate the markdown table.
//
// Notation conventions used in SpecMapping:
//
//   ImplFiles   — production .go files, config templates, scripts
//   TestFiles   — *_test.go files that enforce this section
//   TestOnly    — true when no production file implements the section
//                 (the section is enforced entirely by tests)
//   CrossRef    — true when the section is a reference to other documents,
//                 not an independent implementation target
//   Constraints — used for canonical-environment sections enforced by
//                 multiple layers (values + provisioning + verification)

// SpecSection identifies one section of a semantic model document.
type SpecSection struct {
	Doc     string // document filename without path e.g. "conformance-model.md"
	Section string // section identifier e.g. "§3" or "§4.1"
	Title   string // section title for human readability
}

// SpecMapping maps one SpecSection to the files that implement it.
type SpecMapping struct {
	Spec        SpecSection
	ImplFiles   []string // relative to module root
	TestFiles   []string // relative to module root
	TestOnly    bool     // no production code; enforced by tests alone
	CrossRef    bool     // reference section; points to other documents
	Constraints string   // for layered enforcement; overrides ImplFiles for display
	Note        string   // short clarifying note
}

// SpecIndex is the complete reverse mapping. Every entry has been verified
// against the actual file tree and source code comments.
//
// Integrity guarantee: the CI pipeline runs TestSpecIndex* on every push.
// A passing build means all file references are verified and the committed
// markdown matches GenerateMarkdown() output.
var SpecIndex = []SpecMapping{

	// ── conformance-model.md ─────────────────────────────────────────────────

	{
		Spec: SpecSection{"conformance-model.md", "§3", "Check Catalog"},
		ImplFiles: []string{
			"internal/conformance/check.go",   // Check type, Severity, Category, Layer
			"internal/conformance/catalog.go", // 23 check implementations + CheckByID
		},
		TestFiles: []string{
			"internal/conformance/runner_test.go",
			"internal/catalog/catalog_test.go",
		},
		Note: "§3.1 defines the Check schema; §3.2–§3.7 are the catalog entries themselves",
	},
	{
		Spec: SpecSection{"conformance-model.md", "§4", "Validation Semantics"},
		ImplFiles: []string{
			"internal/conformance/result.go", // V-1/V-2/V-3 verdict derivation; Classify()
			"internal/conformance/runner.go", // execution order; dependency marking; exit codes
		},
		TestFiles: []string{
			"internal/conformance/runner_test.go",
			"internal/conformance/runner_edge_cases_test.go",
		},
		Note: "§4.1 verdict rules → result.go; §4.4 ordering + dependent marking → runner.go; §4.7 output schema → output/model.go",
	},
	{
		Spec: SpecSection{"conformance-model.md", "§4.7", "Validation Output Schema"},
		ImplFiles: []string{
			"internal/output/model.go",   // ValidateResult JSON shape
			"internal/output/render.go",  // renderer
		},
		TestFiles: []string{
			"internal/output/golden_test.go",
			"internal/output/render_test.go",
		},
		Note: "§4.7 is the output subset; the full in-memory representation is SuiteResult in result.go",
	},
	{
		Spec:      SpecSection{"conformance-model.md", "§5", "Model Completeness Condition"},
		TestFiles: []string{"internal/invariants/invariants_test.go"},
		TestOnly:  true,
		Note:      "Bidirectional completeness (FailingChecks ↔ fault catalog) enforced by TestInvariant_FaultFailingChecks_ExistInCatalog",
	},

	// ── system-state-model.md ────────────────────────────────────────────────

	{
		Spec: SpecSection{"system-state-model.md", "§2", "State Definitions"},
		ImplFiles: []string{
			"internal/state/state.go", // six State constants; All(); IsValid(); guard methods
		},
		TestFiles: []string{"internal/state/state_test.go"},
		Note:      "Six constants, invariant methods (RequiresActiveFault, ForbidsActiveFault, CanApplyFault, CanReset) all in state.go",
	},
	{
		Spec: SpecSection{"system-state-model.md", "§3", "Transition Model"},
		ImplFiles: []string{
			"cmd/fault.go",              // CONFORMANT→DEGRADED transition (fault apply)
			"cmd/reset.go",              // DEGRADED/BROKEN→RECOVERING→CONFORMANT
			"internal/state/store.go",   // atomic state write; logical atomicity guarantee
			"internal/state/state.go",   // CanApplyFault, CanReset guards
		},
		TestFiles: []string{
			"cmd/fault_test.go",
			"cmd/interrupt_test.go",
			"internal/state/state_test.go",
		},
		Note: "Logical atomicity (§3.1) → store.go; guard checks (§3.4) → state.go; transitions themselves → cmd/fault.go + cmd/reset.go",
	},
	{
		Spec: SpecSection{"system-state-model.md", "§4", "State Detection"},
		ImplFiles: []string{
			"internal/state/detect.go", // detection algorithm §4.2; conflict resolution §4.3
			"cmd/status.go",            // LightweightRun invocation; reconciliation write
		},
		TestFiles: []string{
			"internal/state/detect_test.go",
			"internal/state/signal_combinations_test.go",
			"cmd/status_test.go",
		},
		Note: "§4.1 authority precedence + §4.2 algorithm + §4.3 four conflict cases all in detect.go; §4.4 UNKNOWN → exit 5 in status.go",
	},
	{
		Spec: SpecSection{"system-state-model.md", "§5", "Constraint Graph"},
		ImplFiles: []string{
			"internal/state/state.go",  // I-2: RequiresActiveFault / ForbidsActiveFault
			"internal/state/store.go",  // I-1: ClassificationValid; I-3: ring buffer cap
		},
		TestFiles: []string{
			"internal/state/state_test.go",
			"internal/state/store_test.go",
			"internal/invariants/invariants_test.go",
		},
		Note: "I-2 (active_fault constraint) → state.go; I-1 (classification_valid) + I-3 (history cap) → store.go",
	},

	// ── fault-model.md ───────────────────────────────────────────────────────

	{
		Spec: SpecSection{"fault-model.md", "§3", "Fault Schema"},
		ImplFiles: []string{
			"internal/catalog/fault.go", // FaultDef; FaultImpl; PostconditionSpec
		},
		TestFiles: []string{
			"internal/catalog/catalog_test.go",
			"internal/catalog/content_integrity_test.go",
		},
		Note: "§3.1 FaultDef fields → fault.go; §3.2 PostconditionSpec → fault.go; FaultImpl (Apply/Recover) also in fault.go",
	},
	{
		Spec: SpecSection{"fault-model.md", "§4", "Mutation Rules"},
		ImplFiles: []string{
			"cmd/fault.go",                 // §4.2 logical atomicity of Apply
			"internal/catalog/faults.go",   // §4.1 executor-only mutations in all Apply/Recover
			"internal/executor/executor.go", // the Executor interface itself
		},
		TestFiles: []string{
			"cmd/fault_test.go",
			"internal/executor/boundary_test.go",
			"internal/executor/trace_test.go",
		},
		Note: "§4.1 executor requirement enforced structurally by type system; §4.2 atomicity contract → cmd/fault.go (ApplyFailure test)",
	},
	{
		Spec: SpecSection{"fault-model.md", "§5", "Pre/Post Conditions"},
		ImplFiles: []string{
			"cmd/fault.go",                // 6-step precondition sequence; PreconditionChecks enforcement
			"internal/conformance/catalog.go", // CheckByID used to resolve PreconditionChecks
		},
		TestFiles: []string{
			"cmd/fault_test.go",
			"internal/invariants/invariants_test.go",
		},
		Note: "§5.1 standard precondition (steps 2-4) → fault.go; §5.2 PreconditionChecks (step 5) → fault.go + catalog.go; §5.3 postcondition → catalog/fault.go",
	},
	{
		Spec: SpecSection{"fault-model.md", "§6", "Reversibility Semantics"},
		ImplFiles: []string{
			"internal/catalog/faults.go", // IsReversible; non-reversible Recover returns error
			"cmd/reset.go",               // selectTier reads ResetTier from fault def
		},
		TestFiles: []string{
			"internal/catalog/catalog_test.go",
			"internal/catalog/content_integrity_test.go",
		},
	},
	{
		Spec: SpecSection{"fault-model.md", "§7", "Fault Catalog"},
		ImplFiles: []string{
			"internal/catalog/faults.go", // 16 fault constructors F-001–F-010, F-013–F-018
		},
		TestFiles: []string{
			"internal/catalog/catalog_test.go",
			"internal/catalog/content_integrity_test.go",
			"internal/invariants/invariants_test.go",
		},
		Note: "§7.2 is the canonical catalog; each fault constructor is the mechanical projection of the corresponding §7.2 entry",
	},
	{
		Spec:      SpecSection{"fault-model.md", "§10", "Baseline Network Behaviours"},
		TestFiles: []string{"internal/invariants/invariants_test.go"},
		TestOnly:  true,
		Note:      "B-001/B-002 are absent from the Go catalog by design; TestInvariant_NoBaselineFaultsInCatalog enforces absence",
	},

	// ── control-plane-contract.md ────────────────────────────────────────────

	{
		Spec: SpecSection{"control-plane-contract.md", "§3", "Global Contract"},
		ImplFiles: []string{
			"app.go",                     // §3.1 invocation model; §3.4 global flags
			"internal/output/render.go",  // §3.3 stdout/stderr stream discipline
			"internal/executor/lock.go",  // §3.5 advisory lock; no waiting
		},
		TestFiles: []string{
			"internal/executor/lock_test.go",
			"internal/executor/lock_stale_system_process_test.go",
			"internal/output/render_test.go",
		},
		Note: "§3.2 exit code table is distributed across all cmd/ files; §3.6 signal handling is the interrupt path (see §4.5)",
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§4.1", "lab status"},
		ImplFiles: []string{"cmd/status.go"},
		TestFiles: []string{"cmd/status_test.go"},
		Note:      "Reconciliation authority; only command that updates state classification; LightweightRun + Detect",
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§4.2", "lab validate"},
		ImplFiles: []string{"cmd/validate.go"},
		TestFiles: []string{"cmd/validate_test.go"},
		Note:      "Observation-only; updates last_validate; must not update state field",
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.3", "lab fault list"},
		ImplFiles: []string{"cmd/fault.go"},
		TestFiles: []string{"cmd/fault_test.go"},
		Note:      "FaultListCmd; AllDefs() only; no executor dependency",
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.4", "lab fault info"},
		ImplFiles: []string{"cmd/fault.go"},
		TestFiles: []string{"cmd/fault_test.go"},
		Note:      "FaultInfoCmd; DefByID() only; no executor dependency",
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§4.5", "lab fault apply"},
		ImplFiles: []string{"cmd/fault.go"},
		TestFiles: []string{
			"cmd/fault_test.go",
			"cmd/interrupt_test.go",
			"cmd/live_fault_matrix_test.go",
		},
		Note: "6-step precondition sequence; PreconditionChecks (step 5); atomicity; audit; interrupt path (§3.6) handled here",
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§4.6", "lab reset"},
		ImplFiles: []string{"cmd/reset.go"},
		TestFiles: []string{"cmd/live_fault_matrix_test.go"},
		Note:      "R1/R2/R3 tiers; auto-select from fault ResetTier; post-reset validation always runs",
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.7", "lab provision"},
		ImplFiles: []string{"cmd/reset_provision_history.go"},
		TestFiles: []string{},
		Note:      "Delegates to bootstrap.sh via RunMutation; idempotent",
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§4.8", "lab history"},
		ImplFiles: []string{"cmd/reset_provision_history.go"},
		TestFiles: []string{},
		Note:      "Ring buffer read; reverse chronological; read-only; no lock",
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§5", "Executor Behavioral Contract"},
		ImplFiles: []string{
			"internal/executor/executor.go", // interface declaration + method contracts
			"internal/executor/real.go",     // concrete implementation
		},
		TestFiles: []string{
			"internal/executor/audit_test.go",
			"internal/executor/boundary_test.go",
			"internal/executor/trace_test.go",
			"internal/executor/embed_test.go",
			"internal/executor/restore_test.go",
		},
		Note: "§5.1 capabilities → executor.go interface; §5.2 audit obligation → audit.go; §5.3 ordering → trace_test.go; §5.5 privilege → real.go (runSudo)",
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§6", "State File Contract"},
		ImplFiles: []string{
			"internal/state/store.go", // File schema; atomic write; InvalidateClassification
		},
		TestFiles: []string{
			"internal/state/store_test.go",
			"internal/state/store_edge_cases_test.go",
		},
		Note: "§6.1 schema → store.go File struct; §6.2 atomic write → Store.Write; §6.3 lock relationship → lock.go; §6.5 corruption recovery → Store.Read ErrStateFileCorrupt",
	},
	{
		Spec: SpecSection{"control-plane-contract.md", "§7", "Audit Log Contract"},
		ImplFiles: []string{
			"internal/executor/audit.go", // AuditEntry schema; 6 entry types; append-only
		},
		TestFiles: []string{
			"internal/executor/audit_test.go",
			"internal/executor/mutation_failure_test.go",
		},
		Note: "§7.2 entry schema + §7.3 entry types + §7.4 ordering guarantee all in audit.go",
	},
	{
		Spec:      SpecSection{"control-plane-contract.md", "§8", "Error Catalog"},
		ImplFiles: []string{"cmd/fault.go", "internal/executor/lock.go"},
		TestFiles: []string{"cmd/fault_test.go"},
		Note:      "Named error strings (ErrUnknownFaultID, ErrFaultAlreadyActive, ErrLockHeld, etc.) live in the files that return them",
	},
	{
		Spec:     SpecSection{"control-plane-contract.md", "§9", "Conformance with Semantic Models"},
		CrossRef: true,
		Note:     "Cross-reference section only; points to conformance-model, system-state-model, and fault-model. No independent implementation.",
	},

	// ── canonical-environment.md ─────────────────────────────────────────────

	{
		Spec: SpecSection{"canonical-environment.md", "§2", "Canonical Environment Contract"},
		ImplFiles: []string{
			"internal/config/config.go", // all path, mode, user constants
		},
		TestFiles: []string{
			"internal/executor/embed_test.go",
		},
		Constraints: "constants (internal/config/config.go) + provisioning (scripts/bootstrap.sh) + verification (conformance checks)",
		Note:        "§2.2 users + §2.3 filesystem layout → config.go constants; §2.4 baseline service state → checked by conformance suite",
	},
	{
		Spec: SpecSection{"canonical-environment.md", "§3", "Go Service Interface Contract"},
		ImplFiles: []string{
			"service/main.go",        // startup contract §3.1; signal handling §3.5
			"service/server/server.go", // endpoint contracts §3.3
			"service/logging/logging.go", // log file behavior §3.6; request logging §3.4
			"service/signals/signals.go", // signal files; startup ordering
		},
		TestFiles: []string{
			"service/server/server_test.go",
			"service/server/server_edge_test.go",
			"service/signals/signals_test.go",
			"service/logging/logging_test.go",
		},
		Note: "§3.1 startup contract → main.go; §3.2 process model → main.go (GOMAXPROCS, build flags); §3.3 endpoints → server.go; §3.4 logging → logging.go; §3.5 signals → main.go; §3.6 log file → logging.go",
	},
	{
		Spec: SpecSection{"canonical-environment.md", "§4", "Canonical Artifact Contents"},
		ImplFiles: []string{
			"internal/config/app.service",    // §4.1 exact unit file content
			"internal/config/config.yaml",    // §4.2 app config schema
			"internal/config/nginx.conf",     // §4.3 nginx config
			"internal/config/logrotate.conf", // §4.5 logrotate config
			"service/config/config.go",       // §4.2 parser with strict YAML
		},
		TestFiles: []string{
			"internal/executor/embed_test.go",
			"service/config/config_test.go",
			"service/config/config_edge_test.go",
		},
		Constraints: "embedded content (internal/config/*) + parser enforcement (service/config/config.go) + R2 restore (internal/executor/canonical_files.go)",
	},
	{
		Spec: SpecSection{"canonical-environment.md", "§5", "Provisioning Contract"},
		ImplFiles: []string{
			"scripts/bootstrap.sh", // 16-step idempotent provisioning
		},
		TestFiles: []string{"cmd/live_fault_matrix_test.go"},
		Constraints: "script (scripts/bootstrap.sh) + idempotency strategy (docs/provisioning-blueprint.md) + final gate (lab validate)",
		Note: "§5.4 idempotency contract → each step's guard condition in bootstrap.sh",
	},
	{
		Spec: SpecSection{"canonical-environment.md", "§8", "State Control"},
		ImplFiles: []string{
			"cmd/reset.go",       // R1/R2/R3 tier operations
			"scripts/reset.sh",   // thin wrapper; flag-to-tier mapping
		},
		TestFiles: []string{"cmd/live_fault_matrix_test.go"},
		Note: "§8.1 reset tiers → cmd/reset.go executeTier; §8.2 reset.sh contract → scripts/reset.sh",
	},
}

// ── Document ordering ────────────────────────────────────────────────────────

// DocOrder defines the canonical display order of the five semantic documents.
// The markdown table is generated in this order. Tests verify that SpecIndex
// entries follow this order, and that no document is missing or duplicated.
var DocOrder = []string{
	"conformance-model.md",
	"system-state-model.md",
	"fault-model.md",
	"control-plane-contract.md",
	"canonical-environment.md",
}

// ── Markdown generation ──────────────────────────────────────────────────────

// GenerateMarkdown produces the canonical markdown for the
// Specification → Implementation Index section of codebase-reference.md.
// It is the single rendering function — both the committed markdown and
// TestSpecIndex_MarkdownIsUpToDate call this function, so the two can never
// diverge without a test failure.
//
// Output format: the section is wrapped in HTML-comment guard markers so
// extraction is deterministic regardless of what appears above or below it
// in codebase-reference.md. The markers are:
//
//	<!-- BEGIN GENERATED: Specification → Implementation Index -->
//	<!-- END GENERATED: Specification → Implementation Index -->
//
// Documents are emitted in DocOrder. Entries within each document are emitted
// in SpecIndex order (which must match section order in the document).
func GenerateMarkdown() string {
	var sb strings.Builder

	sb.WriteString("<!-- BEGIN GENERATED: Specification → Implementation Index -->\n")
	sb.WriteString("## Specification → Implementation Index\n")
	sb.WriteString("\n")
	sb.WriteString("> **Source of truth:** `internal/invariants/spec_index.go` — the Go data structure that backs this table.\n")
	sb.WriteString("> Every file reference is verified by `TestSpecIndex_AllReferencedFilesExist` on every test run.\n")
	sb.WriteString("> The markdown in this section is kept in sync by `TestSpecIndex_MarkdownIsUpToDate`.\n")
	sb.WriteString(">\n")
	sb.WriteString("> **Integrity guarantee:** the CI pipeline runs `TestSpecIndex*` on every push. A passing build means all mappings are verified.\n")
	sb.WriteString(">\n")
	sb.WriteString("> **To update:** edit `internal/invariants/spec_index.go`, then run:\n")
	sb.WriteString("> ```\n")
	sb.WriteString("> go generate ./internal/invariants/\n")
	sb.WriteString("> ```\n")
	sb.WriteString("> **Notation:** `→ (test-only)` = enforced by tests only · `→ (cross-reference)` = points to other documents · `constraints:` = layered enforcement\n")
	sb.WriteString("\n")

	// Group entries by document in DocOrder.
	byDoc := make(map[string][]SpecMapping)
	for _, m := range SpecIndex {
		byDoc[m.Spec.Doc] = append(byDoc[m.Spec.Doc], m)
	}

	for _, doc := range DocOrder {
		entries, ok := byDoc[doc]
		if !ok {
			continue
		}

		sb.WriteString("---\n\n")
		sb.WriteString("### `" + doc + "`\n\n")
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

// renderImpl returns the implementation cell content for a SpecMapping.
func renderImpl(m SpecMapping) string {
	if m.CrossRef {
		return "→ (cross-reference)"
	}
	if m.TestOnly {
		return "→ (test-only)"
	}
	if m.Constraints != "" {
		return "constraints: " + m.Constraints
	}
	if len(m.ImplFiles) == 0 {
		return "—"
	}
	parts := make([]string, len(m.ImplFiles))
	for i, f := range m.ImplFiles {
		parts[i] = "`" + f + "`"
	}
	return strings.Join(parts, " · ")
}

// renderTests returns the test cell content for a SpecMapping.
func renderTests(m SpecMapping) string {
	if len(m.TestFiles) == 0 {
		return "—"
	}
	parts := make([]string, len(m.TestFiles))
	for i, f := range m.TestFiles {
		// Use basename only for brevity in the table.
		base := f
		if idx := strings.LastIndex(f, "/"); idx >= 0 {
			base = f[idx+1:]
		}
		parts[i] = "`" + base + "`"
	}
	return strings.Join(parts, " · ")
}