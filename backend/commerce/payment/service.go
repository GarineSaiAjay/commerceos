package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/policy"
	"github.com/garinesaiajay/commerceos/statemachine"
)

type Service struct {
	provider   Provider
	repo       Repository
	attempts   AttemptRepository
	orders     OrderStatusTransitioner
	authorizer Authorizer
}

// Authorizer is the policy engine's verification surface. The payment
// service requires a valid authorization before any money movement.
type Authorizer interface {
	VerifyAuthorization(
		ctx context.Context,
		authorizationID string,
	) (policy.Authorization, error)
}

// AuthorizationConsumer marks an authorization as consumed AFTER a new
// payment was created, so the same authorization cannot be replayed to
// create a second payment. The idempotent-return path is unaffected.
type AuthorizationConsumer interface {
	MarkAuthorizationUsed(ctx context.Context, authorizationID string) error
}

// OrderStatusTransitioner is the subset of the order repository the
// payment service needs to mark orders paid on verified capture, and
// to tag an order with the agent run that authorized its payment
// (PLAN-05-SELLER-DASHBOARD.md §2's "Orders -> Runs audit-trail
// link") -- despite the name, this interface is really "the order
// writes payment cares about", not status alone; SetRunID was added
// here rather than as a second optional dependency because every
// concrete caller (main.go) already wires the same *order.
// PostgresRepository value in for both.
type OrderStatusTransitioner interface {
	TransitionStatus(
		ctx context.Context,
		orderID string,
		to string,
	) (order.Order, error)

	SetRunID(
		ctx context.Context,
		orderID string,
		runID string,
	) error
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

func NewServiceWithOrderTransitioner(
	provider Provider,
	repo Repository,
	attempts AttemptRepository,
	orders OrderStatusTransitioner,
) *Service {
	return &Service{
		provider: provider,
		repo:     repo,
		attempts: attempts,
		orders:   orders,
	}
}

func NewServiceWithAuthorizer(
	provider Provider,
	repo Repository,
	attempts AttemptRepository,
	orders OrderStatusTransitioner,
	authorizer Authorizer,
) *Service {
	return &Service{
		provider:   provider,
		repo:       repo,
		attempts:   attempts,
		orders:     orders,
		authorizer: authorizer,
	}
}

// GetPayment reads a payment by order ID (read-only).
func (s *Service) GetPayment(
	ctx context.Context,
	orderID string,
) (Payment, error) {
	return s.repo.GetByOrderID(ctx, orderID)
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
	idempotencyKey string,
	authorizationID string,
) (Payment, error) {
	// Hard chokepoint: no money movement without a valid authorization
	// issued by the Policy Engine.
	if s.authorizer == nil {
		return Payment{}, fmt.Errorf("payment service requires an authorizer")
	}

	auth, err := s.authorizer.VerifyAuthorization(
		ctx,
		authorizationID,
	)
	if err != nil {
		return Payment{}, fmt.Errorf("authorization required: %w", err)
	}

	// Idempotency: if a payment already exists for this idempotency
	// key, return it — never create a second Razorpay order.
	if idempotencyKey != "" {
		existing, err := s.repo.GetByIdempotencyKey(ctx, idempotencyKey)
		if err == nil {
			return existing, nil
		}

		if err != ErrPaymentNotFound {
			return Payment{}, err
		}
	}

	// Idempotency: if a payment already exists for this CommerceOS
	// order, return it instead of creating another Razorpay order.
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
	payment.IdempotencyKey = idempotencyKey

	// The Razorpay order now exists and awaits payment: the payment
	// moves from created -> pending (per the Phase 2 state machine).
	payment.Status = "pending"

	// Use a deterministic internal ID for now.
	payment.ID = fmt.Sprintf("payment_%s", ord.ID)

	if err := s.repo.Create(ctx, payment); err != nil {
		return Payment{}, err
	}

	// The authorization was genuinely consumed to create a NEW payment
	// (not an idempotent repeat), so mark it used — a single
	// authorization must not create a second payment.
	if consumer, ok := s.authorizer.(AuthorizationConsumer); ok {
		if err := consumer.MarkAuthorizationUsed(ctx, authorizationID); err != nil {
			return Payment{}, fmt.Errorf("mark authorization used: %w", err)
		}
	}

	// Tag the order with the run that authorized it (PLAN-05-SELLER-
	// DASHBOARD.md §2's "Orders -> Runs audit-trail link") -- best-
	// effort, same as the audit-write convention elsewhere (e.g.
	// policy/service.go's UpdatePolicyConfig): a failure here is a
	// missing convenience link on the dashboard, never a reason to
	// fail a payment that has already been authorized and created.
	// auth.ActionID can be empty for an authorization issued before
	// this field existed (a pre-migration row), which is silently
	// skipped rather than writing an empty run_id.
	if s.orders != nil && auth.ActionID != "" {
		if err := s.orders.SetRunID(ctx, ord.ID, auth.ActionID); err != nil {
			fmt.Printf("[payment] failed to tag order %s with run %s: %v\n", ord.ID, auth.ActionID, err)
		}
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
			IdempotencyKey:  idempotencyKey,
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

	// Guarded transitions: pending -> authorized -> captured (the
	// client-side signature verification is the capture proof in
	// Phase 2).
	if _, err := s.repo.TransitionStatus(
		ctx,
		orderID,
		statemachine.PaymentAuthorized,
		razorpayPaymentID,
	); err != nil {
		if errors.Is(err, statemachine.ErrIllegalTransition) {
			// Already captured/paid — return the existing payment.
			return payment, nil
		}

		return Payment{}, err
	}

	paidPayment, err := s.repo.TransitionStatus(
		ctx,
		orderID,
		statemachine.PaymentCaptured,
		razorpayPaymentID,
	)

	if errors.Is(err, statemachine.ErrIllegalTransition) {
		// Already captured/paid — return the existing payment.
		return payment, nil
	}

	if err != nil {
		return Payment{}, err
	}

	// The order becomes paid on verified capture, mirroring the
	// webhook path (Phase 2 state machine: payment_pending -> paid).
	// If the webhook already marked it paid, the guarded transition is
	// an illegal no-op, which we treat as already done.
	if s.orders != nil {
		_, orderErr := s.orders.TransitionStatus(
			ctx,
			orderID,
			statemachine.OrderPaid,
		)

		if orderErr != nil &&
			!errors.Is(orderErr, statemachine.ErrIllegalTransition) &&
			!errors.Is(orderErr, order.ErrOrderNotFound) {
			return Payment{}, orderErr
		}
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
