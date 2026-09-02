// Package ratelimit implements a minimal per-key (IP) token-bucket rate
// limiter (item 34, P3 / PLAN-06-ADDITIONAL-OPPORTUNITIES.md §5): "A
// simple per-IP token bucket is sufficient; doesn't need to be
// sophisticated." Hand-rolled against the standard library rather than
// pulling in golang.org/x/time/rate or a similar dependency -- this
// session has no working Go toolchain to run `go get`/`go mod tidy` and
// verify a new module actually resolves and vendors correctly, and the
// plan's own framing says a hand-rolled bucket is enough for this.
//
// Deliberately in-memory, not Redis-backed, even though Redis is
// already part of this stack (backend/infra/db): the Commerce Service
// runs as a single process (infra/docker-compose.yml has one `backend`
// service, not a pool behind a load balancer), so there is exactly one
// limiter instance to keep consistent. A distributed store would trade
// a Redis round trip on every LLM-backed request for a coordination
// problem this deployment doesn't actually have.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// bucket is one caller's token bucket.
type bucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

// Limiter is an in-memory, per-key token-bucket rate limiter. Safe for
// concurrent use.
type Limiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	capacity   float64
	refillRate float64 // tokens per second
	now        func() time.Time
}

// NewLimiter creates a limiter that allows a burst of up to `burst`
// requests immediately per key, refilling at `perSecond` tokens/second
// thereafter (fractional -- 0.25 means one token every 4 seconds).
func NewLimiter(burst int, perSecond float64) *Limiter {
	return &Limiter{
		buckets:    map[string]*bucket{},
		capacity:   float64(burst),
		refillRate: perSecond,
		now:        time.Now,
	}
}

// Allow reports whether the caller identified by key may proceed right
// now. On success it atomically consumes one token.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		// A key's first-ever request starts with a full bucket minus the
		// one it's about to spend -- capacity < 1 is a misconfigured
		// limiter (nothing would ever be allowed); guard it explicitly
		// rather than silently rejecting every caller's very first
		// request in a way that's hard to tell apart from "limit
		// reached."
		if l.capacity < 1 {
			return false
		}
		l.buckets[key] = &bucket{tokens: l.capacity - 1, lastRefill: now, lastSeen: now}
		return true
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * l.refillRate
	if b.tokens > l.capacity {
		b.tokens = l.capacity
	}
	b.lastRefill = now
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Sweep removes buckets untouched for longer than idleFor, bounding this
// limiter's memory growth from the many distinct IPs a public judging
// URL sees over hours/days. Allow itself never does this work inline --
// call Sweep periodically instead (main.go runs it on a ticker).
func (l *Limiter) Sweep(idleFor time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := l.now().Add(-idleFor)
	for key, b := range l.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// Middleware wraps next with a 429 response for any request whose
// rate-limit key (as extracted by keyFunc -- typically ClientIP below)
// has exceeded its bucket. keyFunc is a parameter rather than always
// ClientIP so a test can supply a fixed key instead of a real remote
// address.
func (l *Limiter) Middleware(keyFunc func(*http.Request) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(keyFunc(r)) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded -- this endpoint calls a paid LLM API and is capped per caller, try again shortly", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP extracts the caller's IP for use as a rate-limit key,
// preferring the leftmost X-Forwarded-For entry (the original client,
// when this service sits behind a reverse proxy or load balancer -- a
// public judging deployment plausibly does) and falling back to the
// connection's own RemoteAddr.
//
// X-Forwarded-For is caller-supplied and trivially spoofable by anyone
// connecting directly rather than through a trusted proxy -- this
// limiter's job is blunt cost control against casual/naive abuse of a
// public URL with an unmetered path to a paid LLM API, not a hardened
// defense against a determined attacker who rotates headers (or source
// IPs, which per-IP limiting can't stop either). That's the plan's own
// framing for this item: "a simple per-IP token bucket is sufficient;
// doesn't need to be sophisticated" -- a floor against runaway API
// cost, not a ceiling against a motivated adversary.
func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i >= 0 {
			fwd = fwd[:i]
		}
		if ip := strings.TrimSpace(fwd); ip != "" {
			return ip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
