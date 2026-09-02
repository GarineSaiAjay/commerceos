# Prioritized Roadmap — 2026-08-30

Ties together `REALITY-CHECK-2026-08-30.md` and `PLAN-01` through
`PLAN-06`. Read the reality check first for the "why" behind every item
here; this document is only the "what order."

I don't know your buildathon submission deadline, so this is organized
by priority tier and dependency, not calendar dates — tell me the
deadline and I'll turn this into a day-by-day schedule.

## How to read the table

- **Tier** — P0 must happen regardless of anything else (correctness,
  security, or the literally-reported bug); P1 is what makes the
  submission genuinely stronger against this specific track; P2 is
  real polish; P3 is stretch, do only if time remains.
- **Judge-visible** — would a judge clicking through the demo actually
  notice this, or is it only visible in code/architecture review.
- **Effort** — rough person-days, single developer.

## P0 — do these first, regardless of what else you choose

| # | Item | Plan | Effort | Judge-visible |
|---|---|---|---|---|
| 1 | Auth-gate `POST/PATCH/DELETE /products` | 02 §5.1 / 05 | 1 hr | No (but it's a real vulnerability today) |
| 2 | Fix the upsell silence bug: decouple suggestion fetch from `step` | 03 §1 | 2–3 hrs | **Yes — this is the literal bug you reported** |
| 3 | Cross-sell inline in agent chat after accept | 03 §2 | 3–4 hrs | Yes |
| 4 | Persist suggestion dismissal server-side | 03 §5 | 3–4 hrs | Indirectly (prevents re-nagging) |
| 5 | Race LLM vs. deterministic extractor instead of serial 60s timeout | 04 B1 | 1 day | **Yes — directly cuts perceived agent latency** |
| 6 | Conversation memory for the agent chat (fixes "retype everything") | 01 §3 | 1 day | Yes |
| 7 | Show alternatives, not just top-1 pick | 01 §5 | 0.5 day | Yes |
| 8 | Fix Analytics' missing history list (dashboard consistency) | 05 | 0.5 day | Yes |
| 9 | Unify checkout/dashboard color palette | 04 A1 | 1 hr | Yes, subtle |

**P0 total: ~4–5 days.** This alone fixes the exact bug reported, closes
the one real security hole found, and measurably improves both
perceived latency and agent believability — the highest ratio of
judge-visible impact to effort in the whole plan set.

## P1 — track-differentiating work

| # | Item | Plan | Effort |
|---|---|---|---|
| 10 | Real product variants (colors/lengths/tiers) via seed data + picker | 02 §1 | 1 day |
| 11 | Reviews & ratings (real, order-linked) + growth-agent rating input | 02 §2 | 2 days |
| 12 | Client-side search/sort/filter on catalog | 02 §3 | 1 day |
| 13 | Product detail panel + "frequently paired with" | 02 §4 | 1 day |
| 14 | `/dashboard/catalog` management page | 05 | 2 days |
| 15 | `/dashboard/orders` page | 05 | 1.5 days |
| 16 | Extend audit trail with agent reasoning stages (intent/tool-calls/alternatives) | 01 §4 | 1 day |
| 17 | Unify in-app agent + MCP tools onto one tool layer | 01 §1 | 1–2 days |
| 18 | Bounded tool-calling agent loop | 01 §2 | 2–3 days |
| 19 | Cross-sell on product detail + post-checkout | 03 §3–4 | 1.5 days |
| 20 | Suggestion frequency cap + impression/acceptance tracking | 03 §6, §8 | 1.5 days |
| 21 | `checkout.tsx` component split | 04 A2 | 1–2 days |
| 22 | Apply existing `Skeleton` component to checkout screens | 04 A3 | 0.5 day |
| 23 | Catalog caching (client TTL + Redis) | 04 B2 | 1 day |
| 24 | `/dashboard/growth` funnel page | 05 | 1.5 days |

**P1 total: ~20–24 days** if done exhaustively — realistically, pick the
subset that fits your remaining time. If forced to choose a top 6 from
this tier: **17, 18** (the actual "agentic" upgrade — this is what most
directly answers "is it truly agentic"), **11** (reviews — the single
biggest catalog-depth lever), **14** (catalog dashboard — closes both a
UX gap and reinforces the P0 auth fix), **10** (variants — cheap,
meaningfully improves what the agent has to reason over), **19–20**
(finishes the cross-sell fix from a one-off patch into a real feature).

## P2 — polish

| # | Item | Plan | Effort |
|---|---|---|---|
| 25 | `/dashboard/settings` mandate view/edit | 05 | 1.5 days |
| 26 | Notification badges + persistent integrity banner | 05 | 1 day |
| 27 | CSV export | 05 | 1 day |
| 28 | Motion + accessibility pass | 04 A4–A5 | 1 day |
| 29 | Optimistic client-side cross-sell pre-score | 04 B3 | 1 day |
| 30 | Code-splitting rarely-used checkout panels | 04 B4 | 0.5 day |
| 31 | Response compression | 04 B5 | 0.5 day |
| 32 | CI-enforced catalog↔allowlist sync check | 02 §6 | 0.5 day |
| 33 | Proactive rejection-recovery agent turn | 01 §6 | 1 day |

## P3 — stretch, only if everything above is done

| # | Item | Plan | Effort |
|---|---|---|---|
| 34 | Rate limiting on LLM-backed endpoints | 06 §5 | 1 day |
| 35 | Agent-readable catalog manifest (`/.well-known/agent-commerce.json`) | 06 §2 | 1 day |
| 36 | Public `/trust` page | 06 §3 | 1 day |
| 37 | Root-level vision doc cleanup | 06 §6 | 0.5 day |
| 38 | Guided in-app demo mode | 06 §4 | 1.5 days |
| 39 | x402 payment-adapter stub | 06 §1 | 2–3 days |
| 40 | Multi-operator invite flow | 05 §7 | 2–3 days |
| 41 | Load/chaos testing pass | 06 §5 | 1 day |
| 42 | Event-driven cross-sell via Redis Streams | 03 §7 | 2–3 days |
| 43 | Pitch/README alignment re-check | 06 §8 | 0.5 day (do this last, once other items ship) |

---

## If you only have a few days

Do exactly the P0 table (items 1–9). It fixes the reported bug, closes
the one real vulnerability, and is the highest-leverage latency/
believability fix — all without touching the policy/audit architecture
that's already your strongest asset.

## If you have 1–2 weeks

P0 + the "top 6" P1 picks called out above (17, 18, 11, 14, 10, 19–20).
This is the set that most changes how a judge scores "is this truly
agentic" and "is the catalog/upsell real," which are exactly the two
things you flagged as weakest.

## What I need from you to turn this into a schedule

- Submission deadline (date/time, timezone).
- Whether you want me to start implementing now, and in what order —
  I'd suggest starting at item 1 (the security fix) and item 2 (the
  reported bug) immediately regardless of your answer to everything
  else, since both are small, safe, and high-value.
- Whether backend Go changes should go through the same branch-per-PR
  workflow documented in `files/GIT-WORKFLOW.md` (recommended — it's
  what's kept this repo's audit trail clean so far) or whether, given
  time pressure, some of the smaller P0 items should land more directly.....
