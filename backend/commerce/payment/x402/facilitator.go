package x402

import "fmt"

// TestModeFacilitator is a stand-in for a real x402 "facilitator" --
// the third party a resource server normally delegates payment
// verification and settlement to, which checks a signed transfer
// authorization against a real blockchain and reports back whether it
// settled. This one does neither: it accepts a Payload as valid
// payment if and only if its Scheme/Network/Asset/Amount exactly
// match the Requirements it's checked against AND its Signature
// equals the shared secret this facilitator was constructed with -- a
// deliberately trivial, offline, test-mode-only check. No chain is
// contacted and nothing settles anywhere real.
//
// This mirrors the posture backend/commerce/payment's real Razorpay
// integration already has for its own rail: files/demo-script.md's
// entire live demo runs on Razorpay Test Mode keys, never real money.
// TestModeFacilitator is that same "clearly labeled, no real money
// moves" posture applied to a rail this project cannot actually wire
// to a real facilitator or wallet within a buildathon-scoped stub.
type TestModeFacilitator struct {
	sharedSecret string
}

// NewTestModeFacilitator constructs a facilitator that treats
// sharedSecret as the one valid "signature" any payload can present.
// sharedSecret is not a real secret in the credential sense -- it is
// published in this package's own tests and README precisely so
// anyone can demo paying the challenge; treating it as sensitive would
// misrepresent what a test-mode-only stub is for.
func NewTestModeFacilitator(sharedSecret string) *TestModeFacilitator {
	return &TestModeFacilitator{sharedSecret: sharedSecret}
}

// Verify reports whether payload is a valid, test-mode payment against
// requirements. See the type doc comment for exactly what "valid"
// means here.
func (f *TestModeFacilitator) Verify(payload Payload, requirements Requirements) error {
	if payload.Scheme != requirements.Scheme {
		return fmt.Errorf("scheme mismatch: payload has %q, requires %q", payload.Scheme, requirements.Scheme)
	}
	if payload.Network != requirements.Network {
		return fmt.Errorf("network mismatch: payload has %q, requires %q", payload.Network, requirements.Network)
	}
	if payload.Asset != requirements.Asset {
		return fmt.Errorf("asset mismatch: payload has %q, requires %q", payload.Asset, requirements.Asset)
	}
	if payload.Amount != requirements.Amount {
		return fmt.Errorf("amount mismatch: payload has %q, requires %q", payload.Amount, requirements.Amount)
	}
	if payload.Payer == "" {
		return fmt.Errorf("payload missing payer")
	}
	if f.sharedSecret == "" || payload.Signature != f.sharedSecret {
		return fmt.Errorf("invalid payment signature")
	}

	return nil
}
