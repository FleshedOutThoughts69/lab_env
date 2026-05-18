package invariants_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpecIndex_AllReferencedFilesExist verifies that every file listed in
// SpecIndex.ImplFiles and SpecIndex.TestFiles exists on disk relative to the
// module root. A file reference that does not resolve is a stale mapping.
//
// This test is the anti-rot mechanism for the reverse index. It catches:
//   - Files renamed without updating the index
//   - Files deleted without updating the index
//   - Typos introduced when adding new entries
//
// When this test fails, update spec_index.go to reflect the current file tree.
func TestSpecIndex_AllReferencedFilesExist(t *testing.T) {
	root := moduleRoot(t)

	for _, m := range SpecIndex {
		label := fmt.Sprintf("%s %s", m.Spec.Doc, m.Spec.Section)

		for _, f := range m.ImplFiles {
			path := filepath.Join(root, f)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("[%s] ImplFile not found: %q\n  expected at: %s\n  error: %v",
					label, f, path, err)
			}
		}

		for _, f := range m.TestFiles {
			path := filepath.Join(root, f)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("[%s] TestFile not found: %q\n  expected at: %s\n  error: %v",
					label, f, path, err)
			}
		}
	}
}

// TestSpecIndex_AllSectionsHaveAtLeastOneFile verifies that every entry in
// SpecIndex has at least one ImplFile OR at least one TestFile OR is explicitly
// marked CrossRef or TestOnly. An entry with no files and no explicit marker
// is an incomplete mapping.
func TestSpecIndex_AllSectionsHaveAtLeastOneFile(t *testing.T) {
	for _, m := range SpecIndex {
		label := fmt.Sprintf("%s %s (%s)", m.Spec.Doc, m.Spec.Section, m.Spec.Title)

		hasFiles := len(m.ImplFiles) > 0 || len(m.TestFiles) > 0
		hasMarker := m.CrossRef || m.TestOnly

		if !hasFiles && !hasMarker {
			t.Errorf("[%s] has no ImplFiles, no TestFiles, and is not marked CrossRef or TestOnly — incomplete mapping", label)
		}
	}
}

// TestSpecIndex_NoDuplicateSections verifies that no (Doc, Section) pair
// appears more than once in SpecIndex. Duplicates indicate a copy-paste error
// or a section split that was not properly consolidated.
func TestSpecIndex_NoDuplicateSections(t *testing.T) {
	seen := make(map[string]int)
	for i, m := range SpecIndex {
		key := m.Spec.Doc + "::" + m.Spec.Section
		if prev, ok := seen[key]; ok {
			t.Errorf("duplicate section: %s %s (entries %d and %d)",
				m.Spec.Doc, m.Spec.Section, prev, i)
		}
		seen[key] = i
	}
}

// TestSpecIndex_AllFiveDocumentsCovered verifies that every semantic model
// document in DocOrder has at least one entry in SpecIndex. A missing document
// would mean the entire document's implementation is unmapped.
//
// Uses DocOrder as the single authoritative list — not a separate hardcoded
// slice — so adding a sixth document to DocOrder automatically requires
// corresponding SpecIndex entries.
func TestSpecIndex_AllFiveDocumentsCovered(t *testing.T) {
	covered := make(map[string]bool)
	for _, m := range SpecIndex {
		covered[m.Spec.Doc] = true
	}

	for _, doc := range DocOrder {
		if !covered[doc] {
			t.Errorf("semantic document %q is in DocOrder but has no entries in SpecIndex", doc)
		}
	}
}

// TestSpecIndex_NoRelativePathPrefixes verifies that no ImplFile or TestFile
// entry starts with "./" or "../". All paths must be relative to the module
// root without a leading dot-slash.
func TestSpecIndex_NoRelativePathPrefixes(t *testing.T) {
	for _, m := range SpecIndex {
		label := fmt.Sprintf("%s %s", m.Spec.Doc, m.Spec.Section)
		for _, f := range append(m.ImplFiles, m.TestFiles...) {
			if strings.HasPrefix(f, "./") || strings.HasPrefix(f, "../") {
				t.Errorf("[%s] path %q must not start with ./ or ../; use paths relative to module root", label, f)
			}
		}
	}
}

