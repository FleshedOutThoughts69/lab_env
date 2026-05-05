package conformance

// runner_test.go validates the conformance engine: check execution,
// severity classification, dependency marking, and suite result derivation.
// All tests use a mock Observer — no real system calls.

import (
	"fmt"
	"io/fs"
	"testing"
	"time"


)

// ── Mock Observer ─────────────────────────────────────────────────────────────

// mockObserver implements Observer with configurable responses.
// Zero value is safe: all methods return "passing" defaults.
type mockObserver struct {
	serviceActive  map[string]bool
	serviceEnabled map[string]bool
	processes      map[string]ProcessStatus
	ports          map[string]PortStatus
	endpoints      map[string]EndpointStatus
	files          map[string][]byte
	fileStats      map[string]mockFileInfo
	hostResolution map[string]string
	commandOutputs map[string]string
	commandErrors  map[string]error
}

type mockFileInfo struct {
	size int64
	mode fs.FileMode
}

func (m *mockFileInfo) Name() string      { return "" }
func (m *mockFileInfo) Size() int64       { return m.size }
func (m *mockFileInfo) Mode() fs.FileMode { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool       { return false }
func (m *mockFileInfo) Sys() interface{}  { return nil }

func newMock() *mockObserver {
	return &mockObserver{
		serviceActive:  map[string]bool{},
		serviceEnabled: map[string]bool{},
		processes:      map[string]ProcessStatus{},
		ports:          map[string]PortStatus{},
		endpoints:      map[string]EndpointStatus{},
		files:          map[string][]byte{},
		fileStats:      map[string]mockFileInfo{},
		hostResolution: map[string]string{},
		commandOutputs: map[string]string{},
		commandErrors:  map[string]error{},
	}
}

func (m *mockObserver) ServiceActive(unit string) (bool, error) {
	active, ok := m.serviceActive[unit]
	return ok && active, nil
}
func (m *mockObserver) ServiceEnabled(unit string) (bool, error) {
	enabled, ok := m.serviceEnabled[unit]
	return ok && enabled, nil
}
func (m *mockObserver) CheckProcess(name, user string) (ProcessStatus, error) {
	key := name + "@" + user
	ps, ok := m.processes[key]
	if !ok {
		return ProcessStatus{Running: false}, nil
	}
	return ps, nil
}
func (m *mockObserver) CheckPort(addr string) (PortStatus, error) {
	ps, ok := m.ports[addr]
	if !ok {
		return PortStatus{Listening: false, Addr: addr}, nil
	}
	return ps, nil
}
func (m *mockObserver) CheckEndpoint(url string, _ bool) (EndpointStatus, error) {
	ep, ok := m.endpoints[url]
	if !ok {
		return EndpointStatus{Reachable: false, StatusCode: 0}, nil
	}
	return ep, nil
}
func (m *mockObserver) ResolveHost(name string) (string, error) {
	addr, ok := m.hostResolution[name]
	if !ok {
		return "", fmt.Errorf("no resolution for %q", name)
	}
	return addr, nil
}
func (m *mockObserver) Stat(path string) (fs.FileInfo, error) {
	info, ok := m.fileStats[path]
	if !ok {
		return nil, fmt.Errorf("stat %s: no such file", path)
	}
	return &info, nil
}
func (m *mockObserver) ReadFile(path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("readfile %s: no such file", path)
	}
	return data, nil
}
func (m *mockObserver) RunCommand(cmd string, args ...string) (string, error) {
	key := cmd
	if len(args) > 0 {
		key = cmd + " " + args[0]
	}
	if err, ok := m.commandErrors[key]; ok {
		return "", err
	}
	if out, ok := m.commandOutputs[key]; ok {
		return out, nil
	}
	return "", nil
}

