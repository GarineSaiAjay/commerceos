package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/garinesaiajay/commerceos/audit"
	"github.com/garinesaiajay/commerceos/auth"
)

// Handler exposes the policy engine over HTTP: propose an action and
// verify the audit chain.
type Handler struct {
	service  *Service
	verifier *audit.Verifier
}

func NewHandler(service *Service, verifier *audit.Verifier) *Handler {
	return &Handler{
		service:  service,
		verifier: verifier,
	}
}

type proposeRequest struct {
	Action    string   `json:"action"`
	Amount    int64    `json:"amount"`
	Currency  string   `json:"currency"`
	Merchant  string   `json:"merchant"`
	Items     []string `json:"items"`
	MandateID string   `json:"mandate_id"`
	CartID    string   `json:"cart_id"`
}

type createMandateRequest struct {
	Buyer                     string   `json:"buyer"`
	Merchant                  string   `json:"merchant"`
	AllowedCategories         []string `json:"allowed_categories"`
	MaximumAmount             int64    `json:"maximum_amount"`
	Currency                  string   `json:"currency"`
	RequiresConfirmationAbove int64    `json:"requires_confirmation_above"`
	AllowedPaymentMethods     []string `json:"allowed_payment_methods"`
	ExpiresAt                 string   `json:"expires_at"`
	Purpose                   string   `json:"purpose"`
	CartID                    string   `json:"cart_id"`
}

// CreateMandate creates the explicit consent record used by the policy
// chokepoint. Amounts are always paise.
func (h *Handler) CreateMandate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req createMandateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Buyer == "" || req.Merchant == "" || req.Currency == "" || req.MaximumAmount <= 0 {
		http.Error(w, "buyer, merchant, currency, and positive maximum_amount are required", http.StatusBadRequest)
		return
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	if req.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil || !parsed.After(time.Now()) {
			http.Error(w, "expires_at must be a future RFC3339 timestamp", http.StatusBadRequest)
			return
		}
		expiresAt = parsed
	}
	mandate := Mandate{
		ID: fmt.Sprintf("mandate_%d", time.Now().UnixNano()), Buyer: req.Buyer,
		Merchant: req.Merchant, AllowedCategories: req.AllowedCategories,
		MaximumAmount: req.MaximumAmount, Currency: req.Currency,
		RequiresConfirmationAbove: req.RequiresConfirmationAbove,
		AllowedPaymentMethods:     req.AllowedPaymentMethods, ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
		Purpose: req.Purpose, Status: "ACTIVE", CartID: req.CartID,
	}
	if err := h.service.CreateMandate(r.Context(), mandate); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, mandate)
}

func (h *Handler) Propose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req proposeRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	action := ProposedAction{
		Action:   req.Action,
		Amount:   req.Amount,
		Currency: req.Currency,
		Merchant: req.Merchant,
		Items:    req.Items,
		CartID:   req.CartID,
	}

	decision, err := h.service.Propose(r.Context(), action, req.MandateID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, decision)
}

