> **Historical document.** This plan lists eight stretch/hardening
> opportunities identified on 2026-08-30. A fresh audit against the
> real codebase (2026-09-04) found every one of §1-§8 has since shipped
> -- rate limiting, the agent-readable manifest, the public trust page,
> guided demo mode, the root-doc cleanup, and the x402 stub are all
> live and wired, each behind its own merged PR. Kept in full below for
> design-history reference, with a **Shipped:** note added under each
> section pointing at the actual files.

# Plan 06 — Additional Opportunities and Track Alignment

Depends on: `REALITY-CHECK-2026-08-30.md`, all prior plans. This is the
"other ideas" document — track-alignment moves and judging-day hardening
that don't belong inside any single plan above.

---

## 1. x402 payment-rail adapter (stretch, but cheap given existing design)

> **Shipped:** `backend/commerce/payment/x402/` -- a standalone
> handler for the demo scenario, scoped exactly as this section asked
> ("one code path, one demo scenario, not a general x402 client"). Its
> own README is explicit that x402 does NOT fit `payment.PaymentAdapter`
> the way this section's framing assumed ("Why this isn't
> payment.PaymentAdapter") -- the "one-line change" premise quoted
> below was accurate for a rail shaped like Razorpay's own
> merchant-initiated flow, not for x402's resource-initiated one. Both
> `adapter.go` and `main.go` have since been corrected to stop citing
> x402 as an example of that one-line swap.

`backend/commerce/payment/adapter.go`'s `Adapter` interface was already
designed for this — its own comment: *"swapping the real rail (Razorpay)
for a synthetic one (mock, x402, …) is a one-line change."* A minimal
`X402Adapter` (test-mode only, handling the HTTP 402 challenge/response
handshake for a single fixed scenario) is a concrete, demoable artifact
that speaks directly to the track's "why now" framing (x402 named
explicitly in the brief). Scope it small: one code path, one demo
scenario, not a general x402 client. Even a partial, clearly-labeled
implementation is a stronger signal than none, given how directly it
answers the brief.

## 2. Agent-readable catalog manifest

> **Shipped:** `GET /.well-known/agent-commerce.json`
> (`backend/mcp/manifest.go`), generated live via reflection off
> `s.Tools()`/`policy.Mandate`/`policy.ProposedAction` -- no
> hand-duplicated schema, exactly as this section asked.

