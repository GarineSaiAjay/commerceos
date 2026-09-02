// Package x402 is a minimal, test-mode-only implementation of the
// HTTP 402 challenge/response handshake described by the x402
// payment protocol (https://x402.org, https://github.com/coinbase/x402)
// -- item 39, P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §1: "A minimal
// X402Adapter (test-mode only, handling the HTTP 402 challenge/
// response handshake for a single fixed scenario) is a concrete,
// demoable artifact that speaks directly to the track's 'why now'
// framing (x402 named explicitly in the brief). Scope it small: one
// code path, one demo scenario, not a general x402 client."
//
// Wire-format honesty note: x402's own public documentation was
// inconsistent across the sources checked while building this
// (September 2026). docs.x402.org and Cloudflare's x402 docs both
// independently describe a header-based handshake -- a 402 response
// carrying a base64-encoded challenge in a PAYMENT-REQUIRED header,
// a client retry carrying its payment proof in a PAYMENT-SIGNATURE
// header, and a settled response carrying confirmation in a
// PAYMENT-RESPONSE header -- which is the shape this package
// implements, since those two sources were independent and agreed.
// Other material (including the protocol's own earlier write-ups)
// describes a different, older shape: the challenge in the response
// BODY as a JSON object with an "accepts" array, and an X-PAYMENT
// request header instead of PAYMENT-SIGNATURE. This package makes NO
// claim to be a certified, spec-exact, wire-compatible x402 client or
// facilitator, for either shape -- it exists to demonstrate the
// mechanism (a resource server issuing a priced 402 challenge and
// only serving the resource once a matching payment proof comes
// back), not to interoperate with a real x402 facilitator or settle a
// real on-chain payment. See TestModeFacilitator's own doc comment
// for exactly what "verification" means here, and Handler's for why
// this is wired as one standalone demo route rather than into this
// project's real checkout/policy/audit pipeline.
package x402

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Header names this package reads and writes. See the package doc
// comment above for the wire-format ambiguity across x402's own
// documentation and why these three (not X-PAYMENT/a JSON response
// body) were chosen.
const (
	HeaderPaymentRequired  = "PAYMENT-REQUIRED"
	HeaderPaymentSignature = "PAYMENT-SIGNATURE"
	HeaderPaymentResponse  = "PAYMENT-RESPONSE"
)

// ProtocolVersion is the x402Version this stub claims. 1, not because
// it targets any specific numbered spec revision with confidence (see
// the package doc comment), but because it is this stub's own first
// and only version.
const ProtocolVersion = 1

// Requirements is what a resource server demands before it will serve
// a paywalled resource -- one entry of a Challenge's Accepts list.
// Field names/shape follow the closest published approximation found
// (a Go PaymentRequirements type referenced from x402's own tooling)
// for scheme/network/asset/amount/payTo/maxTimeoutSeconds; Resource
// and Description are this package's own addition for a
// self-describing challenge, not a claim they're spec-mandated.
type Requirements struct {
	Scheme            string `json:"scheme"`
	Network           string `json:"network"`
	Asset             string `json:"asset"`
	Amount            string `json:"amount"`
	PayTo             string `json:"payTo"`
	Resource          string `json:"resource"`
	Description       string `json:"description,omitempty"`
	MaxTimeoutSeconds int    `json:"maxTimeoutSeconds"`
}

// Challenge is the full PAYMENT-REQUIRED payload. The real protocol
// allows several simultaneously-acceptable schemes/networks in
// Accepts; this stub always offers exactly one, matching its "one
// fixed scenario" scope.
type Challenge struct {
	X402Version int            `json:"x402Version"`
	Error       string         `json:"error,omitempty"`
	Accepts     []Requirements `json:"accepts"`
}

// Payload is what a paying client sends back in the PAYMENT-SIGNATURE
// header -- a claimed payment against one of a Challenge's Accepts
// entries. A real x402 payload carries a chain-specific signed
// transfer authorization (e.g. an EIP-3009 transferWithAuthorization
// for a stablecoin) that a facilitator verifies against a real chain.
// This stub's Signature field is a shared-secret string checked by
// TestModeFacilitator -- NOT a real cryptographic signature over a
// real transfer, and nothing here contacts any blockchain.
type Payload struct {
	X402Version int    `json:"x402Version"`
	Scheme      string `json:"scheme"`
	Network     string `json:"network"`
	Asset       string `json:"asset"`
	Amount      string `json:"amount"`
	Payer       string `json:"payer"`
	Signature   string `json:"signature"`
}

// Settlement is the PAYMENT-RESPONSE payload confirming the resource
// server accepted the payment.
type Settlement struct {
	Success   bool   `json:"success"`
	Network   string `json:"network"`
	Payer     string `json:"payer"`
	SettledAt string `json:"settledAt"`
}

// EncodeHeader base64-encodes v as JSON, the way every PAYMENT-*
// header in this stub carries its payload.
func EncodeHeader(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("x402: encode header payload: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// DecodeHeader reverses EncodeHeader into a typed value.
func DecodeHeader[T any](header string) (T, error) {
	var out T

	raw, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		return out, fmt.Errorf("x402: decode header payload: %w", err)
	}

	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("x402: unmarshal header payload: %w", err)
	}

	return out, nil
}
