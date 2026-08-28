# Phase 2 — State & Reliability

**Prerequisite:** Phase 1 fully verified.

**Status: nearly complete.** Every reliability mechanism (payment/order state machines, webhook signature verification, dedup, the outbox pattern, idempotency keys, the checkout saga, and the basic hash-chain-ready `audit_events` table) is built and verified live:

- Duplicate `x-razorpay-event-id` delivery is a no-op (exactly one state transition, one `webhook_events` row).
- A forged webhook signature is rejected (400) and logged as a `[security]` event without reaching the state machine.
- Killing the outbox worker mid-batch and restarting it publishes the pending event exactly once (no loss, no duplicate).
- An out-of-order transition (`DRAFT → COMPLETED`) is rejected.
- The same idempotency key submitted twice returns the original result and creates zero additional Razorpay orders (dashboard `count: 1`).
- `audit_events` shows a complete, ordered trace for both a successful and a failed run.
- The server-driven recovery read model (`GET /orders/{id}/recovery`) is implemented and wired into the checkout UI, with retry / change-payment-method / cancel actions and the exact required message pattern.

## Remaining — Failure Demo #1 gap

One recovery option from the original spec is still missing: **"Remove ₹1,999 accessory"**. The checkout failure screen currently offers Retry / Change payment method / Cancel, but there is no path that shows eligible removable items, lets the buyer drop the accessory, recomputes the total, and re-runs policy/authorization on the smaller cart. Build this fourth recovery action (see `files/frontend-payment-recovery.md` for the full UX spec).

Separately, a forced `payment.failed` run in Test Mode has not yet been driven through an actual browser to visually confirm the recovery screen end to end (the server-side behavior is verified via signed webhook tests, not a live browser session) — do this once the accessory-removal option above is built.

**Do not consider Phase 2 fully closed until the accessory-removal option is built and the browser run is confirmed.**
