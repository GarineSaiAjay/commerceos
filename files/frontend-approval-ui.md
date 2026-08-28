# Level 2/3 Approval UI — Implementation Specification

**Status: backend done, Level 3 hard gate and UX polish remaining.** The durable approval-request state machine described below is fully built and verified live: `POST /policy/propose` returns `{decision, level, approval_request_id, authorization_id}`; Level 1 gets an immediate authorization; Level 2/3 produce `PENDING_HUMAN_APPROVAL`; `GET/POST /approval-requests/{id}/approve|reject` issue a one-time authorization or block, are idempotent, and the payment service still independently verifies and consumes the authorization (browser state is never authority). `/dashboard/approvals` is a working approve/reject queue, and checkout shows a real approval step before payment.

What's below is what the current UI does **not** yet do.

## Level 2 experience — remaining gaps

The current review step shows amount, merchant, items, and a plain-language reason, with Approve/Reject actions. Missing: a confirmation step for accidental-click resistance, an explicit expiry countdown, and rendering the authorization ID only in a separate technical-details drawer (it's currently not surfaced in the main flow at all, which is fine, but there's no drawer pattern established for it either).

## Level 3 hard gate — not yet built

Level 2 and Level 3 currently render through the **identical** screen. Level 3 needs a dedicated full-page route, not a shared step in the checkout flow's state machine: it must say why it is gated, what changed, and what the user can do (approve after deliberate confirmation, edit the cart, or cancel). No background checkout button, keyboard shortcut, direct client-side route mutation, or stale authorization may bypass it. Re-fetch the approval request immediately before the user acts; if amount/items/cart/merchant/policy version changed since it was shown, invalidate and return to review.

## States and accessibility — not yet built

Cover loading, expired, changed, already-resolved, network failure, rejected, and unauthorized states (today only a happy-path pending state and a generic error string exist). Show a countdown with text, not only animation. Use native buttons, focus management, screen-reader announcement of decision state, and a mobile layout where the total and approve/reject controls remain visible without obscuring legal information.

## Security checklist — verify explicitly

- ✅ Policy is recomputed server-side at approval and payment time; amount/level/risk/authorization ID are never trusted from the browser.
- ✅ One-time authorization consumption is enforced (`MarkAuthorizationUsed`).
- Not yet verified: idempotency-key protection against double-submit on the approve/reject buttons themselves (the payment path has idempotency keys; the approval-click path does not visibly send one).
- Not yet verified: audit correlation — proposal, display, approval/rejection, authorization issuance, and payment attempt should share one correlation/run ID end to end for a single transaction; confirm this is actually queryable, not just theoretically possible.

## Acceptance tests — not yet built

Test L1 auto-path, L2 approve/reject/expire, L3 approve/reject/edit, tampered browser payload, changed cart, replayed request, simultaneous approval clicks, and payment attempted before approval. No test may assert only UI visibility: verify zero provider calls before a valid authorization exists.
