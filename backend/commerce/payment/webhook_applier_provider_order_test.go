package payment

// Full-codebase re-audit (P2): ApplyCaptured/ApplyFailed used to look
// payments up via GetByOrderID(paymentEntity.OrderID) -- but
// paymentEntity.OrderID is Razorpay's OWN order ID
// (payload.payment.entity.order_id), and razorpay.go never sends
// commerceos's own order ID to Razorpay as anything but the opaque
// "receipt" field, so the two ID spaces never actually matched: a real
// Razorpay webhook could never resolve a payment this way. See
// webhook_applier.go's ApplyCaptured doc comment for the full
// explanation. These tests are the regression coverage for both halves
// of that fix: resolving via GetByProviderOrderID (using Razorpay's
// order ID) and then acting on the resolved payment's own commerceos
// OrderID, plus the new captured-amount/currency cross-check that fix
// made possible to verify.

import (
	"context"
	"errors"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/statemachine"
)

// providerLookupRepository is a fuller Repository fake than
// capturedPaymentRepository (webhook_applier_idempotency_test.go),
// which returns the identical fixture from both GetByOrderID and
// GetByProviderOrderID and so can't tell them apart. This one keys its
// GetByProviderOrderID stub on the payment's own ProviderOrderID, fails
// GetByOrderID outright (recording that it was called, since a correct
// ApplyCaptured/ApplyFailed must never call it with a Razorpay order
// ID), and records every order ID TransitionStatus is actually invoked
// with -- so a test can prove the resolved payment's commerceos
// OrderID is what flows downstream, not the Razorpay order ID the
// webhook payload carried.
type providerLookupRepository struct {
	payment            Payment
	getByOrderIDCalled bool
	transitionOrderIDs []string
}

func (r *providerLookupRepository) GetByOrderID(context.Context, string) (Payment, error) {
	r.getByOrderIDCalled = true
	return Payment{}, ErrPaymentNotFound
}

func (r *providerLookupRepository) GetByProviderOrderID(_ context.Context, providerOrderID string) (Payment, error) {
	if providerOrderID != r.payment.ProviderOrderID {
		return Payment{}, ErrPaymentNotFound
	}
	return r.payment, nil
}

func (r *providerLookupRepository) GetByIdempotencyKey(context.Context, string) (Payment, error) {
	return Payment{}, ErrPaymentNotFound
}

func (r *providerLookupRepository) Create(context.Context, Payment) error { return nil }

func (r *providerLookupRepository) TransitionStatus(
	_ context.Context,
	orderID string,
	to string,
	_ string,
) (Payment, error) {
	r.transitionOrderIDs = append(r.transitionOrderIDs, orderID)
	p := r.payment
	p.Status = to
	return p, nil
}

// stubOrders is a minimal OrderStatusTransitioner fake that records the
// order ID it was called with and always succeeds.
type stubOrders struct {
	transitionedOrderID string
}

func (o *stubOrders) TransitionStatus(_ context.Context, orderID string, to string) (order.Order, error) {
	o.transitionedOrderID = orderID
	return order.Order{ID: orderID, Status: to}, nil
}

func (o *stubOrders) SetRunID(context.Context, string, string) error { return nil }

// TestApplyCapturedResolvesByProviderOrderIDNotInternalOrderID proves
// ApplyCaptured resolves the payment via GetByProviderOrderID (keyed on
// Razorpay's order ID from the webhook payload) rather than GetByOrderID
// (which would be keyed on that same value misread as commerceos's own
// order ID), and that every downstream call uses the resolved payment's
// own commerceos OrderID, not the Razorpay order ID from the payload.
func TestApplyCapturedResolvesByProviderOrderIDNotInternalOrderID(t *testing.T) {
	repo := &providerLookupRepository{
		payment: Payment{
			ID:              "payment_1",
			OrderID:         "order_commerceos_abc",
			ProviderOrderID: "order_razorpay_xyz",
			Amount:          249000,
			Currency:        "INR",
			Status:          statemachine.PaymentPending,
		},
	}
	orders := &stubOrders{}
	applier := NewWebhookApplier(repo, orders, nil, nil)

	payload := RazorpayWebhookPayload{}
	payload.Payload.Payment.Entity.OrderID = "order_razorpay_xyz"
	payload.Payload.Payment.Entity.ID = "pay_razorpay_1"
	payload.Payload.Payment.Entity.Amount = 249000
	payload.Payload.Payment.Entity.Currency = "INR"

	if err := applier.ApplyCaptured(context.Background(), payload); err != nil {
		t.Fatalf("ApplyCaptured: %v", err)
	}
	if repo.getByOrderIDCalled {
		t.Error("ApplyCaptured must resolve the payment via GetByProviderOrderID, never GetByOrderID, given a Razorpay order ID")
	}
	for _, gotOrderID := range repo.transitionOrderIDs {
		if gotOrderID != "order_commerceos_abc" {
			t.Errorf("payment TransitionStatus called with order ID %q, want the resolved commerceos order ID %q", gotOrderID, "order_commerceos_abc")
		}
	}
	if orders.transitionedOrderID != "order_commerceos_abc" {
		t.Errorf("order TransitionStatus called with order ID %q, want the resolved commerceos order ID %q", orders.transitionedOrderID, "order_commerceos_abc")
	}
}

