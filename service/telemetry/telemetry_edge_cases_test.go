package telemetry

// telemetry_edge_test.go
//
// Edge case tests for the telemetry collector.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lab-env/service/telemetry"
)

// TestCollector_UptimeSeconds_Monotonicallyincreasing verifies that consecutive
// telemetry snapshots have strictly increasing uptime_seconds.
//
// A clock bug or incorrect start time recording would produce non-monotonic
// uptime, which would break dashboard displays and diagnostic tooling.
func TestCollector_UptimeSeconds_MonotonicallyIncreasing(t *testing.T) {
	dir := t.TempDir()
	telPath := filepath.Join(dir, "telemetry.json")
	telemetry.SetFilePathForTest(telPath)
	defer telemetry.ResetFilePath()

	metrics := &telemetry.Metrics{}
	collector := telemetry.New(
		metrics,
		func() bool { return false },
		func() []string { return nil },
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go collector.Run(ctx)

	// Collect two samples separated by at least 2 seconds (one update interval)
	readSnapshot := func() telemetry.Snapshot {
		for i := 0; i < 20; i++ {
			data, err := os.ReadFile(telPath)
			if err == nil && len(data) > 0 {
				var snap telemetry.Snapshot
				if json.Unmarshal(data, &snap) == nil {
					return snap
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("telemetry file not written within 2s")
		return telemetry.Snapshot{}
	}

	first := readSnapshot()

	// Wait for at least one update interval
	time.Sleep(telemetry.UpdateInterval + 200*time.Millisecond)

	// Remove the file so we can detect the new write
	os.Remove(telPath)
	time.Sleep(telemetry.UpdateInterval + 200*time.Millisecond)

	second := readSnapshot()

	if second.UptimeSeconds <= first.UptimeSeconds {
		t.Errorf("uptime_seconds not monotonically increasing: first=%d second=%d",
			first.UptimeSeconds, second.UptimeSeconds)
	}
}

// TestCollector_WrittenWithZeroRequests verifies that telemetry.json is written
// even before any requests arrive, with requests_total=0 and errors_total=0.
//
// The control plane uses telemetry file presence as a liveness signal.
// If the file only appears after the first request, the control plane would
// misclassify a freshly started service as unhealthy.
func TestCollector_WrittenWithZeroRequests(t *testing.T) {
	dir := t.TempDir()
	telPath := filepath.Join(dir, "telemetry.json")
	telemetry.SetFilePathForTest(telPath)
	defer telemetry.ResetFilePath()

	metrics := &telemetry.Metrics{}
	// No requests have been made — counters are zero
	collector := telemetry.New(
		metrics,
		func() bool { return false },
		func() []string { return nil },
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go collector.Run(ctx)

	// Give it time to write
	time.Sleep(300 * time.Millisecond)

	data, err := os.ReadFile(telPath)
	if err != nil {
		t.Fatalf("telemetry not written with zero requests: %v", err)
	}

	var snap telemetry.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if snap.RequestsTotal != 0 {
		t.Errorf("requests_total: got %d, want 0 (no requests made)", snap.RequestsTotal)
	}
	if snap.ErrorsTotal != 0 {
		t.Errorf("errors_total: got %d, want 0 (no requests made)", snap.ErrorsTotal)
	}
	if snap.PID == 0 {
		t.Error("pid is 0; must be the actual process ID")
	}
	if snap.Ts == "" {
		t.Error("ts is empty; must be RFC3339 timestamp")
	}
}

// TestCollector_MemoryRSSMB_NonZeroWhenRunning verifies that memory_rss_mb
// is non-zero when the process is running.
//
// A parsing bug (e.g., wrong field name in /proc/self/status) would produce
// 0.0, masking OOM conditions and making memory monitoring useless.
func TestCollector_MemoryRSSMB_NonZeroWhenRunning(t *testing.T) {
	dir := t.TempDir()
	telPath := filepath.Join(dir, "telemetry.json")
	telemetry.SetFilePathForTest(telPath)
	defer telemetry.ResetFilePath()

	metrics := &telemetry.Metrics{}
	collector := telemetry.New(
		metrics,
		func() bool { return false },
		func() []string { return nil },
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go collector.Run(ctx)
	time.Sleep(300 * time.Millisecond)

	data, _ := os.ReadFile(telPath)
	var snap telemetry.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// A running Go process should have at least 5 MiB RSS
	if snap.MemoryRSSMB < 1.0 {
		t.Errorf("memory_rss_mb: got %.2f; expected >= 1.0 MiB for a running process", snap.MemoryRSSMB)
	}
}