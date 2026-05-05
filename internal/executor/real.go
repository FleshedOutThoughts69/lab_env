package executor

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"lab_env/internal/conformance"
)

// Real implements both conformance.Observer and Executor against the
// actual Ubuntu VM system. It is the only implementation that makes
// real system calls.
//
// The audit logger is nil for Observer-only operations. Mutation operations
// require a non-nil audit logger (enforced at the executor contract level —
// the Executor interface carries the audit logger as a dependency).
type Real struct {
	audit    *AuditLogger
	canonMap map[string]canonicalFile // path → canonical content + mode + ownership
}

type canonicalFile struct {
	content []byte
	mode    fs.FileMode
	owner   string
	group   string
}

// NewObserver returns a Real configured for read-only use.
// Used by the conformance runner and state detection logic.
// No audit logger is attached — Observer operations are never audited.
func NewObserver() *Real {
	return &Real{}
}

// NewExecutor returns a Real configured for mutation operations.
// The audit logger must be non-nil.
// The canonical file map is populated from the embedded config content.
func NewExecutor(audit *AuditLogger) *Real {
	r := &Real{
		audit:    audit,
		canonMap: buildCanonicalMap(),
	}
	return r
}

// ── Observer implementation ──────────────────────────────────────────────────

func (r *Real) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func (r *Real) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (r *Real) CheckProcess(name, user string) (conformance.ProcessStatus, error) {
	args := []string{"-x", name}
	if user != "" {
		args = append([]string{"-u", user}, args...)
	}
	out, err := r.runRaw("pgrep", args...)
	if err != nil {
		// pgrep exits 1 when no process found — not a system error.
		return conformance.ProcessStatus{Running: false}, nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	pid := 0
	if len(lines) > 0 && lines[0] != "" {
		fmt.Sscanf(lines[0], "%d", &pid)
	}
	return conformance.ProcessStatus{Running: pid > 0, PID: pid, User: user}, nil
}

func (r *Real) CheckPort(addr string) (conformance.PortStatus, error) {
	// Use ss -ltnp to check for a listening TCP socket on the given address.
	out, err := r.runRaw("ss", "-ltnp")
	if err != nil {
		return conformance.PortStatus{}, fmt.Errorf("running ss: %w", err)
	}
	listening := strings.Contains(out, addr)
	return conformance.PortStatus{Listening: listening, Addr: addr}, nil
}

func (r *Real) CheckEndpoint(url string, skipTLSVerify bool) (conformance.EndpointStatus, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	if skipTLSVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}

	resp, err := client.Get(url)
	if err != nil {
		return conformance.EndpointStatus{Reachable: false}, nil
	}
	defer resp.Body.Close()

	var body []byte
	// Read body for E-003 (status field check) — limit to 4KB.
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body = buf[:n]

	return conformance.EndpointStatus{
		StatusCode: resp.StatusCode,
		Reachable:  true,
		Body:       body,
	}, nil
}

func (r *Real) ResolveHost(name string) (string, error) {
	addrs, err := net.LookupHost(name)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", name, err)
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no addresses for %q", name)
	}
	return addrs[0], nil
}

func (r *Real) ServiceActive(unit string) (bool, error) {
	_, err := r.runRaw("systemctl", "is-active", "--quiet", unit)
	return err == nil, nil
}

func (r *Real) ServiceEnabled(unit string) (bool, error) {
	_, err := r.runRaw("systemctl", "is-enabled", "--quiet", unit)
	return err == nil, nil
}

func (r *Real) RunCommand(cmd string, args ...string) (string, error) {
	return r.runRaw(cmd, args...)
}

// ── Executor implementation ──────────────────────────────────────────────────

func (r *Real) WriteFile(path string, data []byte, mode fs.FileMode, owner, group string) error {
	start := time.Now()
	err := r.atomicWrite(path, data, mode, owner, group)
	if r.audit != nil {
		code := 0
		if err != nil {
			code = 1
		}
		r.audit.LogOp("WriteFile", path, time.Since(start).Milliseconds(), code, err)
	}
	return err
}

func (r *Real) Chmod(path string, mode fs.FileMode) error {
	start := time.Now()
	err := os.Chmod(path, mode)
	if r.audit != nil {
		code := 0
		if err != nil {
			code = 1
		}
		r.audit.LogOp("Chmod", fmt.Sprintf("%s %04o", path, mode), time.Since(start).Milliseconds(), code, err)
	}
	return err
}

