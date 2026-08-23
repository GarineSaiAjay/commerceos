package payment

import "context"

type Provider interface {
	CreatePayment(
		ctx context.Context,
		req CreatePaymentRequest,
	) (Payment, error)

	VerifyPaymentSignature(
		razorpayOrderID string,
		razorpayPaymentID string,
		signature string,
	) error
}
