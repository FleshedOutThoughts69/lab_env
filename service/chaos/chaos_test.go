package chaos

// chaos_test.go
//
// Tests the chaos middleware contract:
//   1. Latency is NOT applied to /health (preserves E-001 diagnostic integrity)
//   2. Drop increments both requests_total AND errors_total
//   3. OOM goroutine starts at most once regardless of concurrent calls

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lab-env/service/chaos"
)

// TestChaosHandler_Latency_ExemptedForHealth verifies that CHAOS_LATENCY_MS
// is NOT applied to GET /health, so E-001 conformance check always returns
// quickly regardless of active chaos latency.
//
// If latency were applied to /health, a CHAOS_LATENCY_MS value longer than
// the conformance check's HTTP timeout would cause E-001 to fail, making
// F-020 (chaos latency fault) look like a service crash.
func TestChaosHandler_Latency_ExemptedForHealth(t *testing.T) {
	const latencyMS = 200 // 200ms chaos latency

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := chaos.New(base, latencyMS, 0, nil, nil, nil)

	t.Run("health endpoint not delayed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()

		start := time.Now()
		handler.ServeHTTP(w, req)
		elapsed := time.Since(start)

		// Should return in well under latencyMS (allow 50ms for test overhead)
		if elapsed >= time.Duration(latencyMS)*time.Millisecond {
			t.Errorf("/health took %v; chaos latency must not apply (latencyMS=%d)",
				elapsed, latencyMS)
		}
		if w.Code != http.StatusOK {
			t.Errorf("/health: status %d, want 200", w.Code)
		}
	})

	t.Run("root endpoint is delayed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		start := time.Now()
		handler.ServeHTTP(w, req)
		elapsed := time.Since(start)

		// Should take at least latencyMS (allow 20ms tolerance)
		minExpected := time.Duration(latencyMS-20) * time.Millisecond
		if elapsed < minExpected {
			t.Errorf("GET / took %v; expected at least %v (latencyMS=%d)",
				elapsed, minExpected, latencyMS)
		}
	})
}

// TestChaosHandler_Drop_IncrementsRequestsAndErrors verifies that a chaos drop
// (HTTP 503) increments BOTH requests_total and errors_total counters.
//
// A dropped request is a request arrival — it must count toward requests_total.
// It is also a non-2xx response — it must count toward errors_total.
// Without this, telemetry shows correct error rate but wrong request rate.
func TestChaosHandler_Drop_IncrementsRequestsAndErrors(t *testing.T) {
	var reqCount, errCount atomic.Int64

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("base handler should not be called when drop=100%")
	})

	handler := chaos.New(
		base,
		0,   // no latency
		100, // 100% drop rate
		func() { reqCount.Add(1) },
		func() { errCount.Add(1) },
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("drop: status %d, want 503", w.Code)
	}
	if reqCount.Load() != 1 {
		t.Errorf("requests_total: got %d, want 1", reqCount.Load())
	}
	if errCount.Load() != 1 {
		t.Errorf("errors_total: got %d, want 1", errCount.Load())
	}
}

// TestChaosHandler_Drop_BeforeLatency verifies the ordering invariant:
// the drop decision is made before the latency sleep. A dropped request
// must not sleep first.
func TestChaosHandler_Drop_BeforeLatency(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// 500ms latency + 100% drop — request should return in << 500ms
	handler := chaos.New(base, 500, 100, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(w, req)
	elapsed := time.Since(start)

	// Should be well under 500ms if drop fires before latency
	if elapsed >= 400*time.Millisecond {
		t.Errorf("drop+latency: took %v; drop should fire before latency (no sleep)", elapsed)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", w.Code)
	}
}

// TestChaosHandler_ZeroDrop_PassesThrough verifies that 0% drop rate never
// drops any requests — CHAOS_DROP_PERCENT=0 must be a no-op.
func TestChaosHandler_ZeroDrop_PassesThrough(t *testing.T) {
	called := 0
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	handler := chaos.New(base, 0, 0, nil, nil, nil)

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	if called != 100 {
		t.Errorf("0%% drop: base handler called %d/100 times; should always pass through", called)
	}
}

// TestStartOOM_CalledTwiceConcurrently_StartsOnlyOneGoroutine verifies that
// concurrent calls to StartOOM start at most one OOM goroutine.
//
// Multiple calls (e.g., from concurrent requests if OOM were triggered per-request)
// must not multiply the allocation rate, which could produce a different failure
// pattern than the single-goroutine OOM the fault matrix expects.
//
// This test uses a no-op logger and does NOT actually trigger OOM —
// it verifies the sync.Once guard via a mock allocator.
func TestStartOOM_SyncOnce_GuardsAgainstDuplicateStart(t *testing.T) {
	var startCount atomic.Int32

	// Use the test-hook version of StartOOM that accepts an allocator function
	// instead of allocating real memory.
	for i := 0; i < 10; i++ {
		chaos.StartOOMForTest(func() {
			startCount.Add(1)
		})
	}

	// Allow goroutines to start
	time.Sleep(50 * time.Millisecond)

	if startCount.Load() != 1 {
		t.Errorf("OOM goroutine started %d times; sync.Once should ensure exactly 1",
			startCount.Load())
	}
}

// TestChaosHandler_NilCallbacks_NoPanic verifies that passing nil callbacks
// (reqCounter, errCounter) to chaos.New does not panic when drops occur.
func TestChaosHandler_NilCallbacks_NoPanic(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := chaos.New(base, 0, 100, nil, nil, nil) // nil callbacks

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic with nil callbacks: %v", r)
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
}

// TestChaosHandler_ConcurrentRequests_CountersAccurate verifies that atomic
// counter increments from concurrent goroutines produce the correct total.
func TestChaosHandler_ConcurrentRequests_CountersAccurate(t *testing.T) {
	var reqCount, errCount atomic.Int64

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// 50% drop rate
	handler := chaos.New(
		base,
		0,
		50,
		func() { reqCount.Add(1) },
		func() { errCount.Add(1) },
		nil,
	)

	const n = 1000
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}()
	}
	wg.Wait()

	// Every dropped request increments both counters; non-dropped requests
	// increment neither (those are counted in the base handler in real code).
	// reqCount == errCount for dropped requests
	if reqCount.Load() != errCount.Load() {
		t.Errorf("counter mismatch: reqCount=%d errCount=%d; for dropped requests both must increment",
			reqCount.Load(), errCount.Load())
	}

	// With 50% drop and 1000 requests, expect roughly 500 drops.
	// Allow wide tolerance (300-700) to account for randomness.
	dropped := reqCount.Load()
	if dropped < 300 || dropped > 700 {
		t.Errorf("50%% drop rate produced %d drops in %d requests; expected ~500 (300-700 range)", dropped, n)
	}
}