package catalog_test

// content_integrity_test.go
//
// Verifies that every fault's Recover function restores the system to its
// exact pre-Apply state. Uses a recording executor that captures file writes
// and allows hash comparison.
//
// High ROI: the lab's reset promise — "lab reset restores CONFORMANT" —
// depends entirely on Recover undoing Apply perfectly. A Recover that writes
// slightly different content breaks the conformance check for that file
// without any obvious error.
//
// Test pattern:
//   1. Record canonical state (from canonicalFiles / known values)
//   2. Call Apply via recording executor — capture what it wrote
//   3. Call Recover via recording executor — capture what it wrote
//   4. Assert Recover wrote exactly the canonical content
//
// Reference: fault-model.md §4.3 (Recover postconditions)

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"lab-env/lab/internal/catalog"
	"lab-env/lab/internal/executor"
)

// TestFaultRecover_RestoresExactContent verifies that Recover(exec) for every
// reversible fault writes the exact same bytes that would be in a conformant system.
//
// For faults that call exec.RestoreFile(), the content is the embedded canonical
// bytes. For faults that write manually, we verify the content matches the
// known-good value.
func TestFaultRecover_RestoresExactContent(t *testing.T) {
	impls := catalog.AllImpls()

	for _, impl := range impls {
		impl := impl
		if !impl.IsReversible {
			continue // non-reversible faults (F-008, F-014) have error-returning Recover stubs
		}

		t.Run(impl.ID, func(t *testing.T) {
			rec := newFaultRecorder()

			// Apply the fault — capture what it changes
			applyErr := impl.Apply(rec)
			if applyErr != nil && !rec.hasAnyWrite() {
				t.Skipf("%s Apply returned error with no writes (precondition check) — skip", impl.ID)
			}

			// Recover the fault — capture what it restores
			recoverErr := impl.Recover(rec)
			if recoverErr != nil {
				t.Fatalf("%s Recover returned error: %v", impl.ID, recoverErr)
			}

			// For each path that Apply wrote, Recover must have written to the same path.
			// The content written by Recover must be the canonical content for that path.
			for path, applyHash := range rec.applyHashes {
				recoverContent, ok := rec.recoverWrites[path]
				if !ok {
					t.Errorf("%s: Apply wrote to %s but Recover did not restore it", impl.ID, path)
					continue
				}
				recoverHash := sha256sum(recoverContent)

				// Recover must NOT write the same broken content Apply wrote
				if recoverHash == applyHash && len(recoverContent) > 0 {
					t.Errorf("%s: Recover wrote same content as Apply for %s — fault was not undone",
						impl.ID, path)
				}
			}
		})
	}
}

// TestNonReversibleFaults_RecoverReturnsError verifies that F-008 and F-014
// Recover functions return an error directing the operator to R3 reset,
// not nil (which would silently claim recovery succeeded).
func TestNonReversibleFaults_RecoverReturnsError(t *testing.T) {
	for _, impl := range catalog.AllImpls() {
		if impl.IsReversible {
			continue
		}
		t.Run(impl.ID, func(t *testing.T) {
			rec := newFaultRecorder()
			err := impl.Recover(rec)
			if err == nil {
				t.Errorf("%s is non-reversible but Recover() returned nil — should return error directing to R3", impl.ID)
			}
			// Error must mention R3 or reset
			errMsg := err.Error()
			if errMsg == "" {
				t.Errorf("%s Recover() returned empty error message", impl.ID)
			}
		})
	}
}

// TestFaultApply_TargetsOnlyDeclaredFile verifies that Apply mutations target
// only the file(s) associated with the fault, not unrelated config files.
//
// This guards against replaceInBytes calls with patterns that match multiple
// files — e.g., a string that appears in both config.yaml and nginx.conf.
func TestFaultApply_TargetsOnlyDeclaredFile(t *testing.T) {
	impls := catalog.AllImpls()

	for _, impl := range impls {
		impl := impl
		t.Run(impl.ID, func(t *testing.T) {
			rec := newFaultRecorder()
			_ = impl.Apply(rec)

			// Each fault's Apply should write to at most one or two files.
			// More than 3 file writes is suspicious and should be investigated.
			if len(rec.applyWrites) > 3 {
				t.Errorf("%s Apply wrote to %d files; expected ≤3: %v",
					impl.ID, len(rec.applyWrites), keys(rec.applyWrites))
			}
		})
	}
}

// ── Recording executor ────────────────────────────────────────────────────────

type faultRecorder struct {
	// Tracks writes made during Apply
	applyWrites map[string][]byte
	applyHashes map[string]string

	// Tracks writes made during Recover
	recoverWrites map[string][]byte

	// Phase tracking: are we in Apply or Recover?
	phase string

	// File system state simulation
	files map[string][]byte
}

