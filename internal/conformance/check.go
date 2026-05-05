package conformance

import "fmt"

// Severity classifies a check's impact on the conformance classification.
// Defined in conformance-model.md §3.1.
type Severity int

const (
	// SeverityBlocking: failure produces NON-CONFORMANT classification.
	// The conformance suite exits 1 when any blocking check fails.
	SeverityBlocking Severity = iota

	// SeverityDegraded: failure produces DEGRADED-CONFORMANT sub-classification.
	// The conformance suite exits 0 when only degraded checks fail.
	// Current degraded checks: F-006, L-001, L-002, L-003.
	SeverityDegraded
)

func (s Severity) String() string {
	switch s {
	case SeverityBlocking:
		return "blocking"
	case SeverityDegraded:
		return "degraded"
	default:
		return "unknown"
	}
}

// Category identifies which series a check belongs to.
// Defined in conformance-model.md §3.1.
type Category int

const (
	CategorySystemState Category = iota // S-series
	CategoryProcess                     // P-series
	CategoryEndpoint                    // E-series
	CategoryFilesystem                  // F-series
	CategoryLog                         // L-series
)

func (c Category) String() string {
	switch c {
	case CategorySystemState:
		return "S"
	case CategoryProcess:
		return "P"
	case CategoryEndpoint:
		return "E"
	case CategoryFilesystem:
		return "F"
	case CategoryLog:
		return "L"
	default:
		return "?"
	}
}

// CheckLayer identifies which conformance layer a check belongs to.
// Defined in conformance-model.md §2.
type CheckLayer int

const (
	LayerBehavioral  CheckLayer = iota // primary semantic authority
	LayerStructural                    // explanatory authority
	LayerOperational                   // informational authority
)

// Check is a single conformance assertion. Each check in the catalog is
// an instance of this type. The ID, Category, Layer, Severity, and
// assertion text are fixed at definition time; Execute is called by the
// runner to evaluate the check against the current system state.
type Check struct {
	// ID is the stable identifier: S-001, P-002, E-003, F-004, L-001, etc.
	ID string

	// Category determines the execution order (S before P before E).
	Category Category

	// Layer determines which conformance authority the check carries.
	Layer CheckLayer

	// Severity determines whether failure blocks conformance or degrades it.
	Severity Severity

	// Assertion is the human-readable description of what must be true.
	Assertion string

	// FailureMeaning is the semantic interpretation of a failing check —
	// not the error message, but what the failure means operationally.
	FailureMeaning string

	// ObservableCommand is the exact shell command that tests the assertion,
	// for use in output and documentation.
	ObservableCommand string

	// Execute runs the check against the system via the Observer.
	// Returns nil if the check passes, a descriptive error if it fails.
	Execute func(o Observer) error
}

// CheckResult is the outcome of running a single Check.
type CheckResult struct {
	Check    *Check
	Passed   bool
	// Err is nil when Passed is true. When Passed is false, Err describes
	// why the check failed in operational terms.
	Err      error
	// Dependent is true when this check failed because a higher-priority
	// check it depends on also failed (e.g., E-series failing because
	// S-001 already failed). Dependent failures are noted but do not
	// add independent diagnostic signal.
	Dependent bool
}

func (r CheckResult) String() string {
	if r.Passed {
		return fmt.Sprintf("[PASS] %s  %s", r.Check.ID, r.Check.Assertion)
	}
	if r.Dependent {
		return fmt.Sprintf("[SKIP] %s  %s (dependent on failed check)", r.Check.ID, r.Check.Assertion)
	}
	return fmt.Sprintf("[FAIL] %s  %s — %v", r.Check.ID, r.Check.Assertion, r.Err)
}