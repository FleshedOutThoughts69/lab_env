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