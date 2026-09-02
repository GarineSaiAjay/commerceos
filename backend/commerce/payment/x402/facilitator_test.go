package x402

import "testing"

func testRequirements() Requirements {
	return Requirements{
		Scheme:            "exact",
		Network:           "base-sepolia",
		Asset:             "USDC",
		Amount:            "10000",
		PayTo:             "0xabc",
		MaxTimeoutSeconds: 60,
	}
}

func validPayloadFor(req Requirements, secret string) Payload {
	return Payload{
		X402Version: ProtocolVersion,
		Scheme:      req.Scheme,
		Network:     req.Network,
		Asset:       req.Asset,
		Amount:      req.Amount,
		Payer:       "0xpayer",
		Signature:   secret,
	}
}

func TestFacilitatorVerifyAcceptsMatchingPayload(t *testing.T) {
	f := NewTestModeFacilitator("test-secret")
	req := testRequirements()

	if err := f.Verify(validPayloadFor(req, "test-secret"), req); err != nil {
		t.Fatalf("expected a matching payload to verify, got: %v", err)
	}
}

func TestFacilitatorVerifyRejectsWrongSignature(t *testing.T) {
	f := NewTestModeFacilitator("test-secret")
	req := testRequirements()

	err := f.Verify(validPayloadFor(req, "wrong-secret"), req)
	if err == nil {
		t.Fatal("expected an error for a wrong signature")
	}
}

func TestFacilitatorVerifyRejectsAmountMismatch(t *testing.T) {
	f := NewTestModeFacilitator("test-secret")
	req := testRequirements()

	payload := validPayloadFor(req, "test-secret")
	payload.Amount = "1"

	err := f.Verify(payload, req)
	if err == nil {
		t.Fatal("expected an error for an amount mismatch")
	}
}

func TestFacilitatorVerifyRejectsAssetMismatch(t *testing.T) {
	f := NewTestModeFacilitator("test-secret")
	req := testRequirements()

	payload := validPayloadFor(req, "test-secret")
	payload.Asset = "ETH"

	err := f.Verify(payload, req)
	if err == nil {
		t.Fatal("expected an error for an asset mismatch")
	}
}

func TestFacilitatorVerifyRejectsMissingPayer(t *testing.T) {
	f := NewTestModeFacilitator("test-secret")
	req := testRequirements()

	payload := validPayloadFor(req, "test-secret")
	payload.Payer = ""

	err := f.Verify(payload, req)
	if err == nil {
		t.Fatal("expected an error for a missing payer")
	}
}

func TestFacilitatorVerifyRejectsEmptySharedSecret(t *testing.T) {
	// A facilitator with no configured secret must never treat an
	// empty Signature as valid -- that would make "no payment
	// configured" silently equivalent to "payment verified".
	f := NewTestModeFacilitator("")
	req := testRequirements()

	payload := validPayloadFor(req, "")

	err := f.Verify(payload, req)
	if err == nil {
		t.Fatal("expected an error when the facilitator has no shared secret configured")
	}
}