// conformantMock returns a mock observer set up to pass all 23 checks.
func conformantMock() *mockObserver {
	m := newMock()

	// S-series
	m.serviceActive["app.service"] = true
	m.serviceEnabled["app.service"] = true
	m.serviceActive["nginx"] = true
	m.serviceEnabled["nginx"] = true

	// P-series
	m.processes["server@appuser"] = ProcessStatus{Running: true, PID: 1234, User: "appuser"}
	m.ports["127.0.0.1:8080"] = PortStatus{Listening: true, Addr: "127.0.0.1:8080"}
	m.ports["0.0.0.0:80"] = PortStatus{Listening: true, Addr: "0.0.0.0:80"}
	m.ports["0.0.0.0:443"] = PortStatus{Listening: true, Addr: "0.0.0.0:443"}

	// E-series
	m.endpoints["http://localhost/health"] = EndpointStatus{
		StatusCode: 200, Reachable: true,
		Body: []byte(`{"status":"ok"}`),
	}
	m.endpoints["http://localhost/"] = EndpointStatus{StatusCode: 200, Reachable: true}
	m.commandOutputs["curl -sI"] = "HTTP/1.1 200 OK\r\nX-Proxy: nginx\r\n"
	m.endpoints["https://app.local/health"] = EndpointStatus{StatusCode: 200, Reachable: true}

	// F-series (stat command outputs)
	m.commandOutputs["stat -c"] = "appuser:appuser 750"
	// Each path needs its own stat output keyed by cmd+arg
	// The catalog uses RunCommand("stat", "-c", "%U:%G %a", path)
	// mockObserver keys on cmd + args[0], which would be "stat -c"
	// We set the generic key so all stat calls pass:
	m.commandOutputs["stat"] = "appuser:appuser 750"

	// nginx -t and openssl x509 pass by default (no error set)
	m.hostResolution["app.local"] = "127.0.0.1"

	// L-series
	m.fileStats["/var/log/app/app.log"] = mockFileInfo{size: 1024}
	m.files["/var/log/app/app.log"] = []byte(
		`{"level":"info","msg":"server started","ts":"2026-01-01T00:00:00Z"}` + "\n" +
			`{"level":"info","msg":"request","ts":"2026-01-01T00:00:01Z"}` + "\n",
	)

	return m
}

// ── Classification tests ──────────────────────────────────────────────────────

func TestSuiteResult_Classify_AllPass(t *testing.T) {
	sr := &SuiteResult{
		At:    time.Now(),
		Total: 3,
		Results: []CheckResult{
			{Check: &Check{ID: "S-001", Severity: SeverityBlocking}, Passed: true},
			{Check: &Check{ID: "P-001", Severity: SeverityBlocking}, Passed: true},
			{Check: &Check{ID: "L-001", Severity: SeverityDegraded}, Passed: true},
		},
	}
	sr.Classify()

	if sr.Classification != ClassConformant {
		t.Errorf("Classification = %v, want ClassConformant", sr.Classification)
	}
	if sr.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0", sr.ExitCode())
	}
	if len(sr.FailingBlockingIDs) != 0 {
		t.Errorf("FailingBlockingIDs = %v, want empty", sr.FailingBlockingIDs)
	}
}

func TestSuiteResult_Classify_BlockingFails(t *testing.T) {
	sr := &SuiteResult{
		Total: 2,
		Results: []CheckResult{
			{Check: &Check{ID: "S-001", Severity: SeverityBlocking}, Passed: false, Err: fmt.Errorf("not active")},
			{Check: &Check{ID: "L-001", Severity: SeverityDegraded}, Passed: true},
		},
	}
	sr.Classify()

	if sr.Classification != ClassNonConformant {
		t.Errorf("Classification = %v, want ClassNonConformant", sr.Classification)
	}
	if sr.ExitCode() != 1 {
		t.Errorf("ExitCode = %d, want 1", sr.ExitCode())
	}
	if len(sr.FailingBlockingIDs) != 1 || sr.FailingBlockingIDs[0] != "S-001" {
		t.Errorf("FailingBlockingIDs = %v, want [S-001]", sr.FailingBlockingIDs)
	}
}

