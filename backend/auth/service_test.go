package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeRepository is an in-memory auth.Repository for tests -- no
// Postgres needed. Prior to this file, backend/auth had zero automated
// test coverage for the entire operator login/session stack, including
// the RequireOperator/OptionalOperator middleware that gates the
// merchant dashboard, every /safety/* route, /audit/verify, and the
// approval-request and run list endpoints (files/AUDIT-2026-08-29.md §4).
type fakeRepository struct {
	operators map[string]OperatorRecord // keyed by (already-normalized) email
	sessions  map[string]sessionRecord  // keyed by token hash
}

type sessionRecord struct {
	operator  Operator
	expiresAt time.Time
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		operators: map[string]OperatorRecord{},
		sessions:  map[string]sessionRecord{},
	}
}

func (f *fakeRepository) seedOperator(rec OperatorRecord) {
	f.operators[rec.Email] = rec
}

func (f *fakeRepository) GetOperatorByEmail(ctx context.Context, email string) (OperatorRecord, error) {
	rec, ok := f.operators[email]
	if !ok {
		return OperatorRecord{}, ErrOperatorNotFound
	}
	return rec, nil
}

func (f *fakeRepository) CreateSession(ctx context.Context, tokenHash string, operatorID string, expiresAt time.Time) error {
	for _, rec := range f.operators {
		if rec.ID == operatorID {
			f.sessions[tokenHash] = sessionRecord{
				operator:  Operator{ID: rec.ID, MerchantID: rec.MerchantID, Email: rec.Email},
				expiresAt: expiresAt,
			}
			return nil
		}
	}
	return ErrOperatorNotFound
}

func (f *fakeRepository) GetSession(ctx context.Context, tokenHash string) (Operator, time.Time, error) {
	rec, ok := f.sessions[tokenHash]
	if !ok {
		return Operator{}, time.Time{}, ErrSessionNotFound
	}
	return rec.operator, rec.expiresAt, nil
}

func (f *fakeRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	delete(f.sessions, tokenHash)
	return nil
}

// --- test setup helpers ---

const testPassword = "CommerceOS!2026"

func newTestService(t *testing.T) (*Service, *fakeRepository) {
	t.Helper()
	repo := newFakeRepository()
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	repo.seedOperator(OperatorRecord{
		ID:           "operator_1",
		MerchantID:   "merchant_001",
		Email:        "owner@commerceos.demo",
		PasswordHash: hash,
	})
	return NewService(repo), repo
}

// --- Login ---

func TestLoginSuccess(t *testing.T) {
	svc, _ := newTestService(t)

	token, operator, err := svc.Login(context.Background(), "owner@commerceos.demo", testPassword)
	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty session token")
	}
	if operator.ID != "operator_1" || operator.MerchantID != "merchant_001" || operator.Email != "owner@commerceos.demo" {
		t.Fatalf("unexpected operator: %+v", operator)
	}
}

