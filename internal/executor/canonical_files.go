package executor

// canonical_files.go populates the canonicalFiles map using go:embed.
// These are the file contents RestoreFile writes during R2 reset.
//
// Source files live in internal/config/ alongside config.go.
// They are embedded at build time — no runtime filesystem access is needed.
//
// When R2 reset calls exec.RestoreFile(path), the executor looks up the path
// in canonicalFiles and writes the embedded content with the correct mode
// and ownership. This is the authoritative source for what "canonical" means
// for each file.
//
// Paths must match the constants in internal/config/config.go exactly:
//
//	ConfigPath   = "/etc/app/config.yaml"         ← config.yaml
//	UnitFilePath = "/etc/systemd/system/app.service" ← app.service
//	NginxConfigPath = "/etc/nginx/sites-enabled/app" ← nginx.conf
//
// Note: logrotate.conf is NOT in the R2RestoreFiles list because logrotate
// configuration is managed by the provisioning bootstrap, not by R2 reset.
// It is embedded here for reference and can be restored explicitly if needed.

import (
	_ "embed"

	"lab-env/lab/internal/config"
)

//go:embed ../config/config.yaml
var embeddedAppConfig []byte

//go:embed ../config/app.service
var embeddedUnitFile []byte

//go:embed ../config/nginx.conf
var embeddedNginxConfig []byte

// init populates canonicalFiles from the embedded file contents.
// Called automatically at package initialization before any executor is created.
// Ownership and modes are sourced from internal/config/config.go constants
// so they cannot drift from the canonical definitions.
func init() {
	canonicalFiles = map[string]canonicalFile{
		config.ConfigPath: {
			content: embeddedAppConfig,
			mode:    config.ModeConfig,
			owner:   config.ServiceUser,
			group:   config.ServiceGroup,
		},
		config.UnitFilePath: {
			content: embeddedUnitFile,
			mode:    config.ModeUnitFile,
			owner:   config.RootUser,
			group:   config.RootGroup,
		},
		config.NginxConfigPath: {
			content: embeddedNginxConfig,
			mode:    config.ModeNginxConfig,
			owner:   config.RootUser,
			group:   config.RootGroup,
		},
	}
}