func TestSuiteResult_Classify_DegradedOnly(t *testing.T) {
	// Degraded failures → ClassDegradedConformant, exit 0
	// This is the core of conformance-model.md §4.3
	sr := &SuiteResult{
		Total: 4,
		Results: []CheckResult{
			{Check: &Check{ID: "S-001", Severity: SeverityBlocking}, Passed: true},
			{Check: &Check{ID: "L-001", Severity: SeverityDegraded}, Passed: false, Err: fmt.Errorf("log empty")},
			{Check: &Check{ID: "L-002", Severity: SeverityDegraded}, Passed: false, Err: fmt.Errorf("bad json")},
			{Check: &Check{ID: "F-006", Severity: SeverityDegraded}, Passed: false, Err: fmt.Errorf("cert expired")},
		},
	}
	sr.Classify()

	if sr.Classification != ClassDegradedConformant {
		t.Errorf("Classification = %v, want ClassDegradedConformant", sr.Classification)
	}
	// Critical: exit 0 even with degraded failures
	if sr.ExitCode() != 0 {
		t.Errorf("ExitCode = %d, want 0 (degraded failures must not affect exit code)", sr.ExitCode())
	}
	if !sr.Classification.IsConformant() {
		t.Error("IsConformant() should be true for DegradedConformant")
	}
}

func TestSuiteResult_Classify_DependentNotCounted(t *testing.T) {
	// Dependent failures (E-series when S-001 fails) should not contribute
	// to classification as independent failures
	sr := &SuiteResult{
		Total: 3,
		Results: []CheckResult{
			{Check: &Check{ID: "S-001", Severity: SeverityBlocking}, Passed: false, Err: fmt.Errorf("not active")},
			{Check: &Check{ID: "E-001", Severity: SeverityBlocking}, Passed: false, Err: fmt.Errorf("connection refused"), Dependent: true},
			{Check: &Check{ID: "E-002", Severity: SeverityBlocking}, Passed: false, Err: fmt.Errorf("connection refused"), Dependent: true},
		},
	}
	sr.Classify()

	// Only S-001 should be in FailingBlockingIDs; dependent failures are excluded
	if len(sr.FailingBlockingIDs) != 1 {
		t.Errorf("FailingBlockingIDs = %v, want only [S-001]", sr.FailingBlockingIDs)
	}
	if sr.FailingBlockingIDs[0] != "S-001" {
		t.Errorf("FailingBlockingIDs[0] = %q, want S-001", sr.FailingBlockingIDs[0])
	}
}

func TestSuiteResult_HasFailingCheck(t *testing.T) {
	sr := &SuiteResult{}
	sr.FailingBlockingIDs = []string{"S-001", "E-001"}
	sr.FailingDegradedIDs = []string{"L-001"}

	if !sr.HasFailingCheck("S-001") {
		t.Error("S-001 should be reported as failing")
	}
	if !sr.HasFailingCheck("L-001") {
		t.Error("L-001 (degraded) should be reported as failing")
	}
	if sr.HasFailingCheck("P-001") {
		t.Error("P-001 should not be reported as failing")
	}
}

// ── Runner dependency ordering ────────────────────────────────────────────────

func TestRunner_DependentMarking(t *testing.T) {
	// When S-001 fails, E-series checks should be marked Dependent.
	runner := NewRunner()
	m := conformantMock()
	// Make S-001 fail
	m.serviceActive["app.service"] = false

	sr := runner.Run(m)

	s001 := sr.CheckByID("S-001")
	if s001 == nil {
		t.Fatal("S-001 result not found")
	}
	if s001.Passed {
		t.Error("S-001 should fail")
	}
	if s001.Dependent {
		t.Error("S-001 should not be marked dependent")
	}

	// E-series checks should be marked dependent
	for _, id := range []string{"E-001", "E-002", "E-003", "E-004", "E-005"} {
		res := sr.CheckByID(id)
		if res == nil {
			t.Fatalf("%s result not found", id)
		}
		if !res.Dependent {
			t.Errorf("%s should be marked Dependent when S-001 fails", id)
		}
	}
}

