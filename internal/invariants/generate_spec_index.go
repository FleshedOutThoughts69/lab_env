//go:build ignore

// generate_spec_index.go rewrites the Specification → Implementation Index
// section in docs/codebase-reference.md from the SpecIndex declared in
// spec_index.go.
//
// Run with:
//
//	go run internal/invariants/generate_spec_index.go
//
// Or via go generate:
//
//	go generate ./internal/invariants/
//
// CI / pre-commit integration:
//
//	To guarantee the committed markdown always matches spec_index.go, add
//	this to your CI pipeline (after running go generate):
//
//	  go generate ./internal/invariants/
//	  git diff --exit-code docs/codebase-reference.md || \
//	    (echo "codebase-reference.md is out of sync; run go generate ./internal/invariants/" && exit 1)
//
//	TestSpecIndex_MarkdownIsUpToDate catches the same issue during tests,
//	but the CI check provides feedback before tests run and prevents the
//	committed file from ever being stale.
//
// The generated section replaces everything between the start marker
// "## Specification → Implementation Index" and the end of the file
// (the section is always the last major section in the document).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	_, file, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Dir(filepath.Dir(filepath.Dir(file)))

	docPath := filepath.Join(moduleRoot, "docs", "codebase-reference.md")

	data, err := os.ReadFile(docPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", docPath, err)
		os.Exit(1)
	}

	content := string(data)

	beginGuard := "<!-- BEGIN GENERATED: Specification → Implementation Index -->"
	endGuard   := "<!-- END GENERATED: Specification → Implementation Index -->"

	beginIdx := strings.Index(content, beginGuard)
	endIdx   := strings.Index(content, endGuard)

	var before, after string

	if beginIdx >= 0 && endIdx >= 0 && endIdx > beginIdx {
		// Guards present: replace the section between them (inclusive).
		before = strings.TrimRight(content[:beginIdx], "\n")
		after  = strings.TrimLeft(content[endIdx+len(endGuard):], "\n")
	} else {
		// First generation: no guards yet, find the old ## heading.
		marker := "\n---\n\n## Specification → Implementation Index\n"
		idx := strings.Index(content, marker)
		if idx < 0 {
			marker2 := "## Specification → Implementation Index\n"
			idx = strings.Index(content, marker2)
			if idx < 0 {
				fmt.Fprintln(os.Stderr, "neither guard markers nor fallback heading found in codebase-reference.md")
				os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "write %s: %v\n", docPath, err)
		os.Exit(1)
	}

	fmt.Printf("regenerated spec index in %s\n", docPath)
}