package payment

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/garinesaiajay/commerceos/statemachine"
)

// ErrWebhookAmountMismatch is returned when a Razorpay webhook reports
// a captured amount/currency that doesn't match the payment record we
// created for this order (full-codebase re-audit, P2). This is not
// expected to fire in normal operation -- Razorpay orders are created
// amount-locked (razorpay.go), so a captured payment should always
// report back exactly what we asked for -- but a captured amount is
// real money movement, so it must never be accepted (and the order
// never marked paid) on data we can't reconcile against what we
// expected, whatever the cause.
var ErrWebhookAmountMismatch = errors.New("webhook reported amount/currency does not match the payment we created")

// WebhookApplierImpl applies verified, deduplicated webhook events to
// the order and payment state machines, and records audit + outbox
// events for each transition.
type WebhookApplierImpl struct {
	payments Repository
	orders   OrderStatusTransitioner
	audit    AuditWriter
	outbox   OutboxWriter
	attempts AttemptRepository
}

// AuditWriter appends an immutable audit record.
type AuditWriter interface {
	Write(
		ctx context.Context,
		actor string,
		action string,
		entityType string,
		entityID string,
		detail map[string]any,
	) error
}

// OutboxWriter inserts a domain event into the outbox.
type OutboxWriter interface {
	Insert(
		ctx context.Context,
		eventType string,
		payload any,
	) (int64, error)
}

func NewWebhookApplier(
	payments Repository,
	orders OrderStatusTransitioner,
	audit AuditWriter,
	outbox OutboxWriter,
) *WebhookApplierImpl {
	return &WebhookApplierImpl{
		payments: payments,
		orders:   orders,
		audit:    audit,
		outbox:   outbox,
	}
}

// WithAttempts attaches the payment-attempt repository so the applier
// can record the failed attempt when a payment fails.
func (a *WebhookApplierImpl) WithAttempts(attempts AttemptRepository) *WebhookApplierImpl {
	a.attempts = attempts
	return a
}

// ApplyCaptured handles payment.captured: the payment moves
// pending -> authorized -> captured and the order moves
// payment_pending -> paid.
func (a *WebhookApplierImpl) ApplyCaptured(
	ctx context.Context,
	p RazorpayWebhookPayload,
) error {
	paymentEntity := p.Payload.Payment.Entity
	// paymentEntity.OrderID is Razorpay's OWN order ID
	// (payload.payment.entity.order_id -- e.g. "order_..."), not
	// commerceos's order ID: razorpay.go only ever sends commerceos's
	// order ID to Razorpay as the opaque "receipt" field when creating
	// the order, never as the order_id itself (see
	// Repository.GetByProviderOrderID's doc comment). Full-codebase
	// re-audit (P2) found this file previously used GetByOrderID here
	// -- looking payment.captured/payment.failed webhooks up by
	// commerceos's own order_id column against a value that is never
	// in that column -- which meant a real Razorpay webhook could never
	// match an existing payment and this whole path silently no-op'd
	// (ErrPaymentNotFound below) on every genuine delivery. The
	// client-side VerifyPayment browser callback was therefore the
	// only path that had ever actually confirmed a payment; this
	// webhook confirmation channel -- the defense-in-depth path for a
	// buyer who closes the tab before that callback fires -- had never
	// worked. GetByProviderOrderID resolves the payment (and, via
	// existing.OrderID below, commerceos's own order ID) correctly.
	//
	// A capture for a payment we do not track (e.g. a dashboard test)
	// is a no-op rather than a rejection.
	existing, err := a.payments.GetByProviderOrderID(ctx, paymentEntity.OrderID)
	if err != nil {
		if errors.Is(err, ErrPaymentNotFound) {
			return nil
		}
		return fmt.Errorf("get payment: %w", err)
	}

	// Cross-check the captured amount/currency Razorpay reports against
	// what we created this payment for (full-codebase re-audit, P2).
	// Razorpay orders are amount-locked at creation
	// (razorpay.go's CreateOrder), so this should never actually
	// mismatch -- but a captured payment is real money movement, and
	// silently trusting webhook-reported figures with no cross-check
	// against our own record would let a mismatch (a Razorpay-side bug,
	// a manually captured different amount via their dashboard, or
	// corrupted/replayed webhook data) mark an order paid for the wrong
	// amount with no trace. Reject outright rather than accept it: the
	// order is left in payment_pending (the buyer's own VerifyPayment
	// callback, or an operator investigating the failed webhook, is the
	// recovery path), not silently marked paid on unreconciled data.
	if paymentEntity.Currency != existing.Currency || paymentEntity.Amount != existing.Amount {
		return fmt.Errorf(
			"%w: webhook reports %d %s for payment %s, expected %d %s",
			ErrWebhookAmountMismatch,
			paymentEntity.Amount, paymentEntity.Currency,
			existing.ID,
			existing.Amount, existing.Currency,
		)
	}

	if existing.Status == statemachine.PaymentCaptured || existing.Status == statemachine.PaymentCompleted {
		return nil
	}

	// Payment: pending -> authorized -> captured.
	payment, err := a.payments.TransitionStatus(
		ctx,
		existing.OrderID,
		statemachine.PaymentAuthorized,
		paymentEntity.ID,
	)
	if err != nil {
		return fmt.Errorf("transition payment to authorized: %w", err)
	}

	payment, err = a.payments.TransitionStatus(
		ctx,
		existing.OrderID,
		statemachine.PaymentCaptured,
		paymentEntity.ID,
	)
	if err != nil {
		return fmt.Errorf("transition payment to captured: %w", err)
	}

	// Order: payment_pending -> paid.
	if _, err := a.orders.TransitionStatus(
		ctx,
		existing.OrderID,
		statemachine.OrderPaid,
	); err != nil {
		return fmt.Errorf("transition order to paid: %w", err)
	}

	// Mark the attempt paid so the payment_attempts row is consistent
	// with the payment/order state (mirrors the client-verify path).
	if a.attempts != nil {
		_, _ = a.attempts.MarkPaid(ctx, existing.OrderID, paymentEntity.ID)
	}

	if a.audit != nil {
		// Best-effort: a failed audit write must not fail the webhook
		// apply (the payment/order state transition above already
		// committed), but it was previously discarded silently -- an
		// audit-log outage would leave no trace anywhere. Log it.
		if err := a.audit.Write(
			ctx,
			"razorpay_webhook",
			"payment.captured",
			"payment",
			payment.ID,
			map[string]any{
				"order_id":            existing.OrderID,
				"razorpay_order_id":   paymentEntity.OrderID,
				"razorpay_payment_id": paymentEntity.ID,
				"amount":              paymentEntity.Amount,
				"currency":            paymentEntity.Currency,
			},
		); err != nil {
			log.Printf("[webhook] audit write failed for payment.captured (payment %s): %v", payment.ID, err)
		}
	}

	if a.outbox != nil {
		_, _ = a.outbox.Insert(ctx, "payment.captured", map[string]any{
			"order_id":            existing.OrderID,
			"razorpay_order_id":   paymentEntity.OrderID,
			"razorpay_payment_id": paymentEntity.ID,
			"amount":              paymentEntity.Amount,
			"currency":            paymentEntity.Currency,
		})
	}

	return nil
}

