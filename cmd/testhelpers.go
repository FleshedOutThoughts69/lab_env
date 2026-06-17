package cmd

import (
	"fmt"
	"io/fs"
	"time"
	"lab_env/internal/conformance"
)

// ── stubObserver ──────────────────────────────────────────────────────────────
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

func (s *stubObserver) ServiceActive(unit string) (bool, error) { return s.serviceActive[unit], nil }
func (s *stubObserver) ServiceEnabled(unit string) (bool, error) { return s.serviceActive[unit], nil }
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
func (s *stubObserver) ResolveHost(name string) (string, error)               { return "127.0.0.1", nil }
func (s *stubObserver) Stat(path string) (fs.FileInfo, error)                  { return nil, fmt.Errorf("stub") }
func (s *stubObserver) ReadFile(path string) ([]byte, error)                   { return nil, fmt.Errorf("stub") }
func (s *stubObserver) RunCommand(cmd string, args ...string) (string, error) { return "appuser:appuser 750", nil }

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
type trackingExecutor struct {
	stubObserver
	mutationCalls []string
}

func newTrackingExecutor() *trackingExecutor {
	te := &trackingExecutor{}
	te.serviceActive = map[string]bool{"app.service": true, "nginx": true}
	te.portListening = map[string]bool{"127.0.0.1:8080": true}
	te.endpointStatus = map[string]int{}
	return te
}

func (t *trackingExecutor) WriteFile(path string, _ []byte, _ fs.FileMode, _, _ string) error {
	t.mutationCalls = append(t.mutationCalls, "WriteFile:"+path)
	return nil
}
func (t *trackingExecutor) Chmod(path string, _ fs.FileMode) error {
	t.mutationCalls = append(t.mutationCalls, "Chmod:"+path)
	return nil
}
func (t *trackingExecutor) Chown(path, _, _ string) error {
	t.mutationCalls = append(t.mutationCalls, "Chown:"+path)
	return nil
}
func (t *trackingExecutor) Remove(path string) error {
	t.mutationCalls = append(t.mutationCalls, "Remove:"+path)
	return nil
}
func (t *trackingExecutor) MkdirAll(path string, _ fs.FileMode, _, _ string) error {
	t.mutationCalls = append(t.mutationCalls, "MkdirAll:"+path)
	return nil
}
func (t *trackingExecutor) Systemctl(action, unit string) error {
	t.mutationCalls = append(t.mutationCalls, "Systemctl:"+action+":"+unit)
	return nil
}
func (t *trackingExecutor) NginxReload() error {
	t.mutationCalls = append(t.mutationCalls, "NginxReload")
	return nil
}
func (t *trackingExecutor) RestoreFile(path string) error {
	t.mutationCalls = append(t.mutationCalls, "RestoreFile:"+path)
	return nil
}
func (t *trackingExecutor) RunMutation(cmd string, args ...string) error {
	t.mutationCalls = append(t.mutationCalls, "RunMutation:"+cmd)
	return nil
}

// ── injectErrorExecutor ──────────────────────────────────────────────────────
type injectErrorExecutor struct {
	trackingExecutor
	errorOnChmod bool
}

func (e *injectErrorExecutor) Chmod(path string, mode fs.FileMode) error {
	e.mutationCalls = append(e.mutationCalls, "Chmod:"+path)
	if e.errorOnChmod {
		return fmt.Errorf("chmod failed: permission denied")
	}
	return nil
}