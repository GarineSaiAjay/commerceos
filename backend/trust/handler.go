// Package trust implements the public, judge-friendly audit-verification
// surface (item 36, P3 / PLAN-06-ADDITIONAL-OPPORTUNITIES.md §3): a
// small, deliberately UNAUTHENTICATED window onto evidence that already
// exists and is already correct elsewhere in this codebase --
// audit.Verifier (the hash-chain integrity check behind the gated
// POST /audit/verify), the live Razorpay call counter (already public at
// GET /adapter/calls, payment.Handler.CallCount), and the 14-attack
// safety suite (safety.Runner, gated behind POST /safety/evaluations/run).
// This package adds no new evidence and no new backend capability -- per
// the plan's own framing, it's a presentation change: the same numbers,
// reachable without an operator login, for a judge (or a judge's own
// tooling) who has none.
package trust

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/garinesaiajay/commerceos/audit"
	"github.com/garinesaiajay/commerceos/safety"
)

// CallCounter exposes the provider adapter's outbound-call counter.
// Defined locally rather than imported from commerce/payment or safety --
// the same minimal-own-interface convention those two packages already
// use for this identical single method, so this package doesn't have to
// import either of them just to name a one-method interface.
type CallCounter interface {
	CallCount() int64
}

// Verifier is the audit-chain integrity check. An interface (not
// *audit.Verifier directly) purely so a test can fake it without a real
// Postgres pool -- audit.Verifier itself has no interface today, only a
// concrete struct wrapping *pgxpool.Pool, and a real *audit.Verifier
// satisfies this without any change to that package.
type Verifier interface {
	Verify(ctx context.Context) (audit.VerificationResult, error)
}

// Runner runs the safety attack suite. Matches safety.Runner's RunSuite
// method exactly, same reasoning as Verifier above.
type Runner interface {
	RunSuite(ctx context.Context, mandateID string) (safety.Evaluation, error)
}

// EvaluationStore persists/lists safety evaluations. Matches the subset
// of *safety.Store's methods this package needs.
type EvaluationStore interface {
	SaveEvaluation(ctx context.Context, e safety.Evaluation) error
	ListEvaluations(ctx context.Context, limit int) ([]safety.Evaluation, error)
}

// suiteCooldown bounds how often POST /trust/run-suite can execute the
// full attack library. This handler is deliberately unauthenticated (see
// the package doc comment above), and an unauthenticated POST that opens
// a database transaction and inserts rows on every call (safety.Store.
// SaveEvaluation) is exactly the kind of thing worth a cheap guard even
// in a hackathon submission. Item 34 (P3, rate limiting on LLM-backed
// endpoints) wouldn't cover this even if it existed: RunSuite never
// calls an LLM, it only proposes canned attack payloads through the
// deterministic policy engine (safety/runner.go's RunAttack), so it
// falls outside that item's scope entirely and needed its own answer
// here. This is deliberately NOT a general rate limiter -- no per-IP
// tracking, no token bucket -- just one shared cooldown, proportionate
// to what a demo/judging page actually needs: stop a double-click or a
// script loop from piling up evaluation rows, nothing more.
const suiteCooldown = 10 * time.Second

// Handler serves the public /trust/* endpoints.
type Handler struct {
	verifier Verifier
	counter  CallCounter
	runner   Runner
	store    EvaluationStore

	suiteMu      sync.Mutex
	lastSuiteRun time.Time
	now          func() time.Time
}

func NewHandler(verifier Verifier, counter CallCounter, runner Runner, store EvaluationStore) *Handler {
	return &Handler{
		verifier: verifier,
		counter:  counter,
		runner:   runner,
		store:    store,
		now:      time.Now,
	}
}

// Summary is the full public trust snapshot returned by GET /trust/summary.
type Summary struct {
	AuditChain       audit.VerificationResult `json:"audit_chain"`
	RazorpayCalls    int64                    `json:"razorpay_calls"`
	LatestEvaluation *safety.Evaluation       `json:"latest_evaluation,omitempty"`
}

// Summary serves GET /trust/summary -- the current audit chain integrity
// status, the live adapter call counter, and the most recent safety
// evaluation on record (if any suite has ever been run). Read-only: it
// writes no row that didn't already exist.
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	chain, err := h.verifier.Verify(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	summary := Summary{
		AuditChain:    chain,
		RazorpayCalls: h.counter.CallCount(),
	}

	// A ListEvaluations error here is deliberately swallowed, not
	// surfaced as a 500: the chain-integrity status and call counter
	// above are the load-bearing evidence on this page and are already
	// computed successfully by this point -- one evaluation-history
	// query failing shouldn't take the whole summary down when
	// latest_evaluation is already `omitempty` for exactly this
	// "no evaluation yet" case.
	if h.store != nil {
		if evals, err := h.store.ListEvaluations(r.Context(), 1); err == nil && len(evals) > 0 {
			summary.LatestEvaluation = &evals[0]
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

// RunSuite serves POST /trust/run-suite -- runs the same 14-attack
// library safety.Runner.RunSuite already runs behind the gated
// POST /safety/evaluations/run, publicly, so a judge can trigger it
// without an operator login. Bounded by suiteCooldown (see its own doc
// comment above).
func (h *Handler) RunSuite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.suiteMu.Lock()
	now := h.now()
	if !h.lastSuiteRun.IsZero() {
		if elapsed := now.Sub(h.lastSuiteRun); elapsed < suiteCooldown {
			h.suiteMu.Unlock()
			retryAfter := suiteCooldown - elapsed
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			http.Error(w, fmt.Sprintf("suite was just run, try again in %s", retryAfter.Round(time.Second)), http.StatusTooManyRequests)
			return
		}
	}
	h.lastSuiteRun = now
	h.suiteMu.Unlock()

	eval, err := h.runner.RunSuite(r.Context(), "mnd_demo")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h.store != nil {
		_ = h.store.SaveEvaluation(r.Context(), eval)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(eval)
}
