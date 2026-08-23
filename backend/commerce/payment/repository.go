package payment

import "context"

type Repository interface {
	GetByOrderID(ctx context.Context, orderID string) (Payment, error)

	Create(ctx context.Context, payment Payment) error

	MarkPaid(
		ctx context.Context,
		orderID string,
		providerPaymentID string,
	) (Payment, error)
}
