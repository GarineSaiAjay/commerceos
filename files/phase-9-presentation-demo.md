# Phase 9 — Presentation (Dashboard Polish & Demo Readiness)

**Prerequisite:** Phase 8 mostly verified (see remaining items there).

**Status: artifacts drafted 2026-08-26.** `files/demo-script.md` (the timed five-minute script below, refined against the current build) and `files/pitch-one-pager.md` (the one-page pitch) now exist. **What remains is entirely yours**: the live rehearsal itself. No agent can run a stopwatch against a real conversation with a judge — see the Rehearsal Log in `files/demo-script.md` for where to record your 3 practice runs.

**Governing principle:** the system must be demonstrable end-to-end, live, in under five minutes, with every claim backed by something a judge can see happen in real time — not a slide, not a canned screenshot.

---

## 1. Merchant Dashboard Polish

Bring the following into one coherent screen (each number sourced from a real Phase 1–8 artifact, never freshly hardcoded for the demo):

```text
Revenue: ₹4,82,900
AI-attributed revenue: ₹37,420 (↑18.4%)
Conversion: 7.8% (↑1.9%)
Average Order Value: ₹2,840 (↑₹420)
AI Actions: cross-sell suggested · intent classified · cart optimized · payment authorized · payment captured
Audit Trail: 14:31:02 recommendation → 14:31:04 approved → 14:31:05 policy pass → 14:31:05 order created → 14:31:42 captured
Audit Integrity: Events 127 · Verified ✓ · Chain broken: No · Root hash 8d3a...
Agent Safety Evaluation: 100 scenarios · 0 unauthorized · 0 duplicate · 98% graceful failure
```

The dashboard's Overview screen already sources revenue/AI-attributed revenue/conversion/AOV, AI Actions, Audit Trail, Audit Integrity, and the Agent Safety Evaluation summary from the real Phase 3/6/8 endpoints — this section is largely satisfied structurally. What's left is a final pass for visual hierarchy so this is legible as the single screen judges look at most.

## 2. Rehearse the Closing Framing

Do **not** pitch this as "we built an AI payment agent." Pitch it as:

> **"We built the trust layer for agentic commerce."**

Map every component to its conceptual role explicitly when presenting:

| Component | Represents |
|---|---|
| AI (LLM) | Reasoning |
| Policy Engine | Permission |
| Authorization / Mandate | Consent |
| Payment Engine | Execution |
| Webhook / Event System | Truth |
| Audit Ledger | Accountability |

Practice stating this mapping out loud, unscripted, so it doesn't sound read-off-a-slide during the actual presentation.

## 3. Rehearse the Five-Minute Demo Script

Run this live, end to end, **at least 3 times** before presenting — including the failure and red-team beats, since those are the moments that differentiate this from a thin wrapper:

```text
0:00  Problem framing (30s)
      "Merchants are built for humans clicking buttons; AI agents will
       increasingly be the buyer — but can't get unrestricted money access."

0:30  Merchant setup
      Ingest catalog → generate AI-readable schema
      Merchant sets ₹30,000 spending ceiling

1:00  Buyer request
      Judge: "Find me something for my brother under ₹25,000"
      Agent searches catalog

1:30  Growth reasoning
      Agent proposes product + compatible accessory, shows expected value

2:00  Authorization
      Agent → Policy: Proposed cart ₹26,899 (limit ₹30,000, risk LOW)
      Policy presents for approval → Judge approves

2:30  Real transaction
      Policy → Razorpay: Create Test Mode order → Payment completed

3:00  Webhook confirmation
      Razorpay → Agent: payment.captured → ORDER_CONFIRMED

3:30  Red team
      Judge: "Buy ₹90,000 laptop"
      Policy: BLOCKED — no Razorpay call made

4:00  Failure recovery
      Razorpay → Agent: payment.failed (forced)
      Agent: "Payment failed. No duplicate charge. Cart preserved.
              Recovery options shown."

4:30  Full audit trail
      Judge: "Explain this transaction"
      Agent: Intent → Recommendation → Policy → Authorization →
              Order → Payment → Webhook → Order

5:00  Closing line
      "The LLM can recommend and reason. It never decides whether
       money moves. That belongs to the deterministic policy and
       authorization layer."
```

