package config

// config_test.go
//
// Tests configuration loading correctness.
//
// High ROI:
//   - KnownFields strict mode: a typo like "app_envv" silently produces
//     empty app_env, causing "/" to return {"env":""} — E-002 passes but
//     wrong diagnostic output breaks the fault matrix
//   - parseBool: an operator setting CHAOS_OOM_TRIGGER=true (not "1") would
//     silently not activate OOM, making F-014 appear to not work
//   - sanitizeEnvString: a newline in app_env breaks log JSON and L-002

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lab-env/service/config"
)

// TestLoad_ValidConfig_Parses verifies that a well-formed config.yaml parses
// correctly with correct default field values.
func TestLoad_ValidConfig_Parses(t *testing.T) {
	path := writeConfig(t, "server:\n  addr: 127.0.0.1:8080\napp_env: prod\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != "127.0.0.1:8080" {
		t.Errorf("Server.Addr: got %q, want 127.0.0.1:8080", cfg.Server.Addr)
	}
	if cfg.AppEnv != "prod" {
		t.Errorf("AppEnv: got %q, want prod", cfg.AppEnv)
	}
}

// TestLoad_MissingFile_ReturnsError verifies that Load returns an error
// when the config file is absent, causing the service to exit at startup
// rather than continuing with empty defaults.
//
// This is the correct behavior: fault F-001 deletes the config, and the
// expected diagnostic is S-001 failure (service not running), not a running
// service with empty configuration.
func TestLoad_MissingFile_ReturnsError(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Load with missing file: expected error, got nil")
	}
}

// TestLoad_UnknownKey_ReturnsError verifies that strict YAML parsing rejects
// unknown keys. A typo like "app_envv" must fail, not silently produce empty env.
func TestLoad_UnknownKey_ReturnsError(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "typo in top-level key",
			yaml: "server:\n  addr: 127.0.0.1:8080\napp_envv: prod\n", // 'app_envv' not 'app_env'
		},
		{
			name: "unknown nested key",
			yaml: "server:\n  addr: 127.0.0.1:8080\n  port: 8080\napp_env: prod\n", // 'port' not in schema
		},
		{
			name: "extra top-level key",
			yaml: "server:\n  addr: 127.0.0.1:8080\napp_env: prod\nversion: 1\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.yaml)
			_, err := config.Load(path)
			if err == nil {
				t.Errorf("Load with unknown key %q: expected error (KnownFields strict), got nil", tc.name)
			}
		})
	}
}

// TestLoad_DefaultAddr_WhenMissing verifies that server.addr defaults to
// "127.0.0.1:8080" if the config file omits it.
func TestLoad_DefaultAddr_WhenMissing(t *testing.T) {
	path := writeConfig(t, "app_env: prod\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != "127.0.0.1:8080" {
		t.Errorf("Server.Addr default: got %q, want 127.0.0.1:8080", cfg.Server.Addr)
	}
}

// TestLoad_DefaultAppEnv_WhenEmpty verifies that an empty app_env defaults to "prod".
func TestLoad_DefaultAppEnv_WhenEmpty(t *testing.T) {
	path := writeConfig(t, "server:\n  addr: 127.0.0.1:8080\napp_env: \"\"\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppEnv != "prod" {
		t.Errorf("AppEnv default: got %q, want prod", cfg.AppEnv)
	}
}

// TestParseBool_AcceptsMultipleTrueValues verifies that CHAOS_OOM_TRIGGER
// and CHAOS_IGNORE_SIGTERM accept "1", "true", and "yes" (case-insensitive).
//
// An operator typing CHAOS_OOM_TRIGGER=true in a shell one-liner or chaos.env
// file must activate the mode. Without this, F-014/F-008 appear to not work.
func TestParseBool_AcceptsMultipleTrueValues(t *testing.T) {
	trueValues := []string{"1", "true", "TRUE", "True", "yes", "YES", "Yes"}
	falseValues := []string{"0", "false", "no", "FALSE", "NO", "", "maybe", "2"}

	for _, v := range trueValues {
		t.Run("true:"+v, func(t *testing.T) {
			path := writeConfig(t, "server:\n  addr: 127.0.0.1:8080\napp_env: prod\n")
			os.Setenv("CHAOS_OOM_TRIGGER", v)
			defer os.Unsetenv("CHAOS_OOM_TRIGGER")

			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !cfg.Chaos.OOMTrigger {
				t.Errorf("CHAOS_OOM_TRIGGER=%q: expected OOMTrigger=true, got false", v)
			}
		})
	}

	for _, v := range falseValues {
		t.Run("false:"+v, func(t *testing.T) {
			path := writeConfig(t, "server:\n  addr: 127.0.0.1:8080\napp_env: prod\n")
			os.Setenv("CHAOS_OOM_TRIGGER", v)
			defer os.Unsetenv("CHAOS_OOM_TRIGGER")

			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Chaos.OOMTrigger {
				t.Errorf("CHAOS_OOM_TRIGGER=%q: expected OOMTrigger=false, got true", v)
			}
		})
	}
}

