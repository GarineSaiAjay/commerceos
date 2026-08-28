# Phase 5 — Growth Agent (Cross-Sell, Upsell, Bundling)

**Status: ✅ COMPLETE — fully verified against a live observed run.**

The Growth Agent, its deterministic expected-value scoring module, the `recommendations` table, the Merchant Simulator dataset, and the explanation endpoint are all built and verified:

- Budget-aware rejection (no tolerance) and tolerance-aware eligibility (+10%) both reproduce the spec's exact numbers live.
- The EV formula (`P(purchase) × margin × confidence − risk cost`) is a pure, unit-tested function independent of any LLM call; the engine picks the argmax regardless of candidate order.
- `/growth/recommend/{id}` returns a real explanation populated from the `recommendations` table.
- A cross-sell candidate that would push the cart over the mandate ceiling produces a `policy_evaluations` row and is blocked by the Policy Engine — no shortcut around authorization.

The Phase 5 stretch goal (full Merchant Agent campaign flow) was explicitly out of scope and was not attempted.

No remaining tasks for this phase. See `PROJECT-AUDIT.md` for the full history of what was built and fixed.
