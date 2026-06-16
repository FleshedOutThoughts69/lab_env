package invariants_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lab_env/internal/invariants"
)

// moduleRoot returns the module root path, failing the test if empty.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root := invariants.ModuleRoot
	if root == "" {
		t.Fatal("ModuleRoot is empty")
	}
	return root
}

// TestSpecIndex_AllReferencedFilesExist verifies that every file listed in
// SpecIndex.ImplFiles and SpecIndex.TestFiles exists on disk relative to the
// module root. A file reference that does not resolve is a stale mapping.
func TestSpecIndex_AllReferencedFilesExist(t *testing.T) {
	root := moduleRoot(t)

	for _, m := range invariants.SpecIndex {
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
// marked CrossRef or TestOnly.
func TestSpecIndex_AllSectionsHaveAtLeastOneFile(t *testing.T) {
	for _, m := range invariants.SpecIndex {
		label := fmt.Sprintf("%s %s (%s)", m.Spec.Doc, m.Spec.Section, m.Spec.Title)

		hasFiles := len(m.ImplFiles) > 0 || len(m.TestFiles) > 0
		hasMarker := m.CrossRef || m.TestOnly

		if !hasFiles && !hasMarker {
			t.Errorf("[%s] has no ImplFiles, no TestFiles, and is not marked CrossRef or TestOnly — incomplete mapping", label)
		}
	}
}

// TestSpecIndex_NoDuplicateSections verifies that no (Doc, Section) pair
// appears more than once in SpecIndex.
func TestSpecIndex_NoDuplicateSections(t *testing.T) {
	seen := make(map[string]int)
	for i, m := range invariants.SpecIndex {
		key := m.Spec.Doc + "::" + m.Spec.Section
		if prev, ok := seen[key]; ok {
			t.Errorf("duplicate section: %s %s (entries %d and %d)",
				m.Spec.Doc, m.Spec.Section, prev, i)
		}
		seen[key] = i
	}
}

// TestSpecIndex_AllFiveDocumentsCovered verifies that every semantic model
// document in DocOrder has at least one entry in SpecIndex.
func TestSpecIndex_AllFiveDocumentsCovered(t *testing.T) {
	covered := make(map[string]bool)
	for _, m := range invariants.SpecIndex {
		covered[m.Spec.Doc] = true
	}

	for _, doc := range invariants.DocOrder {
		if !covered[doc] {
			t.Errorf("semantic document %q is in DocOrder but has no entries in SpecIndex", doc)
		}
	}
}

// TestSpecIndex_NoRelativePathPrefixes verifies that no ImplFile or TestFile
// entry starts with "./" or "../".
func TestSpecIndex_NoRelativePathPrefixes(t *testing.T) {
	for _, m := range invariants.SpecIndex {
		label := fmt.Sprintf("%s %s", m.Spec.Doc, m.Spec.Section)
		for _, f := range append(m.ImplFiles, m.TestFiles...) {
			if strings.HasPrefix(f, "./") || strings.HasPrefix(f, "../") {
				t.Errorf("[%s] path %q must not start with ./ or ../; use paths relative to module root", label, f)
			}
		}
	}
}

// TestSpecIndex_CrossRefEntriesHaveNoImplFiles verifies that entries marked
// CrossRef do not list ImplFiles.
func TestSpecIndex_CrossRefEntriesHaveNoImplFiles(t *testing.T) {
	for _, m := range invariants.SpecIndex {
		if m.CrossRef && len(m.ImplFiles) > 0 {
			t.Errorf("%s %s: CrossRef=true but has ImplFiles %v — cross-reference sections must not list implementation files",
				m.Spec.Doc, m.Spec.Section, m.ImplFiles)
		}
	}
}

// TestSpecIndex_TestOnlyEntriesHaveNoImplFiles verifies that entries marked
// TestOnly do not list ImplFiles.
func TestSpecIndex_TestOnlyEntriesHaveNoImplFiles(t *testing.T) {
	for _, m := range invariants.SpecIndex {
		if m.TestOnly && len(m.ImplFiles) > 0 {
			t.Errorf("%s %s: TestOnly=true but has ImplFiles %v — test-only sections must not list production implementation files",
				m.Spec.Doc, m.Spec.Section, m.ImplFiles)
		}
	}
}

// TestSpecIndex_DocumentsExistOnDisk verifies that the document filenames
// referenced in SpecIndex actually exist in the expected docs/ location.
func TestSpecIndex_DocumentsExistOnDisk(t *testing.T) {
	root := moduleRoot(t)

	searchDirs := []string{
		root,
		filepath.Dir(root),
		filepath.Join(root, "docs"),
	}

	seen := make(map[string]bool)
	for _, m := range invariants.SpecIndex {
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

// ── Markdown sync tests ──────────────────────────────────────────────────────

const (
	beginGuard = "<!-- BEGIN GENERATED: Specification → Implementation Index -->"
	endGuard   = "<!-- END GENERATED: Specification → Implementation Index -->"
)

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

func TestSpecIndex_MarkdownIsUpToDate(t *testing.T) {
	root := moduleRoot(t)
	docPath := filepath.Join(root, "docs", "codebase-reference.md")

	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("reading codebase-reference.md: %v", err)
	}
	content := string(data)

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

	committed := strings.TrimRight(
		content[beginIdx:endIdx+len(endGuard)], "\n")

	generated := strings.TrimRight(invariants.GenerateMarkdown(), "\n")

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

func TestSpecIndex_GeneratedMarkdownStructure(t *testing.T) {
	md := invariants.GenerateMarkdown()
	lines := strings.Split(md, "\n")

	for i, line := range lines {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		cols := parts[1 : len(parts)-1]
		if len(cols) != 5 {
			t.Errorf("line %d: expected 5 columns, got %d: %q", i+1, len(cols), line)
		}
		if strings.Contains(line, "||") {
			t.Errorf("line %d: adjacent pipes (empty cell) found: %q", i+1, line)
		}
	}
}

// ── Document section coverage ─────────────────────────────────────────────────

func TestSpecIndex_NoUndocumentedSections(t *testing.T) {
	root := moduleRoot(t)

	excluded := map[string]bool{
		"§1":  true,
		"§9":  true,
		"§11": true,
	}

	skipForDoc := map[string]map[string]bool{
		"conformance-model.md": {
			"§6": true,
		},
		"system-state-model.md": {
			"§6": true,
			"§7": true,
		},
		"fault-model.md": {
			"§8": true,
			"§9": true,
		},
		"control-plane-contract.md": {
			"§2": true,
		},
		"canonical-environment.md": {
			"§6":  true,
			"§7":  true,
			"§10": true,
			"§13": true,
		},
	}

	mapped := make(map[string]map[string]bool)
	for _, m := range invariants.SpecIndex {
		if mapped[m.Spec.Doc] == nil {
			mapped[m.Spec.Doc] = make(map[string]bool)
		}
		section := m.Spec.Section
		if dot := strings.Index(section, "."); dot >= 0 {
			section = section[:dot]
		}
		mapped[m.Spec.Doc][section] = true
	}

	searchDirs := []string{root, filepath.Dir(root)}

	for _, doc := range invariants.DocOrder {
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

// TestSpecIndex_EntriesFollowDocumentOrder verifies ordering within each document.
func TestSpecIndex_EntriesFollowDocumentOrder(t *testing.T) {
	root := moduleRoot(t)
	searchDirs := []string{root, filepath.Dir(root)}

	byDoc := make(map[string][]string)
	for _, m := range invariants.SpecIndex {
		byDoc[m.Spec.Doc] = append(byDoc[m.Spec.Doc], m.Spec.Section)
	}

	for _, doc := range invariants.DocOrder {
		var docPath string
		for _, dir := range searchDirs {
			candidate := filepath.Join(dir, doc)
			if _, err := os.Stat(candidate); err == nil {
				docPath = candidate
				break
			}
		}
		if docPath == "" {
			t.Fatalf("document %q not found in any of %v — cannot verify section order", doc, searchDirs)
		}

		data, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("reading %s: %v", docPath, err)
		}

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

		if len(docSections) == 0 {
			t.Fatalf("document %s: zero §-sections extracted — heading format may have changed", doc)
		}

		docPos := make(map[string]int)
		for i, s := range docSections {
			docPos[s] = i
		}

		lastPos := -1
		for _, entry := range byDoc[doc] {
			entrySection := entry
			if dot := strings.Index(entrySection, "."); dot >= 0 {
				entrySection = entrySection[:dot]
			}
			pos, ok := docPos[entrySection]
			if !ok {
				continue
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

// TestSpecIndex_ConstraintPathsExist scans Constraints strings for file paths.
func TestSpecIndex_ConstraintPathsExist(t *testing.T) {
	root := moduleRoot(t)

	fileExtensions := []string{
		".go", ".sh", ".yaml", ".service", ".conf", ".md", ".json",
	}

	for _, m := range invariants.SpecIndex {
		if m.Constraints == "" {
			continue
		}
		label := fmt.Sprintf("%s %s", m.Spec.Doc, m.Spec.Section)

		tokens := strings.FieldsFunc(m.Constraints, func(r rune) bool {
			return r == ' ' || r == '+' || r == '(' || r == ')' || r == ','
		})

		for _, token := range tokens {
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

			path := filepath.Join(root, token)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("[%s] Constraints string contains path %q that does not exist at %s\n"+
					"  Either fix the path or move it to ImplFiles where it will be verified automatically",
					label, token, path)
			}
		}
	}
}