func TestRunner_AllChecksRunEvenOnFailure(t *testing.T) {
	// All 23 checks must run even when early checks fail.
	// conformance-model.md §4.2: no early abort.
	runner := NewRunner()
	m := newMock() // all failing

	sr := runner.Run(m)

	if sr.Total != 23 {
		t.Errorf("Total = %d, want 23", sr.Total)
	}
	if len(sr.Results) != 23 {
		t.Errorf("len(Results) = %d, want 23", len(sr.Results))
	}
}

func TestRunner_LightweightRunChecks(t *testing.T) {
	runner := NewRunner()
	m := conformantMock()

	sr := runner.LightweightRun(m)

	// Should run exactly 4 checks
	if sr.Total != 4 {
		t.Errorf("LightweightRun Total = %d, want 4", sr.Total)
	}
	// Should contain P-001, P-002, S-003, E-001
	ids := map[string]bool{}
	for _, r := range sr.Results {
		ids[r.Check.ID] = true
	}
	for _, expected := range []string{"P-001", "P-002", "S-003", "E-001"} {
		if !ids[expected] {
			t.Errorf("LightweightRun missing check %s", expected)
		}
	}
}

func TestRunner_RunSingle(t *testing.T) {
	runner := NewRunner()
	m := conformantMock()

	res, err := runner.RunSingle("E-001", m)
	if err != nil {
		t.Fatalf("RunSingle error: %v", err)
	}
	if res.Check.ID != "E-001" {
		t.Errorf("Check.ID = %q, want E-001", res.Check.ID)
	}

	_, err = runner.RunSingle("Z-999", m)
	if err == nil {
		t.Error("RunSingle with unknown ID should return error")
	}
}

func TestRunner_RunIDs(t *testing.T) {
	runner := NewRunner()
	m := conformantMock()

	results := runner.RunIDs([]string{"S-001", "E-001", "L-001"}, m)
	if len(results) != 3 {
		t.Errorf("RunIDs returned %d results, want 3", len(results))
	}
}

// ── Catalog completeness ──────────────────────────────────────────────────────

func TestCatalog_Has23Checks(t *testing.T) {
	checks := Catalog()
	if len(checks) != 23 {
		t.Errorf("Catalog has %d checks, want 23", len(checks))
	}
}

func TestCatalog_UniqueIDs(t *testing.T) {
	checks := Catalog()
	seen := map[string]bool{}
	for _, c := range checks {
		if seen[c.ID] {
			t.Errorf("duplicate check ID: %s", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestCatalog_AllHaveExecute(t *testing.T) {
	checks := Catalog()
	for _, c := range checks {
		if c.Execute == nil {
			t.Errorf("check %s has nil Execute function", c.ID)
		}
	}
}

func TestCatalog_SeverityDistribution(t *testing.T) {
	// conformance-model.md §3: F-006, L-001, L-002, L-003 are degraded; all others blocking
	expectedDegraded := map[string]bool{"F-006": true, "L-001": true, "L-002": true, "L-003": true}
	for _, c := range Catalog() {
		if expectedDegraded[c.ID] {
			if c.Severity != SeverityDegraded {
				t.Errorf("check %s should be SeverityDegraded", c.ID)
			}
		} else {
			if c.Severity != SeverityBlocking {
				t.Errorf("check %s should be SeverityBlocking", c.ID)
			}
		}
	}
}

func TestCatalog_OrderSPEFL(t *testing.T) {
	// Checks must be ordered S, P, E, F, L per conformance-model §4.2
	checks := Catalog()
	categoryOrder := map[Category]int{
		CategorySystemState: 0,
		CategoryProcess:     1,
		CategoryEndpoint:    2,
		CategoryFilesystem:  3,
		CategoryLog:         4,
	}
	lastOrder := -1
	for _, c := range checks {
		order := categoryOrder[c.Category]
		if order < lastOrder {
			t.Errorf("check %s (category %v) is out of order", c.ID, c.Category)
		}
		lastOrder = order
	}
}