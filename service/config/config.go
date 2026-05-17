// Package config loads and validates the service configuration.
//
// Configuration sources:
//   - /etc/app/config.yaml  — canonical environment config (server address, app_env)
//   - Environment variables — chaos injection variables set via systemd EnvironmentFile
//     at /etc/app/chaos.env
//
// Chaos variables are read once at startup. SIGHUP reload is intentionally NOT
// supported: the fault runbook (F-020) restarts the service to activate chaos modes,
// so runtime reload is unnecessary and would introduce split-semantics with log fd
// handling (copytruncate must not trigger log reopen on SIGHUP).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath is the canonical config file location.
// canonical-environment.md §2.3, internal/config/config.go AppConfigPath.
const DefaultConfigPath = "/etc/app/config.yaml"

// Config holds all runtime configuration for the service.
type Config struct {
	Server ServerConfig `yaml:"server"`
	AppEnv string       `yaml:"app_env"`
	Chaos  ChaosConfig  // populated from environment variables, not YAML
}

// ServerConfig holds network binding configuration.
type ServerConfig struct {
	// Addr is the address the service binds to.
	// Canonical value: "127.0.0.1:8080" (internal/config/config.go AppBindAddr).
	// Fault F-002 changes this to "127.0.0.1:9090" — P-002 check then fails.
	Addr string `yaml:"addr"`
}

// ChaosConfig holds chaos injection parameters.
// All values read from environment at startup via systemd EnvironmentFile.
// Application Runtime Contract §5.1.
type ChaosConfig struct {
	// LatencyMS adds a fixed delay to every request.
	// Source: CHAOS_LATENCY_MS env var. Default: 0 (no latency).
	// Fault F-020 sets this to 400.
	LatencyMS int

	// DropPercent randomly drops this percentage of requests with HTTP 503.
	// Source: CHAOS_DROP_PERCENT env var. Default: 0.
	DropPercent int

	// OOMTrigger causes the service to allocate memory until OOM killed.
	// Source: CHAOS_OOM_TRIGGER env var. Default: false.
	// WARNING: activating this will terminate the process. No recovery possible
	// without R3 reset. Equivalent to fault F-014 via chaos interface.
	OOMTrigger bool

	// IgnoreSIGTERM causes the service to mask SIGTERM.
	// Source: CHAOS_IGNORE_SIGTERM env var. Default: false.
	// Equivalent to fault F-008 via chaos interface.
	// When active: systemctl stop app will hang for ~90s until SIGKILL.
	IgnoreSIGTERM bool
}

// IsActive returns true if any chaos variable is non-default.
// Used to set chaos_active=true in telemetry.
func (c ChaosConfig) IsActive() bool {
	return c.LatencyMS > 0 || c.DropPercent > 0 || c.OOMTrigger || c.IgnoreSIGTERM
}

// ActiveModes returns the names of active chaos modes.
// Used to populate chaos_modes in telemetry.
// Application Runtime Contract §5.1: mode names are "latency", "drop", "oom", "nosigterm".
func (c ChaosConfig) ActiveModes() []string {
	var modes []string
	if c.LatencyMS > 0 {
		modes = append(modes, "latency")
	}
	if c.DropPercent > 0 {
		modes = append(modes, "drop")
	}
	if c.OOMTrigger {
		modes = append(modes, "oom")
	}
	if c.IgnoreSIGTERM {
		modes = append(modes, "nosigterm")
	}
	return modes
}

// Load reads the YAML config file and environment variables.
// Returns an error if the config file is missing, malformed, or contains
// unknown keys. Unknown keys are rejected to catch typos like "app_envv"
// that would otherwise silently leave app_env empty.
// Missing chaos env vars default to their zero values (no chaos).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true) // reject unknown keys — catches typos
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Apply defaults.
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = "127.0.0.1:8080"
	}
	if cfg.AppEnv == "" {
		cfg.AppEnv = "prod"
	}

	// Sanitize app_env: strip control characters and newlines.
	// The value is embedded in JSON response bodies and log lines.
	// A malformed value (e.g., "prod\nmalicious") would break line-oriented
	// log parsers and conformance check L-002 (last line valid JSON).
	cfg.AppEnv = sanitizeEnvString(cfg.AppEnv)

	// Load chaos variables from environment.
	// These are injected by systemd via EnvironmentFile=/etc/app/chaos.env.
	cfg.Chaos = loadChaos()

	return &cfg, nil
}

// loadChaos reads chaos injection variables from the process environment.
// Called once at startup; see package doc for why SIGHUP reload is not supported.
//
// Boolean variables accept: "1", "true", "yes" (case-insensitive) → true.
// All other values (including missing) → false.
// This allows lab operators to set variables manually without memorising
// the "1" convention.
func loadChaos() ChaosConfig {
	var c ChaosConfig

	if v := os.Getenv("CHAOS_LATENCY_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.LatencyMS = n
		}
	}
	if v := os.Getenv("CHAOS_DROP_PERCENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 100 {
			c.DropPercent = n
		}
	}
	if parseBool(os.Getenv("CHAOS_OOM_TRIGGER")) {
		c.OOMTrigger = true
	}
	if parseBool(os.Getenv("CHAOS_IGNORE_SIGTERM")) {
		c.IgnoreSIGTERM = true
	}

	return c
}

// parseBool returns true for "1", "true", "yes" (case-insensitive).
// Returns false for all other values including empty string.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// sanitizeEnvString strips ASCII control characters (0x00-0x1F, 0x7F) from s.
// Applied to app_env which is embedded in JSON response bodies and log lines.
// Newlines in app_env would break L-002 (last log line must be valid JSON).
// Only printable ASCII is permitted; non-ASCII Unicode is passed through.
func sanitizeEnvString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7F {
			b.WriteRune(r)
		}
	}
	return b.String()
}