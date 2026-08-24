package payment

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
)

// WebhookHandler is the Phase 2 Razorpay webhook receiver.
//
// Pipeline: signature verification → dedup → event store → state machine.
// A forged signature is rejected as a security event and never reaches
// the state machine. A duplicate delivery is a strict no-op.
type WebhookHandler struct {
	processor *WebhookProcessor
}

func NewWebhookHandler(processor *WebhookProcessor) *WebhookHandler {
	return &WebhookHandler{processor: processor}
}

func (h *WebhookHandler) HandleRazorpay(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	eventID := r.Header.Get("x-razorpay-event-id")
	signature := r.Header.Get("x-razorpay-signature")

	err = h.processor.Process(
		r.Context(),
		body,
		eventID,
		signature,
	)

	if err != nil {
		// A duplicate delivery is a successful no-op at the HTTP layer.
		if errors.Is(err, ErrWebhookEventDuplicate) {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Signature failures and processing errors are not acknowledged
		// as success — Razorpay will retry, and the security log already
		// recorded the forgery attempt.
		log.Printf("[webhook] rejected: %v", err)
		http.Error(w, "webhook rejected", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

var _ = context.Background
