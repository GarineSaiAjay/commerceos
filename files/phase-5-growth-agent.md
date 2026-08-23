# Phase 5 — Growth Agent (Cross-Sell, Upsell, Bundling)

**Prerequisite:** Phase 4 fully verified (Buyer Agent produces valid Proposed Actions, contract documented).
**Governing principle:** every cross-sell/upsell recommendation is backed by an explicit, auditable expected-value calculation. The LLM never assigns a revenue-impact number itself — "I think this will increase revenue" is exactly the failure mode this phase exists to prevent.

---

## 0. Objective of This Phase

The Growth Agent proposes additional items (accessories, bundles, upgrades) to a cart already built by the Buyer Agent — but every proposal is scored by a deterministic expected-value formula, filtered by budget-awareness logic, and routed through the same Phase 3 Policy Engine as any other proposal. There is no shortcut around authorization for cross-sell items just because they're "only an accessory."

---

## 1. Budget-Aware Reasoning Before Proposing Anything

1. Reject candidates that exceed budget outright, with no tolerance applied:
   ```text
   Cart: ₹24,900   Budget: ₹25,000   Margin available: ₹100
   Candidate: AirPods Case (₹1,999)  →  REJECT (exceeds budget)
   Candidate: USB-C Adapter (₹1,299) →  REJECT (exceeds budget)
   ```
2. Respect explicit buyer-signaled flexibility. If the buyer says something like "around ₹25k is okay," apply a budget tolerance (e.g. +10%) and re-evaluate:
   ```text
   Current cart:        ₹24,900
   Cross-sell candidate: AirPods Case, ₹1,999
   New total:            ₹26,899
   Budget tolerance:     +10% → max allowed ₹27,500
   Result:                ELIGIBLE
   ```
3. Implement the tolerance-detection step (does the buyer's language signal flexibility?) as an LLM classification with a validated output (a boolean or an explicit tolerance percentage), and implement the arithmetic comparison (`new_total ≤ max_allowed`) as pure deterministic code — never let the LLM do the comparison itself.

## 2. Expected-Value Scoring Formula

Compute this deterministically for every candidate — the LLM never assigns this number:

```text
Expected Incremental Revenue = P(purchase | recommendation) × incremental margin × confidence − risk cost
```

Example:

| Candidate | P(purchase) | Margin | Confidence | Expected Value |
|---|---|---|---|---|
| A | 0.21 | ₹600 | 0.85 | ₹107.10 |
| B | 0.13 | ₹1,000 | 0.95 | ₹123.50 |

→ The engine picks **B**, the argmax, regardless of which candidate the LLM happened to mention first in its reasoning.

1. Implement this scoring function as a pure, deterministic, independently unit-testable module (no LLM call inside it).
2. `P(purchase | recommendation)`, `margin`, and `confidence` inputs can come from historical data (Phase 5 §5, the Merchant Simulator) or simple heuristics for now — what matters is that the *formula* is deterministic given those inputs, not that the inputs themselves are perfectly calibrated.

## 3. Division of Responsibility (enforce in code structure, not just intent)

| Layer | Responsible for |
|---|---|
| LLM | Semantic reasoning, intent interpretation, explanation, natural language |
| Deterministic engine | Prices, limits, eligibility, expected value, money movement, authorization |

Structure the code so this division is visible: the LLM-calling code and the deterministic scoring/policy code should live in clearly separate modules with a narrow interface between them (candidate list in, scored+filtered list out), not interleaved.

## 4. Explainability Endpoint

Every recommendation must be clickable and produce a real explanation, populated from actual computed values — not placeholder text:

```text
RECOMMENDATION EXPLANATION
Product:  AirPods Case
Reason:
  • Compatible with selected product
  • 18% of similar buyers purchase it
  • Expected margin: ₹600
  • Current cart remains within authorization
  • Buyer expressed willingness to consider accessories
Expected incremental revenue: ₹107.10
Confidence: 84%
Policy: cross_sell_policy_v4
Decision: RECOMMEND
```

Build this as an actual endpoint/UI view that pulls from the `recommendations` table (section artifact below), not a static template.

## 5. Merchant Simulator

Rather than sourcing real merchant data, synthesize an environment so the Growth Agent has something to learn from and reason over:

```text
10,000 customers · 50,000 sessions · 2,000 products
historical purchases · clicks · cart additions · abandoned carts · returns
```

1. Build a generator script that produces this synthetic dataset with a fixed random seed (so it's reproducible).
2. Use it to demonstrate segment-level reasoning, e.g.:
   ```text
   Segment: "Budget-conscious students"
     High-probability bundle: laptop + mouse
     Low-probability bundle:  premium accessories
   ```
3. This dataset also feeds Phase 6's experimentation engine — build it with that reuse in mind.

## 6. Growth Agent Proposals Still Flow Through the Policy Engine

A cross-sell candidate is a Proposed Action like any other Phase 3 proposal. It does **not** get a shortcut around authorization just because it originated from the Growth Agent rather than the Buyer Agent. Confirm this by checking that adding a cross-sell item still produces a `policy_evaluations` row, same as a primary-item purchase would.

## 7. Merchant Agent (stretch, if time allows)

Given a goal like *"increase revenue without pushing refund rate above 3%,"* run:
```text
analyze → identify opportunity → create campaign → select audience → choose offer → run experiment → measure → adapt
```
This is optional — attempt only after sections 1–6 are fully verified.

---

## Phase 5 — Full Artifact List

- `recommendations` table (candidate, EV components, decision, policy version)
- Expected-value scoring module (pure deterministic function, unit-testable independent of any LLM call)
- Merchant Simulator dataset + generator script (fixed seed, reproducible)
- Explanation endpoint/UI
- (Stretch) Merchant Agent flow

---

## Phase 5 — Verification Checklist

- [ ] Given the AirPods Pro + AirPods Case scenario at exactly ₹25,000 budget with no tolerance signaled, the case is **REJECTED** with the exact "over budget" reasoning shown
- [ ] With buyer-signaled flexibility, the same scenario at +10% tolerance is **ELIGIBLE**, and the math (₹26,899 ≤ ₹27,500) is shown correctly
- [ ] Given the two-candidate EV table (A: ₹107.10, B: ₹123.50), the engine selects candidate B, and re-running the *same inputs* through the EV formula by hand reproduces the same numbers — no hidden LLM-supplied fudge factor anywhere in the computation
- [ ] The EV scoring function has unit tests independent of any LLM call (pass hardcoded P/margin/confidence, assert the exact output)
- [ ] Every recommendation shown to a buyer has a working "why" / explanation view populated with real values, not placeholder text
- [ ] A cross-sell candidate that would push the cart over the mandate ceiling is blocked by the **Policy Engine**, not silently filtered by the Growth Agent — confirm by checking a `policy_evaluations` row exists for the rejected candidate

**Do not start Phase 6 until every box above is checked against an actual observed run.**
