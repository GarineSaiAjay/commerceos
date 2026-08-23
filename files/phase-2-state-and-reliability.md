# Phase 2 — State & Reliability

**Prerequisite:** Phase 1 fully verified (real Test Mode purchase works end to end, Razorpay Adapter is the sole call path).
**Governing principle:** the LLM has intent authority, never financial authority — this phase has no LLM code either. It exists to make the deterministic core survive duplicate webhooks, partial failures, and crashes, which are *real* distributed-systems requirements for anything touching payments, not optional hardening.

---

## 0. Objective of This Phase

Phase 1 proved the happy path works once. Phase 2 proves the system behaves correctly when things go wrong in the specific ways real payment systems go wrong: duplicate webhook delivery, signature forgery attempts, process crashes between a DB commit and an event publish, out-of-order state transitions, and repeated commands. None of this is theoretical — Razorpay's own documentation explicitly calls out duplicate webhook delivery, so treat deduplication as a hard requirement, not decoration.

---

## 1. Payment State Machine

Replace any boolean `payment = true/false` field. A boolean cannot represent "authorized but not yet captured" or "failed after a retry" — both occur in production.

```text
CREATED → PENDING → AUTHORIZED → CAPTURED → COMPLETED
PENDING → FAILED
```

1. Add a `status` enum column to the `payments` table with exactly these values.
2. Implement guarded transition functions — a transition is only legal if it appears in an explicit allowed-edges table. Reject anything else with an explicit error, don't silently coerce it.
3. Do **not** implement this as scattered `if` statements across the codebase — centralize the transition logic in one place (e.g. `/backend/events` or a dedicated state-machine module) so Phase 8's evaluation suite can test it directly.

## 2. Order State Machine

```text
DRAFT → AUTHORIZED → PAYMENT_PENDING → PAID → FULFILLMENT_PENDING → COMPLETED
```

1. Same pattern: explicit state table + guarded transition functions.
2. Write a test that attempts an illegal transition (e.g. `DRAFT → COMPLETED` directly, skipping every intermediate state) and asserts it is rejected.

## 3. Webhook Signature Verification

