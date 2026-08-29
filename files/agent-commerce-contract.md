# Agent Commerce Contract

This is the documented request/response contract for how an AI agent
(buyer-side) transacts against CommerceOS. Every endpoint states what it
**may** do and what it is **explicitly not allowed** to do. The Buyer
Agent produces *proposals* only — it never moves money.

All endpoints are served by the Commerce Service (default `:8081`).
This document was rewritten to match the actual route registrations in
`backend/cmd/server/main.go` — an earlier version described a
decomposed `/agent/catalog` → `/agent/search` → `/agent/cart` →
`/agent/authorize` → `/agent/payment` flow that was never implemented;
the real surface is below.

---

## GET /products

- **Allowed:** List the full seeded catalog (AI-native schema: features,
  attributes, use_cases, price, availability).
- **Not allowed:** Anything else.

## GET /products/{id} · GET /variants/{id}

Read a single product or variant. Read-only.

## POST /agent/checkout

A single fused call that does intent extraction, catalog search, and
Proposed Action assembly — there is no separate search or cart-building
step in the agent-facing surface (a human-facing cart still exists at
`POST /carts` / `POST /carts/{id}/items`, used by the checkout UI, not
by the agent contract).

**Request:**
```json
{ "prompt": "I need wireless earbuds for my sister. Budget ₹25,000...", "merchant": "merchant_001" }
```

**Response:** a `CheckoutPlan` — the extracted `Intent`, a
`policy.ProposedAction` (`CREATE_ORDER` with amount/currency/merchant/
items), the selected `product_id`, and a `reasoning` string naming the
actual product and numbers behind the choice.

- **Allowed:** Rank products by soft preference (features, use_cases)
  and produce a Proposed Action from intent + ranked search. Hard
  constraints (price ≤ budget, `availability > 0`) are applied
  deterministically server-side (`agents.Searcher.Search`); the LLM
  never computes or skips them.
- **Not allowed (explicit):** Perform a payment, create a Razorpay
  order, write prices or availability, or otherwise move money. Calling
  `/agent/checkout` alone NEVER results in a Razorpay call — verified
  by the adapter call counter (`GET /adapter/calls`).
- Ambiguous prompts (missing budget, "buy me something") return an
  error rather than a guessed proposal — the agent asks for
  clarification instead of fabricating an intent.

To execute, the caller must take the Proposed Action and route it via
the Policy Engine (`POST /policy/propose` → authorization) — never
directly to the Payment Service.

## POST /policy/propose

Submits a Proposed Action to the deterministic Policy Engine.

**Request:**
```json
{ "action": "CREATE_ORDER", "amount": 2490000, "currency": "INR", "merchant": "merchant_001", "items": ["airpods-pro-2"], "cart_id": "cart_123", "mandate_id": "mnd_demo" }
```

**Response:** APPROVED/REJECTED + `authorization_id` when approved, or
the specific `failed_check` and a human-readable reason when rejected.
No money moves here either.

## POST /orders/{id}/payment

Executes an approved authorization through the Payment Service. This is
the ONLY endpoint that touches money movement, and it requires a valid
`Authorization-Id` header issued by `/policy/propose`.

- **Allowed:** Move money for an authorization issued by Policy.
- **Not allowed:** Any other path (no auth is required and enforced —
  there is no alternate entry point that skips this check).

## GET /orders/{id} · GET /orders?merchant_id=...

Reads order status / order history. Read-only.

## POST /mcp

The same capabilities above, exposed as 11 narrow MCP tools
(`search_products`, `get_product`, `create_cart`, `add_item`,
`recommend_bundle`, `calculate_total`, `request_authorization`,
`create_checkout`, `execute_authorized_checkout`, `get_payment_status`,
`explain_decision` — a `tools/list` JSON-RPC call enumerates all of
them, each with a real JSON Schema `properties`/`required` block, not a
bare `{"type":"object"}`) over JSON-RPC 2.0, for an MCP-speaking agent
rather than a direct HTTP client. `add_item` matters specifically: a
cart created via `create_cart` starts empty, and `add_item` is the only
MCP tool that can put something into it — without it, an MCP-only agent
could search and create a cart but never actually complete a purchase.
Same governing rule applies: there is no single `make_payment(amount)`
tool with unlimited blast radius — money movement is always a distinct,
narrow `request_authorization` step gated by the same Policy Engine.

**Handshake:** `initialize` returns the spec's `InitializeResult` shape
(`protocolVersion`, `capabilities`, `serverInfo`), echoing back the
client's requested `protocolVersion` when present. **Transport:** plain
HTTP POST JSON-RPC only — the server does not implement the
Streamable-HTTP transport's server-initiated SSE stream, so an MCP
client that requires that half of the spec (rather than simple
synchronous request/response) will not work against this endpoint.

---

**Governing rule:** every money-moving call requires an
`authorization_id` from the Policy Engine, verified inside the Payment
Service before any Razorpay call. There is no alternate entry point.
