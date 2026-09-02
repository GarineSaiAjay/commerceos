package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token      string `json:"token"`
	OperatorID string `json:"operator_id"`
	MerchantID string `json:"merchant_id"`
	Email      string `json:"email"`
	ExpiresIn  int    `json:"expires_in_seconds"`
}

// Login serves POST /auth/login. On success the caller sends the
// returned token back as "Authorization: Bearer <token>" on every
// request to an operator-gated route.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	token, operator, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrTooManyAttempts) {
			// Distinct from the invalid-credentials response below: telling
			// the caller they're rate-limited doesn't leak whether the
			// email exists beyond what their own repeated attempts already
			// imply, and a legitimate operator locked out by a slow typo
			// needs to know to wait rather than keep guessing.
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		// Same status and message whether the email is unknown or the
		// password is wrong -- see Service.Login.
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:      token,
		OperatorID: operator.ID,
		MerchantID: operator.MerchantID,
		Email:      operator.Email,
		ExpiresIn:  int(SessionTTL.Seconds()),
	})
}

// Logout serves POST /auth/logout.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.service.Logout(r.Context(), bearerToken(r)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- item 40: multi-operator invites ---
//
// Routing (wired in backend/cmd/server/main.go, following the same
// exact-route-before-prefix-route convention as e.g. "/campaigns" vs.
// "/campaigns/"):
//   POST   /auth/invites         RequireOperator  -> Invites (create)
//   GET    /auth/invites         RequireOperator  -> Invites (list)
//   DELETE /auth/invites/{id}    RequireOperator  -> InviteByID (revoke)
//   POST   /auth/invites/accept  public           -> AcceptInvite
//   GET    /auth/operators       RequireOperator  -> Operators (list)
//   DELETE /auth/operators/{id}  RequireOperator  -> OperatorByID (remove)

type inviteRequest struct {
	Email string `json:"email"`
}