// TestApplyCapturedRejectsAmountMismatch is the regression test for the
// webhook amount/currency cross-check (full-codebase re-audit, P2):
// ApplyCaptured must reject (and never transition anything) a captured
// webhook whose amount doesn't match what the payment was created for,
// rather than silently marking the order paid on unreconciled data.
func TestApplyCapturedRejectsAmountMismatch(t *testing.T) {
	repo := &providerLookupRepository{
		payment: Payment{
			ID:              "payment_1",
			OrderID:         "order_commerceos_abc",
			ProviderOrderID: "order_razorpay_xyz",
			Amount:          249000,
			Currency:        "INR",
			Status:          statemachine.PaymentPending,
		},
	}
	orders := &stubOrders{}
	applier := NewWebhookApplier(repo, orders, nil, nil)

	payload := RazorpayWebhookPayload{}
	payload.Payload.Payment.Entity.OrderID = "order_razorpay_xyz"
	payload.Payload.Payment.Entity.ID = "pay_razorpay_1"
	payload.Payload.Payment.Entity.Amount = 1 // mismatched -- payment was created for 249000
	payload.Payload.Payment.Entity.Currency = "INR"

	err := applier.ApplyCaptured(context.Background(), payload)
	if err == nil {
		t.Fatal("expected ApplyCaptured to reject a captured amount that doesn't match the payment record")
	}
	if !errors.Is(err, ErrWebhookAmountMismatch) {
		t.Errorf("expected error to wrap ErrWebhookAmountMismatch, got: %v", err)
	}
	if len(repo.transitionOrderIDs) != 0 {
		t.Errorf("expected no payment transition on a mismatched capture, got %v", repo.transitionOrderIDs)
	}
	if orders.transitionedOrderID != "" {
		t.Errorf("expected no order transition on a mismatched capture, got order ID %q", orders.transitionedOrderID)
	}
}

// TestApplyCapturedRejectsCurrencyMismatch mirrors the amount-mismatch
// test for currency, proving the check catches either dimension.
func TestApplyCapturedRejectsCurrencyMismatch(t *testing.T) {
	repo := &providerLookupRepository{
		payment: Payment{
			ID:              "payment_1",
			OrderID:         "order_commerceos_abc",
			ProviderOrderID: "order_razorpay_xyz",
			Amount:          249000,
			Currency:        "INR",
			Status:          statemachine.PaymentPending,
		},
	}
	applier := NewWebhookApplier(repo, &stubOrders{}, nil, nil)

	payload := RazorpayWebhookPayload{}
	payload.Payload.Payment.Entity.OrderID = "order_razorpay_xyz"
	payload.Payload.Payment.Entity.ID = "pay_razorpay_1"
	payload.Payload.Payment.Entity.Amount = 249000
	payload.Payload.Payment.Entity.Currency = "USD" // mismatched -- payment was created for INR

	err := applier.ApplyCaptured(context.Background(), payload)
	if !errors.Is(err, ErrWebhookAmountMismatch) {
		t.Errorf("expected error to wrap ErrWebhookAmountMismatch for a currency mismatch, got: %v", err)
	}
	if len(repo.transitionOrderIDs) != 0 {
		t.Errorf("expected no payment transition on a mismatched currency, got %v", repo.transitionOrderIDs)
	}
}
