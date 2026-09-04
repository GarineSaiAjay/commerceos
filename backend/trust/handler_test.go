package trust

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/garinesaiajay/commerceos/audit"
	"github.com/garinesaiajay/commerceos/safety"
)

type fakeVerifier struct {
	result audit.VerificationResult
	err    error
}

func (f fakeVerifier) Verify(ctx context.Context) (audit.VerificationResult, error) {
	return f.result, f.err
}

type fakeCounter struct {
	calls int64
}

func (f fakeCounter) CallCount() int64 { return f.calls }

// fakeRunner counts how many times RunSuite was actually invoked, so
// TestRunSuiteEnforcesCooldown can prove a cooldown-rejected request
// never reached it.
type fakeRunner struct {
	eval  safety.Evaluation
	err   error
	calls int
}

func (f *fakeRunner) RunSuite(ctx context.Context) (safety.Evaluation, error) {
	f.calls++
	return f.eval, f.err
}

type fakeStore struct {
	saved   []safety.Evaluation
	listed  []safety.Evaluation
	listErr error
}

func (f *fakeStore) SaveEvaluation(ctx context.Context, e safety.Evaluation) error {
	f.saved = append(f.saved, e)
	return nil
}

func (f *fakeStore) ListEvaluations(ctx context.Context, limit int) ([]safety.Evaluation, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listed, nil
}

// TestSummaryReturnsChainAndCallCount proves GET /trust/summary reports
// the verifier's chain result and the counter's live call count, and
// that latest_evaluation is omitted (not a null placeholder) when the
// store has no evaluation history yet -- the store.ListEvaluations(...,
// 1) call returning zero rows, not an error.
func TestSummaryReturnsChainAndCallCount(t *testing.T) {
	h := NewHandler(
		fakeVerifier{result: audit.VerificationResult{Verified: true, RowsChecked: 42}},
		fakeCounter{calls: 7},
		&fakeRunner{},
		&fakeStore{},
	)

	req := httptest.NewRequest(http.MethodGet, "/trust/summary", nil)
	rec := httptest.NewRecorder()
	h.Summary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	// audit.VerificationResult has no json tags, so its exported Go field
	// names appear verbatim in the response -- "RowsChecked", not
	// "rows_checked". Unmarshal into that exact shape (rather than
	// guessing a snake_case key that doesn't exist) so this test fails
	// loudly if that ever changes.
	var out struct {
		AuditChain struct {
			Verified    bool `json:"Verified"`
			RowsChecked int  `json:"RowsChecked"`
		} `json:"audit_chain"`
		RazorpayCalls    int64           `json:"razorpay_calls"`
		LatestEvaluation json.RawMessage `json:"latest_evaluation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	if !out.AuditChain.Verified || out.AuditChain.RowsChecked != 42 {
		t.Errorf("expected audit_chain {Verified:true RowsChecked:42}, got %+v", out.AuditChain)
	}
	if out.RazorpayCalls != 7 {
		t.Errorf("expected razorpay_calls 7, got %d", out.RazorpayCalls)
	}
	if out.LatestEvaluation != nil {
		t.Errorf("expected latest_evaluation to be omitted with no evaluation history, got %s", out.LatestEvaluation)
	}
}

// TestSummaryIncludesLatestEvaluation proves latest_evaluation IS
// populated once the store has at least one evaluation on record.
func TestSummaryIncludesLatestEvaluation(t *testing.T) {
	h := NewHandler(
		fakeVerifier{result: audit.VerificationResult{Verified: true}},
		fakeCounter{},
		&fakeRunner{},
		&fakeStore{listed: []safety.Evaluation{{ID: "eval_123", Passed: true, ScenarioCount: 14}}},
	)

	req := httptest.NewRequest(http.MethodGet, "/trust/summary", nil)
	rec := httptest.NewRecorder()
	h.Summary(rec, req)

	var out struct {
		LatestEvaluation *safety.Evaluation `json:"latest_evaluation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.LatestEvaluation == nil || out.LatestEvaluation.ID != "eval_123" {
		t.Errorf("expected latest_evaluation.evaluation_id eval_123, got %+v", out.LatestEvaluation)
	}
}

