package payment

import (
	"context"
	"fmt"
)

type FakeProvider struct{}

func NewFakeProvider() *FakeProvider {
	return &FakeProvider{}
}

func (p *FakeProvider) CreatePayment(
	ctx context.Context,
	req CreatePaymentRequest,
) (Payment, error) {
	return Payment{
		ID:       fmt.Sprintf("pay_%s", req.OrderID),
		OrderID:  req.OrderID,
		Amount:   req.Amount,
		Currency: req.Currency,
		Status:   "created",
	}, nil
}
