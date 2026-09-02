package x402

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func testHandler(secret string) *Handler {
	facilitator := NewTestModeFacilitator(secret)
	return NewHandler(facilitator, DemoRequirements(), DemoResource)
}

func TestHandlerReturns402ChallengeWithNoPaymentHeader(t *testing.T) {
	h := testHandler("test-secret")

	req := httptest.NewRequest(http.MethodGet, "/x402/priority-support", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", rec.Code)
	}

	headerValue := rec.Header().Get(HeaderPaymentRequired)
	if headerValue == "" {
		t.Fatal("expected a PAYMENT-REQUIRED header on the 402 response")
	}

	challenge, err := DecodeHeader[Challenge](headerValue)
	if err != nil {
		t.Fatalf("decode PAYMENT-REQUIRED header: %v", err)
	}
	if len(challenge.Accepts) != 1 {
		t.Fatalf("expected exactly one accepted requirement, got %d", len(challenge.Accepts))
	}
	if challenge.Accepts[0] != DemoRequirements() {
		t.Fatalf("expected the challenge to offer DemoRequirements(), got %+v", challenge.Accepts[0])
	}
}

func TestHandlerServesResourceOnValidPayment(t *testing.T) {
	h := testHandler("test-secret")
	req := DemoRequirements()

	payload := Payload{
		X402Version: ProtocolVersion,
		Scheme:      req.Scheme,
		Network:     req.Network,
		Asset:       req.Asset,
		Amount:      req.Amount,
		Payer:       "0xpayer",
		Signature:   "test-secret",
	}
	sig, err := EncodeHeader(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodGet, "/x402/priority-support", nil)
	httpReq.Header.Set(HeaderPaymentSignature, sig)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a valid payment, got %d: %s", rec.Code, rec.Body.String())
	}

	settlementHeader := rec.Header().Get(HeaderPaymentResponse)
	if settlementHeader == "" {
		t.Fatal("expected a PAYMENT-RESPONSE header on a settled request")
	}

	settlement, err := DecodeHeader[Settlement](settlementHeader)
	if err != nil {
		t.Fatalf("decode PAYMENT-RESPONSE header: %v", err)
	}
	if !settlement.Success {
		t.Fatal("expected Settlement.Success to be true")
	}
	if settlement.Payer != "0xpayer" {
		t.Fatalf("expected settlement.Payer to echo the paying address, got %q", settlement.Payer)
	}
}

func TestHandlerReChallengesOnInvalidSignature(t *testing.T) {
	h := testHandler("test-secret")
	req := DemoRequirements()

	payload := Payload{
		X402Version: ProtocolVersion,
		Scheme:      req.Scheme,
		Network:     req.Network,
		Asset:       req.Asset,
		Amount:      req.Amount,
		Payer:       "0xpayer",
		Signature:   "wrong-secret",
	}
	sig, err := EncodeHeader(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodGet, "/x402/priority-support", nil)
	httpReq.Header.Set(HeaderPaymentSignature, sig)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402 for an invalid signature, got %d", rec.Code)
	}

	challenge, err := DecodeHeader[Challenge](rec.Header().Get(HeaderPaymentRequired))
	if err != nil {
		t.Fatalf("decode PAYMENT-REQUIRED header: %v", err)
	}
	if challenge.Error == "" {
		t.Fatal("expected the re-issued challenge to explain what was wrong with the attempt")
	}
}

func TestHandlerRejectsMalformedPaymentSignatureHeader(t *testing.T) {
	h := testHandler("test-secret")

	httpReq := httptest.NewRequest(http.MethodGet, "/x402/priority-support", nil)
	httpReq.Header.Set(HeaderPaymentSignature, "not valid base64!!!")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed PAYMENT-SIGNATURE header, got %d", rec.Code)
	}
}

func TestHandlerNeverCallsResourceForUnpaidRequest(t *testing.T) {
	facilitator := NewTestModeFacilitator("test-secret")
	called := false
	h := NewHandler(facilitator, DemoRequirements(), func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/x402/priority-support", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("expected the gated resource to never be called for an unpaid request")
	}
}
