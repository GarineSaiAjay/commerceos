package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
)

// RazorpayWebhookPayload is the subset of a Razorpay webhook payload we
// consume in Phase 2.
type RazorpayWebhookPayload struct {
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity struct {
				ID        string `json:"id"`
				OrderID   string `json:"order_id"`
				Amount    int64  `json:"amount"`
				Currency  string `json:"currency"`
				ErrorCode string `json:"error_code"`
				ErrorDesc string `json:"error_description"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

// WebhookProcessor is the Phase 2 webhook pipeline:
//
//	Razorpay → Signature Verification → Event Dedup → Event Store
//	→ Order/Payment State Machine
//
// A duplicate delivery is a strict no-op: the state machine is never
// invoked for it.
type WebhookProcessor struct {
	verifier *WebhookSignatureVerifier
	store    WebhookEventStore
	applier  WebhookApplier
}

// WebhookApplier applies a verified, deduplicated webhook event to the
// order/payment state machines.
type WebhookApplier interface {
	ApplyCaptured(ctx context.Context, p RazorpayWebhookPayload) error
	ApplyFailed(ctx context.Context, p RazorpayWebhookPayload) error
}

func NewWebhookProcessor(
	verifier *WebhookSignatureVerifier,
	store WebhookEventStore,
	applier WebhookApplier,
) *WebhookProcessor {
	return &WebhookProcessor{
		verifier: verifier,
		store:    store,
		applier:  applier,
	}
}

// Process handles one webhook delivery. It returns:
//   - nil for a valid, processed (or duplicate) event
//   - ErrWebhookDuplicate for a repeat delivery (no-op)
//   - an error for an invalid signature or processing failure
func (p *WebhookProcessor) Process(
	ctx context.Context,
	body []byte,
	eventID string,
	signature string,
) error {
	// 1. Signature verification. A failure is a security event and must
	// never reach the state machine.
	if err := p.verifier.Verify(body, signature); err != nil {
		log.Printf(
			"[security] webhook signature verification failed: %v",
			err,
		)
		return fmt.Errorf("webhook signature verification failed: %w", err)
	}

	var payload RazorpayWebhookPayload

	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode webhook payload: %w", err)
	}

	// 2. Dedup + event store. A duplicate is a no-op — the state
	// machine is never invoked.
	if err := p.store.Store(
		ctx,
		eventID,
		payload.Event,
		json.RawMessage(body),
	); err != nil {
		if errors.Is(err, ErrWebhookEventDuplicate) {
			log.Printf(
				"[webhook] duplicate event %s ignored (no-op)",
				eventID,
			)
			return ErrWebhookEventDuplicate
		}

		return fmt.Errorf("store webhook event: %w", err)
	}

	// 3. Apply to the state machine.
	switch payload.Event {
	case "payment.captured":
		if err := p.applier.ApplyCaptured(ctx, payload); err != nil {
			return fmt.Errorf("apply payment.captured: %w", err)
		}

	case "payment.failed":
		if err := p.applier.ApplyFailed(ctx, payload); err != nil {
			return fmt.Errorf("apply payment.failed: %w", err)
		}

	default:
		log.Printf("[webhook] unhandled event type: %s", payload.Event)
	}

	return nil
}
