# CommerceOS — Trust Boundary

**Phase 8 §1 artifact.** Everything north of the boundary is **untrusted**;
everything south of it is **re-validated** before it can move money.

```mermaid
flowchart LR
    subgraph Untrusted["UNTRUSTED ZONE"]
        U1[User Natural Language] --> A1
        U2[Product Descriptions] --> A1
        U3[Merchant Metadata] --> A1
        U4[External APIs] --> A1
    end

    subgraph Agent["AGENT LAYER"]
        A1[LLM / IntentExtractor] --> P1[Proposed Action]
    end

    subgraph TrustBoundary["<< TRUST BOUNDARY — everything re-validated here >>"]
        P1 --> PE[Policy Engine]
        PE -->|approve| AUTH[Authorization]
        PE -->|reject| EXPLAIN[Why-Not Explanation]
    end

    AUTH --> PA[Payment Adapter]
    PA --> RAZORPAY[Razorpay]
    RAZORPAY -->|webhook| WH[Webhook: verify + dedup]
    WH --> SM[Order / Payment State Machines]
    SM --> AUDIT[Audit Ledger (hash-chained)]
```

## What crosses the boundary (and how it is re-validated)

| Input | Re-validation on the trusted side |
|---|---|
| User prompt | Strict structured-output schema (`ParseIntentJSON`) |
| Product descriptions | Catalog is read-only; the LLM names a `product_id`, never a price |
| LLM proposal | `ValidateProposal` + the Policy Engine's deterministic checks |
| Merchant metadata | `merchant_allowlisted` + `mandate_bound` |
| Webhook body | HMAC signature verification + `x-razorpay-event-id` dedup |
| Payment signature | HMAC-SHA256 over `order_id|payment_id` |

## The core guarantee

> The LLM never crosses the trust boundary directly. Everything it produces is
> a **proposal**, re-validated on the trusted side (Phase 3 Policy Engine)
> before any payment API call. The `RazorpayClient` call counter
> (`GET /adapter/calls`) is how this is *proven*: a blocked action must show
> `razorpay_calls: 0`.

## Attack surface map (Phase 8 red-team)

| Attack | Where it is stopped |
|---|---|
| Prompt injection in product description | Proposal schema validation + policy checks |
| Tool injection (MCP) | Narrow tools; each re-validates server-side |
| Data exfiltration via LLM output | LLM never receives secrets (Key Secret never in context) |
| Goal hijacking | Budget/category/priority validated against intent schema |
| Price manipulation | Cart service looks up authoritative price; policy re-checks amount |
| Authorization bypass | `CreatePaymentOrder` requires a valid `Authorization-Id` |