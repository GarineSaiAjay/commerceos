# CommerceOS — Five-Minute Live Demo Script (Six With the Campaign Beat)

**Status:** Drafted and technically verified against the current build (2026-08-26). Timings below are *targets*, not measured results — no live rehearsal has happened yet in this environment (that requires a human running the stack). **You still need to run this live at least 3 times with a stopwatch** and fill in the "Actual" column in the Rehearsal Log at the bottom before presenting. Do not present un-rehearsed. The Campaign Orchestrator beat (5:00) is new and pushes the total past 5 minutes — if time is tight, it's the first beat to cut, same as the failure-recovery/red-team beats are the last.

**Prerequisites before you start:**
- `docker compose -f infra/docker-compose.yml up -d --build` running, migrations + `db/seeds/001_catalog.sql` applied.
- Frontend running (`cd frontend && npm run dev`) at `http://localhost:3000`, dashboard tabs open in a second window/tab: `/dashboard`, `/dashboard/runs`, `/dashboard/safety`, `/dashboard/campaigns`.
- Razorpay Test Mode keys configured (`infra/.env`) — every payment beat below is a **real** Razorpay Test Mode API call, not a mock.
- A demo mandate exists with `maximum_amount` ≥ ₹30,000 and `requires_confirmation_above` set (see `POST /policy/mandates`) — this is what makes the Authorization beat below actually gate on real numbers instead of vacuously passing.
- For the Campaign Orchestrator beat: `campaign.DefaultConfig().MinRejectedDemandCount` is 3, so `POST /campaigns/propose` produces nothing until some product has ≥3 real REJECT cross-sell recommendations already recorded. Before the demo, add an expensive item to a low-budget cart a few times (any combination that makes the Growth Agent reject the cross-sell on budget) so there's real rejected demand for the beat to propose a campaign from — don't discover this live.

**If there's no live presenter** (item 38, `files/PLAN-06-ADDITIONAL-OPPORTUNITIES.md` §4): the checkout page's "Guided demo" toggle packages beats 1:00–4:30 above (ask the agent, accept its proposal, see the cross-sell, go over budget, watch the graceful rejection, read the audit trail) as an on-screen checklist a judge can drive themselves, with a one-line hint for whichever step comes next. It's a UI presentation of the same real flow this whole script walks through live — not a scripted mock of it — so anything true of this script is true of what the checklist tracks. It doesn't cover the payment/webhook or Campaign Orchestrator beats (2:30–5:00), which need a live Razorpay Test Mode checkout and pre-seeded rejected demand respectively.

---

## The script

