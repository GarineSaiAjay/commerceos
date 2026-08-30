package agents

import (
	"context"
	"encoding/json"
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
	SaveAgentPlan(ctx context.Context, id string, action policy.ProposedAction, steps []policy.RunStep) error
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
func (h *Handler) recordPlan(ctx context.Context, plan CheckoutPlan) {
	if h.runs == nil || len(plan.ReasoningTrail) == 0 {
		return
	}
	id := fmt.Sprintf("plan_%d", time.Now().UnixNano())
	if err := h.runs.SaveAgentPlan(ctx, id, plan.Proposal, plan.ReasoningTrail); err != nil {
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

	h.recordPlan(r.Context(), plan)

	writeJSON(w, http.StatusOK, plan)
}

type loopRequest struct {
	Prompt   string `json:"prompt"`
	Merchant string `json:"merchant"`
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

	result, err := h.loopAgent.Run(r.Context(), req.Prompt, req.Merchant)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if result.Plan != nil {
		h.recordPlan(r.Context(), *result.Plan)
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