func (h *Handler) VerifyAuditChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := h.verifier.Verify(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ListApprovalRequests serves GET /approval-requests?status=PENDING.
func (h *Handler) ListApprovalRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := r.URL.Query().Get("status")
	reqs, err := h.service.ListApprovalRequests(r.Context(), status, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if reqs == nil {
		reqs = []ApprovalRequest{}
	}
	writeJSON(w, http.StatusOK, reqs)
}

// approvalID extracts the approval-request id from /approval-requests/{id}.
func approvalID(path string) string {
	const prefix = "/approval-requests/"
	id := strings.TrimPrefix(path, prefix)
	return strings.Trim(id, "/")
}

// GetApprovalRequest serves GET /approval-requests/{id}.
func (h *Handler) GetApprovalRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := approvalID(r.URL.Path)
	if id == "" {
		http.Error(w, "approval request ID required", http.StatusBadRequest)
		return
	}
	req, err := h.service.GetApprovalRequest(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrApprovalRequestNotFound) {
			http.Error(w, "approval request not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// Approve serves POST /approval-requests/{id}/approve. The caller must be
// either the buyer who created this request (proven by sending back the
// cart_id it was created for) or a logged-in merchant operator (proven by
// a valid bearer session, attached to the request context by
// auth.Service.OptionalOperator -- see main.go's route wiring). See
// files/AUTH.md and Service.resolveApprover.
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/approval-requests/"), "/approve")
	id = strings.Trim(id, "/")
	if id == "" {
		http.Error(w, "approval request ID required", http.StatusBadRequest)
		return
	}
	var req struct {
		CartID string `json:"cart_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	operatorEmail := ""
	if operator, ok := auth.OperatorFromContext(r.Context()); ok {
		operatorEmail = operator.Email
	}
	decision, err := h.service.Approve(r.Context(), id, req.CartID, operatorEmail)
	if err != nil {
		if errors.Is(err, ErrApprovalUnauthorized) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

// Reject serves POST /approval-requests/{id}/reject. See Approve for who
// is allowed to call this.
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/approval-requests/"), "/reject")
	id = strings.Trim(id, "/")
	if id == "" {
		http.Error(w, "approval request ID required", http.StatusBadRequest)
		return
	}
	var req struct {
		CartID string `json:"cart_id"`
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	operatorEmail := ""
	if operator, ok := auth.OperatorFromContext(r.Context()); ok {
		operatorEmail = operator.Email
	}
	if err := h.service.Reject(r.Context(), id, req.CartID, operatorEmail, req.Reason); err != nil {
		if errors.Is(err, ErrApprovalUnauthorized) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "REJECTED"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type policySettingsResponse struct {
	Ceiling           int64    `json:"ceiling"`
	BudgetTolerance   float64  `json:"budget_tolerance"`
	AllowedCurrencies []string `json:"allowed_currencies"`
	AllowedMerchants  []string `json:"allowed_merchants"`
}

type updatePolicySettingsRequest struct {
	Ceiling           int64    `json:"ceiling"`
	BudgetTolerance   float64  `json:"budget_tolerance"`
	AllowedCurrencies []string `json:"allowed_currencies"`
	AllowedMerchants  []string `json:"allowed_merchants"`
}

// GetSettings serves GET /dashboard/settings/policy: a read-only window
// into the live policy configuration (item 25, P2,
// PLAN-05-SELLER-DASHBOARD.md §4). RequireOperator-gated at the route
// (main.go), same as every other /dashboard/* endpoint. Reads
// Service.GetPolicyConfig -- the engine's in-memory copy, not a fresh
// DB row -- so this always reflects exactly what Evaluate is enforcing
// right now, not what a concurrent SaveConfig might have persisted a
// moment ago but not yet applied.
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := h.service.GetPolicyConfig()
	writeJSON(w, http.StatusOK, policySettingsResponse{
		Ceiling:           cfg.Ceiling,
		BudgetTolerance:   cfg.BudgetTolerance,
		AllowedCurrencies: cfg.AllowedCurrencies,
		AllowedMerchants:  cfg.AllowedMerchants,
	})
}

// UpdateSettings serves PATCH /dashboard/settings/policy. AllowedProducts
// is not part of the request shape at all -- it isn't a real editable
// knob, see Engine.UpdateConfig's doc comment -- so there's no field
// here a caller could even attempt to set it through. Still validated
// deterministically server-side by policy.Engine exactly as before:
// this endpoint changes nothing about HOW policy decides, only WHAT the
// numbers/lists it decides against are (PLAN-05-SELLER-DASHBOARD.md
// §4's own framing).
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req updatePolicySettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if msg := validatePolicySettings(req); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	operatorEmail := ""
	if operator, ok := auth.OperatorFromContext(r.Context()); ok {
		operatorEmail = operator.Email
	}

	cfg := PolicyConfig{
		Ceiling:           req.Ceiling,
		BudgetTolerance:   req.BudgetTolerance,
		AllowedCurrencies: req.AllowedCurrencies,
		AllowedMerchants:  req.AllowedMerchants,
	}

	updated, err := h.service.UpdatePolicyConfig(r.Context(), cfg, operatorEmail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, policySettingsResponse{
		Ceiling:           updated.Ceiling,
		BudgetTolerance:   updated.BudgetTolerance,
		AllowedCurrencies: updated.AllowedCurrencies,
		AllowedMerchants:  updated.AllowedMerchants,
	})
}

// validatePolicySettings mirrors catalog.validateProduct's convention: a
// plain function returning a non-empty message on the first problem
// found, empty string when the request is acceptable. Deliberately
// conservative -- rejecting a config that would make policy.Engine
// reject every future proposal (empty allowlists, a non-positive
// ceiling) is far cheaper to catch here than to debug "why is checkout
// broken" after the fact.
func validatePolicySettings(req updatePolicySettingsRequest) string {
	if req.Ceiling <= 0 {
		return "ceiling must be a positive number of paise"
	}
	if req.BudgetTolerance < 0 {
		return "budget_tolerance cannot be negative"
	}
	if req.BudgetTolerance > 5 {
		return "budget_tolerance looks like a mistake (over 500%); double-check the value"
	}
	if len(req.AllowedCurrencies) == 0 {
		return "allowed_currencies cannot be empty"
	}
	for _, currency := range req.AllowedCurrencies {
		if strings.TrimSpace(currency) == "" {
			return "allowed_currencies cannot contain a blank entry"
		}
	}
	if len(req.AllowedMerchants) == 0 {
		return "allowed_merchants cannot be empty"
	}
	for _, merchant := range req.AllowedMerchants {
		if strings.TrimSpace(merchant) == "" {
			return "allowed_merchants cannot contain a blank entry"
		}
	}
	return ""
}

var _ = context.Background