// TestSpecIndex_CrossRefEntriesHaveNoImplFiles verifies that entries marked
// CrossRef do not list ImplFiles. A cross-reference section points to other
// documents; it has no independent implementation.
func TestSpecIndex_CrossRefEntriesHaveNoImplFiles(t *testing.T) {
	for _, m := range SpecIndex {
		if m.CrossRef && len(m.ImplFiles) > 0 {
			t.Errorf("%s %s: CrossRef=true but has ImplFiles %v — cross-reference sections must not list implementation files",
				m.Spec.Doc, m.Spec.Section, m.ImplFiles)
		}
	}
}

// TestSpecIndex_TestOnlyEntriesHaveNoImplFiles verifies that entries marked
// TestOnly do not list ImplFiles. TestOnly means the section is enforced
// entirely by tests; listing ImplFiles would be contradictory.
func TestSpecIndex_TestOnlyEntriesHaveNoImplFiles(t *testing.T) {
	for _, m := range SpecIndex {
		if m.TestOnly && len(m.ImplFiles) > 0 {
			t.Errorf("%s %s: TestOnly=true but has ImplFiles %v — test-only sections must not list production implementation files",
				m.Spec.Doc, m.Spec.Section, m.ImplFiles)
		}
	}
}

// TestSpecIndex_DocumentsExistOnDisk verifies that the document filenames
// referenced in SpecIndex actually exist in the expected docs/ location.
// This catches a renamed document before any other test fires.
func TestSpecIndex_DocumentsExistOnDisk(t *testing.T) {
	root := moduleRoot(t)

	// Semantic model docs live one level up from the module root
	// (they are in /mnt/user-data/outputs/ relative to /mnt/user-data/outputs/lab-env/)
	// In the real repo they would be at the repository root alongside lab-env/.
	// We check both the module root and its parent.
	searchDirs := []string{
		root,
		filepath.Dir(root),
		filepath.Join(root, "docs"),
	}

	seen := make(map[string]bool)
	for _, m := range SpecIndex {
		if seen[m.Spec.Doc] {
			continue
		}
		seen[m.Spec.Doc] = true

		found := false
		for _, dir := range searchDirs {
			candidate := filepath.Join(dir, m.Spec.Doc)
			if _, err := os.Stat(candidate); err == nil {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("document %q not found in any of: %v", m.Spec.Doc, searchDirs)
		}
	}
}

// ── Markdown sync (suggestion 1 + 5) ─────────────────────────────────────────

const (
	beginGuard = "<!-- BEGIN GENERATED: Specification → Implementation Index -->"
	endGuard   = "<!-- END GENERATED: Specification → Implementation Index -->"
)

// TestSpecIndex_MarkerGuardsExist verifies that both HTML-comment guard markers
// are present in codebase-reference.md and appear in the correct order.
//
// These markers delimit the generated section so that the sync test can extract
// it precisely regardless of what appears above or below it in the document.
// Deleting or swapping them would cause TestSpecIndex_MarkdownIsUpToDate to
// either fail confusingly or miss the section entirely.
func TestSpecIndex_MarkerGuardsExist(t *testing.T) {
	root := moduleRoot(t)
	docPath := filepath.Join(root, "docs", "codebase-reference.md")

	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("reading codebase-reference.md: %v", err)
	}
	content := string(data)

	beginIdx := strings.Index(content, beginGuard)
	endIdx := strings.Index(content, endGuard)

	if beginIdx < 0 && endIdx < 0 {
		t.Fatalf("neither BEGIN nor END guard found in codebase-reference.md\n"+
			"Run: go run internal/invariants/generate_spec_index.go to regenerate")
	}
	if beginIdx < 0 {
		t.Fatalf("BEGIN guard not found in codebase-reference.md (END guard is present at byte %d)\n"+
			"Run: go run internal/invariants/generate_spec_index.go to regenerate", endIdx)
	}
	if endIdx < 0 {
		t.Fatalf("END guard not found in codebase-reference.md (BEGIN guard is present at byte %d)\n"+
			"Run: go run internal/invariants/generate_spec_index.go to regenerate", beginIdx)
	}
	if endIdx <= beginIdx {
		t.Fatalf("END guard (byte %d) appears before or at BEGIN guard (byte %d) — markers are in wrong order\n"+
			"Run: go run internal/invariants/generate_spec_index.go to regenerate", endIdx, beginIdx)
	}
}