// TestLoginNormalizesEmail proves case and surrounding whitespace in the
// submitted email don't cause a false "wrong password" -- emails are
// stored lowercase (db/seeds/002_operator.sql) and Postgres TEXT
// equality is case-sensitive, so a copy-pasted "Owner@..." or a
// trailing space must still resolve to the same operator.
func TestLoginNormalizesEmail(t *testing.T) {
	svc, _ := newTestService(t)

	_, operator, err := svc.Login(context.Background(), "  Owner@CommerceOS.Demo  ", testPassword)
	if err != nil {
		t.Fatalf("expected login to normalize case/whitespace, got error: %v", err)
	}
	if operator.ID != "operator_1" {
		t.Fatalf("unexpected operator: %+v", operator)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	svc, _ := newTestService(t)

	_, _, err := svc.Login(context.Background(), "owner@commerceos.demo", "not-the-password")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

// TestLoginUnknownEmailReturnsSameError proves an unknown email and a
// wrong password are indistinguishable from the caller's side --
// Service.Login's own doc comment says this is deliberate, so a
// judge/attacker can't use this endpoint to enumerate registered
// operator emails.
func TestLoginUnknownEmailReturnsSameError(t *testing.T) {
	svc, _ := newTestService(t)

	_, _, err := svc.Login(context.Background(), "nobody@commerceos.demo", testPassword)
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for an unknown email, got %v", err)
	}
}

// --- Logout / ValidateToken ---

func TestLogoutIsIdempotent(t *testing.T) {
	svc, _ := newTestService(t)

	token, _, err := svc.Login(context.Background(), "owner@commerceos.demo", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if err := svc.Logout(context.Background(), token); err != nil {
		t.Fatalf("first logout: %v", err)
	}
	if err := svc.Logout(context.Background(), token); err != nil {
		t.Fatalf("second logout on an already-invalid token must not error: %v", err)
	}
	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("logout with an empty token must not error: %v", err)
	}

	if _, err := svc.ValidateToken(context.Background(), token); err != ErrInvalidSession {
		t.Fatalf("expected the logged-out token to be invalid, got %v", err)
	}
}

func TestValidateTokenUnknown(t *testing.T) {
	svc, _ := newTestService(t)

	if _, err := svc.ValidateToken(context.Background(), "not-a-real-token"); err != ErrInvalidSession {
		t.Fatalf("expected ErrInvalidSession for an unknown token, got %v", err)
	}
	if _, err := svc.ValidateToken(context.Background(), ""); err != ErrInvalidSession {
		t.Fatalf("expected ErrInvalidSession for an empty token, got %v", err)
	}
}

// TestValidateTokenExpired proves a session past its TTL is rejected --
// using Service's injectable clock (svc.now) rather than sleeping past
// the real 24h SessionTTL.
func TestValidateTokenExpired(t *testing.T) {
	svc, _ := newTestService(t)

	loginTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return loginTime }

	token, _, err := svc.Login(context.Background(), "owner@commerceos.demo", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Still valid one second before expiry.
	svc.now = func() time.Time { return loginTime.Add(SessionTTL).Add(-time.Second) }
	if _, err := svc.ValidateToken(context.Background(), token); err != nil {
		t.Fatalf("expected the token to still be valid just before its TTL, got %v", err)
	}

	// Expired one second after expiry.
	svc.now = func() time.Time { return loginTime.Add(SessionTTL).Add(time.Second) }
	if _, err := svc.ValidateToken(context.Background(), token); err != ErrInvalidSession {
		t.Fatalf("expected ErrInvalidSession once the TTL has passed, got %v", err)
	}
}

// --- RequireOperator / OptionalOperator middleware ---

func TestRequireOperatorRejectsMissingToken(t *testing.T) {
	svc, _ := newTestService(t)

	called := false
	handler := svc.RequireOperator(func(w http.ResponseWriter, r *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodGet, "/dashboard/overview", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no token, got %d", rec.Code)
	}
	if called {
		t.Fatal("the wrapped handler must not run without a valid token")
	}
}

func TestRequireOperatorRejectsInvalidToken(t *testing.T) {
	svc, _ := newTestService(t)

	handler := svc.RequireOperator(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the wrapped handler must not run with an invalid token")
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/overview", nil)
	req.Header.Set("Authorization", "Bearer garbage-not-a-real-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with an invalid token, got %d", rec.Code)
	}
}

func TestRequireOperatorAllowsValidToken(t *testing.T) {
	svc, _ := newTestService(t)

	token, _, err := svc.Login(context.Background(), "owner@commerceos.demo", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	var gotOperator Operator
	var gotOK bool
	handler := svc.RequireOperator(func(w http.ResponseWriter, r *http.Request) {
		gotOperator, gotOK = OperatorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/overview", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with a valid token, got %d", rec.Code)
	}
	if !gotOK {
		t.Fatal("expected an operator attached to the request context")
	}
	if gotOperator.ID != "operator_1" {
		t.Fatalf("unexpected operator in context: %+v", gotOperator)
	}
}

// TestOptionalOperatorProceedsAnonymously proves a request with no
// bearer token at all still reaches the handler (e.g. a buyer
// self-confirming their own approval by cart_id) -- it just has no
// operator attached to its context.
func TestOptionalOperatorProceedsAnonymously(t *testing.T) {
	svc, _ := newTestService(t)

	var reached bool
	var gotOK bool
	handler := svc.OptionalOperator(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		_, gotOK = OperatorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/approval-requests/req_1/approve", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for an anonymous request, got %d", rec.Code)
	}
	if !reached {
		t.Fatal("expected the wrapped handler to run for an anonymous request")
	}
	if gotOK {
		t.Fatal("expected no operator in context for an anonymous request")
	}
}

// TestOptionalOperatorRejectsInvalidToken proves a *present but
// invalid* token is still rejected outright -- OptionalOperator must
// never silently treat a garbage/expired token the same as "no token
// at all" (Service.OptionalOperator's own doc comment states this).
func TestOptionalOperatorRejectsInvalidToken(t *testing.T) {
	svc, _ := newTestService(t)

	handler := svc.OptionalOperator(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the wrapped handler must not run with an invalid-but-present token")
	})

	req := httptest.NewRequest(http.MethodPost, "/approval-requests/req_1/approve", nil)
	req.Header.Set("Authorization", "Bearer garbage-not-a-real-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an invalid-but-present token, got %d", rec.Code)
	}
}

func TestOptionalOperatorAttachesValidOperator(t *testing.T) {
	svc, _ := newTestService(t)

	token, _, err := svc.Login(context.Background(), "owner@commerceos.demo", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	var gotOperator Operator
	var gotOK bool
	handler := svc.OptionalOperator(func(w http.ResponseWriter, r *http.Request) {
		gotOperator, gotOK = OperatorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/approval-requests/req_1/approve", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !gotOK || gotOperator.ID != "operator_1" {
		t.Fatalf("expected operator_1 attached to context, got ok=%v operator=%+v", gotOK, gotOperator)
	}
}