Time each rehearsal run explicitly with a stopwatch — if any beat consistently overruns, trim narration there rather than cutting a functional beat (the failure-recovery and red-team beats are the most important to keep; they're what proves the claims rather than asserting them).

## 4. Final Anti-Pattern Check

Walk the finished system against this list and confirm none of these crept in anywhere:

| Anti-pattern | Why it's weak |
|---|---|
| Generic chatbot ("ask our AI anything") | Low differentiation |
| Thin Razorpay wrapper (`LLM → create_order()`) | No architectural story |
| Fake analytics with fabricated uplift numbers | Undermines credibility once questioned |
| Autonomous payments with no controls | Directly contradicts "bounded and gated" |
| Blockchain "because commerce" | Adds complexity with no matching requirement |
| Seven microservices for architecture theatre | A modular monolith is the right size for a prototype |

---

## Phase 9 — Full Artifact List

- Finished, polished dashboard (single coherent screen) — mostly built, needs a final visual pass
- Demo script written — `files/demo-script.md` (2026-08-26). **🫵 you**: rehearse it live 3+ times and fill in the Rehearsal Log at the bottom with actual timings.
- One-page pitch summary mapping components to their conceptual roles — `files/pitch-one-pager.md` (2026-08-26)

---

## Phase 9 — Verification Checklist

- [ ] The Five-Minute Demo Script (`files/demo-script.md`) runs live, start to finish, without a single manual DB edit or "pretend this worked" moment
- [ ] Every dashboard number shown during the demo is real (either a live Test Mode transaction or a clearly labeled simulated experiment — nothing ambiguous)
- [ ] The failure-recovery beat (forced `payment.failed`) and the red-team beat (blocked ₹90,000 attempt) both fire correctly in the live run-through, at least twice in rehearsal
- [ ] None of the six anti-patterns above are present in the final build
- [ ] A person unfamiliar with the project can watch the demo once and correctly state, afterward, "the LLM never directly moved money" — if they can't, the presentation isn't done yet

**Do not start the demo rehearsal until Phase 8's remaining threat-coverage gaps are closed — a judge red-teaming this live is exactly what Phase 8 needs to withstand.**

---

## Master Verification Checklist (Whole-System Confirmation)

Before calling the build complete, confirm every item below in one continuous session — ideally by literally running the Five-Minute Demo Script live:

**Commerce Core**
- [x] Real Razorpay Test Mode transaction completes successfully, end to end
- [ ] A forced payment failure is handled gracefully with zero duplicate charge — recovery works but the "remove accessory" option (Phase 2) is still missing

**Reliability**
- [x] Duplicate webhook delivery is deduplicated correctly
- [x] Outbox pattern prevents event loss on a simulated crash
- [x] Idempotent payment commands never create a second payment

**Authorization**
- [x] An over-limit action is rejected before any Razorpay call is made
- [x] Stale-authorization scenario blocks correctly with a clear explanation
- [ ] All three authorization levels (auto-approve, confirm, hard gate) are demonstrated correctly — L1/L2 yes, L3's distinct hard-gate screen is still missing (Phase 3)
- [x] The audit log's hash chain verifies, and a tampered entry is detected

**Intelligence**
- [x] The Buyer Agent correctly extracts intent and builds a compliant cart from a natural-language prompt
- [x] The Growth Agent's cross-sell decision matches the deterministic expected-value calculation, not an LLM guess
- [x] Every recommendation has a working, accurate explanation view

**Analytics**
- [x] Dashboard numbers are traceable to real data; simulated figures are clearly labeled as simulated

**Agent Interface**
- [ ] An external MCP client can call the Commerce MCP tools and get correct results — not yet driven from Claude Desktop/Inspector (Phase 7)
- [x] No MCP tool calls Razorpay directly — all route through the Policy Engine

**Security**
- [ ] All red-team attacks are blocked with zero Razorpay API calls — the 10 canned attacks pass; 3 threat categories (tool injection, data exfiltration, goal hijacking) have no dedicated test yet (Phase 8)
- [x] The 100-scenario evaluation suite reports 0 unauthorized payments, 0 duplicate payments, 0 policy bypasses

**Presentation**
- [ ] The full five-minute script (`files/demo-script.md`, written 2026-08-26) runs live without manual intervention — not yet rehearsed

If every box above is checked against a real, observed run — not assumed — the build is complete.
