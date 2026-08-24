# Level 2/3 Approval UI — Implementation Specification

## Product outcome

Implement `/dashboard/approvals` and the buyer checkout approval step so policy routing has an unavoidable human interface. Level 1 proceeds without a dialog. Level 2 requires an informed approve/reject choice. Level 3 is a hard stop: payment controls do not exist until a fresh explicit approval is recorded.

## Non-negotiable backend contract

The current `POST /policy/propose` issues an authorization immediately when approved, which is insufficient for Level 2/3. Introduce a durable approval request state:

```text
POST /policy/propose -> { decision, level, approval_request_id?, authorization_id? }
GET  /approval-requests/{id}
POST /approval-requests/{id}/approve  -> one-time authorization
POST /approval-requests/{id}/reject
```

Only Level 1 returns an active authorization immediately. Level 2/3 produce `PENDING_HUMAN_APPROVAL`; the approval endpoint must authenticate the approver, bind the decision/version/cart/amount/merchant/items, expire quickly, be idempotent, and write an audit event. The payment service still verifies and consumes the authorization; browser state is never authority.

## Level 2 experience

Present a review sheet with items, quantities, total, merchant, payment method, policy version, risk level, expiry, and plain-language rationale. The primary action is `Approve ₹…`; the secondary action is `Reject`. Approval uses a confirmation step for accidental-click resistance. On success, render the authorization ID only in the technical details drawer, then proceed to payment.

## Level 3 hard gate

Use a dedicated full-page route, not a dismissible modal. It must say why it is gated, what changed, and what the user can do: approve after deliberate confirmation, edit the cart, or cancel. No background checkout button, keyboard shortcut, direct client-side route mutation, or stale authorization may bypass it. Re-fetch the approval request immediately before action; if amount/items/cart/merchant/policy version changed, invalidate and return to review.

## States and accessibility

Cover loading, expired, changed, already-resolved, network failure, rejected, and unauthorized states. Show a countdown with text, not only animation. Use native buttons, focus management, screen-reader announcement of decision state, and a mobile layout where the total and approve/reject controls remain visible without obscuring legal information.

## Security checklist

- Recompute policy on the trusted server at approval and payment time.
- Require authenticated, authorized approver identity and record it in audit.
- Do not trust amount, level, risk, or authorization ID supplied by the browser.
- Enforce one-time authorization consumption and prevent double submit with idempotency keys.
- Audit proposal, display, approval/rejection, authorization issuance, and payment attempt with the same correlation/run ID.

## Acceptance tests

Test L1 auto-path, L2 approve/reject/expire, L3 approve/reject/edit, tampered browser payload, changed cart, replayed request, simultaneous approval clicks, and payment attempted before approval. No test may assert only UI visibility: verify zero provider calls before a valid authorization exists.
