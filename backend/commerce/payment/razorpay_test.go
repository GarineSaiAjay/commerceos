package payment

import (
	"context"
	"testing"
)

func TestRazorpayCallCounterStartsAtZero(t *testing.T) {
	client := NewRazorpayClient("rzp_test_key", "test_secret")

	if client.CallCount() != 0 {
		t.Fatalf(
			"expected call count 0, got %d",
			client.CallCount(),
		)
	}
}

func TestRazorpayCallCounterIncrementsOnAttempt(t *testing.T) {
	client := NewRazorpayClient("rzp_test_key", "test_secret")

	// The adapter increments the counter before attempting the outbound
	// call. With no network / invalid keys the call fails, but the
	// counter must still reflect that an attempt was made.
	_, _ = client.CreatePayment(
		context.Background(),
		CreatePaymentRequest{
			OrderID:  "order_001",
			Amount:   49800,
			Currency: "INR",
		},
	)

	if client.CallCount() != 1 {
		t.Fatalf(
			"expected call count 1 after one attempt, got %d",
			client.CallCount(),
		)
	}
}
