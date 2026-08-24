package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeStore records stored events and can simulate duplicates.
type fakeStore struct {
	events    map[string]bool
	duplicate bool
}

func (s *fakeStore) Store(
	ctx context.Context,
	eventID string,
	eventType string,
	payload json.RawMessage,
) error {
	if s.events == nil {
		s.events = map[string]bool{}
	}

	if s.events[eventID] {
		return ErrWebhookEventDuplicate
	}

	s.events[eventID] = true

	return nil
}

// fakeApplier records applied events.
type fakeApplier struct {
	captured int
	failed   int
}

func (a *fakeApplier) ApplyCaptured(
	ctx context.Context,
	p RazorpayWebhookPayload,
) error {
	a.captured++
	return nil
}

func (a *fakeApplier) ApplyFailed(
	ctx context.Context,
	p RazorpayWebhookPayload,
) error {
	a.failed++
	return nil
}

func newTestProcessor(
	secret string,
	store WebhookEventStore,
	applier WebhookApplier,
) *WebhookProcessor {
	return NewWebhookProcessor(
		NewWebhookSignatureVerifier(secret),
		store,
		applier,
	)
}

func TestWebhookHandlerAcceptsCaptured(t *testing.T) {
	store := &fakeStore{}
	applier := &fakeApplier{}
	processor := newTestProcessor("test_secret", store, applier)
	handler := NewWebhookHandler(processor)

	body := []byte(`{"event":"payment.captured"}`)
	sig := signBody("test_secret", body)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/razorpay",
		bytes.NewReader(body),
	)
	req.Header.Set("x-razorpay-event-id", "evt_001")
	req.Header.Set("x-razorpay-signature", sig)
	rec := httptest.NewRecorder()

	handler.HandleRazorpay(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestWebhookHandlerRejectsForgedSignature(t *testing.T) {
	store := &fakeStore{}
	applier := &fakeApplier{}
	processor := newTestProcessor("test_secret", store, applier)
	handler := NewWebhookHandler(processor)

	body := []byte(`{"event":"payment.captured"}`)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/razorpay",
		bytes.NewReader(body),
	)
	req.Header.Set("x-razorpay-event-id", "evt_forged")
	req.Header.Set("x-razorpay-signature", "forged_signature_123")
	rec := httptest.NewRecorder()

	handler.HandleRazorpay(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf(
			"expected status %d for forged signature, got %d",
			http.StatusBadRequest,
			rec.Code,
		)
	}

	// The state machine must never be invoked.
	if applier.captured != 0 || applier.failed != 0 {
		t.Fatalf(
			"state machine invoked for forged signature: captured=%d failed=%d",
			applier.captured,
			applier.failed,
		)
	}

	// The event must not be stored.
	if len(store.events) != 0 {
		t.Fatalf("forged event was stored: %v", store.events)
	}
}

func TestWebhookHandlerRejectsMissingSignature(t *testing.T) {
	store := &fakeStore{}
	applier := &fakeApplier{}
	processor := newTestProcessor("test_secret", store, applier)
	handler := NewWebhookHandler(processor)

	body := []byte(`{"event":"payment.captured"}`)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/razorpay",
		bytes.NewReader(body),
	)
	req.Header.Set("x-razorpay-event-id", "evt_nosig")
	rec := httptest.NewRecorder()

	handler.HandleRazorpay(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf(
			"expected status %d for missing signature, got %d",
			http.StatusBadRequest,
			rec.Code,
		)
	}
}

func TestWebhookDuplicateIsNoOp(t *testing.T) {
	store := &fakeStore{}
	applier := &fakeApplier{}
	processor := newTestProcessor("test_secret", store, applier)
	handler := NewWebhookHandler(processor)

	body := []byte(`{"event":"payment.captured"}`)
	sig := signBody("test_secret", body)

	send := func() int {
		req := httptest.NewRequest(
			http.MethodPost,
			"/webhooks/razorpay",
			bytes.NewReader(body),
		)
		req.Header.Set("x-razorpay-event-id", "evt_dup")
		req.Header.Set("x-razorpay-signature", sig)
		rec := httptest.NewRecorder()

		handler.HandleRazorpay(rec, req)

		return rec.Code
	}

	if code := send(); code != http.StatusOK {
		t.Fatalf("first delivery: expected 200, got %d", code)
	}

	if code := send(); code != http.StatusOK {
		t.Fatalf("duplicate delivery: expected 200 (no-op), got %d", code)
	}

	// Exactly one state transition.
	if applier.captured != 1 {
		t.Fatalf(
			"expected exactly 1 state transition, got %d",
			applier.captured,
		)
	}
}

func TestWebhookHandlerRejectsInvalidJSON(t *testing.T) {
	store := &fakeStore{}
	applier := &fakeApplier{}
	processor := newTestProcessor("test_secret", store, applier)
	handler := NewWebhookHandler(processor)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/razorpay",
		bytes.NewReader([]byte(`{not json`)),
	)
	rec := httptest.NewRecorder()

	handler.HandleRazorpay(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestWebhookHandlerRejectsNonPost(t *testing.T) {
	store := &fakeStore{}
	applier := &fakeApplier{}
	processor := newTestProcessor("test_secret", store, applier)
	handler := NewWebhookHandler(processor)

	req := httptest.NewRequest(
		http.MethodGet,
		"/webhooks/razorpay",
		nil,
	)
	rec := httptest.NewRecorder()

	handler.HandleRazorpay(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusMethodNotAllowed,
			rec.Code,
		)
	}
}

var _ = errors.New

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