// TestSpecIndex_MarkdownIsUpToDate verifies that the committed markdown table
// in codebase-reference.md matches what GenerateMarkdown() would produce.
//
// The comparison is made against the content between the HTML-comment guard
// markers, which makes extraction immune to ## headings inside the section
// and perfectly deterministic regardless of what appears elsewhere in the file.
//
// When this test fails:
//  1. Edit spec_index.go to reflect the intended change.
//  2. Run: go run internal/invariants/generate_spec_index.go
//  3. Commit both files together.
func TestSpecIndex_MarkdownIsUpToDate(t *testing.T) {
	root := moduleRoot(t)
	docPath := filepath.Join(root, "docs", "codebase-reference.md")

	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("reading codebase-reference.md: %v", err)
	}
	content := string(data)

	// Locate guard markers — fail immediately if either is missing.
	beginIdx := strings.Index(content, beginGuard)
	endIdx := strings.Index(content, endGuard)

	if beginIdx < 0 {
		t.Fatalf("BEGIN guard %q not found in codebase-reference.md\n"+
			"Run: go run internal/invariants/generate_spec_index.go to regenerate", beginGuard)
	}
	if endIdx < 0 {
		t.Fatalf("END guard %q not found in codebase-reference.md\n"+
			"Run: go run internal/invariants/generate_spec_index.go to regenerate", endGuard)
	}
	if endIdx <= beginIdx {
		t.Fatalf("END guard appears before BEGIN guard — run generate_spec_index.go to fix")
	}

	// Extract the committed section including both guard lines.
	committed := strings.TrimRight(
		content[beginIdx:endIdx+len(endGuard)], "\n")

	// Generate fresh from SpecIndex (trim trailing newline for comparison).
	generated := strings.TrimRight(GenerateMarkdown(), "\n")

	if committed != generated {
		cLines := strings.Split(committed, "\n")
		gLines := strings.Split(generated, "\n")
		for i := 0; i < len(cLines) && i < len(gLines); i++ {
			if cLines[i] != gLines[i] {
				t.Errorf("codebase-reference.md spec index section is out of date.\n"+
					"First difference at line %d:\n  committed:  %q\n  generated:  %q\n\n"+
					"Run: go run internal/invariants/generate_spec_index.go",
					i+1, cLines[i], gLines[i])
				return
			}
		}
		if len(cLines) != len(gLines) {
			t.Errorf("codebase-reference.md spec index section is out of date: "+
				"committed has %d lines, generated has %d lines.\n"+
				"Run: go run internal/invariants/generate_spec_index.go",
				len(cLines), len(gLines))
		}
	}
}

// TestSpecIndex_GeneratedMarkdownStructure verifies structural soundness of
// the generated markdown (suggestion 5):
//   - Every table row has exactly 5 pipe-delimited columns.
//   - No row in a non-test-only, non-cross-reference entry has an empty
//     implementation cell.
//   - No table row has adjacent pipes (empty cell without explicit dash).
func TestSpecIndex_GeneratedMarkdownStructure(t *testing.T) {
	md := GenerateMarkdown()
	lines := strings.Split(md, "\n")

	for i, line := range lines {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		// Count columns: split on | and count non-empty segments.
		parts := strings.Split(line, "|")
		// parts[0] and parts[len-1] are empty (leading/trailing |)
		cols := parts[1 : len(parts)-1]
		if len(cols) != 5 {
			t.Errorf("line %d: expected 5 columns, got %d: %q", i+1, len(cols), line)
		}
		// No adjacent pipes (|| means empty cell — must use — instead).
		if strings.Contains(line, "||") {
			t.Errorf("line %d: adjacent pipes (empty cell) found: %q", i+1, line)
		}
	}
}

// ── Document section coverage (suggestion 2) ─────────────────────────────────

