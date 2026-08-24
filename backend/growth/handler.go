package growth

import (
	"context"
	"encoding/json"
	"net/http"
)

// Handler exposes the Growth Agent over HTTP.
type Handler struct {
	agent *GrowthAgent
	store *PostgresStore
}

func NewHandler(agent *GrowthAgent, store *PostgresStore) *Handler {
	return &Handler{agent: agent, store: store}
}

type evaluateRequest struct {
	CartID            string  `json:"cart_id"`
	CartTotal         int64   `json:"cart_total"`
	Budget            int64   `json:"budget"`
	Tolerance         float64 `json:"tolerance"`
	ProductID         string  `json:"product_id"`
	PurchaseProb      float64 `json:"purchase_probability"`
	IncrementalMargin int64   `json:"incremental_margin"`
	Confidence        float64 `json:"confidence"`
	RiskCost          int64   `json:"risk_cost"`
}

func (h *Handler) Evaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req evaluateRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	rec, err := h.agent.EvaluateCandidate(
		r.Context(),
		req.CartID,
		req.CartTotal,
		BudgetCheck{CartTotal: req.CartTotal, Budget: req.Budget, Tolerance: req.Tolerance},
		req.ProductID,
		EVInputs{
			PurchaseProbability: req.PurchaseProb,
			IncrementalMargin:   req.IncrementalMargin,
			Confidence:          req.Confidence,
			RiskCost:            req.RiskCost,
		},
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, rec)
}

// Explain returns the full explanation view for a recommendation.
func (h *Handler) Explain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Path[len("/growth/recommend/"):]

	rec, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "recommendation not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, rec)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var _ = context.Background
