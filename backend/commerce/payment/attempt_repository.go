package payment

import "context"

type AttemptRepository interface {
	Create(
		ctx context.Context,
		attempt PaymentAttempt,
	) error

	MarkPaid(
		ctx context.Context,
		orderID string,
		razorpayPaymentID string,
	) (PaymentAttempt, error)
}