// TestLoad_MissingChaosVars_AllDefault verifies that when no CHAOS_* env vars
// are set, all chaos fields default to their zero values (no chaos active).
func TestLoad_MissingChaosVars_AllDefault(t *testing.T) {
	// Ensure no CHAOS vars are set
	for _, k := range []string{"CHAOS_LATENCY_MS", "CHAOS_DROP_PERCENT", "CHAOS_OOM_TRIGGER", "CHAOS_IGNORE_SIGTERM"} {
		os.Unsetenv(k)
	}

	path := writeConfig(t, "server:\n  addr: 127.0.0.1:8080\napp_env: prod\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Chaos.LatencyMS != 0 {
		t.Errorf("LatencyMS: got %d, want 0", cfg.Chaos.LatencyMS)
	}
	if cfg.Chaos.DropPercent != 0 {
		t.Errorf("DropPercent: got %d, want 0", cfg.Chaos.DropPercent)
	}
	if cfg.Chaos.OOMTrigger {
		t.Error("OOMTrigger: got true, want false")
	}
	if cfg.Chaos.IgnoreSIGTERM {
		t.Error("IgnoreSIGTERM: got true, want false")
	}
	if cfg.Chaos.IsActive() {
		t.Error("IsActive(): got true with no chaos vars set; want false")
	}
	if len(cfg.Chaos.ActiveModes()) != 0 {
		t.Errorf("ActiveModes(): got %v, want empty", cfg.Chaos.ActiveModes())
	}
}

// TestSanitizeEnvString_StripsControlChars verifies that control characters
// and newlines are stripped from app_env before embedding in JSON responses.
//
// A newline in app_env would produce a multi-line body from GET /, breaking
// L-002 (last line of app.log must be valid JSON) and conformance check parsing.
func TestSanitizeEnvString_StripsControlChars(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"prod", "prod"},
		{"pro\nd", "prod"},          // newline stripped
		{"pro\td", "prod"},          // tab stripped
		{"pro\x00d", "prod"},        // null byte stripped
		{"pro\x1bd", "prod"},        // ESC stripped
		{"pro\x7fd", "prod"},        // DEL stripped
		{"prod\n", "prod"},          // trailing newline
		{"\nprod", "prod"},          // leading newline
		{"pro duction", "pro duction"}, // space preserved
		{"prod-v2", "prod-v2"},      // hyphens preserved
		{"prod_v2", "prod_v2"},      // underscores preserved
		{"", ""},                    // empty → stays empty (default applied separately)
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			// Load a config where app_env would produce the raw value.
			// We use the sanitize function directly if exported, or via Load.
			got := config.SanitizeEnvString(tc.raw)
			if got != tc.want {
				t.Errorf("SanitizeEnvString(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestLoad_InvalidLatencyMS_DisablesMode verifies that a non-integer
// CHAOS_LATENCY_MS is silently disabled, not a crash.
func TestLoad_InvalidLatencyMS_DisablesMode(t *testing.T) {
	path := writeConfig(t, "server:\n  addr: 127.0.0.1:8080\napp_env: prod\n")
	os.Setenv("CHAOS_LATENCY_MS", "abc")
	defer os.Unsetenv("CHAOS_LATENCY_MS")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load with invalid latency: %v", err)
	}
	if cfg.Chaos.LatencyMS != 0 {
		t.Errorf("invalid CHAOS_LATENCY_MS 'abc': LatencyMS should be 0, got %d", cfg.Chaos.LatencyMS)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}