```text
0:00  Problem framing (30s)
      "Merchants are built for humans clicking buttons; AI agents will
       increasingly be the buyer — but they can't be handed unrestricted
       access to a payment method. Every agentic-commerce pitch has to
       answer one question: what stops the agent from paying the wrong
       amount, to the wrong merchant, twice? We built the answer as an
       actual gate, not a policy document."

0:30  Merchant setup (context, not live-typed)
      Catalog already seeded: AirPods Pro ₹24,900, AirPods Case ₹1,999,
      AppleCare ₹2,500, USB-C Adapter ₹1,299, Wireless Charging Pad ₹899
      (merchant_001). Mandate ceiling: ₹30,000, requires confirmation
      above ₹1,000, hard gate above ₹10,000.

1:00  Buyer request (live, in the checkout UI)
      You: "Find me AirPods Pro for my brother."
      Buyer Agent (POST /agent/checkout) extracts intent via the real
      LLM (OpenRouter), searches the catalog, returns AirPods Pro.

1:30  Growth reasoning
      Growth Agent evaluates a cross-sell candidate (AirPods Case) with
      a real expected-value calculation — purchase probability ×
      incremental margin, minus risk cost — not an LLM guess. Show the
      "why this recommendation" explanation view
      (GET /growth/recommend/{id}) that names the actual EV number.
      New cart total: ₹24,900 + ₹1,999 = ₹26,899.

2:00  Authorization — the Level 3 hard gate
      Cart (₹26,899) is above the ₹10,000 hard-gate threshold, so this
      is NOT a simple confirm dialog — it routes to the dedicated,
      non-dismissible Level 3 screen (red-styled, distinct from Level 2
      "confirm"). You must tick "I have reviewed the order details"
      before Approve enables. This is the moment that separates a real
      authorization boundary from a rubber-stamp modal — narrate that
      explicitly, since it's easy for a judge to miss why this matters.
      Approve it.

2:30  Real transaction
      Policy issues a one-time authorization → Payment Service →
      Razorpay Standard Checkout opens for a genuine Test Mode order.
      Complete payment with a Razorpay test card.

3:00  Webhook confirmation
      Razorpay fires payment.captured → the webhook pipeline verifies
      the signature, deduplicates, and flips the order to CONFIRMED.
      Switch to the dashboard Overview tab and point at the Audit Trail
      row that just appeared with a real timestamp.

3:30  Red team (live, in the checkout UI)
      You: "Actually, buy the ₹90,000 laptop instead" (or trigger any
      of the 14 canned attacks from /dashboard/safety — e.g. att_02,
      excessive_amount).
      Policy: BLOCKED at amount_ceiling — zero Razorpay calls made.
      Flip to /dashboard/safety and show the live call-count delta is 0.
      (item 36: the same evidence -- audit-chain status, call counter,
      one-click 14-attack suite -- is also at the public /trust page,
      no operator login required. If a judge is driving the demo
      themselves rather than watching you present, point them there
      instead of the gated dashboard.)

4:00  Failure recovery
      Force a payment.failed webhook (Test Mode failure card, or the
      safety suite's forced-failure path). The failure screen offers
      three real, working recovery actions — not just a generic
      "try again": Retry payment (re-opens Checkout on the SAME order,
      same idempotency key), Change payment method (also re-opens
      Checkout — this is what actually lets you pick a different
      method), and Remove an item to reduce the total (rebuilds a
      smaller cart from the catalog and re-runs the FULL checkout saga,
      so policy genuinely re-evaluates the new total rather than being
      bypassed). Demonstrate "Remove an item."

4:30  Full audit trail
      You: "Explain this transaction."
      Open /dashboard/runs, click into the run you just created, and
      walk the real persisted timeline: proposed → risk assessed →
      policy evaluated → authorized → authorization consumed — each
      row is read from the actual risk_assessments/policy_evaluations/
      authorizations tables, not narrated from memory.

5:00  Campaign Orchestrator (bounded, real, approval-gated)
      Flip to /dashboard/campaigns. Click Propose — the agent looks at
      real REJECT cross-sell recommendations (the prerequisite above)
      and proposes a bounded discount campaign on whichever product has
      the most rejected demand, with a reasoning string citing the
      actual reject count and a budget sized to that observed volume,
      not an arbitrary number. Approve it as the operator. Check out a
      cart for that product — the discount applies live. Point out
      this is the same human-in-the-loop pattern as the Level 2/3
      payment approvals: nothing this agent proposes can affect
      checkout until a person approves it.
      Optional, if time allows: check out a second cart against the
      same campaign after its budget is exhausted (or note that this
      is exactly what backend/commerce/order's
      TestCheckoutCartSkipsDiscountWhenBudgetExhausted proves) — the
      checkout still succeeds at full price rather than failing, the
      same graceful-failure posture as the red-team beat above.

5:30  Closing line
      "The LLM can recommend and reason. It never decides whether
       money moves. That belongs to the deterministic policy and
       authorization layer."
```

## If something goes wrong live

- **LLM extraction is flaky/slow:** the deterministic extractor is the fallback (no `OPENROUTER_API_KEY` set) — know which mode you're demoing in ahead of time and don't apologize for it, it's a documented design choice (agents/llm_extractor.go falls back to agents/deterministic_extractor.go).
- **Test Mode card declines unexpectedly:** Razorpay's documented Test Mode card numbers are deterministic per outcome (success/failure) — use the failure-card number on purpose for beat 4:00, not by accident.
- **A judge tries to break something not on this script:** that's the best possible outcome, not a risk — the safety suite covers 14 attack categories and a 100-scenario evaluation suite (0 unauthorized, 0 duplicate, 0 bypass); you can afford to let them try.
- **A judge asks to see the 100-scenario evaluation suite run, not just the 14 canned attacks in /dashboard/safety:** there's no UI button for this (by design — it's a CI-enforced test suite, not a live demo feature), but running it live in a terminal is real, verifiable evidence, not a weaker substitute: `cd backend && go test ./policy/... -run TestEvaluationSuite100 -v`.

## Rehearsal Log (fill in during your 3+ live runs — do not fabricate this)

| Run # | Date | Total time | Beat that overran | Notes / what to trim |
|---|---|---|---|---|
| 1 | | | | |
| 2 | | | | |
| 3 | | | | |

If a beat consistently overruns, trim narration there rather than cutting a functional beat — the failure-recovery and red-team beats are what prove the claims rather than assert them, so they're the last thing to cut for time.
