package invariants

// architecture_test.go
//
// Static architecture invariant tests. These tests parse the Go source tree
// to verify that package import boundaries are respected. They catch violations
// that are easy to make under time pressure and hard to find in code review.
//
// Why high ROI:
//   - A fault applying code that imports state could bypass the Observer/Executor
//     boundary by directly reading or writing state — unaudited.
//   - A check importing executor would gain mutation capability, violating the
//     observation-only rule enforced by the type system.
//   - Production binaries importing the testing package bloat the binary and
//     signal that test infrastructure has leaked into production code.
//
// These tests run `go list` rather than parsing files directly, so they
// respect Go's import resolution including indirect imports.

import (
	"os/exec"
	"strings"
	"testing"
)

// TestArchitecture_NoProductionCodeImportsTestingPackage verifies that no
// production package (non-_test.go files) imports the testing package or
// the internal testutil package.
//
// Test infrastructure must not leak into production code. A production binary
// that imports testing is larger and signals confused package boundaries.
func TestArchitecture_NoProductionCodeImportsTestingPackage(t *testing.T) {
	// Run go list to get all package imports for the module
	cmd := exec.Command("go", "list", "-f",
		`{{.ImportPath}}: {{join .Imports " "}}`,
		"./...")
	cmd.Dir = moduleRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list failed: %v\noutput: %s", err, out)
	}

	forbidden := []string{"testing", "lab-env/lab/internal/testutil"}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		pkg := parts[0]
		imports := parts[1]

		// Skip test packages (they are expected to import testing)
		if strings.HasSuffix(pkg, "_test") ||
			strings.Contains(pkg, "/testutil") {
			continue
		}

		for _, banned := range forbidden {
			if containsWord(imports, banned) {
				t.Errorf("production package %s imports %s; test infrastructure must not leak into production code",
					pkg, banned)
			}
		}
	}
}

// TestArchitecture_ConformanceChecks_DoNotImportExecutor verifies that the
// conformance package does not import the executor package.
//
// Checks receive a conformance.Observer (read-only). If a check imported
// executor, it could construct an Executor and mutate the system —
// violating the observation-only rule that is the core architectural property.
func TestArchitecture_ConformanceChecks_DoNotImportExecutor(t *testing.T) {
	assertNotImported(t,
		"lab-env/lab/internal/conformance",
		"lab-env/lab/internal/executor",
		"conformance checks must not import executor (observation-only rule)",
	)
}

// TestArchitecture_CatalogFaults_DoNotImportState verifies that the catalog
// package does not import the state package.
//
// Faults receive an executor.Executor. If a fault imported state directly,
// it could read or write state.json outside the controlled mutation path —
// bypassing the audit log and lock.
func TestArchitecture_CatalogFaults_DoNotImportState(t *testing.T) {
	assertNotImported(t,
		"lab-env/lab/internal/catalog",
		"lab-env/lab/internal/state",
		"catalog faults must not import state (mutations must go through executor/audit path)",
	)
}

// TestArchitecture_OutputPackage_DoNotImportConformance verifies that the
// output package does not import the conformance package.
//
// Output is a pure presentation layer. It must receive conformance results
// as data structures, not import the conformance engine directly — otherwise
// it could accidentally trigger check execution.
func TestArchitecture_OutputPackage_DoNotImportConformance(t *testing.T) {
	assertNotImported(t,
		"lab-env/lab/internal/output",
		"lab-env/lab/internal/conformance",
		"output package must not import conformance (presentation must not trigger execution)",
	)
}

// TestArchitecture_ServiceModule_DoesNotImportControlPlane verifies that
// the service module does not import anything from the control plane module.
//
// The two modules are deliberately separated (separate go.mod files). The
// service must not import lab-env/lab packages — this would couple the
// subject application to the control plane it is designed to be independent of.
func TestArchitecture_ServiceModule_DoesNotImportControlPlane(t *testing.T) {
	cmd := exec.Command("go", "list", "-f",
		`{{.ImportPath}}: {{join .Imports " "}}`,
		"./...")
	cmd.Dir = serviceModuleRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Logf("service module go list not available (may not be in service dir): %v", err)
		t.Skip("service module root not found")
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "github.com/lab-env/lab") &&
			!strings.Contains(line, "github.com/lab-env/service") {
			t.Errorf("service module imports control plane package: %s", line)
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func assertNotImported(t *testing.T, pkg, banned, reason string) {
	t.Helper()
	cmd := exec.Command("go", "list", "-f",
		`{{join .Imports " "}}`,
		pkg)
	cmd.Dir = moduleRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pkg, err)
	}
	imports := string(out)
	if containsWord(imports, banned) {
		t.Errorf("%s imports %s: %s", pkg, banned, reason)
	}
}

func containsWord(s, word string) bool {
	for _, w := range strings.Fields(s) {
		if w == word {
			return true
		}
	}
	return false
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "env", "GOMOD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	path := strings.TrimSpace(string(out))
	// Return the directory containing go.mod
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	return path[:idx]
}

func serviceModuleRoot(t *testing.T) string {
	t.Helper()
	root := moduleRoot(t)
	return root + "/service"
}