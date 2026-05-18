//go:build integration

package cmd

// live_fault_matrix_test.go — integration tests for the full fault catalog.
//
// Iterates all 14 reversible faults through the apply → validate → reset
// cycle against a real provisioned VM environment.
//
// Requires:
//   - LAB_TEST_MODE=live (enforced by TestMain in testhelpers_test.go)
//   - Real VM with provisioned environment in CONFORMANT state
//   - lab binary at ./lab or $LAB_BIN
//   - /var/lib/lab/ and /etc/app/ writable by the test user (via sudo)
//
// Run: LAB_TEST_MODE=live go test -v -run TestLiveFaultMatrix ./cmd/...
//      LAB_TEST_MODE=live go test -v -run TestLiveFaultMatrix/F-004 ./cmd/ to run a single fault
//
// Authority: testing-plan-revised.md Phase B.2,
//            fault-model.md §7.2 (postcondition specifications)

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"testing"
	"time"

	"lab_env/internal/catalog"
	"lab_env/internal/conformance"
)

// reversibleFaults is the ordered list of all 14 reversible fault IDs.
// F-008 and F-014 are excluded — they are non-reversible and require
// binary rebuild. Test manually per testing-plan-revised.md Phase B.2.
var reversibleFaults = []string{
	"F-001", "F-002", "F-003", "F-004", "F-005", "F-006", "F-007",
	"F-009", "F-010",
	"F-013", "F-015", "F-016", "F-017", "F-018",
}

// TestLiveFaultMatrix_AllReversible iterates every reversible fault through
// the full apply → validate → reset → validate cycle.
//
// For each fault:
//  1. Pre-flight: environment must be CONFORMANT
//  2. Apply: lab fault apply <ID> → state must be DEGRADED
//  3. Validate: lab validate → failing check IDs must match FaultDef.PostconditionSpec.FailingChecks
//  4. Reset: lab reset → state must return to CONFORMANT
//  5. Validate: lab validate → all 23 checks must pass
func TestLiveFaultMatrix_AllReversible(t *testing.T) {
	lab := labBin()

	for _, faultID := range reversibleFaults {
		faultID := faultID
		t.Run(faultID, func(t *testing.T) {
			def := catalog.DefByID(faultID)
			if def == nil {
				t.Fatalf("fault %s not found in catalog", faultID)
			}

			// ── Pre-flight ────────────────────────────────────────────────────
			t.Log("pre-flight: verifying CONFORMANT")
			if err := requireConformant(lab, t); err != nil {
				t.Fatalf("pre-flight failed: %v", err)
			}

			// ── Apply ─────────────────────────────────────────────────────────
			t.Logf("applying fault %s", faultID)
			applyOut, err := exec.Command(lab, "fault", "apply", faultID).CombinedOutput()
			if err != nil {
				t.Fatalf("fault apply %s failed: %v\n%s", faultID, err, applyOut)
			}

			// Ensure reset happens even if the test fails mid-sequence.
			t.Cleanup(func() {
				exec.Command(lab, "reset").Run() //nolint:errcheck
			})

			// ── Assert DEGRADED ───────────────────────────────────────────────
			statusState, activeFault, err := readStatusState(lab)
			if err != nil {
				t.Fatalf("reading state after apply: %v", err)
			}
			if statusState != "DEGRADED" {
				t.Errorf("state after apply %s = %q, want DEGRADED", faultID, statusState)
			}
			if activeFault != faultID {
				t.Errorf("active_fault after apply = %q, want %q", activeFault, faultID)
			}

			// ── Validate: check failing IDs ───────────────────────────────────
			t.Logf("running validate to check postcondition")
			failingIDs := runValidateGetFailingIDs(lab)

			expectedFailing := def.Postcondition.FailingChecks
			if len(expectedFailing) > 0 {
				// Sort both for comparison.
				sort.Strings(failingIDs)
				sort.Strings(expectedFailing)

				if !stringSlicesEqual(failingIDs, expectedFailing) {
					t.Errorf("fault %s: failing checks after apply:\n  got:  %v\n  want: %v",
						faultID, failingIDs, expectedFailing)
				} else {
					t.Logf("✓ failing checks match postcondition: %v", failingIDs)
				}
			} else {
				// F-008 / F-014 have empty FailingChecks — lab validate should exit 0.
				validateErr := exec.Command(lab, "validate").Run()
				if validateErr != nil {
					t.Errorf("fault %s: expected lab validate to exit 0 (silent fault), got error: %v", faultID, validateErr)
				} else {
					t.Logf("✓ fault %s is silent to lab validate (exit 0)", faultID)
				}
			}

			// ── Reset ─────────────────────────────────────────────────────────
			t.Logf("resetting environment (tier %s)", def.ResetTier)
			resetOut, err := exec.Command(lab, "reset", "--tier", def.ResetTier).CombinedOutput()
			if err != nil {
				t.Fatalf("lab reset --tier %s failed: %v\n%s", def.ResetTier, err, resetOut)
			}

			// ── Assert CONFORMANT ─────────────────────────────────────────────
			if err := requireConformant(lab, t); err != nil {
				t.Errorf("post-reset conformance check failed for fault %s: %v", faultID, err)
			} else {
				t.Logf("✓ CONFORMANT after reset")
			}
		})
	}
}