func (r *Real) Chown(path, owner, group string) error {
	start := time.Now()
	_, err := r.runSudo("chown", owner+":"+group, path)
	if r.audit != nil {
		code := 0
		if err != nil {
			code = 1
		}
		r.audit.LogOp("Chown", fmt.Sprintf("%s %s:%s", path, owner, group), time.Since(start).Milliseconds(), code, err)
	}
	return err
}

func (r *Real) Remove(path string) error {
	start := time.Now()
	err := os.Remove(path)
	if r.audit != nil {
		code := 0
		if err != nil {
			code = 1
		}
		r.audit.LogOp("Remove", path, time.Since(start).Milliseconds(), code, err)
	}
	return err
}

func (r *Real) MkdirAll(path string, mode fs.FileMode, owner, group string) error {
	start := time.Now()
	err := os.MkdirAll(path, mode)
	if err == nil && (owner != "" || group != "") {
		_, err = r.runSudo("chown", owner+":"+group, path)
	}
	if r.audit != nil {
		code := 0
		if err != nil {
			code = 1
		}
		r.audit.LogOp("MkdirAll", path, time.Since(start).Milliseconds(), code, err)
	}
	return err
}

func (r *Real) Systemctl(action, unit string) error {
	start := time.Now()
	args := []string{"systemctl", action}
	if unit != "" {
		args = append(args, unit)
	}
	_, err := r.runSudo(args[0], args[1:]...)
	if r.audit != nil {
		code := 0
		if err != nil {
			code = 1
		}
		r.audit.LogOp("Systemctl", strings.Join(args[1:], " "), time.Since(start).Milliseconds(), code, err)
	}
	return err
}

func (r *Real) NginxReload() error {
	start := time.Now()
	// Verify config syntax before reloading — never reload a broken config.
	if _, err := r.runSudo("nginx", "-t"); err != nil {
		return fmt.Errorf("nginx config syntax check failed: %w", err)
	}
	_, err := r.runSudo("nginx", "-s", "reload")
	if r.audit != nil {
		code := 0
		if err != nil {
			code = 1
		}
		r.audit.LogOp("NginxReload", "", time.Since(start).Milliseconds(), code, err)
	}
	return err
}

func (r *Real) RestoreFile(path string) error {
	canon, ok := r.canonMap[path]
	if !ok {
		return fmt.Errorf("no canonical content for %q", path)
	}
	return r.WriteFile(path, canon.content, canon.mode, canon.owner, canon.group)
}

// RunMutation executes a privileged system command that mutates state.
// Every call is audited. This is the correct path for operations that
// change system state but do not fit the named mutation methods.
// MUST NOT be used for read-only operations — use RunCommand (Observer).
func (r *Real) RunMutation(cmd string, args ...string) error {
	start := time.Now()
	opArgs := cmd
	if len(args) > 0 {
		opArgs = cmd + " " + strings.Join(args, " ")
	}
	_, err := r.runSudo(cmd, args...)
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	if r.audit != nil {
		r.audit.LogOp("RunMutation", opArgs, time.Since(start).Milliseconds(), exitCode, err)
	}
	return err
}

// ── internal helpers ─────────────────────────────────────────────────────────

func (r *Real) atomicWrite(path string, data []byte, mode fs.FileMode, owner, group string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating parent directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("setting temp file mode: %w", err)
	}
	if owner != "" || group != "" {
		if _, err := r.runSudo("chown", owner+":"+group, tmpPath); err != nil {
			return fmt.Errorf("setting temp file ownership: %w", err)
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp file to %s: %w", path, err)
	}

	success = true
	return nil
}

func (r *Real) runRaw(cmd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := exec.CommandContext(ctx, cmd, args...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}

func (r *Real) runSudo(cmd string, args ...string) (string, error) {
	allArgs := append([]string{cmd}, args...)
	return r.runRaw("sudo", allArgs...)
}

// buildCanonicalMap returns the embedded canonical file contents.
// This is the source of truth used by RestoreFile during R2 reset.
// Contents are embedded at build time from lab-env/config/.
func buildCanonicalMap() map[string]canonicalFile {
	// The canonical file contents will be embedded via go:embed in a
	// separate file (canonical_files.go) generated from lab-env/config/.
	// This function returns the map; the embed directives populate it.
	// For now, returns an empty map — populated by canonical_files.go.
	return canonicalFiles
}

// canonicalFiles is populated by canonical_files.go via go:embed.
// Declared here so the rest of the package can reference it.
var canonicalFiles = map[string]canonicalFile{}