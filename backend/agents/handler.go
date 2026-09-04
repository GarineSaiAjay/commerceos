package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/garinesaiajay/commerceos/policy"
)

// Handler exposes the Agent Commerce Contract endpoints.
type Handler struct {
	agent     *BuyerAgent
	loopAgent *ToolCallingAgent
	runs      RunRecorder
}

func NewHandler(agent *BuyerAgent) *Handler {
	return &Handler{agent: agent}
}

// WithLoopAgent attaches the bounded tool-calling agent (item 18) that
// powers POST /agent/loop. Optional and additive: a nil loopAgent (no
// OPENROUTER_API_KEY configured, same convention as buyerAgent's
// LLMExtractor) simply makes /agent/loop respond 503 -- /agent/checkout
// is completely unaffected either way.
func (h *Handler) WithLoopAgent(loopAgent *ToolCallingAgent) *Handler {
	h.loopAgent = loopAgent
	return h
}

// RunRecorder persists an agent's own reasoning trail as an
// independently-retrievable Run (item 16, PLAN-01-AGENTIC-CORE.md §4).
// *policy.Service satisfies this directly (see Service.SaveAgentPlan) --
// declared here as a narrow interface using only policy's already-
// imported types, rather than a concrete *policy.Service field, so
// there's no reverse edge from policy back to agents: policy must never
// import agents.
type RunRecorder interface {
	SaveAgentPlan(ctx context.Context, id string, action policy.ProposedAction, steps []policy.RunStep, cartID string) error
}

// WithRunRecorder attaches the audit-trail sink (item 16) that lets a
// successful plan's reasoning trail be persisted as its own Run.
// Optional and additive, same convention as WithLoopAgent: a nil
// recorder (the zero value, if this is never called) simply means
// plans aren't persisted -- PlanCheckout/PlanCheckoutLoop's actual
// response to the buyer is completely unaffected either way, since
// recordPlan is always best-effort and never blocks or fails the
// request over a persistence error.
func (h *Handler) WithRunRecorder(runs RunRecorder) *Handler {
	h.runs = runs
	return h
}

// recordPlan best-effort persists a successful plan's reasoning trail.
// Never returns an error and never blocks the response on one -- a
// caller who forgot to call WithRunRecorder, or a save that fails, must
// never turn a working checkout proposal into a failed request. Skips
// silently when there's no recorder configured or the plan has no
// reasoning trail to save (e.g. a test double CheckoutPlan built by
// hand, or an old caller that hasn't looked at ReasoningTrail).
//
// cartID is passed through from the originating request (planRequest/
// loopRequest's own CartID -- empty for a memoryless call with no
// cart_id at all) so policy.Service.Propose can later find this plan
// again by cart_id and link it to the checkout it led to -- see
// policy.AgentPlan.CartID and db/migrations/*_link_agent_plans_to_actions.sql.
func (h *Handler) recordPlan(ctx context.Context, plan CheckoutPlan, cartID string) {
	if h.runs == nil || len(plan.ReasoningTrail) == 0 {
		return
	}
	id := fmt.Sprintf("plan_%d", time.Now().UnixNano())
	if err := h.runs.SaveAgentPlan(ctx, id, plan.Proposal, plan.ReasoningTrail, cartID); err != nil {
		log.Printf("[agents] failed to save agent plan reasoning trail: %v", err)
	}
}

type planRequest struct {
	Prompt   string `json:"prompt"`
	Merchant string `json:"merchant"`
	// CartID doubles as the conversation_id for agent memory
	// (PLAN-01-AGENTIC-CORE.md §3). Optional and backward compatible:
	// an empty/omitted CartID falls back to the original memoryless
	// PlanCheckout, unchanged.
	CartID string `json:"cart_id"`
}

// PlanCheckout handles POST /agent/checkout — it produces a Proposed
// Action only. It never moves money.
func (h *Handler) PlanCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req planRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Merchant == "" {
		req.Merchant = "merchant_001"
	}

	var (
		plan CheckoutPlan
		err  error
	)
	if req.CartID != "" {
		plan, err = h.agent.PlanCheckoutInConversation(r.Context(), req.CartID, req.Prompt, req.Merchant)
	} else {
		plan, err = h.agent.PlanCheckout(r.Context(), req.Prompt, req.Merchant)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.recordPlan(r.Context(), plan, req.CartID)

	writeJSON(w, http.StatusOK, plan)
}

type loopRequest struct {
	Prompt   string `json:"prompt"`
	Merchant string `json:"merchant"`
	// CartID doubles as the conversation_id for agent memory, same
	// convention and same backward-compatible default as
	// planRequest.CartID -- an empty/omitted CartID falls back to the
	// original memoryless Run, unchanged.
	CartID string `json:"cart_id"`
}

// PlanCheckoutLoop handles POST /agent/loop -- the bounded tool-calling
// agent (PLAN-01-AGENTIC-CORE.md §2, ROADMAP-PRIORITIZED.md P1 item 18).
// Like PlanCheckout, it only ever produces a Proposed Action; it never
// moves money -- see tool_loop.go's doc comment for why the loop
// structurally cannot reach any money-moving tool. Kept as its own
// endpoint, separate from /agent/checkout, so the existing single-shot
// path stays completely unchanged by this addition.
func (h *Handler) PlanCheckoutLoop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.loopAgent == nil {
		http.Error(w, "tool-calling agent not configured (OPENROUTER_API_KEY not set)", http.StatusServiceUnavailable)
		return
	}

	var req loopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Merchant == "" {
		req.Merchant = "merchant_001"
	}

	var (
		result LoopResult
		err    error
	)
	if req.CartID != "" {
		result, err = h.loopAgent.RunInConversation(r.Context(), req.CartID, req.Prompt, req.Merchant)
	} else {
		result, err = h.loopAgent.Run(r.Context(), req.Prompt, req.Merchant)
	}
	if err != nil {
		// ErrAmbiguousIntent (an empty prompt -- see Run/RunInConversation's
		// own strings.TrimSpace check) is the loop's only genuine
		// user-input error; it's a real, correct answer ("ask the buyer to
		// say more"), same contract as PlanCheckout's identical case
		// above, so it stays a 400.
		//
		// Every other error runLoop/chat can return is an infrastructure
		// failure, not a bad request: a network error reaching the LLM
		// provider, a non-200 upstream response, a malformed response
		// body, or -- the case that was actually happening in
		// production -- "read llm response: context deadline exceeded"
		// when a multi-step tool-calling run (up to loopMaxToolCalls
		// round trips sharing one loopTimeout budget) doesn't finish in
		// time. Returning 400 for these made the buyer-facing card show
		// them as the loop's own real, failed answer instead of what
		// they actually are ("the agent is unavailable right now") --
		// and, critically, frontend/app/checkout.tsx's askAgent() only
		// treats a 503 as "unavailable, fall back to /agent/checkout";
		// a 400 here was silently skipping that fallback and showing the
		// generic error instead of ever trying the single-shot
		// RacingExtractor-backed path, which has a genuine deterministic
		// fallback and would very likely have succeeded. The loop has no
		// rule-based equivalent of its own multi-step reasoning (see
		// NewToolCallingAgentFromEnv's doc comment), so 503 -- the same
		// status a missing OPENROUTER_API_KEY already produces above --
		// is the honest signal here too.
		if errors.Is(err, ErrAmbiguousIntent) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	if result.Plan != nil {
		h.recordPlan(r.Context(), *result.Plan, req.CartID)
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