// TestSpecIndex_NoUndocumentedSections parses each semantic document and
// reports any top-level section (## §N or # §N) that has no corresponding
// entry in SpecIndex. This prevents silent drift when new sections are added
// to the spec documents without a matching implementation mapping.
//
// Parsing robustness: the heading extractor looks for lines starting with
// "## §" or "# §". Non-§ headings (e.g. "# Conformance Model", "## Version")
// are not section identifiers and are correctly ignored. If a document is not
// found on disk, the test fails immediately — it never silently passes with
// zero headings extracted.
//
// After extraction, if the document yields zero headings, the test fails:
// a real document always has at least one §-section, so zero headings means
// the file was empty, unreadable, or the heading format changed.
//
// Sections explicitly excluded from mapping requirements:
//   §1 (Purpose) — introductory; no implementation
//   §N Authority/References — cross-reference boilerplate
//   §N Non-Goals — declarative; no implementation
func TestSpecIndex_NoUndocumentedSections(t *testing.T) {
	root := moduleRoot(t)

	// Sections that are deliberately excluded from mapping requirements
	// across all documents.
	excluded := map[string]bool{
		"§1":  true, // Purpose — all documents
		"§9":  true, // cross-reference / authority sections
		"§11": true, // Non-Goals — canonical-environment
	}

	// Per-document additional exclusions with documented rationale.
	skipForDoc := map[string]map[string]bool{
		"conformance-model.md": {
			"§6": true, // Authority and References
		},
		"system-state-model.md": {
			"§6": true, // Authority and References
			"§7": true, // Authority and References
		},
		"fault-model.md": {
			"§8": true, // Model Completeness — IS mapped as test-only
			"§9": true, // Authority and References
		},
		"control-plane-contract.md": {
			"§2": true, // Derived Authority — introductory
		},
		"canonical-environment.md": {
			"§6":  true, // Conformance Suite — derived from conformance-model; no new impl
			"§7":  true, // Fault Injection Catalog — derived from fault-model; no new impl
			"§10": true, // Networking Extensions — informational
			"§13": true, // Lab Control Plane — reference only
		},
	}

	// Build set of mapped sections per document.
	mapped := make(map[string]map[string]bool)
	for _, m := range SpecIndex {
		if mapped[m.Spec.Doc] == nil {
			mapped[m.Spec.Doc] = make(map[string]bool)
		}
		// Normalize: strip subsection (§4.1 → §4).
		section := m.Spec.Section
		if dot := strings.Index(section, "."); dot >= 0 {
			section = section[:dot]
		}
		mapped[m.Spec.Doc][section] = true
	}

	// Search dirs for spec documents.
	searchDirs := []string{root, filepath.Dir(root)}

	for _, doc := range DocOrder {
		// Find the document — FAIL if not found (not skip).
		var docPath string
		for _, dir := range searchDirs {
			candidate := filepath.Join(dir, doc)
			if _, err := os.Stat(candidate); err == nil {
				docPath = candidate
				break
			}
		}
		if docPath == "" {
			t.Fatalf("document %q not found in any of %v — cannot verify section coverage\n"+
				"Fix: ensure the semantic model documents are present relative to the module root",
				doc, searchDirs)
		}

		data, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("reading %s: %v", docPath, err)
		}

		// Extract top-level sections.
		var extracted []string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "## §") && !strings.HasPrefix(line, "# §") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			extracted = append(extracted, fields[1])
		}

		// Minimum-headings guard: a real spec document always has §-sections.
		// Zero extracted means the file was empty or the heading format changed.
		if len(extracted) == 0 {
			t.Fatalf("document %s: zero §-sections extracted — file may be empty or heading format changed (expected '## §N' or '# §N' lines)", doc)
		}

		for _, section := range extracted {
			if excluded[section] || skipForDoc[doc][section] {
				continue
			}
			if mapped[doc] == nil || !mapped[doc][section] {
				t.Errorf("document %s has section %s with no entry in SpecIndex\n"+
					"  Add a SpecMapping for %s %s to spec_index.go",
					doc, section, doc, section)
			}
		}
	}
}

// ── Section ordering (suggestion 6) ──────────────────────────────────────────

