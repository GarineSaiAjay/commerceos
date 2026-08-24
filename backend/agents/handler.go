package agents

import (
	"context"
	"encoding/json"
	"net/http"
)

// Handler exposes the Agent Commerce Contract endpoints.
type Handler struct {
	agent *BuyerAgent
}

func NewHandler(agent *BuyerAgent) *Handler {
	return &Handler{agent: agent}
}

type planRequest struct {
	Prompt   string `json:"prompt"`
	Merchant string `json:"merchant"`
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

	plan, err := h.agent.PlanCheckout(r.Context(), req.Prompt, req.Merchant)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var _ = context.Background
