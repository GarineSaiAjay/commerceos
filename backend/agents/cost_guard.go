package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/garinesaiajay/commerceos/audit"
)

// ErrDailyCostBudgetExceeded is returned by CostGuard.Check once today's
// accrued spend has reached the configured daily budget. Both call
// sites this guards (LLMExtractor.Extract, ToolCallingAgent.chat)
// already treat any error from an LLM call as a real, expected failure
// mode -- RacingExtractor falls back to the deterministic extractor on
// any llmExtractor error (see its own doc comment), and
// Handler.PlanCheckout/PlanCheckoutLoop both already surface any agent
// error generically via http.Error -- so this needs no special-cased
// handling anywhere else to take effect.
var ErrDailyCostBudgetExceeded = errors.New("agents: daily LLM cost budget exceeded")

// CostGuard is the "hard, unconditional... max-cost/day guard if the
// OpenRouter key is metered" PLAN-01-AGENTIC-CORE.md §2 calls for,
// deliberately kept to exactly what the plan says a demo needs:
// "simple in-memory counter is enough," no persistence, no
// cross-process coordination. It resets the moment the UTC day rolls
// over (checked lazily on every Check/Record call, not a background
// timer), so a long-idle process still starts every new day at zero.
//
// One shared instance guards BOTH OpenRouter call sites in this
// package (LLMExtractor.Extract and ToolCallingAgent.chat) -- they use
// the same OPENROUTER_API_KEY, so a single daily budget is what
// actually matches how the underlying key is billed; two independent
// guards would let combined spend run to double the configured budget
// before either one noticed. See cmd/server/main.go for the shared
// construction.
//
// Cost is an ESTIMATE, not the real bill: OpenRouter's chat/completions
// response never returns actual billed cost (that needs a separate
// generation-stats call this codebase doesn't make), only prompt/
// completion token counts. perTokenCost converts those into an
// approximate USD figure using a single conservative flat rate (the
// higher, output-token rate for LLM_MODEL's default,
// openai/gpt-4o-mini via OpenRouter, ~$0.60/1M tokens as of this
// writing, applied to every token rather than pricing prompt and
// completion tokens separately) -- so the guard always trips at least
// as early as the real bill would, never later.
type CostGuard struct {
	mu           sync.Mutex
	dailyBudget  float64 // USD
	perTokenCost float64 // USD, blended estimate -- see doc comment above
	day          string  // UTC "2006-01-02" spentUSD has accrued for
	spentUSD     float64
	auditWriter  audit.Writer
}

const (
	defaultLLMDailyBudgetUSD = 5.0
	defaultPerTokenCostUSD   = 0.60 / 1_000_000
)

// NewCostGuard builds a guard with an explicit daily budget in USD.
// dailyBudgetUSD <= 0 falls back to defaultLLMDailyBudgetUSD rather
// than a zero/negative budget that would block every call immediately
// -- a misconfigured value should degrade to "usable demo default,"
// not "agent silently stops working."
func NewCostGuard(dailyBudgetUSD float64) *CostGuard {
	if dailyBudgetUSD <= 0 {
		dailyBudgetUSD = defaultLLMDailyBudgetUSD
	}
	return &CostGuard{dailyBudget: dailyBudgetUSD, perTokenCost: defaultPerTokenCostUSD}
}

// NewCostGuardFromEnv reads LLM_DAILY_BUDGET_USD (optional; a
// present-but-unparseable or non-positive value is treated the same as
// unset, per NewCostGuard's own fallback). Mirrors the
// NewXFromEnv-reads-optional-env-vars convention LLMExtractor and
// ToolCallingAgent already use for OPENROUTER_API_KEY/LLM_BASE_URL/
// LLM_MODEL.
func NewCostGuardFromEnv() *CostGuard {
	budget := 0.0
	if raw := os.Getenv("LLM_DAILY_BUDGET_USD"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			budget = parsed
		}
	}
	return NewCostGuard(budget)
}

// WithAuditWriter wires the general hash-chained audit ledger (same
// fluent-setter, nil-receiver-safe convention as policy.Service.
// WithAuditWriter and order.PostgresRepository.WithAuditWriter) so a
// tripped guard is logged there, per the plan's "log to the audit
// chain if it trips." Left unset, Check still enforces the budget --
// it just doesn't have anywhere durable to record the trip.
func (g *CostGuard) WithAuditWriter(w audit.Writer) *CostGuard {
	if g == nil {
		return nil
	}
	g.auditWriter = w
	return g
}

// Check reports whether today's accrued estimated spend has already
// reached the daily budget. Called BEFORE every LLM request (not
// after), so an already-tripped guard blocks the call outright rather
// than letting one more request through and only refusing the next --
// matches how a real metered API's hard cap would behave. A nil
// receiver (CostGuard never configured) always allows the call, same
// nil-safety convention as ToolCallingAgent.WithConversationStore.
//
// The audit write on trip is best-effort, same posture as every other
// audit write in this codebase (see policy.Service.UpdatePolicyConfig's
// doc comment): a missing audit_events row for a cost-guard trip is
// bad, but it must never be the reason a request that should be
// blocked gets through instead.
func (g *CostGuard) Check(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	g.rolloverLocked()
	tripped := g.spentUSD >= g.dailyBudget
	spent, budget, day := g.spentUSD, g.dailyBudget, g.day
	g.mu.Unlock()

	if !tripped {
		return nil
	}
	if g.auditWriter != nil {
		detail := map[string]any{
			"spent_usd_estimate": spent,
			"daily_budget_usd":   budget,
			"day":                day,
		}
		if err := g.auditWriter.Write(ctx, "system", "llm_daily_cost_budget_exceeded", "llm_cost_guard", day, detail); err != nil {
			fmt.Printf("[agents] audit write failed for llm_daily_cost_budget_exceeded (day %s): %v\n", day, err)
		}
	}
	return ErrDailyCostBudgetExceeded
}

// Record adds a completed request's token usage to today's running
// total. Called AFTER a request succeeds -- only a completed request
// has a token count to record -- so a call that itself pushes spend
// over budget is never retroactively blocked; the guard takes effect
// on the NEXT call via Check, exactly like a real metered API bills
// for what was already used rather than pre-empting the request that
// crossed the line. A nil receiver is a no-op, matching Check.
func (g *CostGuard) Record(promptTokens, completionTokens int) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rolloverLocked()
	g.spentUSD += float64(promptTokens+completionTokens) * g.perTokenCost
}

// rolloverLocked resets spentUSD the first time it notices the UTC
// day has changed since the last Check/Record call. Must be called
// with g.mu already held.
func (g *CostGuard) rolloverLocked() {
	today := time.Now().UTC().Format("2006-01-02")
	if today != g.day {
		g.day = today
		g.spentUSD = 0
	}
}

// llmUsage is the OpenAI-compatible "usage" object OpenRouter's
// chat/completions response includes alongside choices -- shared by
// both chatResponse (llm_extractor.go) and loopChatResponse
// (tool_loop.go), the two response shapes in this package.
type llmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}
