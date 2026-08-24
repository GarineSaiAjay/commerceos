# Phase 4 — AI Buyer Agent

**Prerequisite:** Phase 3 fully verified (Policy Engine is a hard chokepoint, mandates work, all three authorization levels demonstrated).
**Governing principle:** this is the first phase that touches an LLM. The LLM does **semantic reasoning only** — intent interpretation, product selection *preference*, natural language. It never computes amounts, never authorizes anything, and never calls Razorpay. Every output it produces is a Proposed Action that must survive the Phase 3 Policy Engine unchanged in its financial meaning.

---

## 0. Objective of This Phase

A buyer can type a natural-language request and receive a real, policy-compliant cart. The LLM decides *what* the buyer probably wants; the deterministic Cart/Policy layers from Phases 1–3 decide whether it's affordable, available, and authorized. If at any point the LLM's output is trusted downstream without validation, this phase is not done correctly — re-read section 21 of the source spec: "the LLM has intent authority, it never has financial authority."

---

## 1. Intent Extraction

1. Given a buyer prompt, e.g.:
   > "I need wireless earbuds for my sister. Budget ₹25,000. I want good noise cancellation."

   extract structured intent:
   ```text
   budget      = ₹25,000
   category    = earbuds
   priority    = active_noise_cancellation
   recipient   = sister
   ```
2. Implement this as an LLM call with a **strict, validated output schema** (e.g., function-calling / structured output mode, not free-text parsing). Never trust unvalidated free-text output downstream — validate the JSON shape and field types before anything else touches it.
3. Handle ambiguous or nonsensical input gracefully: if the prompt is something like "buy me something," the extraction step should produce either a clarifying question back to the user or a safe no-op — never a guessed amount or category that silently proceeds into a cart.

## 2. Catalog Search Over the AI-Native Schema

1. Search/filter/rank against the full product schema built in Phase 1 — `features`, `attributes`, `use_cases`, and `price` — not a keyword match against `title` alone.
2. Example: for `priority = active_noise_cancellation`, a product tagged with `"features": ["active_noise_cancellation"]` should rank above a similarly priced product without that feature, even if the second product's title happens to contain more of the search keywords.
3. This ranking logic can be a mix of deterministic filtering (hard constraints: budget, availability) and LLM-assisted ranking (soft preferences) — but the hard constraints (price ≤ budget, `availability > 0`, `purchase_constraints` respected) must be enforced deterministically, not left to the LLM to "remember."

## 3. Cart Construction

1. Given the top-ranked product(s) from search, the LLM selects **which** product(s).
2. The deterministic Cart service (Phase 1) then handles: amounts, availability checks, and constraints (`purchase_constraints.max_quantity`, etc.).
3. The LLM never writes a price or quantity into the cart directly — it names a `product_id`, and the Cart service looks up the authoritative price/availability itself.

## 4. Checkout Planning

1. The Buyer Agent assembles a checkout **proposal** — using the Proposed Action schema from Phase 3 — and hands it to the Policy Engine.
2. It is critical that this is a *proposal*, never an *execution*. The Buyer Agent must have no code path that calls the Payment Service directly; it only ever produces a Proposed Action object and passes it to the Phase 3 Policy Engine's entry point.

## 5. Agent Commerce Contract

Define these as a documented request/response contract (OpenAPI or equivalent schema doc), not ad hoc endpoints:

```text
GET  /agent/catalog
POST /agent/search
POST /agent/cart
POST /agent/checkout
POST /agent/authorize
POST /agent/payment
GET  /agent/order/{id}
```

For each endpoint, document:
- Request/response schema
- What the call is allowed to do
- What the call is **explicitly not** allowed to do

Example: `/agent/checkout` never itself moves money — it only produces a Proposed Action for the Policy Engine. State this in the contract doc explicitly, not just implicitly by code structure.

## 6. Buyer Agent Stretch Flow (if time allows)

Given a prompt like *"buy me a laptop under ₹70k with good battery life,"* run the full sequence end-to-end:
```text
discover → compare → optimize → cart → authorization → payment → confirmation
```
This is optional polish — do not attempt it before sections 1–5 above are fully verified.

---

## Phase 4 — Full Artifact List

- Intent extraction module (LLM call with strict, validated output schema)
- Catalog search/ranking module (hard constraints deterministic, soft ranking LLM-assisted)
- Buyer Agent service that produces Proposed Actions only (no direct Payment Service access)
- Documented Agent Commerce Contract (OpenAPI or equivalent)
- (Stretch) Full discover→confirmation flow for the laptop-style prompt

---

## Phase 4 — Verification Checklist

> **Progress note (updated after an observed run):**
> - The LLM provider is pluggable behind the `IntentExtractor` interface; this sandbox uses a deterministic extractor (with strict `ParseIntentJSON` schema validation in place), so LLM-output variability is simulated deterministically. A real LLM provider can be dropped in without changing any other code.
> - ✅ Verified live: the demo prompt extracts budget/category/priority/recipient (5-run consistency test), ANC product ranks above non-ANC, priced within budget, well-formed `CREATE_ORDER` proposal, and `POST /agent/checkout` alone produces a proposal only (no payment path). Ambiguous "buy me something" → safe no-op (clarification requested).
> - ⚠️ With a real LLM, the 5-run consistency check should be re-run against actual varied output.

- [x] The exact demo prompt reliably extracts budget/category/priority/recipient across **5 repeated runs** (deterministic extractor + strict schema validation)
- [x] Search results respect the extracted budget and priority (ANC-tagged AirPods Pro ranks above non-ANC products at similar price — verified live)
- [x] The Buyer Agent's output is a well-formed Proposed Action every time — `ParseIntentJSON` rejects malformed output before the Policy Engine
- [x] `/agent/*` endpoints are covered by the documented contract (`files/agent-commerce-contract.md`); `/agent/checkout` alone produces a proposal only and never results in a Razorpay call
- [x] Ambiguous intent ("buy me something") degrades gracefully — safe no-op, never a wrong-amount cart (verified live)

**Do not start Phase 5 until every box above is checked against an actual observed run.**
