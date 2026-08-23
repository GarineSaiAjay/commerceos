package payment

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// WebhookHandler is the Phase 1 basic Razorpay webhook receiver.
//
// It only logs inbound events — signature verification and
// deduplication are Phase 2 work. Phase 2 upgrades this endpoint;
// it does not create it from scratch.
type WebhookHandler struct{}

func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{}
}

type razorpayWebhookEvent struct {
	Event string `json:"event"`
}

func (h *WebhookHandler) HandleRazorpay(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var ev razorpayWebhookEvent

	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "invalid webhook payload", http.StatusBadRequest)
		return
	}

	// Phase 1: log only. Success and failure are logged distinctly so
	// the end-to-end manual test can confirm both appear correctly.
	switch ev.Event {
	case "payment.captured":
		fmt.Println("[webhook] payment.captured — payment succeeded")

	case "payment.failed":
		fmt.Println("[webhook] payment.failed — payment failed")

	case "order.paid":
		fmt.Println("[webhook] order.paid — order paid")

	default:
		fmt.Printf("[webhook] unhandled event: %s\n", ev.Event)
	}

	w.WriteHeader(http.StatusOK)
}
