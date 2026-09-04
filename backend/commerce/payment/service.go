package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/garinesaiajay/commerceos/commerce/order"
	"github.com/garinesaiajay/commerceos/policy"
	"github.com/garinesaiajay/commerceos/statemachine"
)

// ErrAuthorizationMismatch means a caller-supplied authorization_id was
// verified ACTIVE and unexpired (policy.Service.VerifyAuthorization) but
// does not actually cover the order being paid -- a different amount,
// currency, merchant, or cart than what was authorized. Added as a P0
// security fix (full-codebase re-audit 2026-09-04): before this,
// CreatePaymentOrder trusted VerifyAuthorization's ACTIVE/unexpired check
// alone, which let a cheap, auto-approved authorization for a trivial
// amount be spent against an arbitrarily expensive order -- the mandate
// ceiling and budget-tolerance checks that ran against the cheap proposal
// never applied to the real charge. See CreatePaymentOrder's doc comment.
// The word "authorization" in this error's text matters: handler.go maps
// any error containing it to 401, same as an invalid/expired authorization.
var ErrAuthorizationMismatch = errors.New("authorization does not match this order")

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

// AuthorizationConsumer atomically marks an authorization as consumed.
// CreatePaymentOrder calls this BEFORE creating a new payment (moved
// here as part of the P0 fix, full-codebase re-audit 2026-09-04 -- see
// CreatePaymentOrder's inline comment at the call site for why), so the
// same authorization cannot be replayed -- concurrently or sequentially
// -- to create a second payment. The idempotent-return path (an
// existing payment for this idempotency key or order) is unaffected: it
// returns before this is ever called.
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

	// The authorization being ACTIVE and unexpired (just verified above)
	// is not by itself proof it was ever meant to pay for THIS order --
	// see ErrAuthorizationMismatch's doc comment for why this matters.
	// Currency and merchant must match exactly. Amount is allowed to be
	// authorized-for-more-than-charged (never the reverse): ord.Subtotal
	// is already net of any campaign discount applied at checkout
	// (order.Order.Subtotal's doc comment), which can only ever reduce
	// what's actually charged below what was authorized, never increase
	// it. cart_id must match exactly and must be present on the
	// authorization -- an order always has a real cart_id (it is created
	// FROM a cart), so an authorization with none (a cart-less
	// "generalized" proposal, see Engine.checkMandateCartBound) can never
	// legitimately be the one that paid for it.
	if auth.Currency != ord.Currency {
		return Payment{}, fmt.Errorf("%w: authorization currency %s does not match order currency %s", ErrAuthorizationMismatch, auth.Currency, ord.Currency)
	}
	if auth.Merchant != ord.MerchantID {
		return Payment{}, fmt.Errorf("%w: authorization merchant %s does not match order merchant %s", ErrAuthorizationMismatch, auth.Merchant, ord.MerchantID)
	}
	if auth.Amount < ord.Subtotal {
		return Payment{}, fmt.Errorf("%w: authorization covers %d but order subtotal is %d", ErrAuthorizationMismatch, auth.Amount, ord.Subtotal)
	}
	if auth.CartID == "" || auth.CartID != ord.CartID {
		return Payment{}, fmt.Errorf("%w: authorization is not bound to order %s's cart", ErrAuthorizationMismatch, ord.ID)
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

	// This is a genuine new spend (not an idempotent repeat, both checks
	// above having found nothing), so consume the authorization NOW --
	// atomically, and BEFORE calling out to the payment provider or
	// creating any Payment row. This ordering is itself part of the P0
	// fix (full-codebase re-audit 2026-09-04): consuming AFTER payment
	// creation (the previous order) left a window where two concurrent
	// CreatePaymentOrder calls racing the same authorization_id could
	// both pass every check above and both create a payment before
	// either got around to marking the authorization used -- one
	// authorization funding two payments. MarkAuthorizationUsed's
	// UPDATE ... WHERE status = 'ACTIVE' is now the sole atomic
	// arbiter: only one concurrent caller can win it, and the loser is
	// rejected here, before any payment record exists and before the
	// payment provider is ever called. The trade-off this accepts: if
	// the provider call or DB insert below fails AFTER a successful
	// consume, this authorization is burned with no payment to show for
	// it. That is an acceptable, cheap-to-recover-from cost (the caller
	// just requests a fresh authorization; they expire in 10 minutes
	// regardless) next to the alternative of a double-spend.
	if consumer, ok := s.authorizer.(AuthorizationConsumer); ok {
		if err := consumer.MarkAuthorizationUsed(ctx, authorizationID); err != nil {
			return Payment{}, fmt.Errorf("mark authorization used: %w", err)
		}
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