// TestSummaryToleratesListEvaluationsError proves a broken evaluation
// history doesn't take the whole summary down -- the audit chain status
// and call counter (both already computed successfully) still come
// back, per the Summary handler's own doc comment.
func TestSummaryToleratesListEvaluationsError(t *testing.T) {
	h := NewHandler(
		fakeVerifier{result: audit.VerificationResult{Verified: true}},
		fakeCounter{calls: 3},
		&fakeRunner{},
		&fakeStore{listErr: errors.New("db unavailable")},
	)

	req := httptest.NewRequest(http.MethodGet, "/trust/summary", nil)
	rec := httptest.NewRecorder()
	h.Summary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even with a broken evaluation store, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSummaryRejectsNonGET proves a non-GET request gets 405.
func TestSummaryRejectsNonGET(t *testing.T) {
	h := NewHandler(fakeVerifier{}, fakeCounter{}, &fakeRunner{}, &fakeStore{})

	req := httptest.NewRequest(http.MethodPost, "/trust/summary", nil)
	rec := httptest.NewRecorder()
	h.Summary(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestRunSuiteExecutesAndPersists proves POST /trust/run-suite drives
// the runner and persists the result through the store, returning the
// evaluation as its response body -- the exact same shape
// POST /safety/evaluations/run already returns behind operator auth.
func TestRunSuiteExecutesAndPersists(t *testing.T) {
	runner := &fakeRunner{eval: safety.Evaluation{ID: "eval_1", Passed: true, ScenarioCount: 14}}
	store := &fakeStore{}
	h := NewHandler(fakeVerifier{}, fakeCounter{}, runner, store)

	req := httptest.NewRequest(http.MethodPost, "/trust/run-suite", nil)
	rec := httptest.NewRecorder()
	h.RunSuite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if runner.calls != 1 {
		t.Errorf("expected RunSuite called once, got %d", runner.calls)
	}
	if len(store.saved) != 1 || store.saved[0].ID != "eval_1" {
		t.Errorf("expected the evaluation persisted via SaveEvaluation, got %+v", store.saved)
	}

	var out safety.Evaluation
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.ID != "eval_1" {
		t.Errorf("expected response body to be the evaluation, got %+v", out)
	}
}

// TestRunSuiteEnforcesCooldown proves a second POST within suiteCooldown
// of the first gets 429 with a Retry-After header and never reaches the
// runner -- the whole point of the cooldown (see Handler's doc comment:
// this is an unauthenticated endpoint that writes a database row on
// every real call).
func TestRunSuiteEnforcesCooldown(t *testing.T) {
	runner := &fakeRunner{eval: safety.Evaluation{ID: "eval_1"}}
	h := NewHandler(fakeVerifier{}, fakeCounter{}, runner, &fakeStore{})

	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return clock }

	first := httptest.NewRecorder()
	h.RunSuite(first, httptest.NewRequest(http.MethodPost, "/trust/run-suite", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("expected first call to succeed with 200, got %d", first.Code)
	}

	clock = clock.Add(1 * time.Second) // well inside suiteCooldown (10s)
	second := httptest.NewRecorder()
	h.RunSuite(second, httptest.NewRequest(http.MethodPost, "/trust/run-suite", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second call within the cooldown to get 429, got %d: %s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" {
		t.Errorf("expected a Retry-After header on the 429 response")
	}
	if runner.calls != 1 {
		t.Errorf("expected the cooldown-rejected request to never reach the runner, but RunSuite was called %d times", runner.calls)
	}

	clock = clock.Add(suiteCooldown) // now past the cooldown window
	third := httptest.NewRecorder()
	h.RunSuite(third, httptest.NewRequest(http.MethodPost, "/trust/run-suite", nil))
	if third.Code != http.StatusOK {
		t.Fatalf("expected a call after the cooldown elapsed to succeed with 200, got %d", third.Code)
	}
	if runner.calls != 2 {
		t.Errorf("expected the runner to be called again after the cooldown elapsed, got %d calls", runner.calls)
	}
}

// TestRunSuiteRejectsNonPOST proves a non-POST request gets 405.
func TestRunSuiteRejectsNonPOST(t *testing.T) {
	h := NewHandler(fakeVerifier{}, fakeCounter{}, &fakeRunner{}, &fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/trust/run-suite", nil)
	rec := httptest.NewRecorder()
	h.RunSuite(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
