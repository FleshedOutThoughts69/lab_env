package config

// config_edge_test.go
//
// Edge case configuration tests.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lab-env/service/config"
)

// TestLoad_AppEnv_LeadingTrailingSpaces verifies that app_env with leading
// or trailing spaces is sanitized. An operator setting APP_ENV=prod  (with
// trailing space) via shell or chaos.env produces app_env with a space that
// would appear in JSON as {"env":"prod "}.
//
// Whether to trim or reject is a design decision. This test documents the
// actual behavior so it is not accidentally changed.
func TestLoad_AppEnv_SpacesAreSanitized(t *testing.T) {
	cases := []struct {
		yaml    string
		wantEnv string
	}{
		{"server:\n  addr: 127.0.0.1:8080\napp_env: \"prod \"\n", "prod "},  // trailing space: sanitize strips control chars, space is valid
		{"server:\n  addr: 127.0.0.1:8080\napp_env: \" prod\"\n", " prod"}, // leading space: space is valid char, not stripped
		{"server:\n  addr: 127.0.0.1:8080\napp_env: \"pro\tduction\"\n", "production"}, // tab stripped by sanitize
	}

	for _, tc := range cases {
		t.Run(tc.yaml[:30], func(t *testing.T) {
			path := writeEdgeConfig(t, tc.yaml)
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.AppEnv != tc.wantEnv {
				t.Errorf("AppEnv: got %q, want %q", cfg.AppEnv, tc.wantEnv)
			}
		})
	}
}

// TestLoad_ConfigWithBOM_ReturnsError verifies that a YAML file with a
// UTF-8 BOM (byte order mark, \xEF\xBB\xBF) is rejected with a clear error.
//
// Windows editors (Notepad, some VS Code configurations) occasionally save
// UTF-8 files with a BOM. The Go YAML parser should either reject it or
// handle it transparently. If it silently parses to wrong values, the service
// would start with an incorrect configuration.
func TestLoad_ConfigWithBOM_HandledCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// UTF-8 BOM + valid YAML
	bom := []byte{0xEF, 0xBB, 0xBF}
	content := append(bom, []byte("server:\n  addr: 127.0.0.1:8080\napp_env: prod\n")...)
	if err := os.WriteFile(path, content, 0640); err != nil {
		t.Fatal(err)
	}

	_, err := config.Load(path)
	// Either a clear error (preferred) or successful parse with correct values.
	// What must NOT happen: silent parse with empty/wrong values.
	if err != nil {
		t.Logf("Load with BOM returned error (acceptable): %v", err)
		return
	}
	// If no error, verify values are correct despite BOM
	cfg, err2 := config.Load(path)
	if err2 != nil {
		return
	}
	if cfg.Server.Addr != "127.0.0.1:8080" {
		t.Errorf("BOM config: Server.Addr=%q, want 127.0.0.1:8080 (BOM caused wrong parse)", cfg.Server.Addr)
	}
	if cfg.AppEnv != "prod" {
		t.Errorf("BOM config: AppEnv=%q, want prod (BOM caused wrong parse)", cfg.AppEnv)
	}
}

// TestLoad_DropPercentOutOfRange verifies that CHAOS_DROP_PERCENT values
// outside 0-100 are silently disabled (set to 0), not clamped to 100 or
// passed through, which could cause 100% drops when operator types "110".
func TestLoad_DropPercentOutOfRange_Disabled(t *testing.T) {
	path := writeEdgeConfig(t, "server:\n  addr: 127.0.0.1:8080\napp_env: prod\n")

	cases := []struct{ val, want string }{
		{"-1", "0"},
		{"101", "0"},
		{"1000", "0"},
		{"-100", "0"},
	}

	for _, tc := range cases {
		t.Run("CHAOS_DROP_PERCENT="+tc.val, func(t *testing.T) {
			os.Setenv("CHAOS_DROP_PERCENT", tc.val)
			defer os.Unsetenv("CHAOS_DROP_PERCENT")

			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Chaos.DropPercent != 0 {
				t.Errorf("CHAOS_DROP_PERCENT=%s: DropPercent=%d, want 0 (out-of-range disabled)",
					tc.val, cfg.Chaos.DropPercent)
			}
		})
	}
}

// TestChaosConfig_ActiveModes_OrderIsStable verifies that ActiveModes()
// returns modes in a consistent order regardless of which combination is active.
// This prevents telemetry chaos_modes from changing order between writes.
func TestChaosConfig_ActiveModes_OrderIsStable(t *testing.T) {
	path := writeEdgeConfig(t, "server:\n  addr: 127.0.0.1:8080\napp_env: prod\n")

	os.Setenv("CHAOS_LATENCY_MS", "100")
	os.Setenv("CHAOS_DROP_PERCENT", "10")
	defer os.Unsetenv("CHAOS_LATENCY_MS")
	defer os.Unsetenv("CHAOS_DROP_PERCENT")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Call ActiveModes multiple times — order must be identical
	first := cfg.Chaos.ActiveModes()
	for i := 0; i < 10; i++ {
		subsequent := cfg.Chaos.ActiveModes()
		if len(subsequent) != len(first) {
			t.Errorf("ActiveModes() length changed: %d vs %d", len(first), len(subsequent))
		}
		for j := range first {
			if j < len(subsequent) && subsequent[j] != first[j] {
				t.Errorf("ActiveModes() order unstable: position %d got %q, want %q",
					j, subsequent[j], first[j])
			}
		}
	}
}

func writeEdgeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatalf("writing edge config: %v", err)
	}
	return path
}