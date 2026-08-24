package payment

import "context"

type Repository interface {
	GetByOrderID(ctx context.Context, orderID string) (Payment, error)

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
