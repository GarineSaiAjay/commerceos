# x402 payment-adapter stub (item 39, P3)

`PLAN-06-ADDITIONAL-OPPORTUNITIES.md` §1: *"`backend/commerce/payment/
adapter.go`'s `Adapter` interface was already designed for this... A
minimal `X402Adapter` (test-mode only, handling the HTTP 402
challenge/response handshake for a single fixed scenario) is a
concrete, demoable artifact that speaks directly to the track's 'why
now' framing (x402 named explicitly in the brief). Scope it small: one
code path, one demo scenario, not a general x402 client."*

## Why this isn't `payment.PaymentAdapter`

The plan's own framing assumes x402 slots into the existing
`payment.PaymentAdapter`/`Provider` interfaces
(`CreatePayment`/`VerifyPaymentSignature`) the way a second card
processor would. On closer look, it doesn't, and pretending otherwise
would misrepresent both x402 and this project's own architecture:

- Razorpay's flow is **merchant-initiated** -- this backend calls
  `CreatePayment` to open an order, the buyer's browser completes it,
  and a webhook (or client-side signature) confirms it after the fact.
- x402's flow is **resource-initiated** -- a client requests something,
  the *server* replies `402 Payment Required` with a priced challenge,
  and only serves the resource once a matching payment proof comes
  back on retry. There's no order for a merchant to "create" first.

Forcing x402 through `CreatePayment`/`VerifyPaymentSignature` would
require inventing a fictional mapping between two structurally
different flows. Instead, this stub implements x402's actual mechanism
standalone (`Handler` in this package), gating one fixed demo
resource, completely separate from the real Razorpay-backed
checkout/policy/audit pipeline. That's the honest scope for a
buildathon-sized stub -- see "What this deliberately does not do"
below for what a real integration would additionally need.

## Wire-format honesty note

x402's own public documentation was inconsistent across the sources
checked while building this (September 2026). `docs.x402.org` and
Cloudflare's x402 docs both independently describe a header-based
handshake -- `PAYMENT-REQUIRED` / `PAYMENT-SIGNATURE` / `PAYMENT-
RESPONSE` headers carrying base64-encoded JSON -- which is what this
package implements, since those two sources were independent and
agreed with each other. Other material (including the protocol's own
earlier write-ups) describes a different, older shape: the challenge
in the response **body** as JSON with an `accepts` array, and an
`X-PAYMENT` request header instead of `PAYMENT-SIGNATURE`. This
package makes no claim to be a certified, spec-exact, wire-compatible
x402 client or facilitator for either shape -- see `protocol.go`'s
package doc comment for the full citation trail.

## What this actually does

One fixed scenario, `GET /x402/priority-support`:

1. First request, no payment offered → `402 Payment Required` with a
   `PAYMENT-REQUIRED` header: a base64-encoded JSON `Challenge`
   listing one accepted `Requirements` (a $0.01-shaped USDC-on-
   Base-Sepolia demo price -- not wired to any real chain or wallet).
2. Retry with a `PAYMENT-SIGNATURE` header (a base64-encoded `Payload`
   claiming payment against that `Requirements`) → `TestModeFacilitator`
   checks it. "Verification" here means exactly one thing: the
   payload's scheme/network/asset/amount match the requirements
   *and* its `Signature` field equals a shared secret
   (`X402_DEMO_SECRET`, defaulting to `x402-test-mode-demo-secret`).
   That secret is published here, in tests, and in this README on
   purpose -- it's not a real credential, and treating it as one would
   misrepresent what a test-mode-only stub is for.
3. Valid payment → `200 OK`, a `PAYMENT-RESPONSE` header confirming
   settlement, and the gated resource itself (a small JSON message).
   Invalid payment → re-issues the `402` challenge with the reason
   attached, so a client can see what was wrong rather than guess.

No blockchain is contacted. No real money or crypto moves. Nothing
here touches `commerce/order`, `policy`, or `audit` -- paying this
challenge doesn't create a CommerceOS order, consume a mandate, or
write an audit row.

## Demo it yourself

```bash
# 1. First request -- 402, challenge in the PAYMENT-REQUIRED header.
curl -i http://localhost:8081/x402/priority-support

# 2. Decode the header to see the challenge (adjust the header value
#    from step 1's response):
echo '<PAYMENT-REQUIRED header value>' | base64 -d

# 3. Build a payload that matches DemoRequirements() (see demo.go) and
#    signs with the default shared secret, then base64-encode it:
echo -n '{"x402Version":1,"scheme":"exact","network":"base-sepolia","asset":"USDC","amount":"10000","payer":"0xdemo","signature":"x402-test-mode-demo-secret"}' | base64

# 4. Retry with that value in PAYMENT-SIGNATURE -- 200, resource served.
curl -i http://localhost:8081/x402/priority-support \
  -H "PAYMENT-SIGNATURE: <value from step 3>"
```

## What this deliberately does not do

A real x402 integration into this project's actual agentic-commerce
flow -- an agent autonomously paying x402 challenges for a real
resource as part of a larger checkout, gated by the same Policy Engine
every other spend already goes through, settled by a real facilitator
against a real chain -- is materially more work than a buildathon-
scoped stub: a real facilitator relationship, real wallet/signing
infrastructure, and a redesign of how a non-Razorpay rail's spend
would be authorized, audited, and rate-limited alongside everything
`backend/policy` already gates. This package proves the mechanism
works and is a real, working demonstration of the handshake -- it is
not a claim that x402 checkout is live in this app's real order flow.
