package cmd


// testhelpers_test.go contains shared stub types and constructors
// used across all cmd package tests. Centralizing here prevents
// duplicate definitions across status_test, validate_test, fault_test,
// and interrupt_test, which are all in package cmd_test.

import (
	"fmt"
	"io/fs"
	"time"

	"lab_env/internal/conformance"
)

// ── stubObserver ──────────────────────────────────────────────────────────────
// Implements conformance.Observer with configurable responses.
// Zero value is safe: all methods return "unhealthy" defaults.

type stubObserver struct {
	serviceActive  map[string]bool
	portListening  map[string]bool
	endpointStatus map[string]int
}

func healthyObs() *stubObserver {
	return &stubObserver{
		serviceActive: map[string]bool{
			"app.service": true,
			"nginx":       true,
		},
		portListening: map[string]bool{
			"127.0.0.1:8080": true,
			"0.0.0.0:80":     true,
			"0.0.0.0:443":    true,
		},
		endpointStatus: map[string]int{
			"http://localhost/health": 200,
			"http://localhost/":       200,
		},
	}
}

func unhealthyObs() *stubObserver {
	return &stubObserver{
		serviceActive:  map[string]bool{},
		portListening:  map[string]bool{},
		endpointStatus: map[string]int{},
	}
}

func (s *stubObserver) ServiceActive(unit string) (bool, error) {
	return s.serviceActive[unit], nil
}
func (s *stubObserver) ServiceEnabled(unit string) (bool, error) {
	return s.serviceActive[unit], nil
}
func (s *stubObserver) CheckProcess(name, user string) (conformance.ProcessStatus, error) {
	running := s.serviceActive["app.service"]
	return conformance.ProcessStatus{Running: running}, nil
}
func (s *stubObserver) CheckPort(addr string) (conformance.PortStatus, error) {
	return conformance.PortStatus{
		Listening: s.portListening[addr],
		Addr:      addr,
	}, nil
}
func (s *stubObserver) CheckEndpoint(url string, _ bool) (conformance.EndpointStatus, error) {
	code, ok := s.endpointStatus[url]
	if !ok {
		return conformance.EndpointStatus{Reachable: false, StatusCode: 0}, nil
	}
	var body []byte
	if code == 200 && url == "http://localhost/health" {
		body = []byte(`{"status":"ok"}`)
	}
	return conformance.EndpointStatus{
		StatusCode: code,
		Reachable:  code != 0,
		Body:       body,
	}, nil
}
func (s *stubObserver) ResolveHost(name string) (string, error) {
	return "127.0.0.1", nil
}
func (s *stubObserver) Stat(path string) (fs.FileInfo, error) {
	return nil, fmt.Errorf("stat %s: not available in stub", path)
}
func (s *stubObserver) ReadFile(path string) ([]byte, error) {
	return nil, fmt.Errorf("readfile %s: not available in stub", path)
}
func (s *stubObserver) RunCommand(cmd string, args ...string) (string, error) {
	// Return stat-like output for filesystem checks
	return "appuser:appuser 750", nil
}

// ── mockFileInfo ──────────────────────────────────────────────────────────────

type mockFileInfo struct {
	size int64
	mode fs.FileMode
}

func (m *mockFileInfo) Name() string       { return "" }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() fs.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() interface{}   { return nil }

// ── trackingExecutor ─────────────────────────────────────────────────────────
// Implements executor.Executor minimally, recording calls for test assertions.

type trackingExecutor struct {
    serviceActive   map[string]bool
    portListening   map[string]bool
    endpointStatus  map[string]int
    // Add other fields if needed
}

