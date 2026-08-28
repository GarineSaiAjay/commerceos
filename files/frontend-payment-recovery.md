# Payment Failure Recovery UX — Implementation Specification

**Status: mostly done, one required action missing.** The recovery flow is now driven by real server state, not modal dismissal: `GET /orders/{id}/recovery` returns `{payment_status, attempt_status, error_code, safe_message, reservation_expires_at, retry_allowed, cart}`, `AttemptRepository.MarkFailed` is wired from the verified failed-webhook path, and the checkout UI's failed screen fetches this endpoint and shows the exact required assurance message. Retry (idempotency-checked, never auto-retried) and Cancel both work correctly.

## Remaining — Remove accessory

The fourth required action, **"Remove accessory"**, does not exist. Build it: show the eligible removable items and the new total, invalidate the old (already-consumed) authorization, and re-run policy/approval on the smaller cart before creating a new payment attempt.

## Remaining — Change payment method isn't really distinct

"Change payment method" currently resets to a fresh cart and sends the buyer back to the catalog — it doesn't reopen a payment-method selector against the *same* order and start a new authorized attempt for it. Given the single-use cart/order model, decide and implement one of: (a) a genuine method-reselection step that reuses the existing order, or (b) keep the reset-and-restart behavior but stop calling it "change payment method" since it isn't one.

## Remaining — backend model for retry without double-reserving

The current single-use cart/order model means a retry effectively needs a fresh cart today. Revise this so a retry can create a new payment attempt against the *same* valid order, while a cart edit (the accessory removal above) correctly creates a new checkout/authorization path without silently double-reserving inventory.

## Remaining — UI states

Add loading, offline, webhook-pending, duplicate-event, and expired-reservation states (today the failed screen only really handles "here is the current recovery snapshot" and doesn't distinguish these). When the reservation expires, disable recovery actions, explain that inventory may have changed, and offer a fresh cart — this partially exists (`retry_allowed` disables the retry button) but the other three actions don't check expiry.

## Verification

Use Razorpay Test Mode failure cards and confirm: one failed attempt is recorded, no duplicate payment/order appears, the retry creates one new attempt, accessory removal recomputes policy, cancellation is auditable, and the browser flow remains useful on a modal dismissal or page refresh. This full pass hasn't been driven in a live browser yet — do it once "Remove accessory" is built.