type inviteResponse struct {
	InviteID  string `json:"invite_id"`
	Email     string `json:"email"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type inviteListItem struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	InvitedBy  string  `json:"invited_by"`
	ExpiresAt  string  `json:"expires_at"`
	AcceptedAt *string `json:"accepted_at"`
	Status     string  `json:"status"` // "pending" | "accepted" | "expired"
}

// Invites serves both POST (create an invite) and GET (list this
// merchant's invites) at /auth/invites -- both require an operator
// session, and the operator field consulted below is always the
// caller's own (never client-supplied), so there's no cross-merchant
// leakage.
func (h *Handler) Invites(w http.ResponseWriter, r *http.Request) {
	operator, ok := OperatorFromContext(r.Context())
	if !ok {
		// Unreachable in practice -- main.go always wraps this in
		// RequireOperator -- but fail closed rather than trust that.
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.createInvite(w, r, operator)
	case http.MethodGet:
		h.listInvites(w, r, operator)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request, operator Operator) {
	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	token, invite, err := h.service.InviteOperator(r.Context(), operator, req.Email)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidEmail):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrEmailAlreadyRegistered):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusCreated, inviteResponse{
		InviteID:  invite.ID,
		Email:     invite.Email,
		Token:     token,
		ExpiresAt: invite.ExpiresAt.Format(time.RFC3339),
	})
}

func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request, operator Operator) {
	invites, err := h.service.ListInvites(r.Context(), operator.MerchantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	items := make([]inviteListItem, 0, len(invites))
	for _, inv := range invites {
		item := inviteListItem{
			ID:        inv.ID,
			Email:     inv.Email,
			InvitedBy: inv.InvitedBy,
			ExpiresAt: inv.ExpiresAt.Format(time.RFC3339),
			Status:    "pending",
		}
		if inv.AcceptedAt != nil {
			accepted := inv.AcceptedAt.Format(time.RFC3339)
			item.AcceptedAt = &accepted
			item.Status = "accepted"
		} else if now.After(inv.ExpiresAt) {
			item.Status = "expired"
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, items)
}

// InviteByID serves DELETE /auth/invites/{id} -- revoking a still-
// pending invite. Registered on the "/auth/invites/" prefix; the exact
// path "/auth/invites" (Invites, above) and "/auth/invites/accept"
// (AcceptInvite, below) are both more specific and win over this one.
func (h *Handler) InviteByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	operator, ok := OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/auth/invites/")
	if id == "" {
		http.Error(w, "invite id required", http.StatusBadRequest)
		return
	}

	if err := h.service.RevokeInvite(r.Context(), operator, id); err != nil {
		if errors.Is(err, ErrInviteNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type acceptInviteRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// AcceptInvite serves POST /auth/invites/accept. Deliberately public
// (no bearer token possible or required -- the invitee doesn't have an
// account yet): the invite token itself, a 256-bit random value only
// its SHA-256 hash of which is ever stored, is what authorizes this
// call, exactly as a session token authorizes a request to a gated
// route. On success it behaves like Login: it returns an
// already-active session for the brand-new operator account.
func (h *Handler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req acceptInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	token, operator, err := h.service.AcceptInvite(r.Context(), req.Token, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrPasswordTooShort):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrInviteNotFound), errors.Is(err, ErrInviteExpired), errors.Is(err, ErrInviteAlreadyAccepted):
			http.Error(w, err.Error(), http.StatusGone)
		case errors.Is(err, ErrEmailAlreadyRegistered):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if token == "" {
		// The account was created but the follow-up session issuance
		// failed (see AcceptInvite's own doc comment) -- still success
		// from the invitee's side of the API contract, they just need to
		// log in once instead of landing signed-in immediately.
		writeJSON(w, http.StatusCreated, loginResponse{
			OperatorID: operator.ID,
			MerchantID: operator.MerchantID,
			Email:      operator.Email,
		})
		return
	}

	writeJSON(w, http.StatusCreated, loginResponse{
		Token:      token,
		OperatorID: operator.ID,
		MerchantID: operator.MerchantID,
		Email:      operator.Email,
		ExpiresIn:  int(SessionTTL.Seconds()),
	})
}

type operatorListItem struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Operators serves GET /auth/operators -- the merchant's own team list.
func (h *Handler) Operators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	operator, ok := OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	records, err := h.service.ListOperators(r.Context(), operator.MerchantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	items := make([]operatorListItem, 0, len(records))
	for _, rec := range records {
		items = append(items, operatorListItem{ID: rec.ID, Email: rec.Email})
	}

	writeJSON(w, http.StatusOK, items)
}

// OperatorByID serves DELETE /auth/operators/{id} -- removing a
// teammate. See Service.RemoveOperator for the self-removal and
// last-operator guards.
func (h *Handler) OperatorByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	operator, ok := OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/auth/operators/")
	if id == "" {
		http.Error(w, "operator id required", http.StatusBadRequest)
		return
	}

	if err := h.service.RemoveOperator(r.Context(), operator, id); err != nil {
		switch {
		case errors.Is(err, ErrCannotRemoveSelf), errors.Is(err, ErrCannotRemoveLastOperator):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, ErrOperatorNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// bearerToken extracts the token from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}

type contextKey struct{}

var operatorContextKey = contextKey{}

func withOperator(ctx context.Context, operator Operator) context.Context {
	return context.WithValue(ctx, operatorContextKey, operator)
}

// OperatorFromContext returns the operator attached by RequireOperator
// or OptionalOperator, if any.
func OperatorFromContext(ctx context.Context) (Operator, bool) {
	operator, ok := ctx.Value(operatorContextKey).(Operator)
	return operator, ok
}

// RequireOperator gates next behind a valid operator session: a missing
// or invalid token is rejected with 401 before next ever runs. Use for
// routes that are exclusively for the merchant's own back office
// (dashboard data, safety/red-team controls, the approval-request and
// run lists) -- see files/AUTH.md.
func (s *Service) RequireOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		operator, err := s.ValidateToken(r.Context(), bearerToken(r))
		if err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(withOperator(r.Context(), operator)))
	}
}

// OptionalOperator attaches the operator to the request context when a
// valid bearer token is present, but never blocks the request -- an
// absent token proceeds anonymously, and next is responsible for
// deciding whether that's sufficient. A *present but invalid* token is
// still rejected outright (a garbage or expired token should never be
// silently treated the same as "no token"). Used for the approval
// approve/reject endpoints, which have two legitimate callers: the
// buyer confirming their own purchase (proven by supplying the cart_id
// the request was created for) and a logged-in merchant operator
// reviewing from the dashboard (proven by this token) -- see
// files/AUTH.md.
func (s *Service) OptionalOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			next(w, r)
			return
		}
		operator, err := s.ValidateToken(r.Context(), token)
		if err != nil {
			http.Error(w, "invalid or expired session", http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(withOperator(r.Context(), operator)))
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
