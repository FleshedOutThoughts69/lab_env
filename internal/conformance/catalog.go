package conformance

import (
	"encoding/json"
	"fmt"
	"strings"

	cfg "lab-env/lab/internal/config"
)

// Catalog returns the complete ordered check registry as defined in
// conformance-model.md §3. Checks are returned in dependency order:
// S-series, P-series, E-series, F-series, L-series.
//
// The check definitions here are the executable expression of the
// conformance model catalog. Check IDs, severities, and categories
// are authoritative. Observable commands are included for documentation
// and output; the Execute function is the actual test.

// CheckByID returns the Check with the given ID from the catalog, or nil if
// no check with that ID exists. Used by cmd/fault.go to evaluate
// PreconditionChecks before executing Apply (control-plane-contract §4.5 step 5).
func CheckByID(id string) *Check {
	for _, c := range Catalog() {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func Catalog() []*Check {
	return []*Check{
		// ── S-series: System State Checks ───────────────────────────────
		{
			ID:                "S-001",
			Category:          CategorySystemState,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "app.service is active",
			FailureMeaning:    "App process is not running",
			ObservableCommand: "systemctl is-active app.service --quiet",
			Execute: func(o Observer) error {
				active, err := o.ServiceActive(cfg.AppServiceName)
				if err != nil {
					return fmt.Errorf("checking app.service state: %w", err)
				}
				if !active {
					return fmt.Errorf("app.service is not active")
				}
				return nil
			},
		},
		{
			ID:                "S-002",
			Category:          CategorySystemState,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "app.service is enabled",
			FailureMeaning:    "App will not start on reboot",
			ObservableCommand: "systemctl is-enabled app.service --quiet",
			Execute: func(o Observer) error {
				enabled, err := o.ServiceEnabled(cfg.AppServiceName)
				if err != nil {
					return fmt.Errorf("checking app.service enablement: %w", err)
				}
				if !enabled {
					return fmt.Errorf("app.service is not enabled")
				}
				return nil
			},
		},
		{
			ID:                "S-003",
			Category:          CategorySystemState,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "nginx is active",
			FailureMeaning:    "Proxy is not running; no traffic reaches app",
			ObservableCommand: "systemctl is-active nginx --quiet",
			Execute: func(o Observer) error {
				active, err := o.ServiceActive(cfg.NginxServiceName)
				if err != nil {
					return fmt.Errorf("checking nginx state: %w", err)
				}
				if !active {
					return fmt.Errorf("nginx is not active")
				}
				return nil
			},
		},
		{
			ID:                "S-004",
			Category:          CategorySystemState,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "nginx is enabled",
			FailureMeaning:    "nginx will not start on reboot",
			ObservableCommand: "systemctl is-enabled nginx --quiet",
			Execute: func(o Observer) error {
				enabled, err := o.ServiceEnabled(cfg.NginxServiceName)
				if err != nil {
					return fmt.Errorf("checking nginx enablement: %w", err)
				}
				if !enabled {
					return fmt.Errorf("nginx is not enabled")
				}
				return nil
			},
		},

		// ── P-series: Process Checks ─────────────────────────────────────
		{
			ID:                "P-001",
			Category:          CategoryProcess,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         "App process runs as appuser",
			FailureMeaning:    "Service is running with wrong identity — security violation",
			ObservableCommand: "pgrep -u appuser server > /dev/null",
			Execute: func(o Observer) error {
				ps, err := o.CheckProcess(cfg.BinaryName, cfg.ServiceUser)
				if err != nil {
					return fmt.Errorf("checking app process: %w", err)
				}
				if !ps.Running {
					return fmt.Errorf("no %q process running as %s", cfg.BinaryName, cfg.ServiceUser)
				}
				return nil
			},
		},
		{
			ID:                "P-002",
			Category:          CategoryProcess,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         "App listens on 127.0.0.1:8080",
			FailureMeaning:    "App bound to wrong address or port; nginx upstream unreachable",
			ObservableCommand: "ss -ltnp | grep -q '127.0.0.1:8080'",
			Execute: func(o Observer) error {
				ps, err := o.CheckPort(cfg.AppBindAddr)
				if err != nil {
					return fmt.Errorf("checking port 8080: %w", err)
				}
				if !ps.Listening {
					return fmt.Errorf("no listener on %s", cfg.AppBindAddr)
				}
				return nil
			},
		},
		{
			ID:                "P-003",
			Category:          CategoryProcess,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         "nginx listens on 0.0.0.0:80",
			FailureMeaning:    "No HTTP traffic can reach the system",
			ObservableCommand: "ss -ltnp | grep -q '0.0.0.0:80'",
			Execute: func(o Observer) error {
				ps, err := o.CheckPort(cfg.NginxHTTPAddr)
				if err != nil {
					return fmt.Errorf("checking port 80: %w", err)
				}
				if !ps.Listening {
					return fmt.Errorf("no listener on %s", cfg.NginxHTTPAddr)
				}
				return nil
			},
		},
		{
			ID:                "P-004",
			Category:          CategoryProcess,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         "nginx listens on 0.0.0.0:443",
			FailureMeaning:    "No HTTPS traffic can reach the system",
			ObservableCommand: "ss -ltnp | grep -q '0.0.0.0:443'",
			Execute: func(o Observer) error {
				ps, err := o.CheckPort(cfg.NginxHTTPSAddr)
				if err != nil {
					return fmt.Errorf("checking port 443: %w", err)
				}
				if !ps.Listening {
					return fmt.Errorf("no listener on %s", cfg.NginxHTTPSAddr)
				}
				return nil
			},
		},

		// ── E-series: Endpoint Checks ─────────────────────────────────────
		{
			ID:                "E-001",
			Category:          CategoryEndpoint,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         "GET /health returns HTTP 200",
			FailureMeaning:    "App is not serving health checks; process may be running but not functional",
			ObservableCommand: "curl -sf http://localhost/health > /dev/null",
			Execute: func(o Observer) error {
				ep, err := o.CheckEndpoint("http://localhost/health", false)
				if err != nil {
					return fmt.Errorf("reaching /health: %w", err)
				}
				if ep.StatusCode != 200 {
					return fmt.Errorf("/health returned %d, want 200", ep.StatusCode)
				}
				return nil
			},
		},
		{
			ID:                "E-002",
			Category:          CategoryEndpoint,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         "GET / returns HTTP 200",
			FailureMeaning:    "Primary request path is failing",
			ObservableCommand: "curl -sf http://localhost/ > /dev/null",
			Execute: func(o Observer) error {
				ep, err := o.CheckEndpoint("http://localhost/", false)
				if err != nil {
					return fmt.Errorf("reaching /: %w", err)
				}
				if ep.StatusCode != 200 {
					return fmt.Errorf("/ returned %d, want 200", ep.StatusCode)
				}
				return nil
			},
		},
		{
			ID:                "E-003",
			Category:          CategoryEndpoint,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         `/health body contains "status":"ok"`,
			FailureMeaning:    "App is responding but not confirming config loaded",
			ObservableCommand: `curl -s http://localhost/health | jq -e '.status == "ok"' > /dev/null`,
			Execute: func(o Observer) error {
				ep, err := o.CheckEndpoint("http://localhost/health", false)
				if err != nil {
					return fmt.Errorf("reaching /health for body check: %w", err)
				}
				if ep.StatusCode != 200 {
					return fmt.Errorf("/health returned %d, cannot check body", ep.StatusCode)
				}
				var body struct {
					Status string `json:"status"`
				}
				if err := json.Unmarshal(ep.Body, &body); err != nil {
					return fmt.Errorf("parsing /health body: %w", err)
				}
				if body.Status != "ok" {
					return fmt.Errorf(`/health body status=%q, want "ok"`, body.Status)
				}
				return nil
			},
		},
		{
			ID:                "E-004",
			Category:          CategoryEndpoint,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         "Response includes X-Proxy: nginx header",
			FailureMeaning:    "nginx is not proxying — traffic reaching app directly or response from wrong source",
			ObservableCommand: "curl -sI http://localhost/ | grep -q 'X-Proxy: nginx'",
			Execute: func(o Observer) error {
				// We use RunCommand here because CheckEndpoint does not
				// expose response headers. The observable command is the
				// canonical test.
				out, err := o.RunCommand("curl", "-sI", "http://localhost/")
				if err != nil {
					return fmt.Errorf("fetching headers from /: %w", err)
				}
				if !strings.Contains(out, "X-Proxy: nginx") {
					return fmt.Errorf("X-Proxy: nginx header absent from response")
				}
				return nil
			},
		},
		{
			ID:                "E-005",
			Category:          CategoryEndpoint,
			Layer:             LayerBehavioral,
			Severity:          SeverityBlocking,
			Assertion:         "GET https://app.local/health returns 200 (skip verify)",
			FailureMeaning:    "TLS listener or upstream not functioning",
			ObservableCommand: "curl -skf https://app.local/health > /dev/null",
			Execute: func(o Observer) error {
				ep, err := o.CheckEndpoint("https://"+cfg.AppLocalHostname+"/health", true)
				if err != nil {
					return fmt.Errorf("reaching https://%s/health: %w", cfg.AppLocalHostname, err)
				}
				if ep.StatusCode != 200 {
					return fmt.Errorf("https://%s/health returned %d, want 200", cfg.AppLocalHostname, ep.StatusCode)
				}
				return nil
			},
		},

		// ── F-series: Filesystem Checks ──────────────────────────────────
		// Note on naming: these check IDs (F-001 through F-007) share the
		// F-NNN prefix with fault catalog IDs (F-001 through F-018).
		// These are distinct namespaces. See conformance-model.md §3.1.
		{
			ID:                "F-001",
			Category:          CategoryFilesystem,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "/opt/app/server exists, owned appuser:appuser, mode 750",
			FailureMeaning:    "Binary missing, wrong ownership, or not executable",
			ObservableCommand: "stat -c '%U:%G %a' /opt/app/server | grep -q 'appuser:appuser 750'",
			Execute: func(o Observer) error {
				return checkFileOwnerMode(o, cfg.BinaryPath, cfg.ServiceUser, cfg.ServiceGroup, "750")
			},
		},
		{
			ID:                "F-002",
			Category:          CategoryFilesystem,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "/etc/app/config.yaml exists, owned appuser:appuser, mode 640",
			FailureMeaning:    "Config missing, wrong ownership, or unreadable by appuser",
			ObservableCommand: "stat -c '%U:%G %a' /etc/app/config.yaml | grep -q 'appuser:appuser 640'",
			Execute: func(o Observer) error {
				return checkFileOwnerMode(o, cfg.ConfigPath, cfg.ServiceUser, cfg.ServiceGroup, "640")
			},
		},
		{
			ID:                "F-003",
			Category:          CategoryFilesystem,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "/var/log/app/ exists, owned appuser:appuser, mode 755",
			FailureMeaning:    "Log directory missing or wrong permissions",
			ObservableCommand: "stat -c '%U:%G %a' /var/log/app | grep -q 'appuser:appuser 755'",
			Execute: func(o Observer) error {
				return checkFileOwnerMode(o, cfg.LogDir, cfg.ServiceUser, cfg.ServiceGroup, "755")
			},
		},
		{
			ID:                "F-004",
			Category:          CategoryFilesystem,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "/var/lib/app/ exists, owned appuser:appuser, mode 755",
			FailureMeaning:    "State directory missing or wrong permissions — / will return 500",
			ObservableCommand: "stat -c '%U:%G %a' /var/lib/app | grep -q 'appuser:appuser 755'",
			Execute: func(o Observer) error {
				return checkFileOwnerMode(o, cfg.StateDir, cfg.ServiceUser, cfg.ServiceGroup, "755")
			},
		},
		{
			ID:                "F-005",
			Category:          CategoryFilesystem,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "nginx configuration passes syntax check",
			FailureMeaning:    "nginx config has syntax error; nginx will not reload",
			ObservableCommand: "nginx -t 2>/dev/null",
			Execute: func(o Observer) error {
				// nginx -t writes to stderr; a zero exit means valid config.
				_, err := o.RunCommand("nginx", "-t")
				if err != nil {
					return fmt.Errorf("nginx config syntax check failed: %w", err)
				}
				return nil
			},
		},
		{
			ID:                "F-006",
			Category:          CategoryFilesystem,
			Layer:             LayerStructural,
			Severity:          SeverityDegraded,
			Assertion:         "TLS certificate exists and has not expired",
			FailureMeaning:    "HTTPS will fail; certificate requires renewal",
			ObservableCommand: "openssl x509 -checkend 0 -noout -in /etc/nginx/tls/app.local.crt",
			Execute: func(o Observer) error {
				_, err := o.RunCommand("openssl", "x509", "-checkend", "0", "-noout",
					"-in", cfg.TLSCertPath)
				if err != nil {
					return fmt.Errorf("TLS certificate expired or missing: %w", err)
				}
				return nil
			},
		},
		{
			ID:                "F-007",
			Category:          CategoryFilesystem,
			Layer:             LayerStructural,
			Severity:          SeverityBlocking,
			Assertion:         "app.local resolves to 127.0.0.1",
			FailureMeaning:    "TLS hostname resolution broken; HTTPS problems will be misattributed",
			ObservableCommand: "getent hosts app.local | grep -q '127.0.0.1'",
			Execute: func(o Observer) error {
				addr, err := o.ResolveHost(cfg.AppLocalHostname)
				if err != nil {
					return fmt.Errorf("resolving %s: %w", cfg.AppLocalHostname, err)
				}
				if addr != "127.0.0.1" {
					return fmt.Errorf("%s resolves to %q, want 127.0.0.1", cfg.AppLocalHostname, addr)
				}
				return nil
			},
		},

		// ── L-series: Log Checks ──────────────────────────────────────────
		{
			ID:                "L-001",
			Category:          CategoryLog,
			Layer:             LayerOperational,
			Severity:          SeverityDegraded,
			Assertion:         "/var/log/app/app.log exists and is non-empty",
			FailureMeaning:    "No log output — app may not be logging, or log file was deleted",
			ObservableCommand: "test -s /var/log/app/app.log",
			Execute: func(o Observer) error {
				info, err := o.Stat(cfg.LogPath)
				if err != nil {
					return fmt.Errorf("stat app.log: %w", err)
				}
				if info.Size() == 0 {
					return fmt.Errorf("app.log exists but is empty")
				}
				return nil
			},
		},
		{
			ID:                "L-002",
			Category:          CategoryLog,
			Layer:             LayerOperational,
			Severity:          SeverityDegraded,
			Assertion:         "Last line of app.log is valid JSON",
			FailureMeaning:    "Log is corrupted or format has changed",
			ObservableCommand: "tail -1 /var/log/app/app.log | jq . > /dev/null 2>&1",
			Execute: func(o Observer) error {
				data, err := o.ReadFile(cfg.LogPath)
				if err != nil {
					return fmt.Errorf("reading app.log: %w", err)
				}
				lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
				if len(lines) == 0 || lines[len(lines)-1] == "" {
					return fmt.Errorf("app.log is empty")
				}
				lastLine := lines[len(lines)-1]
				var v interface{}
				if err := json.Unmarshal([]byte(lastLine), &v); err != nil {
					return fmt.Errorf("last line of app.log is not valid JSON: %w", err)
				}
				return nil
			},
		},
		{
			ID:                "L-003",
			Category:          CategoryLog,
			Layer:             LayerOperational,
			Severity:          SeverityDegraded,
			Assertion:         `app.log contains a startup entry`,
			FailureMeaning:    "App started but startup log was not produced — logging failure",
			ObservableCommand: `grep -q '"msg":"server started"' /var/log/app/app.log`,
			Execute: func(o Observer) error {
				data, err := o.ReadFile(cfg.LogPath)
				if err != nil {
					return fmt.Errorf("reading app.log: %w", err)
				}
				if !strings.Contains(string(data), `"msg":"server started"`) {
					return fmt.Errorf(`app.log does not contain startup entry ("msg":"server started")`)
				}
				return nil
			},
		},
	}
}

// checkFileOwnerMode is a helper used by F-series checks to verify that a
// path exists with the expected owner, group, and octal permission mode.
func checkFileOwnerMode(o Observer, path, wantOwner, wantGroup, wantMode string) error {
	out, err := o.RunCommand("stat", "-c", "%U:%G %a", path)
	if err != nil {
		return fmt.Errorf("%s: stat failed: %w", path, err)
	}
	out = strings.TrimSpace(out)
	want := wantOwner + ":" + wantGroup + " " + wantMode
	if out != want {
		return fmt.Errorf("%s: got %q, want %q", path, out, want)
	}
	return nil
}