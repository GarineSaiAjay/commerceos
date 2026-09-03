package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCostGuardAllowsUnderBudget proves a fresh guard with spend below
// its budget allows the call.
func TestCostGuardAllowsUnderBudget(t *testing.T) {
	g := NewCostGuard(1.0)
	if err := g.Check(context.Background()); err != nil {
		t.Fatalf("expected no error under budget, got %v", err)
	}
}

// TestCostGuardTripsOverBudget proves Record-ing enough estimated
// spend makes the NEXT Check fail, matching Record's own doc comment:
// the call that pushes spend over budget is never itself blocked,
// only the one after it.
func TestCostGuardTripsOverBudget(t *testing.T) {
	g := NewCostGuard(0.01) // 1 cent/day
	if err := g.Check(context.Background()); err != nil {
		t.Fatalf("first check should pass: %v", err)
	}
	// 0.01 USD / defaultPerTokenCostUSD ($0.60/1M) is ~16,667 tokens --
	// comfortably exceeded by one large completion.
	g.Record(0, 50000)
	if err := g.Check(context.Background()); err != ErrDailyCostBudgetExceeded {
		t.Fatalf("expected ErrDailyCostBudgetExceeded after exceeding budget, got %v", err)
	}
}

// TestCostGuardNonPositiveBudgetFallsBackToDefault proves a
// misconfigured (<=0) budget degrades to the usable demo default
// rather than blocking every call.
func TestCostGuardNonPositiveBudgetFallsBackToDefault(t *testing.T) {
	g := NewCostGuard(0)
	if g.dailyBudget != defaultLLMDailyBudgetUSD {
		t.Fatalf("expected default budget %v, got %v", defaultLLMDailyBudgetUSD, g.dailyBudget)
	}
	g = NewCostGuard(-5)
	if g.dailyBudget != defaultLLMDailyBudgetUSD {
		t.Fatalf("expected default budget for negative input, got %v", g.dailyBudget)
	}
}

// TestCostGuardNilReceiverIsNoOp proves every method is safe to call on
// a nil *CostGuard (the state of costGuard on any LLMExtractor/
// ToolCallingAgent that never had WithCostGuard called) and always
// allows the call through, matching the "leaving this unset is fully
// supported" contract documented on WithCostGuard.
func TestCostGuardNilReceiverIsNoOp(t *testing.T) {
	var g *CostGuard
	if err := g.Check(context.Background()); err != nil {
		t.Fatalf("nil guard should never block, got %v", err)
	}
	g.Record(1_000_000, 1_000_000) // must not panic
	if got := g.WithAuditWriter(nil); got != nil {
		t.Fatalf("expected nil receiver to stay nil, got %v", got)
	}
}

// TestCostGuardRolloverResetsSpend proves accrued spend from a
// previous day is discarded once Check/Record notices the UTC day has
// changed -- simulated here by writing directly to the unexported day
// field (same package) rather than mocking time.Now.
func TestCostGuardRolloverResetsSpend(t *testing.T) {
	g := NewCostGuard(0.01)
	g.day = "2000-01-01"
	g.spentUSD = 100 // far over any real budget
	if err := g.Check(context.Background()); err != nil {
		t.Fatalf("a new day should reset spend and allow the call, got %v", err)
	}
	if g.spentUSD != 0 {
		t.Fatalf("expected spentUSD reset to 0 on day rollover, got %v", g.spentUSD)
	}
}

// TestCostGuardFromEnvDefaultsWithoutVar proves NewCostGuardFromEnv
// falls back to the default budget when LLM_DAILY_BUDGET_USD is unset.
func TestCostGuardFromEnvDefaultsWithoutVar(t *testing.T) {
	t.Setenv("LLM_DAILY_BUDGET_USD", "")
	g := NewCostGuardFromEnv()
	if g.dailyBudget != defaultLLMDailyBudgetUSD {
		t.Fatalf("expected default budget, got %v", g.dailyBudget)
	}
}

// TestCostGuardFromEnvReadsVar proves a valid LLM_DAILY_BUDGET_USD is
// used verbatim.
func TestCostGuardFromEnvReadsVar(t *testing.T) {
	t.Setenv("LLM_DAILY_BUDGET_USD", "2.5")
	g := NewCostGuardFromEnv()
	if g.dailyBudget != 2.5 {
		t.Fatalf("expected budget 2.5, got %v", g.dailyBudget)
	}
}

// TestCostGuardFromEnvIgnoresGarbage proves an unparseable
// LLM_DAILY_BUDGET_USD degrades to the default rather than erroring.
func TestCostGuardFromEnvIgnoresGarbage(t *testing.T) {
	t.Setenv("LLM_DAILY_BUDGET_USD", "not-a-number")
	g := NewCostGuardFromEnv()
	if g.dailyBudget != defaultLLMDailyBudgetUSD {
		t.Fatalf("expected default budget for garbage input, got %v", g.dailyBudget)
	}
}

// serveChatWithUsage is serveChat (llm_extractor_test.go) plus a
// "usage" object, so CostGuard.Record has real token counts to
// accumulate from an end-to-end Extract call.
func serveChatWithUsage(content string, promptTokens, completionTokens int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}},
			},
			"usage": map[string]any{
				"prompt_tokens":     promptTokens,
				"completion_tokens": completionTokens,
			},
		})
	}))
}

// TestLLMExtractorRecordsUsageAndRespectsCostGuard is the end-to-end
// proof for both halves of the guard: a successful Extract call
// records the response's real token usage (not a guess), and once
// that pushes accrued spend over budget, the NEXT call is blocked
// before it ever reaches the server.
func TestLLMExtractorRecordsUsageAndRespectsCostGuard(t *testing.T) {
	srv := serveChatWithUsage(`{"budget": 25000, "category": "earbuds", "priority": "", "recipient": ""}`, 0, 50000)
	defer srv.Close()

	guard := NewCostGuard(0.01) // 1 cent/day, comfortably under 50,000 completion tokens' estimated cost
	ext := NewLLMExtractor("test-key", srv.URL, "test-model").WithCostGuard(guard)

	if _, err := ext.Extract(context.Background(), "earbuds under 25000"); err != nil {
		t.Fatalf("first call should succeed and record usage: %v", err)
	}
	if guard.spentUSD <= 0 {
		t.Fatalf("expected spentUSD to be recorded from the response's usage, got %v", guard.spentUSD)
	}

	if _, err := ext.Extract(context.Background(), "earbuds under 25000"); err != ErrDailyCostBudgetExceeded {
		t.Fatalf("expected second call to be blocked by the now-exceeded daily budget, got %v", err)
	}
}
