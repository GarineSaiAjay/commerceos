package payment

import (
	"context"
	"fmt"

	"github.com/garinesaiajay/commerceos/commerce/order"
)

type Service struct {
	provider Provider
	repo     Repository
	attempts AttemptRepository
}

func NewService(
	provider Provider,
	repo Repository,
) *Service {
	return &Service{
		provider: provider,
		repo:     repo,
	}
}

func NewServiceWithAttempts(
	provider Provider,
	repo Repository,
	attempts AttemptRepository,
) *Service {
	return &Service{
		provider: provider,
		repo:     repo,
		attempts: attempts,
	}
}

func (s *Service) CreatePayment(
	ctx context.Context,
	orderID string,
	amount int64,
	currency string,
) (Payment, error) {
	if orderID == "" {
		return Payment{}, fmt.Errorf("order ID is required")
	}

	if amount <= 0 {
		return Payment{}, fmt.Errorf(
			"payment amount must be greater than zero",
		)
	}

	if currency == "" {
		return Payment{}, fmt.Errorf("currency is required")
	}

	return s.provider.CreatePayment(
		ctx,
		CreatePaymentRequest{
			OrderID:  orderID,
			Amount:   amount,
			Currency: currency,
		},
	)
}

func (s *Service) CreatePaymentOrder(
	ctx context.Context,
	ord order.Order,
) (Payment, error) {
	// Idempotency:
	// If a payment already exists for this CommerceOS order,
	// return it instead of creating another Razorpay order.
	existing, err := s.repo.GetByOrderID(ctx, ord.ID)
	if err == nil {
		return existing, nil
	}

	if err != ErrPaymentNotFound {
		return Payment{}, err
	}

	// No payment exists yet, so create the Razorpay order.
	payment, err := s.CreatePayment(
		ctx,
		ord.ID,
		ord.Subtotal,
		ord.Currency,
	)
	if err != nil {
		return Payment{}, err
	}

	payment.Provider = "razorpay"
	payment.ProviderOrderID = payment.ID

	// Use a deterministic internal ID for now.
	payment.ID = fmt.Sprintf("payment_%s", ord.ID)

	if err := s.repo.Create(ctx, payment); err != nil {
		return Payment{}, err
	}

	// Record the initial payment attempt for this order.
	// Creating the Razorpay order is the start of the attempt, so it
	// begins in "attempted" state; verification later transitions it
	// to "paid" (or a failed flow marks it "failed").
	if s.attempts != nil {
		err := s.attempts.Create(ctx, PaymentAttempt{
			ID:              fmt.Sprintf("attempt_%s", ord.ID),
			PaymentID:       payment.ID,
			OrderID:         ord.ID,
			ProviderOrderID: payment.ProviderOrderID,
			Amount:          ord.Subtotal,
			Currency:        ord.Currency,
			Status:          "attempted",
		})

		if err != nil {
			return Payment{}, err
		}
	}

	return payment, nil
}

func (s *Service) VerifyPayment(
	ctx context.Context,
	orderID string,
	razorpayPaymentID string,
	razorpayOrderID string,
	razorpaySignature string,
) (Payment, error) {
	if orderID == "" {
		return Payment{}, fmt.Errorf("order ID is required")
	}

	if razorpayPaymentID == "" {
		return Payment{}, fmt.Errorf(
			"razorpay payment ID is required",
		)
	}

	if razorpayOrderID == "" {
		return Payment{}, fmt.Errorf(
			"razorpay order ID is required",
		)
	}

	if razorpaySignature == "" {
		return Payment{}, fmt.Errorf(
			"razorpay signature is required",
		)
	}

	// Verify that the Razorpay order ID belongs to the
	// payment record we created for this CommerceOS order.
	payment, err := s.repo.GetByOrderID(ctx, orderID)
	if err != nil {
		return Payment{}, err
	}

	if payment.Provider != "razorpay" {
		return Payment{}, fmt.Errorf(
			"unsupported payment provider: %s",
			payment.Provider,
		)
	}

	if payment.ProviderOrderID != razorpayOrderID {
		return Payment{}, fmt.Errorf(
			"razorpay order ID does not match payment",
		)
	}

	if err := s.provider.VerifyPaymentSignature(
		razorpayOrderID,
		razorpayPaymentID,
		razorpaySignature,
	); err != nil {
		return Payment{}, err
	}

	paidPayment, err := s.repo.MarkPaid(
		ctx,
		orderID,
		razorpayPaymentID,
	)

	if err == ErrPaymentAlreadyPaid {
		return paidPayment, nil
	}

	if err != nil {
		return Payment{}, err
	}

	// Record the successful attempt.
	if s.attempts != nil {
		_, _ = s.attempts.MarkPaid(
			ctx,
			orderID,
			razorpayPaymentID,
		)
	}

	return paidPayment, nil
}
