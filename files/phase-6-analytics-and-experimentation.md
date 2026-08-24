# Phase 6 — Analytics & Experimentation

**Prerequisite:** Phase 5 fully verified (EV scoring works, Merchant Simulator dataset exists, recommendations flow through the Policy Engine).
**Governing principle:** every revenue/conversion/AOV claim in the dashboard must be traceable to a real number — either a live event from the actual Test Mode transaction flow, or a clearly labeled simulated experiment. Never assert an uplift percentage without one of those two backing it, and never let the two be visually or structurally ambiguous with each other.

---

## 0. Objective of This Phase

Build the analytics layer that makes the "revenue agent" claim tangible to a judge or merchant: real dashboard metrics computed from actual event data, plus a proper experimentation engine that reports simulated uplift the way a real A/B test would be reported — population split, lift, confidence interval — never as a bare asserted percentage.

---

## 1. Core Dashboard Metrics

Compute these from real event data (the event bus and DB tables built in Phases 1–2), never hardcoded:

```text
Revenue: ₹4,82,900
AI-attributed revenue: ₹37,420 (↑18.4%)
Conversion: 7.8% (↑1.9%)
Average Order Value: ₹2,840 (↑₹420)
```

1. Build an Analytics service (`/backend/analytics`) that subscribes to the Redis Streams event bus (built in Phase 2) and aggregates: total revenue, AI-attributed revenue (orders where a Growth Agent recommendation was accepted), conversion rate, and AOV.
2. These numbers should be computed live/on-demand from the underlying tables, or via a materialized aggregate refreshed on a schedule — either is fine, but there must be no hardcoded metric value anywhere in the dashboard code.

## 2. Experimentation Engine

Run controlled comparisons using the Merchant Simulator data from Phase 5, instead of asserting a number:

| Metric | Control | AI Cross-sell | AI Bundle |
|---|---|---|---|
| Conversion Rate | 5.9% | 7.4% | — |
| AOV | ₹2,410 | ₹2,930 | — |
| Revenue / Session | ₹142 | ₹217 | — |
| Refund Rate | 2.1% | 2.0% | — |

1. Split the simulated session population into control/treatment groups.
2. Compute each metric per group from the simulated dataset.
3. Store results in `experiments` / `experiment_assignments` tables.

## 3. Causal-Style Experiment Reporting

Report simulated results the way a real A/B test would be reported — with population split and a confidence interval, not a bare percentage:

```text
Experiment:  AI Cross-sell v3
Population:  10,000 sessions (5,000 control / 5,000 treatment)
Metric:      Revenue / session
Control:     ₹182.40
Treatment:   ₹214.80
Lift:        +17.76%
95% CI:      [+12.4%, +23.1%]
```

1. Build an experiment report generator that computes lift and a confidence interval (a standard two-proportion or two-mean CI calculation is sufficient — bootstrap or normal-approximation, your choice) from the simulated population.
2. This must be a **real calculation**, not decorative text — see the verification checklist below for how this gets proven.

## 4. Label Everything Simulated as Simulated

1. Any number sourced from the Merchant Simulator must be visually **and** textually labeled "Simulated / historical" in the UI — a badge, a distinct panel color, or a separate section header, not a small footnote.
2. Keep the **real, unscripted Razorpay Test Mode transaction flow** visually and structurally separate from simulated analytics. A judge glancing at the dashboard should never be able to mistake a simulated number for a live one, or vice versa.

## 5. AI Actions Feed + Audit Trail Widget

Add to the dashboard:

```text
AI Actions: cross-sell suggested · intent classified · cart optimized · payment authorized · payment captured
Audit Trail: 14:31:02 recommendation → 14:31:04 approved → 14:31:05 policy pass → 14:31:05 order created → 14:31:42 captured
```

1. The "AI Actions" feed pulls from `agent_actions` (Phase 3).
2. The "Audit Trail" timeline pulls from `audit_events` (Phase 2/3) for a specific transaction, rendered in chronological order with real timestamps — not a mocked sequence.

---

## Phase 6 — Full Artifact List

- `experiments`, `experiment_assignments` tables
- Analytics service computing metrics from real event-bus data
- Experiment report generator (control vs. treatment, with CI calculation)
- Dashboard widgets: revenue/conversion/AOV panel, AI actions feed, audit trail timeline
- Clear visual/textual "Simulated" labeling system for anything sourced from the Merchant Simulator

---

## Phase 6 — Verification Checklist

> **Progress note (updated after an observed run):**
> - ✅ Live metrics (`/dashboard/metrics`) computed from real DB rows (revenue ₹43,397 from captured payments, conversion 35%, AOV ₹5,914) — `simulated: false`, no hardcoded literals in the service.
> - ✅ Experiment generator (`/dashboard/experiment`) is a real calculation: 50k simulated sessions split 25k/25k, control ₹180.38 vs treatment ₹266.18, lift +47.6%, CI [+45.0%, +50.2%]; running twice on seed 42 returns identical lift & CI (unit test + live).
> - ✅ AI Actions feed source (`agent_actions`) and audit trail timeline source (`audit_events`) both populated with real timestamps.
> - ⚠️ The visual dashboard UI (distinct simulated labeling panel) is served via the dashboard API but the browser wasn't driven in this sandbox; the backend returns `source: "simulated"` to enable the distinct labeling.

- [x] Every number on the live dashboard is traceable to a real DB row/event — no hardcoded metric in the dashboard code (verified: metrics computed via SQL aggregation from `payments`/`orders`/`carts`)
- [ ] Simulated experiment numbers are visually distinct from live transaction data (backend returns `source: "simulated"`; the visual label/panel requires the frontend, which was not driven in this sandbox)
- [x] Running the experiment generator twice on the same fixed seed produces the **same** lift and CI (unit test + two live runs were identical)
- [x] The audit trail timeline source (`audit_events`) matches real rows in order and timestamp (verified in DB)
- [x] No "X% increase" claim is asserted without either a labeled simulated experiment or a live number pulled from real event data (experiment response includes `source: "simulated"`; metrics are computed live)

**Do not start Phase 7 until every box above is checked against an actual observed run.**
