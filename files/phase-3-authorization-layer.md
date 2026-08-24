# Phase 3 — Authorization Layer (Policy Engine, Mandates, Approval Levels)

**Prerequisite:** Phase 2 fully verified.
**This is the heart of the entire system. Do not rush it, and do not let any future phase find a way around it.**

**Governing principle:** No monetary action can reach Razorpay without passing through a deterministic policy check, and the system must be able to prove, after the fact, exactly why any action was approved or rejected. Everything built after this phase — the AI Buyer, the Growth Agent, the MCP interface — produces *proposals*. This phase is what turns a proposal into (or refuses to turn it into) money movement.

---

## 0. Objective of This Phase

At the end of this phase, it must be **structurally impossible** — not just conventionally discouraged — for any code path to call the Payment Service without a valid `authorization_id` issued by the Policy Engine. Every rejection must come with a plain-language explanation. Every approval must be reproducible against the policy version that produced it. And the audit log must become tamper-evident, not just append-only.

---

## 1. Policy Engine as a Hard Chokepoint

```text
LLM → Proposed Action → Policy Engine → Authorization → Payment Service → Razorpay
```

1. Refactor the Payment Service (built in Phase 1) so its money-moving entry point **requires** a valid `authorization_id` as a parameter, and internally verifies that authorization against the `authorizations` table before doing anything.
2. There must be no alternate entry point into the Payment Service that skips this check. Grep the codebase for every call site into the Payment Service and confirm each one carries an authorization.
3. This is a refactor of existing Phase 1 code, not new code sitting alongside it — the old "approved just means exists" placeholder from Phase 1 gets replaced here.

## 2. Proposed Action Schema

Define this as the canonical shape every agent (and even a manual test harness) must use to propose an action:

```json
{
  "action": "CREATE_ORDER",
  "amount": 26899,
  "currency": "INR",
  "merchant": "merchant_001",
  "items": ["airpods-pro-2", "airpods-case"]
}
```

Validate incoming proposals strictly against this schema — malformed proposals are rejected before they reach any policy logic.

## 3. Deterministic Policy Checklist

Implement each of the following as a discrete, independently testable check function, evaluated **in code**, never delegated to the LLM:

