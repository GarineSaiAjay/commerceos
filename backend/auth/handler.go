package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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
