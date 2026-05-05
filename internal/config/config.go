// Package config defines canonical constants for the lab environment.
// Every file path, permission mode, ownership, and service name used by
// the control plane is defined here. No command, fault, or executor code
// may hardcode these values.
//
// Values are derived from canonical-environment.md §2.3 (filesystem layout)
// and control-plane-contract.md §6 (state file paths).
package config

import "io/fs"

// ── Control plane paths ───────────────────────────────────────────────────────

const (
	// StatePath is the authoritative control-plane state file.
	StatePath = "/var/lib/lab/state.json"

	// AuditPath is the append-only mutation audit log.
	AuditPath = "/var/lib/lab/audit.log"

	// LockPath is the exclusive advisory mutation lock file.
	LockPath = "/var/lib/lab/lab.lock"

	// BootstrapScript is the provisioning script path.
	BootstrapScript = "/opt/lab-env/bootstrap.sh"
)

// ── Canonical environment paths (canonical-environment.md §2.3) ──────────────

const (
	// BinaryPath is the compiled Go service binary.
	BinaryPath = "/opt/app/server"

	// BinaryName is the process name as it appears in the process table.
	BinaryName = "server"

	// ConfigPath is the app configuration file.
	ConfigPath = "/etc/app/config.yaml"

	// LogDir is the application log directory.
	LogDir = "/var/log/app"

	// LogPath is the structured application log.
	LogPath = "/var/log/app/app.log"

	// StateDir is the runtime state directory.
	StateDir = "/var/lib/app"

	// StateTouchPath is the runtime state file touched on every / request.
	StateTouchPath = "/var/lib/app/state"

	// UnitFilePath is the systemd service unit file.
	UnitFilePath = "/etc/systemd/system/app.service"

	// NginxConfigPath is the nginx reverse proxy configuration.
	NginxConfigPath = "/etc/nginx/sites-enabled/app"

	// TLSCertPath is the self-signed TLS certificate.
	TLSCertPath = "/etc/nginx/tls/app.local.crt"

	// TLSKeyPath is the TLS private key.
	TLSKeyPath = "/etc/nginx/tls/app.local.key"
)

// ── Canonical file modes (canonical-environment.md §2.3) ─────────────────────

const (
	// ModeBinary is the required mode for /opt/app/server.
	ModeBinary fs.FileMode = 0750

	// ModeConfig is the required mode for /etc/app/config.yaml.
	ModeConfig fs.FileMode = 0640

	// ModeLogDir is the required mode for /var/log/app/.
	ModeLogDir fs.FileMode = 0755

	// ModeLogFile is the required mode for /var/log/app/app.log.
	ModeLogFile fs.FileMode = 0640

	// ModeStateDir is the required mode for /var/lib/app/.
	ModeStateDir fs.FileMode = 0755

	// ModeUnitFile is the required mode for systemd unit files.
	ModeUnitFile fs.FileMode = 0644

	// ModeNginxConfig is the required mode for nginx config files.
	ModeNginxConfig fs.FileMode = 0644
)

// ── Canonical ownership ───────────────────────────────────────────────────────

const (
	// ServiceUser is the non-privileged user that runs the app service.
	ServiceUser = "appuser"

	// ServiceGroup is the group for service-owned files.
	ServiceGroup = "appuser"

	// RootUser is the system root user for system-owned files.
	RootUser = "root"

	// RootGroup is the root group for system-owned files.
	RootGroup = "root"
)

// ── Service names ─────────────────────────────────────────────────────────────

const (
	// AppServiceName is the systemd unit name for the Go service.
	AppServiceName = "app.service"

	// NginxServiceName is the systemd unit name for nginx.
	NginxServiceName = "nginx"
)

// ── Network addresses ─────────────────────────────────────────────────────────

const (
	// AppBindAddr is the canonical address the app binds to.
	AppBindAddr = "127.0.0.1:8080"

	// NginxHTTPAddr is the address nginx listens on for HTTP.
	NginxHTTPAddr = "0.0.0.0:80"

	// NginxHTTPSAddr is the address nginx listens on for HTTPS.
	NginxHTTPSAddr = "0.0.0.0:443"

	// AppLocalHostname is the hostname for the TLS endpoint.
	AppLocalHostname = "app.local"
)

// ── R2 reset targets ──────────────────────────────────────────────────────────

// R2RestoreFiles lists the canonical file paths that R2 reset restores.
// These are the files whose canonical contents are embedded in the binary
// via the canonicalFiles map in executor/real.go.
var R2RestoreFiles = []string{
	ConfigPath,
	UnitFilePath,
	NginxConfigPath,
}

// R2RestoreModes lists the paths and their canonical modes for R2 reset.
// Applied after file restoration to ensure modes match the spec.
var R2RestoreModes = []struct {
	Path string
	Mode fs.FileMode
}{
	{BinaryPath, ModeBinary},
	{ConfigPath, ModeConfig},
	{StateDir, ModeStateDir},
	{LogDir, ModeLogDir},
}