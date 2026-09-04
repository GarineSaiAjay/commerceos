package payment

import "context"

type Repository interface {
	GetByOrderID(ctx context.Context, orderID string) (Payment, error)

	// GetByProviderOrderID looks a payment up by the payment provider's
	// own order identifier (Payment.ProviderOrderID -- e.g. Razorpay's
	// "order_..." ID), not commerceos's own order ID. This is the
	// correct lookup for anything driven by provider-originated data
	// that only ever carries the provider's own order ID, such as a
	// Razorpay webhook payload's payload.payment.entity.order_id (see
	// webhook_applier.go) -- razorpay.go only ever sends commerceos's
	// order ID to Razorpay as the opaque "receipt" field on order
	// creation, never as the order_id itself, so the two ID spaces are
	// never interchangeable.
	GetByProviderOrderID(ctx context.Context, providerOrderID string) (Payment, error)

	GetByIdempotencyKey(ctx context.Context, key string) (Payment, error)

	Create(ctx context.Context, payment Payment) error

	// TransitionStatus atomically moves the payment from its current
	// status to `to`, guarded by the centralized payment state machine.
	// It returns statemachine.ErrIllegalTransition if the edge is not
	// allowed.
	TransitionStatus(
		ctx context.Context,
		orderID string,
		to string,
		providerPaymentID string,
	) (Payment, error)
}
