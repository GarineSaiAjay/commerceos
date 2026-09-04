package payment

import (
	"context"
	"testing"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/policy"
)

// consumingAuthorizer approves authorizations and records when they are
// marked used. CartID/Currency/Amount are echoed back on every
// VerifyAuthorization response so they satisfy CreatePaymentOrder's
// post-fix binding checks (P0 fix, full-codebase re-audit 2026-09-04) --
// each test below sets these to match the order.Order it's exercising.
type consumingAuthorizer struct {
	used     map[string]bool
	CartID   string
	Currency string
	Amount   int64
}

// Compile-time guard: the policy Service must satisfy the
// AuthorizationConsumer interface so a created payment actually marks the
// authorization USED (a silent runtime miss here is a replay hole).
var _ AuthorizationConsumer = (*policy.Service)(nil)

func (a *consumingAuthorizer) VerifyAuthorization(
	ctx context.Context,
	id string,
) (policy.Authorization, error) {
	return policy.Authorization{
		ID:        id,
		Status:    "ACTIVE",
		ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
		CartID:    a.CartID,
		Currency:  a.Currency,
		Amount:    a.Amount,
	}, nil
}

func (a *consumingAuthorizer) MarkAuthorizationUsed(ctx context.Context, id string) error {
	a.used[id] = true
	return nil
}

// TestAuthorizationConsumedAfterNewPayment proves the authorization is
// marked USED only when a NEW payment is created (not on an idempotent
// repeat, which returns the existing payment untouched).
func TestAuthorizationConsumedAfterNewPayment(t *testing.T) {
	provider := &countingProvider{}
	repo := newMemRepo()
	auth := &consumingAuthorizer{
		used: map[string]bool{}, CartID: "cart_auth_consume", Currency: "INR", Amount: 2_490_000,
	}

	service := NewServiceWithAuthorizer(provider, repo, nil, nil, auth)

	ord := order.Order{ID: "order_auth_consume", CartID: "cart_auth_consume", Subtotal: 2_490_000, Currency: "INR"}

	if _, err := service.CreatePaymentOrder(
		context.Background(), ord, "key_1", "auth_new",
	); err != nil {
		t.Fatal(err)
	}
	if !auth.used["auth_new"] {
		t.Fatal("expected authorization to be marked used after creating a new payment")
	}

	// Idempotent repeat: returns the existing payment, must NOT mark used
	// again (already used) nor create a second payment.
	if _, err := service.CreatePaymentOrder(
		context.Background(), ord, "key_1", "auth_new",
	); err != nil {
		t.Fatal(err)
	}
	if provider.CallCount() != 1 {
		t.Fatalf("expected 1 provider call, got %d", provider.CallCount())
	}
}

// TestAuthorizationNotConsumedWhenIdempotentRepeat proves an idempotent
// repeat with a DIFFERENT (already-used) authorization still returns the
// existing payment without calling the provider.
func TestAuthorizationNotConsumedWhenIdempotentRepeat(t *testing.T) {
	provider := &countingProvider{}
	repo := newMemRepo()
	auth := &consumingAuthorizer{
		used: map[string]bool{}, CartID: "cart_idem_repeat", Currency: "INR", Amount: 1_000_00,
	}

	service := NewServiceWithAuthorizer(provider, repo, nil, nil, auth)

	ord := order.Order{ID: "order_idem_repeat", CartID: "cart_idem_repeat", Subtotal: 1_000_00, Currency: "INR"}

	if _, err := service.CreatePaymentOrder(
		context.Background(), ord, "key_repeat", "auth_a",
	); err != nil {
		t.Fatal(err)
	}
	if !auth.used["auth_a"] {
		t.Fatal("expected authorization to be marked used")
	}

	// A repeat of the same idempotency key returns the existing payment
	// before the authorization is even consulted again.
	if _, err := service.CreatePaymentOrder(
		context.Background(), ord, "key_repeat", "auth_b",
	); err != nil {
		t.Fatal(err)
	}
	if !auth.used["auth_a"] {
		t.Fatal("expected first authorization to remain used")
	}
	if provider.CallCount() != 1 {
		t.Fatalf("expected 1 provider call, got %d", provider.CallCount())
	}
}