// ApplyFailed handles payment.failed: the payment moves
// pending -> failed. The order stays payment_pending (Phase 2 keeps the
// cart held under its reservation TTL for recovery).
func (a *WebhookApplierImpl) ApplyFailed(
	ctx context.Context,
	p RazorpayWebhookPayload,
) error {
	paymentEntity := p.Payload.Payment.Entity

	// paymentEntity.OrderID is Razorpay's own order ID, not commerceos's
	// -- see ApplyCaptured's doc comment for the full explanation and
	// why GetByProviderOrderID (not GetByOrderID) is the correct lookup
	// here too.
	//
	// A repeated payment.failed delivery (or a failed event arriving
	// after the payment was already marked failed) is a no-op.
	existing, err := a.payments.GetByProviderOrderID(ctx, paymentEntity.OrderID)
	if err != nil {
		return fmt.Errorf("get payment: %w", err)
	}
	if existing.Status == statemachine.PaymentFailed {
		return nil
	}

	payment, err := a.payments.TransitionStatus(
		ctx,
		existing.OrderID,
		statemachine.PaymentFailed,
		paymentEntity.ID,
	)
	if err != nil {
		return fmt.Errorf("transition payment to failed: %w", err)
	}

	if a.attempts != nil {
		_, _ = a.attempts.MarkFailed(
			ctx,
			existing.OrderID,
			paymentEntity.ID,
			paymentEntity.ErrorCode,
			paymentEntity.ErrorDesc,
		)
	}

	if a.audit != nil {
		if err := a.audit.Write(
			ctx,
			"razorpay_webhook",
			"payment.failed",
			"payment",
			payment.ID,
			map[string]any{
				"order_id":            existing.OrderID,
				"razorpay_order_id":   paymentEntity.OrderID,
				"razorpay_payment_id": paymentEntity.ID,
				"amount":              paymentEntity.Amount,
				"currency":            paymentEntity.Currency,
				"error_code":          paymentEntity.ErrorCode,
				"error_description":   paymentEntity.ErrorDesc,
			},
		); err != nil {
			log.Printf("[webhook] audit write failed for payment.failed (payment %s): %v", payment.ID, err)
		}
	}

	if a.outbox != nil {
		_, _ = a.outbox.Insert(ctx, "payment.failed", map[string]any{
			"order_id":            existing.OrderID,
			"razorpay_order_id":   paymentEntity.OrderID,
			"razorpay_payment_id": paymentEntity.ID,
			"amount":              paymentEntity.Amount,
			"currency":            paymentEntity.Currency,
			"error_code":          paymentEntity.ErrorCode,
			"error_description":   paymentEntity.ErrorDesc,
		})
	}

	return nil
}