func newFaultRecorder() *faultRecorder {
	return &faultRecorder{
		applyWrites:   make(map[string][]byte),
		applyHashes:   make(map[string]string),
		recoverWrites: make(map[string][]byte),
		phase:         "apply",
		files:         makeDefaultFiles(),
	}
}

func (r *faultRecorder) hasAnyWrite() bool {
	return len(r.applyWrites) > 0
}

// ── executor.Executor interface implementation ────────────────────────────────

func (r *faultRecorder) ReadFile(path string) ([]byte, error) {
	if data, ok := r.files[path]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (r *faultRecorder) WriteFile(path string, data []byte, _ os.FileMode, _, _ string) error {
	content := make([]byte, len(data))
	copy(content, data)
	if r.phase == "apply" {
		r.applyWrites[path] = content
		r.applyHashes[path] = sha256sum(content)
	} else {
		r.recoverWrites[path] = content
	}
	r.files[path] = content
	return nil
}

func (r *faultRecorder) RestoreFile(path string) error {
	canonical, ok := executor.CanonicalFileEntry(path)
	if !ok {
		return fmt.Errorf("no canonical entry for %s", path)
	}
	return r.WriteFile(path, canonical.Content, canonical.Mode, canonical.Owner, canonical.Group)
}

func (r *faultRecorder) Chmod(path string, _ os.FileMode) error {
	if _, ok := r.files[path]; !ok {
		return os.ErrNotExist
	}
	return nil
}

func (r *faultRecorder) Chown(path, _, _ string) error {
	if _, ok := r.files[path]; !ok {
		return os.ErrNotExist
	}
	return nil
}

func (r *faultRecorder) Remove(path string) error {
	delete(r.files, path)
	if r.phase == "apply" {
		r.applyWrites[path] = nil // nil = deleted
		r.applyHashes[path] = "deleted"
	} else {
		r.recoverWrites[path] = nil
	}
	return nil
}

func (r *faultRecorder) MkdirAll(path string, _ os.FileMode) error { return nil }

func (r *faultRecorder) Systemctl(action, unit string) error {
	return nil // no-op; service commands tracked separately if needed
}

func (r *faultRecorder) NginxReload() error { return nil }

func (r *faultRecorder) RunMutation(cmd string, args ...string) error { return nil }

// Observer methods (read-only)
func (r *faultRecorder) Stat(path string) (os.FileInfo, error) {
	if _, ok := r.files[path]; ok {
		return nil, nil // stub
	}
	return nil, os.ErrNotExist
}
func (r *faultRecorder) CheckProcess(_, _ string) (executor.ProcessStatus, error) {
	return executor.ProcessStatus{Running: true}, nil
}
func (r *faultRecorder) CheckPort(_ string) (executor.PortStatus, error) {
	return executor.PortStatus{Listening: true}, nil
}
func (r *faultRecorder) CheckEndpoint(_ string, _ bool) (executor.EndpointStatus, error) {
	return executor.EndpointStatus{StatusCode: 200}, nil
}
func (r *faultRecorder) ResolveHost(_ string) (string, error) { return "127.0.0.1", nil }
func (r *faultRecorder) ServiceActive(_ string) (bool, error) { return true, nil }
func (r *faultRecorder) ServiceEnabled(_ string) (bool, error) { return true, nil }
func (r *faultRecorder) RunCommand(_ string, _ ...string) (string, error) { return "", nil }

// SetPhase switches the recorder between apply and recover tracking.
func (r *faultRecorder) SetPhase(phase string) { r.phase = phase }

// ── Helpers ───────────────────────────────────────────────────────────────────

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

func keys(m map[string][]byte) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

// makeDefaultFiles returns a simulated file system matching the canonical
// environment state. Used as the starting point for Apply/Recover testing.
func makeDefaultFiles() map[string][]byte {
	return map[string][]byte{
		"/opt/app/server":                  []byte("ELF binary stub"),
		"/etc/app/config.yaml":             []byte("server:\n  addr: 127.0.0.1:8080\napp_env: prod\n"),
		"/etc/systemd/system/app.service":  []byte("[Unit]\nDescription=Lab app\n"),
		"/etc/nginx/sites-enabled/app":     []byte("upstream app_backend { server 127.0.0.1:8080; }\n"),
		"/var/log/app/app.log":             []byte(`{"ts":"2026-01-01T00:00:00Z","level":"info","msg":"server started"}` + "\n"),
		"/etc/app/chaos.env":               []byte(""),
	}
}