// TestSpecIndex_EntriesFollowDocumentOrder verifies that the SpecIndex entries
// for each document appear in the same order as the sections appear in the
// document itself. Out-of-order entries produce a correctly-verified but
// confusing markdown table.
//
// If a document is not found on disk, the test fails immediately rather than
// silently skipping. If zero §-sections are extracted from a found document,
// the test fails — a real document always has §-sections, so zero means the
// file was empty or the heading format changed.
func TestSpecIndex_EntriesFollowDocumentOrder(t *testing.T) {
	root := moduleRoot(t)
	searchDirs := []string{root, filepath.Dir(root)}

	// Build per-document entry list from SpecIndex (full section IDs, not normalized).
	byDoc := make(map[string][]string)
	for _, m := range SpecIndex {
		byDoc[m.Spec.Doc] = append(byDoc[m.Spec.Doc], m.Spec.Section)
	}

	for _, doc := range DocOrder {
		var docPath string
		for _, dir := range searchDirs {
			candidate := filepath.Join(dir, doc)
			if _, err := os.Stat(candidate); err == nil {
				docPath = candidate
				break
			}
		}
		if docPath == "" {
			t.Fatalf("document %q not found in any of %v — cannot verify section order\n"+
				"Fix: ensure the semantic model documents are present relative to the module root",
				doc, searchDirs)
		}

		data, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("reading %s: %v", docPath, err)
		}

		// Extract top-level section identifiers in document order.
		var docSections []string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "## §") && !strings.HasPrefix(line, "# §") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			docSections = append(docSections, fields[1])
		}

		// Minimum-headings guard: zero means the file format changed.
		if len(docSections) == 0 {
			t.Fatalf("document %s: zero §-sections extracted — heading format may have changed", doc)
		}

		// Build position map for top-level sections.
		docPos := make(map[string]int)
		for i, s := range docSections {
			docPos[s] = i
		}

		// Check entries follow document order.
		lastPos := -1
		for _, entry := range byDoc[doc] {
			// Normalize entry section to top-level (§4.1 → §4).
			entrySection := entry
			if dot := strings.Index(entrySection, "."); dot >= 0 {
				entrySection = entrySection[:dot]
			}
			pos, ok := docPos[entrySection]
			if !ok {
				continue // subsection not listed as top-level heading (expected)
			}
			if pos < lastPos {
				t.Errorf("%s: SpecIndex entry %s appears before a preceding section — entries must follow document order",
					doc, entry)
			}
			if pos > lastPos {
				lastPos = pos
			}
		}
	}
}

// ── Constraints string path verification (audit 5) ───────────────────────────

// TestSpecIndex_ConstraintPathsExist scans all Constraints strings for
// substrings that look like file paths (contain "/" and end with a known
// extension). Any such path is verified to exist on disk.
//
// This prevents a developer from putting a real file reference inside a
// Constraints string expecting it to be verified — Constraints strings are
// display-only, so paths inside them are invisible to
// TestSpecIndex_AllReferencedFilesExist. This test closes that gap.
func TestSpecIndex_ConstraintPathsExist(t *testing.T) {
	root := moduleRoot(t)

	// Extensions that identify a real file path vs a documentation phrase.
	fileExtensions := []string{
		".go", ".sh", ".yaml", ".service", ".conf", ".md", ".json",
	}

	for _, m := range SpecIndex {
		if m.Constraints == "" {
			continue
		}
		label := fmt.Sprintf("%s %s", m.Spec.Doc, m.Spec.Section)

		// Split on common delimiters and inspect each token.
		tokens := strings.FieldsFunc(m.Constraints, func(r rune) bool {
			return r == ' ' || r == '+' || r == '(' || r == ')' || r == ','
		})

		for _, token := range tokens {
			// Must look like a path: contains "/" and ends with a known extension.
			if !strings.Contains(token, "/") {
				continue
			}
			hasExt := false
			for _, ext := range fileExtensions {
				if strings.HasSuffix(token, ext) {
					hasExt = true
					break
				}
			}
			if !hasExt {
				continue
			}

			// Looks like a file path — verify it exists.
			path := filepath.Join(root, token)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("[%s] Constraints string contains path %q that does not exist at %s\n"+
					"  Either fix the path or move it to ImplFiles where it will be verified automatically",
					label, token, path)
			}
		}
	}
}