The track's own example directions list "Agent-readable catalog" as a
first-class direction. This project has the content
(`files/agent-commerce-contract.md`) but not the machine-readable form.
Publish `GET /.well-known/agent-commerce.json` (or similar) describing:
the MCP endpoint (`POST /mcp`), the 11 tools with their schemas (can be
generated directly from `backend/mcp/tools.go`'s `RegisterTools` at
startup — no hand-maintained duplicate), the mandate/policy model in
structured form, and example flows. This turns prose documentation into
something an external agent (or a judge's own tooling) can fetch and
act on directly — cheap, additive, and it's the literal example
direction named in the brief.

## 3. Public, judge-friendly audit verification

> **Shipped:** `backend/trust/handler.go` (`GET /trust/summary`,
> `POST /trust/run-suite`, unauthenticated, cooldown-guarded) +
> `frontend/app/trust/page.tsx`, both wired.

`GET /adapter/calls` (the Razorpay call-counter proof) and the audit
verifier (`audit/verifier.go`) already exist and are genuinely strong
evidence — "blocked actions never hit the provider" is provable, not
asserted. Today this evidence lives inside the gated dashboard and
requires reading code to find. Add a small, explicitly public (no
operator auth — this data is meant to be shown off, not protected)
`/trust` page: shows the current audit chain integrity status, the
adapter call counter, and a one-click "run the 14-attack suite and show
me the results" — essentially a curated, always-available version of
what a judge would otherwise have to find inside the gated Safety page.
This is a presentation change, not a new backend capability — every
number it shows already exists and is already correct.

## 4. Guided demo mode

> **Shipped:** `frontend/app/checkout/DemoGuide.tsx`, toggled by a
> visible "Guided demo" button in `checkout.tsx`, tracking the exact
> six milestones this section describes.

The app already has a genuinely strong "one failure handled gracefully"
story (the policy-rejection screen with Remove buttons, the safety
attack suite, budget-exceeded → campaign-orchestrator flow). Package it
as an optional, explicit **guided demo toggle** in the UI (not hidden in
`files/demo-script.md`, which only the presenter sees) — a small
"walkthrough" affordance that highlights, in order: ask agent → accept →
cross-sell appears → attempt an over-budget item → see the graceful
rejection → open the audit trail. This turns the existing demo script
into something a judge can self-drive without a live presenter, which
matters if judging is asynchronous or time-boxed per submission.

## 5. Load and chaos readiness for judging day

> **Shipped -- all three sub-items:** `scripts/loadtest/k6_load_test.js`
> (targets `/products`, `/agent/checkout`, `/growth/suggest`); pool
> sizing is explicit in `backend/infra/db/postgres.go`
> (`MaxConns=10, MinConns=2, MaxConnLifetime=1h`); and
> `backend/ratelimit/limiter.go` (a token-bucket limiter) is wired onto
> both `/agent/checkout` and `/agent/loop` in `main.go`.

At buildathon scale (~20k applicants, some fraction actually clicking
through live demos), concurrent load on a single Postgres/Redis pair
is a real risk that hasn't been tested. Concrete, cheap prep:
- A basic load test script (k6 or plain Go, hitting `/products`,
  `/agent/checkout`, `/growth/suggest` concurrently) to find the actual
  breaking point before a judge does.
- Confirm connection pool sizing in `backend/infra/db/postgres.go`
  is set explicitly rather than left at pgx defaults, given the added
  concurrent read load `PLAN-04`'s caching work is designed to reduce
  but not eliminate.
- Rate-limit the two LLM-backed endpoints (`/agent/checkout`, and the
  future agentic-loop endpoint from `PLAN-01`) — **currently no rate
  limiting exists anywhere in the codebase.** A public judging URL with
  an unmetered path to a paid LLM API is a real cost-control gap, not
  just a robustness one. A simple per-IP token bucket is sufficient;
  doesn't need to be sophisticated.

## 6. Root-level vision docs — resolve the stale-doc problem properly

> **Shipped:** both root-level docs moved to
> `files/ORIGINAL-VISION-*.md` with an explicit "Historical document"
> banner, alongside the pre-existing "as-built vs. as-designed" note.
> The repo root now has only `README.md`.

`REALITY-CHECK-2026-08-30.md`'s predecessor
(`files/AUDIT-2026-08-29.md`) already flagged that the two root-level
`.md` design docs describe a fully autonomous "Merchant Agent" with
audience segmentation that was never built, while what shipped (the
Campaign Orchestrator) is narrower and more responsibly gated — arguably
*better*, but the docs still describe the unbuilt version and are the
first files anyone browsing the repo root opens. This was previously
patched with an "as-built vs as-designed" note; recommend going further
before judging: either trim these two docs down to a short "original
vision, superseded by what's actually built — see README" pointer, or
move them into `files/` as explicitly historical, so the repo root's
first impression matches the real, more disciplined thing that got
built.

## 7. CORS / production readiness

> **Confirmed:** `FRONTEND_ORIGIN` is env-driven
> (`backend/cmd/server/main.go`, `.env.example`) -- this was always a
> deploy-config checklist item, not a code change, and stays that way.

`FRONTEND_ORIGIN` is now env-overridable (fixed this session) but is
still a single value. If judging involves a public URL different from
`localhost:3000`, confirm this is set correctly in whatever hosting
config is used — worth a pre-flight checklist item, not a code change.

## 8. Pitch/README alignment check

> **Done:** `files/pitch-one-pager.md`'s MCP claim is now accurate and
> hedged ("any MCP client that speaks simple request/response... one
> honestly-documented gap" re: SSE transport) rather than the overclaim
> this section flagged. As this section itself notes, treat this as a
> standing pre-submission checklist, not a one-time fix -- re-check
> before actually submitting if anything else changes.

Per the prior audit, `files/pitch-one-pager.md` previously claimed MCP
supported "any MCP-compatible client" while the add-item gap made that
false — that specific gap is now fixed (§0 of the reality check), so
the claim is now true. Recommend one more pass over
`pitch-one-pager.md` and `README.md` once `PLAN-01`–`PLAN-05` land, to
make sure every claim in the pitch doc still matches the shipped
system exactly — this project's own audit history shows this drifts
easily and quickly, so treat it as a standing pre-submission checklist
item, not a one-time fix.

---

## Priority within this document

> **Status (2026-09-04): every item below has shipped.** Kept as a
> record of the original priority ordering, not a live tracker.

Most of the above is stretch-tier — the core track bar is already met
by the existing policy/audit/safety architecture plus the fixes in
`PLAN-01`–`PLAN-05`. Recommended order if time remains after those:

1. §5 rate limiting (cheap, closes a real cost/robustness gap)
2. §2 agent-readable manifest (cheap, directly named in the brief)
3. §3 public trust page (cheap, presentation-only, high judge-visibility)
4. §6 root-doc cleanup (cheap, first-impression fix)
5. §4 guided demo mode (medium effort, high demo value)
6. §1 x402 adapter stub (medium effort, directly named in the brief, but
   the highest-effort item on this list relative to its demo payoff —
   only take this on if the higher-priority plans are done)
