package payment

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/garinesaiajay/commerceos/statemachine"
)

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
	// Client-side verification may have already captured this payment before
	// Razorpay's (at-least-once) webhook arrives. A repeated capture is a no-op,
	// and a capture for a payment we do not track (e.g. a dashboard test)
	// is also a no-op rather than a rejection.
	existing, err := a.payments.GetByOrderID(ctx, paymentEntity.OrderID)
	if err != nil {
		if errors.Is(err, ErrPaymentNotFound) {
			return nil
		}
		return fmt.Errorf("get payment: %w", err)
	}
	if existing.Status == statemachine.PaymentCaptured || existing.Status == statemachine.PaymentCompleted {
		return nil
	}

	// Payment: pending -> authorized -> captured.
	payment, err := a.payments.TransitionStatus(
		ctx,
		paymentEntity.OrderID,
		statemachine.PaymentAuthorized,
		paymentEntity.ID,
	)
	if err != nil {
		return fmt.Errorf("transition payment to authorized: %w", err)
	}

	payment, err = a.payments.TransitionStatus(
		ctx,
		paymentEntity.OrderID,
		statemachine.PaymentCaptured,
		paymentEntity.ID,
	)
	if err != nil {
		return fmt.Errorf("transition payment to captured: %w", err)
	}

	// Order: payment_pending -> paid.
	if _, err := a.orders.TransitionStatus(
		ctx,
		paymentEntity.OrderID,
		statemachine.OrderPaid,
	); err != nil {
		return fmt.Errorf("transition order to paid: %w", err)
	}

	// Mark the attempt paid so the payment_attempts row is consistent
	// with the payment/order state (mirrors the client-verify path).
	if a.attempts != nil {
		_, _ = a.attempts.MarkPaid(ctx, paymentEntity.OrderID, paymentEntity.ID)
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
				"order_id":            paymentEntity.OrderID,
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
			"order_id":            paymentEntity.OrderID,
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

	// A repeated payment.failed delivery (or a failed event arriving
	// after the payment was already marked failed) is a no-op.
	existing, err := a.payments.GetByOrderID(ctx, paymentEntity.OrderID)
	if err != nil {
		return fmt.Errorf("get payment: %w", err)
	}
	if existing.Status == statemachine.PaymentFailed {
		return nil
	}

	payment, err := a.payments.TransitionStatus(
		ctx,
		paymentEntity.OrderID,
		statemachine.PaymentFailed,
		paymentEntity.ID,
	)
	if err != nil {
		return fmt.Errorf("transition payment to failed: %w", err)
	}

	if a.attempts != nil {
		_, _ = a.attempts.MarkFailed(
			ctx,
			paymentEntity.OrderID,
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
				"order_id":            paymentEntity.OrderID,
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
			"order_id":            paymentEntity.OrderID,
			"razorpay_payment_id": paymentEntity.ID,
			"amount":              paymentEntity.Amount,
			"currency":            paymentEntity.Currency,
			"error_code":          paymentEntity.ErrorCode,
			"error_description":   paymentEntity.ErrorDesc,
		})
	}

	return nil
}
