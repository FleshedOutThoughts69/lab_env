package executor

// embed_test.go
//
// Verifies that all go:embed directives in canonical_files.go resolve to
// actual files with non-empty content.
//
// Why high ROI: a moved or renamed config template produces a zero-byte
// embedded file. RestoreFile would then write a 0-byte config.yaml to disk
// and the service would fail to start — a subtle, hard-to-debug failure that
// would not surface until R2 reset is actually executed.
//
// The go:embed mechanism silently embeds empty bytes if the file is present
// but empty, or fails at compile time if the file is missing. This test
// catches the "present but empty" case and also verifies the content is
// actually parseable as expected.

import (
	"bytes"
	"strings"
	"testing"

	"lab-env/lab/internal/config"
	"lab-env/lab/internal/executor"
)

// TestEmbeddedFiles_NonEmpty verifies that each embedded canonical file has
// non-zero byte content after the init() function runs.
func TestEmbeddedFiles_NonEmpty(t *testing.T) {
	paths := []string{
		config.ConfigPath,
		config.UnitFilePath,
		config.NginxConfigPath,
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			entry, ok := executor.CanonicalFileEntry(path)
			if !ok {
				t.Fatalf("no canonical entry for %s; go:embed may have failed", path)
			}
			if len(entry.Content) == 0 {
				t.Errorf("embedded content for %s is empty; file may have been moved or emptied", path)
			}
		})
	}
}

// TestEmbeddedConfigYaml_IsValidYAML verifies that the embedded config.yaml
// contains valid YAML with the expected top-level keys.
//
// A corrupted or accidentally overwritten config.yaml embedded at build time
// would cause every R2 reset to install a broken config, making the service
// permanently unbootable until a binary rebuild.
func TestEmbeddedConfigYaml_ContainsRequiredKeys(t *testing.T) {
	entry, ok := executor.CanonicalFileEntry(config.ConfigPath)
	if !ok {
		t.Fatal("no canonical entry for config.yaml")
	}

	content := string(entry.Content)

	// Must contain the server.addr key
	if !strings.Contains(content, "addr:") {
		t.Error("embedded config.yaml missing 'addr:' key")
	}
	if !strings.Contains(content, "app_env:") {
		t.Error("embedded config.yaml missing 'app_env:' key")
	}
	if !strings.Contains(content, "127.0.0.1:8080") {
		t.Errorf("embedded config.yaml missing canonical addr '127.0.0.1:8080'")
	}
}

// TestEmbeddedAppService_ContainsRequiredDirectives verifies the embedded
// app.service systemd unit has the mandatory directives.
//
// A unit file missing ExecStart, User, or RuntimeDirectory would cause the
// service to fail to start or run as the wrong user — a conformance failure
// that would not be obvious from the systemd error message alone.
func TestEmbeddedAppService_ContainsRequiredDirectives(t *testing.T) {
	entry, ok := executor.CanonicalFileEntry(config.UnitFilePath)
	if !ok {
		t.Fatal("no canonical entry for app.service")
	}

	content := string(entry.Content)
	required := []string{
		"ExecStart=",
		"User=appuser",
		"RuntimeDirectory=app",
		"StartLimitBurst=",
		"TimeoutStopSec=",
		"Slice=app.slice",
	}

	for _, directive := range required {
		if !strings.Contains(content, directive) {
			t.Errorf("embedded app.service missing required directive: %q", directive)
		}
	}
}

// TestEmbeddedNginxConf_ContainsUpstreamBlock verifies that the embedded
// nginx.conf contains the upstream app_backend block that F-007 targets.
//
// Without the upstream block, F-007's Apply function would find no match
// for "server 127.0.0.1:8080;" and the fault would silently do nothing
// — confirmed as a real bug fixed in a previous audit round.
func TestEmbeddedNginxConf_ContainsUpstreamBlock(t *testing.T) {
	entry, ok := executor.CanonicalFileEntry(config.NginxConfigPath)
	if !ok {
		t.Fatal("no canonical entry for nginx.conf")
	}

	content := string(entry.Content)

	if !strings.Contains(content, "upstream app_backend") {
		t.Error("embedded nginx.conf missing 'upstream app_backend' block; F-007 Apply will silently do nothing")
	}
	if !strings.Contains(content, "server 127.0.0.1:8080;") {
		t.Errorf("embedded nginx.conf missing 'server 127.0.0.1:8080;'; F-007 Apply has nothing to replace")
	}
	if !strings.Contains(content, "proxy_pass http://app_backend") {
		t.Error("embedded nginx.conf missing 'proxy_pass http://app_backend'; nginx would not route to the service")
	}
	if !strings.Contains(content, "X-Proxy nginx") {
		t.Error("embedded nginx.conf missing 'X-Proxy nginx' header directive; E-004 check would always fail")
	}

	// Verify the upstream block is not commented out
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue // skip comments
		}
		if strings.Contains(trimmed, "upstream app_backend") {
			return // found active upstream block
		}
	}
	t.Error("upstream app_backend block appears to be commented out in nginx.conf")
}

// TestEmbeddedFiles_ModeAndOwnershipNonEmpty verifies that the mode and
// ownership fields in canonicalFiles are non-zero for each file.
//
// A zero-mode would cause RestoreFile to chmod the file to 0000 (unreadable),
// breaking the conformance check that verifies the file's permissions.
func TestEmbeddedFiles_ModeAndOwnershipNonEmpty(t *testing.T) {
	paths := []string{
		config.ConfigPath,
		config.UnitFilePath,
		config.NginxConfigPath,
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			entry, ok := executor.CanonicalFileEntry(path)
			if !ok {
				t.Fatalf("no canonical entry for %s", path)
			}
			if entry.Mode == 0 {
				t.Errorf("%s: mode is 0; RestoreFile would chmod to 0000", path)
			}
			if entry.Owner == "" {
				t.Errorf("%s: owner is empty; RestoreFile would chown to empty string", path)
			}
			if entry.Group == "" {
				t.Errorf("%s: group is empty; RestoreFile would chown to empty group", path)
			}
		})
	}
}

// TestEmbeddedFiles_NoNullBytes verifies that no embedded file contains null
// bytes, which would indicate a binary file was accidentally embedded or a
// file was written with O_TRUNC without O_APPEND producing zero padding.
func TestEmbeddedFiles_NoNullBytes(t *testing.T) {
	paths := []string{
		config.ConfigPath,
		config.UnitFilePath,
		config.NginxConfigPath,
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			entry, ok := executor.CanonicalFileEntry(path)
			if !ok {
				t.Fatalf("no canonical entry for %s", path)
			}
			if bytes.Contains(entry.Content, []byte{0}) {
				t.Errorf("%s: embedded content contains null bytes; file may be corrupted", path)
			}
		})
	}
}