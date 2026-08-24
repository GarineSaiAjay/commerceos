package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/policy"
)

// fakeAuthorizer always rejects — used to prove the chokepoint.
type fakeAuthorizer struct {
	called int
}

func (a *fakeAuthorizer) VerifyAuthorization(ctx context.Context, id string) (policy.Authorization, error) {
	a.called++
	return policy.Authorization{}, policy.ErrAuthorizationNotFound
}

// TestPaymentRequiresAuthorization proves spec §1: the money-moving
// entry point requires a valid authorization_id and verifies it BEFORE
// doing anything — zero provider (Razorpay) calls.
func TestPaymentRequiresAuthorization(t *testing.T) {
	provider := &countingProvider{}
	repo := newMemRepo()
	auth := &fakeAuthorizer{}

	service := NewServiceWithAuthorizer(provider, repo, nil, nil, auth)

	ord := order.Order{ID: "order_authz_001", Subtotal: 24900, Currency: "INR"}

	// No authorization_id → rejected before any Razorpay call.
	_, err := service.CreatePaymentOrder(
		context.Background(),
		ord,
		"key1",
		"", // no authorization
	)
	if err == nil {
		t.Fatal("expected error when authorization_id is missing")
	}

	if provider.CallCount() != 0 {
		t.Fatalf("expected 0 provider calls, got %d", provider.CallCount())
	}

	// Invalid authorization_id → rejected before any Razorpay call.
	_, err = service.CreatePaymentOrder(
		context.Background(),
		ord,
		"key2",
		"auth_does_not_exist",
	)
	if err == nil {
		t.Fatal("expected error for invalid authorization")
	}

	if provider.CallCount() != 0 {
		t.Fatalf("expected 0 provider calls after invalid authz, got %d", provider.CallCount())
	}

	if !errors.Is(err, policy.ErrAuthorizationNotFound) {
		t.Fatalf("expected ErrAuthorizationNotFound, got %v", err)
	}
}