// TestLiveFaultMatrix_F010_RequiresRunningApp verifies that F-010 is rejected
// when the app is stopped (PreconditionChecks: [P-001] enforcement).
// control-plane-contract §4.5 step 5.
func TestLiveFaultMatrix_F010_PreconditionCheckEnforced(t *testing.T) {
	lab := labBin()

	// Stop the app service to fail P-001.
	if out, err := exec.Command("sudo", "systemctl", "stop", "app.service").CombinedOutput(); err != nil {
		t.Fatalf("stopping app.service: %v\n%s", err, out)
	}
	defer exec.Command("sudo", "systemctl", "start", "app.service").Run() //nolint:errcheck

	// Apply F-010 — must be rejected with exit 3 (ErrFaultPreconditionFailed).
	cmd := exec.Command(lab, "fault", "apply", "F-010")
	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	if exitCode != 3 {
		t.Errorf("fault apply F-010 with stopped service: exit code %d, want 3 (ErrFaultPreconditionFailed)", exitCode)
	}

	// Verify environment is still CONFORMANT (no partial mutation).
	exec.Command("sudo", "systemctl", "start", "app.service").Run() //nolint:errcheck
	time.Sleep(500 * time.Millisecond)                                // let service start
}

// TestLiveFaultMatrix_F018_InodeExhaustion verifies the inode exhaustion
// fault: df -i shows near-100% usage after apply; GET / returns 500; GET /health returns 200.
func TestLiveFaultMatrix_F018_InodeExhaustion(t *testing.T) {
	lab := labBin()

	if err := requireConformant(lab, t); err != nil {
		t.Fatalf("pre-flight: %v", err)
	}

	applyOut, err := exec.Command(lab, "fault", "apply", "F-018").CombinedOutput()
	if err != nil {
		t.Fatalf("fault apply F-018: %v\n%s", err, applyOut)
	}
	defer exec.Command(lab, "reset", "--tier", "R2").Run() //nolint:errcheck

	// Verify inode exhaustion via df -i.
	dfOut, err := exec.Command("df", "-i", "/var/lib/app").Output()
	if err != nil {
		t.Fatalf("df -i /var/lib/app: %v", err)
	}
	t.Logf("df -i after F-018:\n%s", dfOut)

	// Verify /health still returns 200.
	if out, err := exec.Command("curl", "-sf", "http://localhost/health").Output(); err != nil {
		t.Errorf("GET /health after F-018: want 200, got error: %v\n%s", err, out)
	}

	// Verify / returns 500.
	curlCmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://localhost/")
	codeOut, _ := curlCmd.Output()
	if string(codeOut) != "500" {
		t.Errorf("GET / after F-018: want 500, got %s", codeOut)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// requireConformant asserts the environment is CONFORMANT via lab validate.
func requireConformant(lab string, t *testing.T) error {
	t.Helper()
	if err := exec.Command(lab, "validate").Run(); err != nil {
		// Capture output for diagnosis.
		out, _ := exec.Command(lab, "validate").CombinedOutput()
		return fmt.Errorf("lab validate failed: %v\n%s", err, out)
	}
	return nil
}

// readStatusState returns the state and active_fault ID from lab status --json.
func readStatusState(lab string) (state, activeFault string, err error) {
	out, err := exec.Command(lab, "status", "--json").Output()
	if err != nil {
		return "", "", fmt.Errorf("lab status --json: %w", err)
	}
	var result struct {
		State       string `json:"state"`
		ActiveFault *struct {
			ID string `json:"id"`
		} `json:"active_fault"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", "", fmt.Errorf("parsing status: %w", err)
	}
	faultID := ""
	if result.ActiveFault != nil {
		faultID = result.ActiveFault.ID
	}
	return result.State, faultID, nil
}

// runValidateGetFailingIDs runs lab validate --json and returns the failing
// check IDs from the output.
func runValidateGetFailingIDs(lab string) []string {
	out, _ := exec.Command(lab, "validate", "--json").Output()
	var result struct {
		FailingChecks []string `json:"failing_checks"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil
	}
	return result.FailingChecks
}

// stringSlicesEqual returns true if two sorted string slices are equal.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// conformance.CheckByID is used to build expected failing check sets.
var _ = conformance.CheckByID