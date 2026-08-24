package payment

import (
	"context"
	"testing"

	"github.com/garinesaiajay/commerceos/statemachine"
)

type capturedPaymentRepository struct {
	transitionCalled bool
}

func (r *capturedPaymentRepository) GetByOrderID(context.Context, string) (Payment, error) {
	return Payment{ID: "payment_1", Status: statemachine.PaymentCaptured}, nil
}

func (r *capturedPaymentRepository) GetByIdempotencyKey(context.Context, string) (Payment, error) {
	return Payment{}, ErrPaymentNotFound
}

func (r *capturedPaymentRepository) Create(context.Context, Payment) error { return nil }

func (r *capturedPaymentRepository) TransitionStatus(context.Context, string, string, string) (Payment, error) {
	r.transitionCalled = true
	return Payment{}, nil
}

func TestApplyCapturedIsNoOpWhenAlreadyCaptured(t *testing.T) {
	repo := &capturedPaymentRepository{}
	applier := NewWebhookApplier(repo, nil, nil, nil)
	payload := RazorpayWebhookPayload{}
	payload.Payload.Payment.Entity.OrderID = "order_1"

	if err := applier.ApplyCaptured(context.Background(), payload); err != nil {
		t.Fatalf("repeated capture should succeed: %v", err)
	}
	if repo.transitionCalled {
		t.Fatal("already captured payment must not transition again")
	}
}
