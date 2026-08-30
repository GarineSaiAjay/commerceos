package agents

import (
	"context"
	"encoding/json"
	"net/http"
)

// Handler exposes the Agent Commerce Contract endpoints.
type Handler struct {
	agent     *BuyerAgent
	loopAgent *ToolCallingAgent
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

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var _ = context.Background
