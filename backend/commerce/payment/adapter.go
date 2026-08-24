package payment

import "context"

// PaymentAdapter is the protocol-adapter interface. Razorpay is the
// current implementation; other rails (x402, future protocols) can be
// added without touching the domain model. This is the extension point
// the MCP/ACP/UCP/REST layers all sit on top of.
//
// The adapter deliberately mirrors the Provider interface so the domain
// model (payment.Service) depends only on the narrow Provider surface,
// and swapping the real rail (Razorpay) for a synthetic one (mock,
// simulator, future protocols) is a one-line change in main.go.
type PaymentAdapter interface {
	// CreatePayment creates a payment order for the given amount.
	CreatePayment(ctx context.Context, req CreatePaymentRequest) (Payment, error)

	// VerifyPaymentSignature verifies a client-side payment signature.
	VerifyPaymentSignature(razorpayOrderID string, razorpayPaymentID string, signature string) error

	// CallCount returns the number of outbound provider API calls made
	// through this adapter. It is the Phase 1 call counter surfaced to
	// prove "zero calls" in audit/red-team flows.
	CallCount() int64
}

// RazorpayAdapter adapts the RazorpayClient to the PaymentAdapter /
// Provider interfaces. It is the only code path that touches the
// Razorpay SDK.
type RazorpayAdapter struct {
	client *RazorpayClient
}

func NewRazorpayAdapter(client *RazorpayClient) *RazorpayAdapter {
	return &RazorpayAdapter{client: client}
}

func (a *RazorpayAdapter) CreatePayment(
	ctx context.Context,
	req CreatePaymentRequest,
) (Payment, error) {
	return a.client.CreatePayment(ctx, req)
}

func (a *RazorpayAdapter) VerifyPaymentSignature(
	razorpayOrderID string,
	razorpayPaymentID string,
	signature string,
) error {
	return a.client.VerifyPaymentSignature(
		razorpayOrderID,
		razorpayPaymentID,
		signature,
	)
}

func (a *RazorpayAdapter) CallCount() int64 {
	return a.client.CallCount()
}

// Compile-time assertions: the adapter satisfies both the narrow Provider
// the payment service consumes and the PaymentAdapter extension point.
var _ Provider = (*RazorpayAdapter)(nil)
var _ PaymentAdapter = (*RazorpayAdapter)(nil)
