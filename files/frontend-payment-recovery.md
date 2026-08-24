# Payment Failure Recovery UX — Implementation Specification

## Product outcome

Replace the static failure message with a truthful recovery flow that preserves the cart reservation, explains the actual provider failure without leaking sensitive data, and gives four distinct actions: retry, change payment method, remove accessory, and cancel.

## Required state model

Drive the UI from server state, not Razorpay modal dismissal. Add a checkout status endpoint or SSE stream:

```text
GET /orders/{id}/recovery
{ payment_status, attempt_status, error_code, safe_message,
  reservation_expires_at, cart, removable_items[], retry_allowed }
```

The webhook writes `payment_attempts.failed`, preserves the cart until its reservation expires, and exposes a sanitized error category. Client verification may update optimistically, but the recovery view must re-fetch authoritative state. A dismissal is `payment_pending`, not automatically `payment_failed`.

## Screen behavior

Show the exact core assurance: “Payment wasn't completed. Razorpay reported that the payment failed. Your order has not been charged twice. The cart remains reserved for 9 minutes.” Replace `9 minutes` with a live countdown and accessible expiry timestamp.

| Action | Required behavior |
|---|---|
| Retry payment | Creates a new attempt only after server idempotency/retry validation; never auto-retries. |
| Change payment method | Reopens payment method selection, then starts a new authorized attempt. |
| Remove accessory | Shows eligible removable items and new total; invalidates old authorization and re-runs policy/approval. |
| Cancel | Cancels checkout, releases reservation if appropriate, and returns to catalog with confirmation. |

If no `payment` object exists, render recovery from order/cart state; never conditionally hide the entire screen. When the reservation expires, disable recovery actions, explain that inventory may have changed, and offer a fresh cart.

## Backend changes

Add `AttemptRepository.MarkFailed` and call it from the verified failed webhook path. Create explicit retry and cart-adjustment commands with idempotency keys. Do not reuse a consumed authorization. The current single-use cart/order model must be revised so a retry uses a new payment attempt for the same valid order, while a cart edit creates the correct new checkout/authorization path without silently double-reserving inventory.

## UI quality

Use a calm warning state rather than an alarming error. Display masked payment method details only when available. Add loading, offline, webhook-pending, duplicate-event, expired-reservation, and unknown-failure states. Make every action keyboard-accessible and avoid a fake `Change payment method` button that simply retries.

## Verification

Use Razorpay Test Mode failure cards and assert: one failed attempt is recorded, no duplicate payment/order appears, the retry creates one new attempt, accessory removal recomputes policy, cancellation is auditable, and the browser flow remains useful on a modal dismissal or page refresh.
