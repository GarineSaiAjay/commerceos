package payment

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookHandlerAcceptsCaptured(t *testing.T) {
	handler := NewWebhookHandler()

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/razorpay",
		bytes.NewReader([]byte(`{"event":"payment.captured"}`)),
	)
	rec := httptest.NewRecorder()

	handler.HandleRazorpay(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestWebhookHandlerAcceptsFailed(t *testing.T) {
	handler := NewWebhookHandler()

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/razorpay",
		bytes.NewReader([]byte(`{"event":"payment.failed"}`)),
	)
	rec := httptest.NewRecorder()

	handler.HandleRazorpay(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestWebhookHandlerRejectsInvalidJSON(t *testing.T) {
	handler := NewWebhookHandler()

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
	handler := NewWebhookHandler()

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
