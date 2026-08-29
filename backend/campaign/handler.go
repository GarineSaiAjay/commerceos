package campaign

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/garinesaiajay/commerceos/auth"
)

// Handler exposes the campaign orchestrator over HTTP. Every route is
// operator-gated (see main.go's route wiring, authService.RequireOperator)
// -- there is no buyer-facing action here, unlike policy's approval
// requests, so every handler below can assume auth.OperatorFromContext
// succeeds and scope every read/write to that operator's own merchant.
type Handler struct {
	agent *CampaignAgent
	repo  Repository
}

func NewHandler(agent *CampaignAgent, repo Repository) *Handler {
	return &Handler{agent: agent, repo: repo}
}

type proposeRequest struct {
	WindowDays      int `json:"window_days"`
	DiscountPercent int `json:"discount_percent"`
	DurationDays    int `json:"duration_days"`
}

// Propose serves POST /campaigns/propose. The operator supplies the
// discount percent, campaign duration, and how far back to look for
// rejected demand -- the agent never chooses these itself (see agent.go).
func (h *Handler) Propose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	operator, ok := auth.OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return
	}

	var req proposeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.WindowDays <= 0 {
		req.WindowDays = 7
	}
	if req.DurationDays <= 0 {
		req.DurationDays = 7
	}
	if req.DiscountPercent <= 0 {
		http.Error(w, "discount_percent must be positive", http.StatusBadRequest)
		return
	}

	c, decision, err := h.agent.ProposeFromRejectedDemand(
		r.Context(), operator.MerchantID, req.WindowDays, req.DiscountPercent, req.DurationDays,
	)
	if err != nil {
		if errors.Is(err, ErrNoRejectedDemand) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"campaign": c,
		"decision": decision,
	})
}

// List serves GET /campaigns, optionally filtered by ?status=.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	operator, ok := auth.OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return
	}

	campaigns, err := h.repo.List(r.Context(), operator.MerchantID, r.URL.Query().Get("status"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if campaigns == nil {
		campaigns = []Campaign{}
	}
	writeJSON(w, http.StatusOK, campaigns)
}

// campaignID extracts the campaign id from a /campaigns/{id}[/action] path.
func campaignID(path string) string {
	const prefix = "/campaigns/"
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.Trim(rest, "/")
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// Get serves GET /campaigns/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := campaignID(r.URL.Path)
	if id == "" {
		http.Error(w, "campaign ID required", http.StatusBadRequest)
		return
	}
	c, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrCampaignNotFound) {
			http.Error(w, "campaign not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// Approve serves POST /campaigns/{id}/approve -- transitions PROPOSED to
// ACTIVE (see PostgresRepository.Approve).
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	operator, ok := auth.OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return
	}
	id := campaignID(r.URL.Path)
	if id == "" {
		http.Error(w, "campaign ID required", http.StatusBadRequest)
		return
	}
	c, err := h.repo.Approve(r.Context(), id, operator.Email)
	if err != nil {
		if errors.Is(err, ErrCampaignNotProposed) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

// Reject serves POST /campaigns/{id}/reject.
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := auth.OperatorFromContext(r.Context()); !ok {
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return
	}
	id := campaignID(r.URL.Path)
	if id == "" {
		http.Error(w, "campaign ID required", http.StatusBadRequest)
		return
	}
	var req rejectRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	c, err := h.repo.Reject(r.Context(), id, req.Reason)
	if err != nil {
		if errors.Is(err, ErrCampaignNotProposed) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
