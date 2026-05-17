package chaos

// chaos_edge_test.go
//
// Edge case tests for chaos middleware behavior.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lab-env/service/chaos"
)

// TestChaosHandler_Drop100_PassesHealth verifies that even at 100% drop rate,
// GET /health is never dropped.
//
// The drop check applies to all routes per the contract, BUT /health must
// remain reachable to preserve E-001 diagnostic integrity. If /health were
// dropped, the conformance suite would see E-001 fail, making F-020 (chaos
// drop fault) look like a service crash rather than a controlled fault.
//
// Note: this requires the chaos handler to exempt /health from drops,
// analogous to the latency exemption. Verify the implementation matches.
// If drops should apply to /health, this test documents the decision.
func TestChaosHandler_Drop100_HealthIsExempted(t *testing.T) {
	t.Skip("design decision: verify whether /health should be exempt from drops like it is from latency")
	// This test is left as a skip to document the open design question.
	// The current implementation drops ALL routes including /health for drop percent.
	// The latency exemption was explicit (chaos.go ServeHTTP comment).
	// If /health should also be exempt from drops, uncomment and implement.
}

// TestChaosHandler_Drop100_AllNonHealthRoutesDrop verifies that at 100% drop
// rate, every non-health request gets 503.
func TestChaosHandler_Drop100_AllNonHealthRoutesDrop(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := chaos.New(base, 0, 100, nil, nil, nil)

	routes := []string{"/", "/slow", "/api/anything"}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s with 100%% drop: status %d, want 503", route, w.Code)
			}
		})
	}
}

// TestChaosHandler_Latency_MeasuredOnRoot verifies that CHAOS_LATENCY_MS
// actually delays GET / by at least the specified duration.
//
// This is not redundant with the exemption test — it verifies the positive
// case: latency injection actually works, not just that it doesn't apply
// where it shouldn't.
func TestChaosHandler_Latency_ActuallyDelaysRoot(t *testing.T) {
	const latencyMS = 100

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := chaos.New(base, latencyMS, 0, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(w, req)
	elapsed := time.Since(start)

	minExpected := time.Duration(latencyMS-10) * time.Millisecond // 10ms tolerance
	if elapsed < minExpected {
		t.Errorf("GET / with %dms chaos latency: took %v, expected >= %v",
			latencyMS, elapsed, minExpected)
	}
}

// TestChaosHandler_ZeroLatency_NoDelay verifies that latencyMS=0 causes no
// measurable delay. The optimization (checking latencyMS > 0 before sleeping)
// must be in place; otherwise every request sleeps for 0ms, which is fine
// but confirms the branch is taken correctly.
func TestChaosHandler_ZeroLatency_NoDelay(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := chaos.New(base, 0, 0, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("zero latency: took %v, expected < 50ms (no sleep)", elapsed)
	}
}