- Merchant is allow-listed
- Currency is allowed
- Amount ≤ configured ceiling (e.g. ₹30,000)
- Every product in the proposal is permitted
- Cart is within budget tolerance
- No duplicate transaction (reuse the Phase 2 idempotency machinery — don't reimplement it)
- User consent exists
- Order/mandate has not expired

All checks must pass for an `APPROVED` decision; any single failure produces a `REJECTED` decision with that specific reason attached.

## 4. Policy Decision Output

Version every decision so it's reproducible against the exact policy logic that produced it:

```json
{
  "decision": "APPROVED",
  "policy_version": "v17",
  "authorization_id": "auth_83f...",
  "expires_at": "..."
}
```

1. Store `policy_version` as a literal string/tag bumped whenever policy logic changes.
2. Persist every decision (approved or rejected) to `policy_evaluations`, including the policy_version that produced it.

## 5. Agent Payment Mandate

Model your own authorization object on the philosophy behind Google's AP2 (intent mandates, payment mandates, receipts) — you do not need to implement the full spec, just the shape and the enforcement behavior:

```json
{
  "mandate_id": "mnd_10291",
  "buyer": "buyer_42",
  "merchant": "merchant_001",
  "allowed_categories": ["electronics", "accessories"],
  "maximum_amount": { "value": 30000, "currency": "INR" },
  "requires_confirmation_above": 10000,
  "allowed_payment_methods": ["upi", "card"],
  "expires_at": "2026-08-21T18:00:00Z",
  "purpose": "Purchase wireless earbuds",
  "status": "ACTIVE"
}
```

1. Store in a `mandates` table.
2. **Bind the mandate to the cart end-to-end:** `Mandate → Cart → Amount → Merchant → Payment`. Implement this as an actual foreign-key/reference chain, not a convention.
3. If **any** link in that chain changes after the mandate was issued — price change, merchant swap, amount drift — the mandate must become invalid and the action blocked. This is the exact mechanism behind Failure Demo #2 (section 7 below), so build the invalidation check as a reusable function you call both from the checkout path and explicitly from a test.

## 6. Three Authorization Levels

| Level | Range | Behavior |
|---|---|---|
| 1 — Auto-approve | ≤ ₹1,000, trusted merchant, previously authorized category, no unusual risk | Agent transacts automatically, no human in the loop |
| 2 — Confirm | ₹1,001 – ₹10,000 | Agent prepares the full plan; user sees items, amount, reasoning, then taps Approve |
| 3 — Hard gate | > ₹10,000, unknown merchant, unusual purchase, policy violation, refund-sensitive item | Agent cannot proceed without explicit human authorization |

1. Implement level routing as a function of `(amount, merchant_trust, category_history, risk_score)` → `{1, 2, 3}` — **not** a single amount threshold. Level 3 must trigger on unknown merchant or policy violation **regardless of amount**, even if the amount alone would qualify for Level 1.
2. Build the UI/API surface for each level: Level 1 needs no UI step; Level 2 needs a visible "Approve" button showing items/amount/reasoning; Level 3 needs an explicit hard-gate screen that cannot be bypassed.

## 7. Failure Demo #2 — Stale Authorization

**Setup:** buyer authorizes ₹24,900. Agent (or test harness, since the AI Buyer doesn't exist until Phase 4) tries to add a ₹2,000 accessory, bringing the total to ₹26,900 — but the mandate's ceiling is ₹25,000.

The system must reject **before** any payment API call:

```text
┌──────────────────────────────┐
│        ACTION BLOCKED        │
├──────────────────────────────┤
│ Requested:      ₹26,900      │
│ Authorized:     ₹25,000      │
│ Difference:      ₹1,900      │
│ Reason: spending limit       │
│ exceeded                     │
│ No payment API was called.   │
└──────────────────────────────┘
```

Build this exact scenario as a runnable test/demo case now — it's rehearsed live in Phase 9, so it needs to work deterministically, not just "usually."

## 8. "Why Not?" Explanations

Every rejection anywhere in the system must produce a human-readable explanation, not just a status code or error enum.

Example:
> "The warranty costs ₹2,499. Adding it would raise the cart from ₹24,900 to ₹27,399. Your authorization allows a maximum of ₹25,000. I did not add it, and no payment action was attempted."

Implement a shared "explain rejection" function that takes the failed policy check + relevant numbers and renders this kind of sentence. Every policy check from section 3 should be able to produce its own explanation via this function.

## 9. Tamper-Evident Audit Log

Upgrade the Phase 2 basic `audit_events` table into a hash-chained log:

```text
H0 = SHA256(E0)
H1 = SHA256(E1 + H0)
H2 = SHA256(E2 + H1)
```

1. Add `event_hash` and `prev_hash` columns to `audit_events`.
2. On every insert, compute `event_hash = SHA256(event_content + prev_hash)`.
3. Build a **verifier** (CLI command or admin endpoint) that walks the whole chain and reports `Verified: ✓/✗` and `Chain broken: Yes/No`.
4. This is deliberately **not** a blockchain — no consensus, no distributed ledger. An append-only hash-chained table is the right-sized tool for "tamper-evident," and reaching for more is unjustified complexity for this system.
5. Test the verifier by manually editing one historical row in the DB and confirming it correctly reports `Chain broken: Yes`.

## 10. Risk Engine (first pass — expanded in Phase 8)

```text
Risk Score = f(amount, merchant, buyer_history, category, velocity, authorization, cart_deviation)
```

| Risk Score | Outcome |
|---|---|
| 0.08 | Automatic approval |
| 0.46 | Requires confirmation |
| 0.91 | Blocked |

Build a first-pass scoring function now (even a simple weighted heuristic is fine — Phase 8 hardens it against actual adversarial inputs). Persist scores to `risk_assessments`, and feed the score into the Level 1/2/3 routing function from section 6.

---

## Phase 3 — Full Artifact List

- `authorizations`, `mandates` tables
- `agent_actions`, `agent_decisions`, `policy_evaluations` tables
- `audit_events` table upgraded with hash-chain columns (`event_hash`, `prev_hash`)
- Policy Engine module sitting structurally between every agent and the Payment Service
- Risk Engine module producing a `risk_score` per proposed action
- Audit chain verifier (CLI or endpoint)
- Shared "why not" explanation function

---

## Phase 3 — Verification Checklist

> **Progress note (updated after an observed run against the live stack):**
> - ✅ Hard chokepoint verified live: payment without an `Authorization-Id` is rejected before any Razorpay call; with a valid authorization it proceeds.
> - ✅ Failure Demo #2 verified live: ₹26,900 proposal vs ₹25,000 mandate is REJECTED (`budget_tolerance`), no authorization issued, no payment path reached.
> - ✅ Level routing verified via tests (₹500→L1, ₹5,000→L2, ₹20,000→L3) and live (₹500 auto-approved, L1).
> - ✅ Audit chain verifier: tests prove Verified on untouched log and Chain broken after tampering.
> - ⚠️ Level 2/3 UI (Approve button / hard-gate screen) is backend-routed but the frontend UI was not driven in this sandbox.

- [x] A proposed action with amount above the ceiling is rejected, with **zero** calls made to the Razorpay Adapter (verified via unit test + live: no authorization issued, no payment path reached)
- [x] Failure Demo #2 (stale authorization) reproduces exactly: the block message shows requested/authorized/difference, and the Razorpay Adapter's call count for that request is 0 (verified live)
- [x] Changing any element bound to an active mandate (price, merchant, item) invalidates the mandate before payment (verified via unit test)
- [ ] Level 1/2/3 routing fully verified incl. the visible Approve button (L2) and hard-gate screen (L3) — routing itself proven; UI step not driven in this sandbox
- [x] The audit chain verifier reports verified on an untouched log, and correctly reports broken after manually editing a historical row (unit test + live endpoint)
- [x] Every rejection produces a plain-language "why not" explanation (budget test asserts the sentence with numbers)
- [x] `policy_version` is recorded on every decision (persisted to `policy_evaluations`)

**Do not start Phase 4 until every box above is checked against an actual observed run. This phase is the load-bearing wall of the whole project — verify it thoroughly.**