func (t *trackingExecutor) ServiceActive(unit string) (bool, error) {
    return t.serviceActive[unit], nil
}
func (t *trackingExecutor) ServiceEnabled(unit string) (bool, error) {
    return t.serviceActive[unit], nil
}
func (t *trackingExecutor) CheckProcess(name, user string) (conformance.ProcessStatus, error) {
    return conformance.ProcessStatus{Running: t.serviceActive["app.service"]}, nil
}
func (t *trackingExecutor) CheckPort(addr string) (conformance.PortStatus, error) {
    return conformance.PortStatus{Listening: t.portListening[addr], Addr: addr}, nil
}
func (t *trackingExecutor) CheckEndpoint(url string, _ bool) (conformance.EndpointStatus, error) {
    code, ok := t.endpointStatus[url]
    if !ok {
        return conformance.EndpointStatus{Reachable: false, StatusCode: 0}, nil
    }
    return conformance.EndpointStatus{StatusCode: code, Reachable: true}, nil
}
// ... implement remaining executor.Executor methods as needed; for tests they can be no-ops.
func (t *trackingExecutor) ResolveHost(name string) (string, error) { return "127.0.0.1", nil }
func (t *trackingExecutor) Stat(path string) (fs.FileInfo, error) { return nil, fmt.Errorf("stub") }
func (t *trackingExecutor) ReadFile(path string) ([]byte, error) { return nil, fmt.Errorf("stub") }
func (t *trackingExecutor) RunCommand(cmd string, args ...string) (string, error) { return "", nil }
func (t *trackingExecutor) Systemctl(cmd, unit string) error { return nil }
func (t *trackingExecutor) NginxReload() error { return nil }
func (t *trackingExecutor) WriteFile(path string, content []byte, mode os.FileMode) error { return nil }
func (t *trackingExecutor) Chmod(path string, mode os.FileMode) error { return nil }
func (t *trackingExecutor) Chown(path, user, group string) error { return nil }
func (t *trackingExecutor) RestoreFile(path string) error { return nil }
func (t *trackingExecutor) RunMutation(bin string, args ...string) error { return nil }

// ── injectErrorExecutor ──────────────────────────────────────────────────────
type injectErrorExecutor struct {
    serviceActive  map[string]bool
    portListening  map[string]bool
    endpointStatus map[string]int
    // when true, certain operations return errors
    failServiceActive bool
}

func (e *injectErrorExecutor) ServiceActive(unit string) (bool, error) {
    if e.failServiceActive {
        return false, fmt.Errorf("injected error")
    }
    return e.serviceActive[unit], nil
}
// implement the rest similarly, mirroring trackingExecutor
func (e *injectErrorExecutor) ServiceEnabled(unit string) (bool, error) {
    return e.ServiceActive(unit)
}
func (e *injectErrorExecutor) CheckProcess(name, user string) (conformance.ProcessStatus, error) {
    if e.failServiceActive {
        return conformance.ProcessStatus{}, fmt.Errorf("injected")
    }
    return conformance.ProcessStatus{Running: e.serviceActive["app.service"]}, nil
}
func (e *injectErrorExecutor) CheckPort(addr string) (conformance.PortStatus, error) {
    return conformance.PortStatus{Listening: e.portListening[addr], Addr: addr}, nil
}
func (e *injectErrorExecutor) CheckEndpoint(url string, _ bool) (conformance.EndpointStatus, error) {
    code, _ := e.endpointStatus[url]
    return conformance.EndpointStatus{StatusCode: code, Reachable: code != 0}, nil
}
func (e *injectErrorExecutor) ResolveHost(name string) (string, error) { return "127.0.0.1", nil }
func (e *injectErrorExecutor) Stat(path string) (fs.FileInfo, error) { return nil, fmt.Errorf("stub") }
func (e *injectErrorExecutor) ReadFile(path string) ([]byte, error) { return nil, fmt.Errorf("stub") }
func (e *injectErrorExecutor) RunCommand(cmd string, args ...string) (string, error) { return "", nil }
func (e *injectErrorExecutor) Systemctl(cmd, unit string) error { return nil }
func (e *injectErrorExecutor) NginxReload() error { return nil }
func (e *injectErrorExecutor) WriteFile(path string, content []byte, mode fs.FileMode) error { return nil }
func (e *injectErrorExecutor) Chmod(path string, mode fs.FileMode) error { return nil }
func (e *injectErrorExecutor) Chown(path, user, group string) error { return nil }
func (e *injectErrorExecutor) RestoreFile(path string) error { return nil }
func (e *injectErrorExecutor) RunMutation(bin string, args ...string) error { return nil }