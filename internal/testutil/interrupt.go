// Package testutil provides shared test infrastructure for command-level
// integration tests. It is not imported by production code.
package testutil

import (
	"context"
	"io/fs"
	"sync"
	"time"

	"lab_env/internal/conformance"
	"lab_env/internal/executor"
)

// InterruptableExecutor is an executor.Executor implementation that
// supports interrupt injection for testing the interrupt-path contract.
//
// Usage:
//
//	exec := NewInterruptableExecutor(audit)
//	exec.InterruptAfter(1) // interrupt after 1st mutation
//	result := cmd.Run(...)
//	// verify exit code 4, classification_valid false, audit entry present
type InterruptableExecutor struct {
	audit          *executor.AuditLogger
	mu             sync.Mutex
	interruptAfter int    // interrupt after this many mutation calls (0 = no interrupt)
	mutationCount  int    // number of mutations executed so far
	Interrupted    bool   // set true when interrupt was triggered
	MutationCalls  []string

	// cancelFn is called to simulate a signal
	cancelFn context.CancelFunc
}

// NewInterruptableExecutor creates an executor that can inject interrupts.
func NewInterruptableExecutor(audit *executor.AuditLogger, cancelFn context.CancelFunc) *InterruptableExecutor {
	return &InterruptableExecutor{
		audit:    audit,
		cancelFn: cancelFn,
	}
}

// InterruptAfter causes the executor to trigger a cancellation after n mutations.
// n=0 means no interrupt. n=1 means interrupt after the first mutation completes.
func (e *InterruptableExecutor) InterruptAfter(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.interruptAfter = n
}

func (e *InterruptableExecutor) recordMutation(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mutationCount++
	e.MutationCalls = append(e.MutationCalls, name)
	if e.interruptAfter > 0 && e.mutationCount >= e.interruptAfter {
		e.Interrupted = true
		if e.cancelFn != nil {
			e.cancelFn()
		}
	}
}

// Observer methods (read-only, no interrupt injection)
func (e *InterruptableExecutor) ServiceActive(unit string) (bool, error)  { return true, nil }
func (e *InterruptableExecutor) ServiceEnabled(unit string) (bool, error) { return true, nil }
func (e *InterruptableExecutor) CheckProcess(name, user string) (conformance.ProcessStatus, error) {
	return conformance.ProcessStatus{Running: true}, nil
}
func (e *InterruptableExecutor) CheckPort(addr string) (conformance.PortStatus, error) {
	return conformance.PortStatus{Listening: true}, nil
}
func (e *InterruptableExecutor) CheckEndpoint(url string, _ bool) (conformance.EndpointStatus, error) {
	return conformance.EndpointStatus{StatusCode: 200, Reachable: true,
		Body: []byte(`{"status":"ok"}`)}, nil
}
func (e *InterruptableExecutor) ResolveHost(name string) (string, error) { return "127.0.0.1", nil }
func (e *InterruptableExecutor) Stat(path string) (fs.FileInfo, error) {
	return nil, nil
}
func (e *InterruptableExecutor) ReadFile(path string) ([]byte, error) {
	return []byte("addr: 127.0.0.1:8080\napp_env: prod\n"), nil
}
func (e *InterruptableExecutor) RunCommand(cmd string, args ...string) (string, error) {
	return "appuser:appuser 750", nil
}

// Executor mutation methods — all trigger interrupt counting
func (e *InterruptableExecutor) WriteFile(path string, _ []byte, _ fs.FileMode, _, _ string) error {
	e.recordMutation("WriteFile:" + path)
	return nil
}
func (e *InterruptableExecutor) Chmod(path string, _ fs.FileMode) error {
	e.recordMutation("Chmod:" + path)
	return nil
}
func (e *InterruptableExecutor) Chown(path, _, _ string) error {
	e.recordMutation("Chown:" + path)
	return nil
}
func (e *InterruptableExecutor) Remove(path string) error {
	e.recordMutation("Remove:" + path)
	return nil
}
func (e *InterruptableExecutor) MkdirAll(path string, _ fs.FileMode, _, _ string) error {
	e.recordMutation("MkdirAll:" + path)
	return nil
}
func (e *InterruptableExecutor) Systemctl(action, unit string) error {
	e.recordMutation("Systemctl:" + action + ":" + unit)
	// Simulate time passing during service restart
	time.Sleep(5 * time.Millisecond)
	return nil
}
func (e *InterruptableExecutor) NginxReload() error {
	e.recordMutation("NginxReload")
	return nil
}
func (e *InterruptableExecutor) RestoreFile(path string) error {
	e.recordMutation("RestoreFile:" + path)
	return nil
}
func (e *InterruptableExecutor) RunMutation(cmd string, args ...string) error {
	e.recordMutation("RunMutation:" + cmd)
	return nil
}