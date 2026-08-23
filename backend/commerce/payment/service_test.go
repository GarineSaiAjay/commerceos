package payment

import (
	"context"
	"testing"
)

type fakeProvider struct {
	lastRequest CreatePaymentRequest
}

func (p *fakeProvider) CreatePayment(
	ctx context.Context,
	req CreatePaymentRequest,
) (Payment, error) {
	p.lastRequest = req

	return Payment{
		ID:       "pay_test_001",
		OrderID:  req.OrderID,
		Amount:   req.Amount,
		Currency: req.Currency,
		Status:   "created",
	}, nil
}

func TestCreatePayment(t *testing.T) {
	provider := &fakeProvider{}
	service := NewService(provider, nil)

	payment, err := service.CreatePayment(
		context.Background(),
		"order_001",
		49800,
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	if payment.ID != "pay_test_001" {
		t.Fatalf(
			"expected payment ID pay_test_001, got %s",
			payment.ID,
		)
	}

	if payment.OrderID != "order_001" {
		t.Fatalf(
			"expected order ID order_001, got %s",
			payment.OrderID,
		)
	}

	if payment.Amount != 49800 {
		t.Fatalf(
			"expected amount 49800, got %d",
			payment.Amount,
		)
	}

	if payment.Currency != "INR" {
		t.Fatalf(
			"expected currency INR, got %s",
			payment.Currency,
		)
	}

	if payment.Status != "created" {
		t.Fatalf(
			"expected status created, got %s",
			payment.Status,
		)
	}

	if provider.lastRequest.OrderID != "order_001" {
		t.Fatalf("unexpected provider order ID")
	}

	if provider.lastRequest.Amount != 49800 {
		t.Fatalf("unexpected provider amount")
	}

	if provider.lastRequest.Currency != "INR" {
		t.Fatalf("unexpected provider currency")
	}
}

func (f *fakeProvider) VerifyPaymentSignature(
	razorpayOrderID string,
	razorpayPaymentID string,
	signature string,
) error {
	return nil
}
