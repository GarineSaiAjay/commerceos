package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAllowWithinBurst proves exactly `burst` requests succeed
// back-to-back (no time passing), and the next one is rejected.
func TestAllowWithinBurst(t *testing.T) {
	l := NewLimiter(3, 1.0)

	for i := 0; i < 3; i++ {
		if !l.Allow("caller") {
			t.Fatalf("expected request %d within the burst of 3 to be allowed", i+1)
		}
	}
	if l.Allow("caller") {
		t.Fatalf("expected the 4th request to exceed the burst of 3 and be rejected")
	}
}

// TestAllowRefillsOverTime proves tokens refill at the configured rate
// and a caller who exhausted their burst can proceed again once enough
// (simulated) time has passed -- using an injected clock, not a real
// sleep, so this test is fast and deterministic.
func TestAllowRefillsOverTime(t *testing.T) {
	l := NewLimiter(2, 1.0) // 1 token/sec
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return clock }

	if !l.Allow("caller") || !l.Allow("caller") {
		t.Fatalf("expected both burst tokens to be allowed")
	}
	if l.Allow("caller") {
		t.Fatalf("expected the bucket to be empty immediately after spending the burst")
	}

	clock = clock.Add(500 * time.Millisecond) // 0.5 tokens refilled -- still not enough
	if l.Allow("caller") {
		t.Fatalf("expected 0.5 refilled tokens to still be insufficient for a full request")
	}

	clock = clock.Add(600 * time.Millisecond) // now ~1.1 tokens refilled total
	if !l.Allow("caller") {
		t.Fatalf("expected a request to be allowed once >= 1 token had refilled")
	}
}

// TestAllowIsPerKey proves two different keys (e.g. two different
// caller IPs) have entirely independent buckets -- one caller
// exhausting its burst must never affect another's.
func TestAllowIsPerKey(t *testing.T) {
	l := NewLimiter(1, 1.0)

	if !l.Allow("caller-a") {
		t.Fatalf("expected caller-a's first request to be allowed")
	}
	if l.Allow("caller-a") {
		t.Fatalf("expected caller-a's second request to be rejected (burst of 1)")
	}
	if !l.Allow("caller-b") {
		t.Fatalf("expected caller-b's first request to be allowed independently of caller-a's exhausted bucket")
	}
}

// TestSweepRemovesIdleBuckets proves Sweep evicts buckets that have been
// idle longer than idleFor, and leaves recently-used ones alone.
func TestSweepRemovesIdleBuckets(t *testing.T) {
	l := NewLimiter(5, 1.0)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return clock }

	l.Allow("idle-caller")

	clock = clock.Add(2 * time.Hour)
	l.Allow("fresh-caller") // touched just before the sweep below

	l.Sweep(1 * time.Hour)

	l.mu.Lock()
	_, idleStillPresent := l.buckets["idle-caller"]
	_, freshStillPresent := l.buckets["fresh-caller"]
	l.mu.Unlock()

	if idleStillPresent {
		t.Errorf("expected the 2-hour-idle bucket to be swept")
	}
	if !freshStillPresent {
		t.Errorf("expected the just-used bucket to survive the sweep")
	}
}

// TestMiddlewareBlocksExcessRequestsWith429 proves the HTTP middleware
// wraps a 429 + Retry-After around a rejected request, and that a
// blocked request never reaches the wrapped handler at all.
func TestMiddlewareBlocksExcessRequestsWith429(t *testing.T) {
	l := NewLimiter(1, 1.0)
	reached := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	})
	handler := l.Middleware(func(r *http.Request) string { return "fixed-key" }, inner)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/agent/checkout", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("expected the first request to succeed with 200, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/agent/checkout", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the second request (burst of 1) to get 429, got %d", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Errorf("expected a Retry-After header on the 429 response")
	}
	if reached != 1 {
		t.Errorf("expected the wrapped handler to be reached exactly once (not on the rejected request), got %d", reached)
	}
}

// TestClientIPPrefersXForwardedFor proves ClientIP reads the leftmost
// X-Forwarded-For entry (trimming surrounding whitespace) over
// RemoteAddr when present.
func TestClientIPPrefersXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", " 203.0.113.7 , 10.0.0.2")

	if got := ClientIP(req); got != "203.0.113.7" {
		t.Errorf("expected the leftmost, trimmed X-Forwarded-For entry 203.0.113.7, got %q", got)
	}
}

// TestClientIPFallsBackToRemoteAddr proves ClientIP uses the connection's
// own address (host only, port stripped) when X-Forwarded-For is absent.
func TestClientIPFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.9:12345"

	if got := ClientIP(req); got != "198.51.100.9" {
		t.Errorf("expected RemoteAddr's host 198.51.100.9, got %q", got)
	}
}
