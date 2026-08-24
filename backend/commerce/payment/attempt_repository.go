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

	// MarkFailed records a failed attempt with the Razorpay error
	// details, so recovery flows can surface the failure reason.
	MarkFailed(
		ctx context.Context,
		orderID string,
		razorpayPaymentID string,
		errorCode string,
		errorDescription string,
	) (PaymentAttempt, error)

	// GetLatestForOrder returns the most recent attempt for an order.
	GetLatestForOrder(ctx context.Context, orderID string) (PaymentAttempt, error)
}
