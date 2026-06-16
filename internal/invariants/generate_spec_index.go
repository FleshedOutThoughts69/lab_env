package invariants

// spec_index_gen.go
//
// Contains the SpecIndex, DocOrder, and logic to generate the
// Specification → Implementation Index section in docs/codebase-reference.md.
//
// The actual generator binary lives in cmd/generate_spec_index/main.go;
// this file provides the library functions that both the generator and the
// tests rely on.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ModuleRoot is the absolute path to the repository root.
// Computed once at init time from the source file location.
var ModuleRoot string

func init() {
	_, file, _, _ := runtime.Caller(0)
	// file is .../lab_env/internal/invariants/spec_index_gen.go
	// Walk up three directories to get the repo root.
	ModuleRoot = filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// GenerateSpecIndex rewrites the Specification → Implementation Index section
// in docs/codebase-reference.md using the current SpecIndex and DocOrder.
func GenerateSpecIndex() error {
	docPath := filepath.Join(ModuleRoot, "docs", "codebase-reference.md")

	data, err := os.ReadFile(docPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", docPath, err)
	}

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
				return fmt.Errorf("neither guard markers nor fallback heading found in codebase-reference.md")
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