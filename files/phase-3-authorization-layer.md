# Phase 3 — Authorization Layer (Policy Engine, Mandates, Approval Levels)

**Prerequisite:** Phase 2 mostly verified (see remaining item there).

**Status: nearly complete.** The Policy Engine is a verified hard chokepoint, the mandate model, the three-level risk-aware routing, the tamper-evident audit hash chain, and the shared "why not" explanation function are all built and verified live:

- Payment without a valid `Authorization-Id` is rejected before any Razorpay call; a valid authorization proceeds.
- Failure Demo #2 (₹26,900 proposal vs. ₹25,000 mandate) is REJECTED with the exact requested/authorized/difference explanation, zero Razorpay calls.
- Level routing uses the risk score (not amount alone) and is verified both by unit test and live (₹500 → L1, ₹5,000 → L2, ₹20,000 → L3).
- Changing any element bound to an active mandate (price, merchant, item, cart) invalidates it before payment.
- The audit chain verifier reports `Verified` on an untouched log and `Chain broken` after a manually tampered row.
- Every rejection produces a plain-language explanation with numbers; `policy_version` is recorded on every decision.
- Level 2 now has a real UI: the checkout flow shows a review step with the amount, reasoning, and Approve/Reject actions, and `/dashboard/approvals` is a working approve/reject queue.

## Remaining — Level 3 hard gate

Level 2 and Level 3 currently render through the **same** approval screen in the checkout flow. The spec requires Level 3 to be a distinct, non-dismissible hard gate: no background checkout button, no keyboard shortcut, and no stale authorization can bypass it, and the approval request should be re-fetched immediately before the user acts (to catch a changed amount/cart/merchant/policy version and force a return to review). Build a dedicated Level 3 screen distinct from the Level 2 review sheet — see `files/frontend-approval-ui.md` for the full spec (review sheet fields, confirmation-step for accidental clicks, expiry countdown, and the accessibility/security checklist).

**Do not consider Phase 3 fully closed until the distinct Level 3 hard-gate screen is built and demonstrated.**
