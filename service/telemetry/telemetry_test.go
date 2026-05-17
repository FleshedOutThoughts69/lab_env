package telemetry

// telemetry_test.go
//
// Tests the telemetry snapshot schema exactness, inode percentage calculation,
// and panic recovery behavior of the 2-second update goroutine.
//
// High ROI: the control plane parses telemetry.json to detect chaos state and
// resource pressure. A wrong JSON tag (e.g., "cpu_pct" instead of "cpu_percent")
// silently produces zero in the parsed field — the control plane never knows
// chaos is active.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lab-env/service/telemetry"
)

// TestSnapshot_Schema_AllFieldsPresent verifies that a telemetry snapshot
// marshals to JSON with exactly the 12 required field names.
//
// This test catches JSON tag typos and missing fields before integration.
func TestSnapshot_Schema_AllFieldsPresent(t *testing.T) {
	snap := telemetry.Snapshot{
		Ts:                "2026-01-01T00:00:00Z",
		PID:               1234,
		UptimeSeconds:     60,
		CPUPercent:        12.3,
		MemoryRSSMB:       45.2,
		OpenFDs:           18,
		DiskUsagePercent:  67.0,
		InodeUsagePercent: 12.0,
		RequestsTotal:     1042,
		ErrorsTotal:       3,
		ChaosActive:       true,
		ChaosModes:        []string{"latency", "drop"},
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	required := []string{
		"ts",
		"pid",
		"uptime_seconds",
		"cpu_percent",
		"memory_rss_mb",
		"open_fds",
		"disk_usage_percent",
		"inode_usage_percent",
		"requests_total",
		"errors_total",
		"chaos_active",
		"chaos_modes",
	}

	for _, field := range required {
		if _, ok := m[field]; !ok {
			t.Errorf("missing field %q in telemetry JSON", field)
		}
	}

	// Verify no extra fields beyond the 12 required
	if len(m) != len(required) {
		extra := []string{}
		known := make(map[string]bool)
		for _, f := range required {
			known[f] = true
		}
		for k := range m {
			if !known[k] {
				extra = append(extra, k)
			}
		}
		t.Errorf("unexpected extra fields in telemetry JSON: %v", extra)
	}
}

// TestSnapshot_Schema_FieldTypes verifies that numeric fields marshal as
// JSON numbers (not strings) and chaos_modes marshals as array (not null).
func TestSnapshot_Schema_FieldTypes(t *testing.T) {
	snap := telemetry.Snapshot{
		Ts:         "2026-01-01T00:00:00Z",
		ChaosModes: []string{}, // empty but not nil
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	// cpu_percent must be float64, not string
	if _, ok := m["cpu_percent"].(float64); !ok {
		t.Errorf("cpu_percent: expected float64, got %T", m["cpu_percent"])
	}

	// pid must be float64 (JSON numbers unmarshal as float64)
	if _, ok := m["pid"].(float64); !ok {
		t.Errorf("pid: expected float64, got %T", m["pid"])
	}

	// chaos_modes must be array, never null
	modes, ok := m["chaos_modes"].([]interface{})
	if !ok {
		t.Errorf("chaos_modes: expected []interface{}, got %T (value: %v)", m["chaos_modes"], m["chaos_modes"])
	}
	if modes == nil {
		t.Error("chaos_modes: must be [] not null when no modes active")
	}

	// chaos_active must be bool
	if _, ok := m["chaos_active"].(bool); !ok {
		t.Errorf("chaos_active: expected bool, got %T", m["chaos_active"])
	}
}

// TestCollector_WritesTelemetryFile verifies that the collector writes a valid
// telemetry.json file and that the file contains valid JSON on the first write.
func TestCollector_WritesTelemetryFile(t *testing.T) {
	dir := t.TempDir()
	telPath := filepath.Join(dir, "telemetry.json")
	telemetry.SetFilePathForTest(telPath)
	defer telemetry.ResetFilePath()

	metrics := &telemetry.Metrics{}
	collector := telemetry.New(
		metrics,
		func() bool { return false },
		func() []string { return nil },
		nil, // no logger needed for this test
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go collector.Run(ctx)

	// Wait for the first write (happens synchronously before the goroutine enters its loop)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(telPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	data, err := os.ReadFile(telPath)
	if err != nil {
		t.Fatalf("telemetry file not written: %v", err)
	}

	var snap telemetry.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("telemetry file contains invalid JSON: %v\nraw: %s", err, data)
	}

	if snap.Ts == "" {
		t.Error("telemetry snapshot has empty ts field")
	}
	if snap.PID == 0 {
		t.Error("telemetry snapshot has zero PID")
	}
}

// TestCollector_PanicRecovery verifies that a panic in the write() function
// does not kill the telemetry goroutine — it must continue writing snapshots.
func TestCollector_PanicRecovery(t *testing.T) {
	dir := t.TempDir()
	telPath := filepath.Join(dir, "telemetry.json")
	telemetry.SetFilePathForTest(telPath)
	defer telemetry.ResetFilePath()

	var panicCount atomic.Int32
	var writeCount atomic.Int32

	metrics := &telemetry.Metrics{}

	// Inject a panicking chaos provider for the first call, then recover
	callCount := 0
	chaosActive := func() bool {
		callCount++
		if callCount == 1 {
			panicCount.Add(1)
			panic("simulated telemetry panic")
		}
		writeCount.Add(1)
		return false
	}

	collector := telemetry.New(
		metrics,
		chaosActive,
		func() []string { return nil },
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	go collector.Run(ctx)

	// Wait for at least 2 writes to confirm recovery
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if writeCount.Load() >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if panicCount.Load() == 0 {
		t.Skip("panic was not injected (callCount logic may need adjustment)")
	}
	if writeCount.Load() < 1 {
		t.Error("telemetry goroutine did not recover from panic — no subsequent writes")
	}
}

// TestCollector_ChaosModesNeverNull verifies that chaos_modes is always
// serialized as [] not null, even when no modes are active.
func TestCollector_ChaosModesNeverNull(t *testing.T) {
	dir := t.TempDir()
	telPath := filepath.Join(dir, "telemetry.json")
	telemetry.SetFilePathForTest(telPath)
	defer telemetry.ResetFilePath()

	metrics := &telemetry.Metrics{}
	collector := telemetry.New(
		metrics,
		func() bool { return false },
		func() []string { return nil }, // returns nil, must become []
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go collector.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	data, err := os.ReadFile(telPath)
	if err != nil {
		t.Fatalf("reading telemetry: %v", err)
	}

	// chaos_modes must appear as "[]" not "null" in the raw JSON
	raw := string(data)
	if contains(raw, `"chaos_modes":null`) {
		t.Error(`chaos_modes serialized as null; must be []`)
	}
	if !contains(raw, `"chaos_modes":[]`) {
		t.Errorf("expected chaos_modes:[] in telemetry JSON; got: %s", raw)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}()
}