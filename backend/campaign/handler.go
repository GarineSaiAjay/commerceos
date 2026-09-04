package campaign

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// ExportCSV handles GET /campaigns/export, optionally filtered by the
// same ?status= query parameter List already supports -- item 27 (P2,
// PLAN-05-SELLER-DASHBOARD.md section 6): "CSV export of orders and
// campaigns ... a thin CSV-serialization endpoint over data the
// existing handlers already query; no new business logic." Reuses the
// exact same h.repo.List(ctx, operator.MerchantID, status) call List
// already makes.
//
// Registered as its own exact route ("/campaigns/export"), the same
// way "/campaigns/propose" already is alongside the "/campaigns/"
// catch-all below -- Go's http.ServeMux always prefers a matching
// exact pattern over a matching subtree ("/campaigns/") pattern
// regardless of registration order, so this can never be shadowed by
// or confused with the {id}-based routes campaignID() parses.
//
// Monetary columns are raw paise (budget_cap_paise, spent_paise),
// matching every other amount in this codebase and ExportOrdersCSV's
// identical choice in commerce/order/handler.go -- see that handler's
// doc comment for the reasoning (avoids a rounding disagreement
// between this file and the UI's own INR formatting).
func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="campaigns.csv"`)
	w.WriteHeader(http.StatusOK)

	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"campaign_id", "product_id", "discount_percent", "budget_cap_paise",
		"spent_paise", "duration_days", "status", "rejected_demand_count",
		"policy_version", "approved_by", "rejected_reason", "starts_at",
		"ends_at", "created_at",
	})
	for _, c := range campaigns {
		var startsAt, endsAt string
		if c.StartsAt != nil {
			startsAt = c.StartsAt.Format(time.RFC3339)
		}
		if c.EndsAt != nil {
			endsAt = c.EndsAt.Format(time.RFC3339)
		}
		_ = writer.Write([]string{
			c.ID,
			c.ProductID,
			strconv.Itoa(c.DiscountPercent),
			strconv.FormatInt(c.BudgetCap, 10),
			strconv.FormatInt(c.Spent, 10),
			strconv.Itoa(c.DurationDays),
			c.Status,
			strconv.Itoa(c.RejectedDemandCount),
			c.PolicyVersion,
			c.ApprovedBy,
			c.RejectedReason,
			startsAt,
			endsAt,
			c.CreatedAt.Format(time.RFC3339),
		})
	}
	// Best-effort flush -- see ExportOrdersCSV's identical comment in
	// commerce/order/handler.go for why there is no remaining
	// error-response path once the header/status line is committed.
	writer.Flush()
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

// Get serves GET /campaigns/{id}. Scoped to the calling operator's own
// merchant (P0 security fix, full-codebase re-audit 2026-09-04) -- this
// handler previously never checked auth.OperatorFromContext at all
// (every OTHER handler in this file did), so despite RequireOperator
// gating the whole /campaigns/ subtree (main.go) at the authentication
// layer, there was no authorization check here whatsoever: any
// authenticated operator of any merchant could read any other
// merchant's campaign by id. See Repository.GetByID's doc comment.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	c, err := h.repo.GetByID(r.Context(), operator.MerchantID, id)
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
	c, err := h.repo.Approve(r.Context(), operator.MerchantID, id, operator.Email)
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
	var req rejectRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	c, err := h.repo.Reject(r.Context(), operator.MerchantID, id, req.Reason)
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
