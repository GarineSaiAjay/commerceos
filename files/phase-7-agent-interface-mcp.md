# Phase 7 — Agent Interface (MCP, Protocol Adapters)

**Prerequisite:** Phase 6 fully verified (real dashboard metrics, labeled simulated experiments).
**Governing principle:** the system is consumable by *other* agents through a well-designed MCP layer, and is architecturally ready for ACP/AP2/UCP/x402 — but never claim compliance with a protocol you have not actually implemented.

---

## 0. Objective of This Phase

Wrap the Policy-Engine-gated commerce core in an MCP server that any external LLM/agent client can call, with deliberately narrow tools so no single tool call has unlimited blast radius. Build a thin protocol-adapter layer so the domain model stays independent of any one protocol, and be explicit with judges about what's actually implemented versus architecturally anticipated.

---

## 1. Your Own Commerce MCP Server

Build your own MCP server sitting *between* the LLM and Razorpay's own MCP tools. **Never connect the LLM directly to Razorpay's MCP server** — Razorpay exposes 35+ MCP tools covering payments/orders/refunds/etc., and connecting an LLM to those directly is exactly the "thin wrapper" anti-pattern this whole project exists to avoid.

```text
LLM → Your Commerce MCP (search_products · get_product · create_cart ·
      recommend_bundle · calculate_total · request_authorization ·
      create_checkout · get_payment_status · explain_decision)
    → Policy Engine → Razorpay Adapter
```

1. Implement each tool as a thin wrapper around the existing Phase 1–5 services (Catalog, Cart, Buyer Agent, Growth Agent, Policy Engine) — do not duplicate business logic inside the MCP layer.
2. `explain_decision` should call the shared "why not" explanation function built in Phase 3, §8 — this is the same machinery, exposed as a first-class tool, not a separate reimplementation.

## 2. Deliberately Narrow Tools

Do **not** expose a single tool like:
```text
make_payment(amount)
```
This has unlimited blast radius — a single tool call could move any amount.

Instead, expose a sequence of narrow, independently verifiable steps:
```text
create_checkout(cart_id)
  → request_payment_authorization(cart_id, mandate_id)
  → execute_authorized_checkout(authorization_id)
```

1. The agent can propose any amount it wants through these tools — the backend re-verifies every field against Phase 3 policy before anything executes. No tool call is trusted at face value, regardless of what the calling LLM claims.
2. `execute_authorized_checkout` must require a valid `authorization_id` obtained from `request_payment_authorization`, and must independently verify it against the `authorizations` table (Phase 3) rather than trusting a client-supplied string.

## 3. Protocol Adapter Layer

Keep the domain model independent of any single protocol:

```text
CommerceOS → MCP ─┐
           → ACP/UCP ─┤→ Commerce Domain Model → Payment Adapter → Razorpay
           → REST ─┘                                            → x402
                                                                  → Future protocols
```

1. Define a `PaymentAdapter` interface with Razorpay as the current implementation and clear extension points for other payment protocols (x402, future rails).
2. Define a domain-model layer that MCP, ACP/UCP, and plain REST all sit on top of, so adding a second protocol later is additive, not a rewrite.
3. **Do not claim ACP/AP2/UCP/x402 compliance** unless you have actually implemented the relevant spec. Describe the system as "protocol-ready via an adapter layer" and be explicit with judges about exactly which parts are implemented (MCP, REST) versus architecturally anticipated (ACP/UCP/x402 adapter slots that exist but aren't wired to real external protocol implementations).

## 4. `explain_decision` Tool

Expose the Phase 3 explanation machinery as a first-class MCP tool, callable independently of the UI — an external agent client should be able to call `explain_decision` for any transaction ID and get back the same explanation a human would see in the dashboard.

---

## Phase 7 — Full Artifact List

- MCP server exposing the narrow tool set: `search_products`, `get_product`, `create_cart`, `recommend_bundle`, `calculate_total`, `request_authorization`, `create_checkout`, `get_payment_status`, `explain_decision`
- Protocol adapter interface (`PaymentAdapter`) with Razorpay as the current implementation and clear extension points for others
- MCP tool-level tests (each tool exercised independently)

---

## Phase 7 — Verification Checklist

- [ ] An external MCP client (e.g. Claude Desktop or an MCP inspector tool) can connect to the Commerce MCP server and successfully call `search_products` and `get_product`
- [ ] Calling `create_checkout` alone, without following with `request_payment_authorization` and `execute_authorized_checkout`, results in **no** Razorpay call
- [ ] `execute_authorized_checkout` called with a forged/invalid `authorization_id` is rejected by the **Policy Engine**, not by the MCP layer alone — confirm the rejection is logged as a `policy_evaluations` row
- [ ] The LLM never has a tool that calls Razorpay directly — grep the MCP tool implementations to confirm every money-moving tool routes through the Policy Engine and Razorpay Adapter, with none calling `api.razorpay.com` directly
- [ ] `explain_decision` returns a real explanation (matching the Phase 3 format) for both an approved and a rejected action

**Do not start Phase 8 until every box above is checked against an actual observed run.**
