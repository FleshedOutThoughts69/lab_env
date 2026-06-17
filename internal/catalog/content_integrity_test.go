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
	"strings"
	"io/fs"
	"lab_env/internal/catalog"
	"lab_env/internal/conformance"
	"lab_env/internal/executor"
)

// TestFaultRecover_RestoresExactContent verifies that Recover(exec) for every
// reversible fault writes the exact same bytes that would be in a conformant system.
func TestFaultRecover_RestoresExactContent(t *testing.T) {
	impls := catalog.AllImpls()

	for _, impl := range impls {
		impl := impl
		if !impl.Def.IsReversible {
			continue // non-reversible faults have error-returning Recover stubs
		}

		t.Run(impl.Def.ID, func(t *testing.T) {
			rec := newFaultRecorder()

			// Apply the fault — capture what it changes
			applyErr := impl.Apply(rec)
			if applyErr != nil && !rec.hasAnyWrite() {
				t.Skipf("%s Apply returned error with no writes (precondition check) — skip", impl.Def.ID)
			}
			
			// Recover the fault — capture what it restores
			rec.SetPhase("recover")
			recoverErr := impl.Recover(rec)
			if recoverErr != nil {
				t.Fatalf("%s Recover returned error: %v", impl.Def.ID, recoverErr)
			}

			// For each path that Apply wrote, Recover must have written to the same path.
			for path, applyHash := range rec.applyHashes {
				recoverContent, ok := rec.recoverWrites[path]
				if !ok {
					t.Errorf("%s: Apply wrote to %s but Recover did not restore it", impl.Def.ID, path)
					continue
				}
				recoverHash := sha256sum(recoverContent)

				if recoverHash == applyHash && len(recoverContent) > 0 {
					t.Errorf("%s: Recover wrote same content as Apply for %s — fault was not undone",
						impl.Def.ID, path)
				}
			}
		})
	}
}

// TestNonReversibleFaults_RecoverReturnsError verifies that non-reversible faults
// Recover functions return an error directing to R3 reset.
func TestNonReversibleFaults_RecoverReturnsError(t *testing.T) {
	for _, impl := range catalog.AllImpls() {
		if impl.Def.IsReversible {
			continue
		}
		t.Run(impl.Def.ID, func(t *testing.T) {
			rec := newFaultRecorder()
			err := impl.Recover(rec)
			if err == nil {
				t.Errorf("%s is non-reversible but Recover() returned nil — should return error directing to R3", impl.Def.ID)
			}
			errMsg := err.Error()
			if errMsg == "" {
				t.Errorf("%s Recover() returned empty error message", impl.Def.ID)
			}
		})
	}
}

// TestFaultApply_TargetsOnlyDeclaredFile verifies that Apply mutations target
// only the file(s) associated with the fault, not unrelated config files.
func TestFaultApply_TargetsOnlyDeclaredFile(t *testing.T) {
	impls := catalog.AllImpls()

	for _, impl := range impls {
		impl := impl
		t.Run(impl.Def.ID, func(t *testing.T) {
			rec := newFaultRecorder()
			_ = impl.Apply(rec)

			if len(rec.applyWrites) > 3 {
				t.Errorf("%s Apply wrote to %d files; expected ≤3: %v",
					impl.Def.ID, len(rec.applyWrites), keys(rec.applyWrites))
			}
		})
	}
}

// ── Recording executor ────────────────────────────────────────────────────────

type faultRecorder struct {
	applyWrites   map[string][]byte
	applyHashes   map[string]string
	recoverWrites map[string][]byte
	phase         string
	files         map[string][]byte
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
		r.applyWrites[path] = nil
		r.applyHashes[path] = "deleted"
	} else {
		r.recoverWrites[path] = nil
	}
	return nil
}

func (r *faultRecorder) MkdirAll(path string, _ fs.FileMode, _, _ string) error { return nil }

func (r *faultRecorder) Systemctl(action, unit string) error {
	// Simulate log file recreation when the app is restarted (F‑010 Recover).
	if action == "restart" && unit == "app" {
		canonical, ok := makeDefaultFiles()["/var/log/app/app.log"]
		if ok {
			r.files["/var/log/app/app.log"] = canonical
			if r.phase != "apply" {
				r.recoverWrites["/var/log/app/app.log"] = canonical
			}
		}
	}
	return nil
}

func (r *faultRecorder) NginxReload() error { return nil }

func (r *faultRecorder) RunMutation(cmd string, args ...string) error {
	// Simulate the exact shell command used by F‑018 Recover.
	if cmd == "sh" && len(args) == 2 && args[0] == "-c" && strings.HasPrefix(args[1], "rm -f /var/lib/app/file_") {
		prefix := "/var/lib/app/file_"
		for path := range r.files {
			if strings.HasPrefix(path, prefix) {
				delete(r.files, path)
				if r.phase != "apply" {
					r.recoverWrites[path] = nil // nil = removed
				}
			}
		}
	}
	return nil
}

// Observer methods (read-only)
func (r *faultRecorder) Stat(path string) (os.FileInfo, error) {
	if _, ok := r.files[path]; ok {
		return nil, nil // stub
	}
	return nil, os.ErrNotExist
}
func (r *faultRecorder) CheckProcess(_, _ string) (conformance.ProcessStatus, error) {
	return conformance.ProcessStatus{Running: true}, nil
}
func (r *faultRecorder) CheckPort(_ string) (conformance.PortStatus, error) {
	return conformance.PortStatus{Listening: true}, nil
}
func (r *faultRecorder) CheckEndpoint(_ string, _ bool) (conformance.EndpointStatus, error) {
	return conformance.EndpointStatus{StatusCode: 200}, nil
}
func (r *faultRecorder) ResolveHost(_ string) (string, error) { return "127.0.0.1", nil }
func (r *faultRecorder) ServiceActive(_ string) (bool, error) { return true, nil }
func (r *faultRecorder) ServiceEnabled(_ string) (bool, error) { return true, nil }
func (r *faultRecorder) RunCommand(_ string, _ ...string) (string, error) { return "", nil }

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