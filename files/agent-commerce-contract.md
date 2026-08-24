# Agent Commerce Contract (Phase 4)

This is the documented request/response contract for the Buyer Agent.
Every endpoint states what it **may** do and what it is **explicitly not
allowed** to do. The Buyer Agent produces *proposals* only — it never
moves money.

All endpoints are served by the Commerce Service (default `:8081`).

---

## GET /agent/catalog

- **Allowed:** List the full seeded catalog (AI-native schema: features,
  attributes, use_cases, price, availability).
- **Not allowed:** Anything else.

## POST /agent/search

**Request:**
```json
{ "intent": { "budget": 25000, "category": "earbuds", "priority": "active_noise_cancellation" } }
```

**Response:** ranked products (best-first). Hard constraints (price ≤
budget, `availability > 0`) are applied deterministically server-side;
the LLM never computes or skips them.

- **Allowed:** Rank products by soft preference (features, use_cases).
- **Not allowed:** Change prices, invent availability, bypass budget.

## POST /agent/cart

Builds a cart from named `product_id`s. The Cart service (Phase 1)
looks up authoritative price/availability and applies constraints
(`purchase_constraints.max_quantity`).

- **Allowed:** Name product IDs and quantities.
- **Not allowed:** Write prices or availability directly.

## POST /agent/checkout

**Request:**
```json
{ "prompt": "I need wireless earbuds for my sister. Budget ₹25,000...", "merchant": "merchant_001" }
```

**Response:** a `CheckoutPlan` containing a
`policy.ProposedAction` (`CREATE_ORDER` with amount/currency/merchant/
items) + reasoning.

- **Allowed:** Produce a Proposed Action from intent + ranked search.
- **Not allowed (explicit):** Perform a payment, create a Razorpay
  order, or otherwise move money. Calling `/agent/checkout` alone NEVER
  results in a Razorpay call — verified by the adapter call counter.

To execute, the caller must take the Proposed Action and route it via
the Policy Engine (`/policy/propose` → authorization) — never directly to
the Payment Service.

## POST /agent/authorize

Submits the Proposed Action to the Phase 3 Policy Engine. Returns
APPROVED/REJECTED + `authorization_id` when approved. No money moves
here either.

## POST /agent/payment

Executes an approved authorization through the Payment Service using the
`authorization_id`. This is the ONLY link that touches money movement,
and it requires a valid authorization issued by the Policy Engine.

- **Allowed:** Move money for an authorization_id issued by Policy.
- **Not allowed:** Any other path (no auth is required and enforced).

## GET /agent/order/{id}

Reads the order status. Read-only.

---

**Governing rule:** every money-moving call requires an
`authorization_id` from the Policy Engine, verified inside the Payment
Service before any Razorpay call. There is no alternate entry point.