1. Every inbound webhook must be verified against Razorpay's documented signature scheme before it is trusted in any way.
2. Reject anything that fails verification — do not process it, do not log it as a normal event; log it as a **security event** in a distinguishable way (this becomes relevant again in Phase 8's red-team work).
3. Write a test that sends a webhook payload with a forged/invalid signature and asserts it never reaches the state machine.

## 4. Webhook Deduplication

1. Deduplicate on the `x-razorpay-event-id` header — Razorpay documents duplicate delivery explicitly.
2. Store seen event IDs (a unique index on `webhook_events.event_id` is sufficient — no need for a separate dedup cache).
3. A repeat delivery must be a **no-op**: it should not trigger a second state transition, not even an idempotent-looking one that happens to compute the same result — the transition function should never even be invoked for a duplicate.
4. Write a test: send the identical webhook payload twice, assert exactly one state transition occurred.

## 5. Event Store

1. Persist every inbound webhook event — post-verification, post-dedup — as an immutable record before acting on it.
2. This table (`webhook_events`) is your source of truth for "what actually happened," independent of whatever the state machine later derives from it.

## 6. Outbox Pattern

Prevent the classic failure mode where the DB commits an order/payment update but the corresponding event is silently lost (e.g., because the process crashes right after the DB commit but before publishing to the event bus).

```text
[DB TX: update orders row + insert into outbox_events] → Outbox Worker polls outbox_events → publishes to Event Bus (Redis Streams)
```

1. Write the order/payment state update and its corresponding domain event **in the same database transaction**.
2. Build an Outbox Worker process that polls `outbox_events` for unpublished rows and publishes them to Redis Streams, marking them published only after a successful publish.
3. Deliberately crash-test this: kill the worker process mid-batch (mid-publish), restart it, and confirm it still publishes the pending event with no loss and no duplicate publish of an already-published event.

## 7. Idempotency Keys

1. Every money-moving command carries an idempotency key, e.g.:
   `merchant_001:cart_923:checkout_7:attempt_1`
2. Add an idempotency-key column (with a unique index) to whichever table represents "checkout attempts" or "payment commands."
3. If the same command arrives twice (same key), return the **existing** result — never create a second payment or a second Razorpay order.
4. Write a test: submit the same idempotency key twice, assert the Razorpay Adapter's call counter (built in Phase 1) increased by exactly one, not two.

## 8. Async Processing, Not Timers

Anti-pattern to eliminate if it exists anywhere from Phase 1 prototyping:
```text
create payment → sleep(5s) → assume success
```

Correct pipeline:
```text
Razorpay (payment.captured) → Webhook Gateway → Signature Verification → Event Dedup → Event Store → Order State Machine
```

Confirm the entire system's notion of "did the payment succeed" is driven by this webhook pipeline, not by polling combined with a fixed delay.

## 9. Checkout Saga

Checkout spans multiple systems (your DB, Razorpay, the webhook pipeline) and is **not** a single ACID transaction. Model it explicitly:

```text
CheckoutStarted → AuthorizationValidated → RazorpayOrderCreated → PaymentPending → PaymentCaptured → OrderConfirmed
PaymentPending --failure--> PaymentFailed → ReleaseReservation → InvalidateCheckout → NotifyBuyer
```

Implement this as an explicit sequence of named steps with defined failure branches — not as an implicit chain of function calls with try/catch scattered around.

## 10. Failure Demo #1 — Payment Failure Without Duplicate Charge

This is a **required demo beat** for Phase 9 — build the real recovery UX now, not a stub.

**Setup:** cart = ₹26,899, authorization limit = ₹30,000. Order created in Razorpay. Payment fails; system receives `payment.failed`.

**Build:**
1. On `payment.failed`: run the idempotency check (step 7) — do **not** auto-retry the payment.
2. Analyze the failure reason (Razorpay's webhook payload includes an error code/description — surface it).
3. Present recovery options to the user: **Retry payment · Change payment method · Remove ₹1,999 accessory · Cancel**.
4. Keep the cart held under its reservation TTL (built in Phase 1, section 3) so these recovery options have something valid to act on.
5. Surface this exact message pattern in the UI:
   > "Payment wasn't completed. Razorpay reported that the payment failed. Your order has **not** been charged twice. The cart remains reserved for 9 minutes."

## 11. Immutable Audit Log (basic version)

1. Every state transition and webhook event writes a structured, inspectable record: actor, action, key detail, timestamp.
2. This is deliberately the **basic** version — full tamper-evidence (hash chaining) is Phase 3 work. Right now you just need an append-only table that lets you reconstruct a full lifecycle trace for any given order/payment.
3. Store as `audit_events` (Phase 3 will add hash-chain columns to this same table).

---

## Phase 2 — Full Artifact List

- `webhook_events` table (with `x-razorpay-event-id` as a unique/dedup key)
- `outbox_events` table + Outbox Worker process
- State machine implementation for `orders` and `payments` (explicit transition tables, not scattered `if`s)
- Idempotency key column/index on all money-moving command tables
- Recovery UX for failed payments (matching the exact message pattern above)
- Redis Streams event bus wired to at least a placeholder Analytics/Audit/Notification consumer (full consumers arrive in later phases)
- `audit_events` table (basic, append-only)

---

## Phase 2 — Verification Checklist

- [ ] Sending the same webhook payload twice (same `x-razorpay-event-id`) results in exactly **one** state transition, not two
- [ ] A webhook with an invalid/forged signature is rejected and never reaches the state machine
- [ ] Killing the outbox worker process between "DB commit" and "event publish," then restarting it, results in the pending event still being published — no silent loss (test this by manually crashing the worker mid-batch)
- [ ] An out-of-order transition attempt (e.g. `DRAFT → COMPLETED` directly) is rejected by the state machine
- [ ] Submitting the same idempotency key twice returns the original result and creates **zero** additional Razorpay orders/payments (confirm via the Phase 1 adapter call counter)
- [ ] A forced `payment.failed` in Test Mode shows the exact recovery UX (no duplicate charge, cart preserved, retry/change/remove/cancel options) — confirmed by manually inspecting the Razorpay dashboard to verify no second charge exists
- [ ] The `audit_events` table shows a complete, ordered trace for at least one full successful run and one full failed run

**Do not start Phase 3 until every box above is checked against an actual observed run.**
