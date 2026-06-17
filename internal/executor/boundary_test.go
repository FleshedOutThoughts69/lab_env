package executor

import (
	"reflect"
	"testing"

	"lab_env/internal/conformance"
)

// TestObserver_DoesNotHaveMutationMethods verifies that the Observer interface
// does not contain any mutation methods. Mutation methods (WriteFile, Chmod,
// Chown, Remove, MkdirAll, Systemctl, NginxReload, RestoreFile, RunMutation)
// must live exclusively on the Executor interface.
func TestObserver_DoesNotHaveMutationMethods(t *testing.T) {
	obsType := reflect.TypeOf((*conformance.Observer)(nil)).Elem()

	mutationMethods := []string{
		"WriteFile", "Chmod", "Chown", "Remove", "MkdirAll",
		"Systemctl", "NginxReload", "RestoreFile", "RunMutation",
	}
	for _, name := range mutationMethods {
		if _, ok := obsType.MethodByName(name); ok {
			t.Errorf("Observer interface must not have mutation method %q", name)
		}
	}
}

// TestExecutor_SatisfiesObserver verifies that the Executor interface embeds
// conformance.Observer, meaning every Executor is also an Observer.
func TestExecutor_SatisfiesObserver(t *testing.T) {
	// Compile‑time assertion: *Real implements Executor, and Executor embeds Observer.
	var _ conformance.Observer = NewExecutor(nil)
}

// TestObserver_SatisfiesConformanceObserver verifies that the concrete observer
// returned by NewObserver() (the unit‑test mock or the real Observer) satisfies
// the conformance.Observer interface.
func TestObserver_SatisfiesConformanceObserver(t *testing.T) {
	// NewObserver() is defined in executor/export.go; it returns an Observer.
	var _ conformance.Observer = NewObserver()
}

// TestRunCommand_AvailableOnObserver verifies that RunCommand is accessible
// via the Observer interface. It is used by read‑only checks.
func TestRunCommand_AvailableOnObserver(t *testing.T) {
	var obs conformance.Observer = NewObserver()
	// Must compile: RunCommand is part of the Observer interface.
	_, _ = obs.RunCommand("true")
}

// TestRunMutation_RequiresExecutor verifies that RunMutation is NOT available
// on the Observer interface. It must only be available via Executor.
func TestRunMutation_RequiresExecutor(t *testing.T) {
	var exec Executor = NewExecutor(nil)
	// Must compile: RunMutation is on Executor.
	_ = exec.RunMutation("true")

	// Verify that Observer does NOT have RunMutation.
	obsType := reflect.TypeOf((*conformance.Observer)(nil)).Elem()
	if _, ok := obsType.MethodByName("RunMutation"); ok {
		t.Error("Observer interface must not have RunMutation method")
	}
}