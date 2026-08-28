# Judge-Facing Gaps — What Could Cost You the Shortlist

> **Purpose:** This document audits something different and,
> for a buildathon, more decisive: **what a judge sees when they open the
> app in a browser and click around for five minutes**, with your track's
> actual grading bar in mind. Those two views have diverged — the backend
> is genuinely sophisticated, but almost none of that sophistication is
> visible from the shopper-facing screen a judge will open first. That gap
> is the single biggest risk to getting shortlisted, bigger than any
> remaining backend polish.
>
> Everything below was verified by reading the current code on
> 2026-08-26, not assumed. File/line references are given so you can jump
> straight to each spot.
>
> **Track, for reference (re-read this before triaging anything else):**
> *"Grow the merchant's revenue, and make them sellable to AI buyers... Build
> an agent that grows revenue for a merchant on Razorpay test-mode APIs, or
> that makes a merchant transactable by an AI buyer end to end... the bar:
> every money action explainable, bounded and gated. Show the audit trail
> and one failure handled gracefully."*

---

## How to read this doc

Items are grouped **P0 (fix before you submit — these directly threaten
shortlisting for THIS track), P1 (fix if you have 1-2 more days), P2
(polish, do last)**. Your own three points are folded in with the code
evidence behind them, plus everything else that surfaced while checking.

---

## P0 — Existential for this specific track

### P0.1 — The AI is real and working, but completely invisible in the browser

> **Status: implemented 2026-08-26.** A conversational entry point (calling the real `POST /agent/checkout`) now sits above the catalog, and a cross-sell suggestion card (backed by a new `POST /growth/suggest` endpoint) now appears in the cart step -- see the Fix Log convention in `PROJECT-AUDIT.md` for how these are described. `tsc --noEmit` and `eslint` are clean on the frontend change; the Go changes could not be compiled in this environment (no Go toolchain here) and were hand-verified instead, same caveat `PROJECT-AUDIT.md` already carries for its own Go edits -- build/test this locally before the demo.

This is your point #1, and the code confirms exactly why it happens: **the
AI backend is built and functional, but the frontend never calls it.**

Concretely:

- `backend/agents/llm_extractor.go` is a real, working LLM-backed intent
  extractor (OpenRouter, configurable model). `backend/agents/handler.go`
  exposes it as `PlanCheckout`, mounted at `POST /agent/checkout` (corrected below -- the actual mounted path; this doc originally said /agents/plan)
  (`backend/cmd/server/main.go:492`). This is the "conversational in-app
  checkout" the track literally names as an example direction — **and it
  has no UI.** `frontend/app/checkout.tsx` (930 lines) never references
  `/agent/checkout`, `intent`, or anything conversational — confirmed by
  grep, zero matches. There is no text box, no chat, nothing where a
  shopper types "I need noise-cancelling earbuds under 3k" and an agent
  responds. It only exists behind `curl`.
- `backend/growth/agent.go` (the cross-sell/upsell agent) is wired to
  `POST /growth/evaluate` and `GET /growth/recommend/{id}`
  (`main.go:497-503`). The **only** place this shows up in the entire
  frontend is one static metric card on the merchant dashboard —
  `"AI-attributed revenue"` (`frontend/app/dashboard/merchant-dashboard.tsx:127`)
  — which will read ₹0 unless someone manually calls the growth endpoints
  out-of-band, because the checkout flow never calls them either. A judge
  clicking through the actual shopper checkout will never see a
  recommendation, an upsell prompt, or any agent reasoning surfaced to
  them.
- Net effect: a judge who opens `localhost:3000` and clicks through a
  purchase sees a plain product list → cart → pay flow, indistinguishable
  from a checkout tutorial project. The two most track-relevant
  capabilities you've actually built (LLM intent parsing, agent-driven
  recommendations) are real but **demo-invisible**, which is worse than
  not having built them, because you'll spend the demo explaining code
  instead of showing it.

**Why this is P0, not P1:** this track is graded on "AI Growth & Agentic
Commerce." If the AI never appears on screen, you are not demoing this
track — you're demoing a Razorpay integration tutorial with a policy
engine bolted on, no matter how good that policy engine is.

**Fix direction:**
1. Add a real entry point on the shopper page that calls `POST
   /agents/plan` — a single "Tell the agent what you want" input above
   the catalog is enough; render the returned plan/proposed cart instead
   of (or alongside) manual "Add to cart" browsing.
2. In the cart/checkout step, call `/growth/evaluate` or
   `/growth/recommend/{cart_id}` and render at least one accept/decline
   upsell card ("Agent suggests adding AirPods Case — 34% of buyers of
   AirPods Pro add this, uplift ₹1,999"). Wire the accept path through
   the existing cart-add flow so `ai_revenue` becomes real, not
   theoretical.
3. Put the audit trail *on the same screen as the money action*, not only
   on a separate merchant dashboard tab three clicks away (see P0.4).

### P0.2 — Cart has no remove/quantity controls; no order history; state dies on reload

Your point #2, confirmed line-by-line:

- `frontend/app/checkout.tsx` cart step (`step === "cart"`, ~line 580)
  renders `cart.items.map(...)` as read-only rows — title, qty, price,
  total. There is no remove button, no quantity stepper, no way to change
  your mind about one item without abandoning the whole cart. (The
  *recovery* flow, after a failed payment, does have a "remove item and
  retry" action — `removeAccessoryAndRetry`, line 391 — but that only
  exists post-failure. Ordinary cart editing, before you ever try to pay,
  has nothing.)
- There is no buyer-facing order history anywhere. `backend/commerce/order/handler.go`
  exposes exactly one handler, `Checkout` — no `ListOrders`/`GetOrder` for
  a shopper. The only place past orders surface at all is the merchant
  "Runs" dashboard page (`/dashboard/runs`), which replays agent decision
  trails for the *merchant*, not a "my orders" view for the *buyer*. A
  returning customer — or a judge who completes one purchase, refreshes,
  and asks "can I see what I just bought?" — has no way to.
- Nothing persists. `cartId` is `` `cart_${Date.now()}` `` regenerated in
  `useState` (line 132-134), `cart`/`order`/`payment`/`step` are all plain
  `useState`. There is zero `localStorage`, `sessionStorage`, or cookie
  use anywhere in `frontend/` (confirmed by grep — the only matches were
  unrelated `Authorization` header names). A hard reload doesn't just
  lose the cart, it loses the *step* — mid-approval-gate, mid-payment,
  doesn't matter, you're back at the empty catalog. This is also a real
  demo risk: any accidental refresh during a live judged demo resets
  everything.

**Fix direction:** persist `cartId`/`step`/`order` to `localStorage` at
minimum (cheap, no backend change) so a reload resumes instead of resets;
add remove/qty controls to the cart step calling the cart service's
existing item endpoints; add a minimal `GET /orders?buyer_id=` (or
cart-linked) list and a "My Orders" page.

### P0.3 — No authentication or authorization anywhere — including on the money-gating endpoints themselves

> **Status: implemented 2026-08-28.** Real operator authentication now
> gates the merchant dashboard and the money-authority actions:
> `POST /auth/login`/`logout` (PBKDF2-HMAC-SHA256 password hashing, bearer
> session tokens, `backend/auth/`), and a `RequireOperator` middleware on
> `/dashboard/*`, `/safety/*`, `/audit/verify`, and the *list* endpoints
> for `/approval-requests` and `/runs`. The higher-stakes fix is on
> `POST /approval-requests/{id}/approve` and `/reject` themselves
> (`backend/policy/service.go`'s new `resolveApprover`): the
> client-supplied `approver`/`by` string described below is gone,
> replaced by exactly two verified callers -- the buyer who created the
> request (proven by returning the `cart_id` it was created for) or a
> logged-in merchant operator (proven by a valid bearer session, attached
> via `OptionalOperator`) -- anyone else gets 403. This scoped design was
> a deliberate choice over gating the endpoint to operators only: the
> buyer's own self-confirmation flow in `checkout.tsx` and the merchant's
> review flow in `dashboard/approvals/page.tsx` both call the same two
> endpoints, and only one of those callers can ever hold an operator
> session. Buyer checkout deliberately stays guest -- buyer accounts were
> explicitly out of scope for this pass. `tsc --noEmit` and `eslint` are
> clean on the frontend change; the Go changes could not be compiled in
> this environment (no Go toolchain here) and were hand-verified instead,
> same caveat as P0.1 -- build/test this locally before the demo. See
> `files/AUTH.md` for the demo operator credentials and the auth design's
> trade-offs (notably: PBKDF2 instead of bcrypt, and why).

Your point #3. Verified: the only middleware in the entire backend is
`corsMiddleware` (`backend/cmd/server/main.go:31`). There is no JWT layer,
no API-key check, no session, no login — not on the frontend, not on the
backend HTTP layer, not on the MCP surface. `MERCHANT_ID = "merchant_001"`
is a hardcoded constant in `frontend/app/checkout.tsx`. `.env.example`
and `infra/.env.example` have no auth-related variables at all.

This matters for a normal e-commerce app. For **this track**, it's worse,
because of one specific finding: the **human-approval gate that your own
docs describe as the safety mechanism for high-risk transactions has no
identity check on who can approve.**

`backend/policy/handler.go:187` (`Approve`) and `:214` (`Reject`) — the
handlers behind `POST /approval-requests/{id}/approve`, the exact
Level-2/Level-3 human-in-the-loop gate your `PROJECT-AUDIT.md` Fix Log
proudly documents as newly hardened (2026-08-26, §3.1) — read an optional
`approver`/`by` string from the JSON body and **default it to the literal
string `"operator"` if absent**. There is no check that the caller is who
they claim, no session tying an approver to a merchant, nothing. Anyone
who can reach the backend and knows (or enumerates) an
`approval_request_id` can approve or reject it, full stop.

The track's stated bar is: *"every money action explainable, bounded and
gated."* An approval gate anyone can trip isn't a gate — it's a
confirmation dialog with a lock icon painted on it. This is exactly the
kind of thing a sharp judge (or your own red-team suite, if it tested
*this* instead of prompt injection) would find in under two minutes, and
it directly undercuts the strongest claim in your existing docs.

**Fix direction (scoped for a buildathon, not enterprise IAM):**
1. Give the buyer side a lightweight identity — even an anonymous
   per-session token set on first visit is enough to unlock P0.2's order
   history and cart persistence, and to stop treating every request as
   coming from the same undifferentiated caller.
2. Put a real credential (a shared secret / API key, or a merchant-scoped
   JWT) on the **approver** path specifically — `/approval-requests/*`,
   `/policy/*`, `/orders/*/payment` — since those are the literal
   "money action" endpoints the track's bar is about. This is the
   single highest-leverage security fix available: it's a small, scoped
   change (one middleware + one check in `Approve`/`Reject`) that closes
   the most track-relevant hole in the project.
3. Note in your demo/pitch materials that agent-to-agent protocols in
   this space (see P1.2) treat authenticated, signed mandates as
   foundational — so this isn't just hygiene, it's the thing the
   track's "why now" paragraph is actually about.

### P0.4 — The audit trail exists but is buried; it should be the headline, not a side tab

> **Status: implemented 2026-08-28.** The audit trail is now inline on
> the checkout screen itself, not only in the merchant `/dashboard/runs`
> tab described below. `policy.Decision` gained an `action_id` field --
> the same ID `GET /runs/{id}` already used to key its replay, just never
> returned to the caller that ran the action -- and `checkout.tsx`
> captures it from every propose/approve response and renders the
> resulting `proposed -> risk-assessed -> policy-evaluated -> authorized`
> timeline on both the order-complete screen and the payment-failed
> screen, covering the "show the audit trail and one failure handled
> gracefully" bar in the same breath as the transaction, no side tab
> required. No new subsystem was needed: the replay reconstruction
> already existed in `backend/policy/replay.go` (see below) -- it just
> wasn't wired back to the client that triggered the action it describes.

You already have a hash-chained audit ledger (`backend/audit/`), a policy
explain endpoint (`backend/policy/explain.go`, MCP tool `explain_decision`),
and a full run-replay view (`/dashboard/runs`). That's a genuine
strength — most buildathon entries won't have it. But it's three clicks
away from the money action, on a separate merchant-only dashboard the
shopper never sees. Given the track explicitly asks you to "show the audit
trail" as part of the bar, the highest-value cheap win is surfacing a
live "why is this happening" trail **inline on the checkout screen
itself** — even a collapsed panel showing the last 2-3 policy/risk
decisions for the current cart — so the explainability you built is
visible in the same breath as the payment, not something a judge has to
be told to go find.

---

## P1 — Fix if you have another day or two

### P1.1 — Your own completion docs are overconfident, and that's a submission risk

`files/PROJECT-AUDIT.md`'s Executive Summary states *"every item tracked
in §3 (Open Items) has been closed"* and *"What remains is not code."*
That audit is accurate about backend wiring — but every P0 item above is
a real, user-visible gap that isn't mentioned anywhere in either
`PROJECT-AUDIT.md` or `COMPLETION-PLAN.md`, because both were written
from the code's perspective (does the handler exist, does the migration
apply), not from a browser session. If a reviewer reads
`PROJECT-AUDIT.md` and takes "None remaining" at face value, then opens
the app and immediately finds no AI, no auth, and a cart that forgets
itself on reload, the mismatch between "what the docs claim" and "what
the product does" is itself a bad signal. Treat this file as the real
open-items list going forward, and don't let `PROJECT-AUDIT.md`'s
"complete" framing stop you from doing the P0 items above.

### P1.2 — No nod to the protocols the track explicitly names

The track's "why now" cites NPCI's UAP and "the global protocol race
(ACP, AP2, x402)" by name. Your `Mandate` concept (`backend/policy/`) is
conceptually close to AP2's cryptographically-scoped mandate idea, and
your MCP tool surface (`backend/mcp/tools.go`, 10 tools) is a legitimate
agent-readable-catalog implementation — but nothing in the code or docs
draws the connection explicitly. A judge skimming 20,000 submissions will
reward the ones that visibly demonstrate protocol awareness, not just
protocol-shaped behavior. Cheap fix: a short section in
`files/pitch-one-pager.md` or a code comment on `policy.Mandate` mapping
your design onto AP2/ACP/x402 terminology by name — you likely don't need
new code, just to name what you already built in the vocabulary the
track uses.

### P1.3 — "Campaign orchestrator" (one of the four example directions) has zero implementation

`campaign`/`Campaign` appears only in `files/phase-5-growth-agent.md` as
a mention, nowhere in actual backend code. This is the lowest-value of
the four example directions to chase given you've gone deep on the other
three (conversational checkout once P0.1 ships, agent-readable catalog,
upsell/cross-sell agent) — the track only asks for "an agent," not all
four directions. Leave this alone unless P0/P1.1/P1.2 are already done
and you have spare time.

### P1.4 — Bare-bones catalog UI

`frontend/app/checkout.tsx`'s catalog step (~line 541) is a plain
`<ul>` of text rows — no product images, no search or filter, no
quantity selector on the card itself (you add items one unit at a time).
Functional, but visually it reads as a wireframe next to what 20k
applicants' UIs will look like. Given P0.1 is going to add a
conversational entry point anyway, consider whether the traditional
browse-list even needs much investment beyond "not embarrassing," versus
putting the polish budget into making the agent interaction itself look
good.

---

## P2 — Polish, do last

- **Rate limiting / abuse guard on money endpoints.** Right now nothing
  stops a scripted client from hammering `/policy/propose` or
  `/orders/*/payment`. Low priority since Razorpay Test Mode has no real
  money risk, but worth a one-line mention in your write-up that you're
  aware of it, especially once P0.3's auth lands (auth without rate
  limiting is still poke-able).
- **Loading/empty states.** `page.tsx` already degrades gracefully when
  the catalog fetch fails (empty array, not a crash) — good — but there's
  no visible "reconnecting..." state anywhere else if the backend is
  slow or restarting mid-demo.
- **Mobile/responsive check on the shopper flow specifically.** The
  dashboard shell (`dashboard/layout.tsx`) is responsive; nobody's
  verified the checkout flow the same way.

---

## Recommended order, given limited time before submission

1. **P0.1** (surface the AI) and **P0.3** (auth on the approval/money
   endpoints) first — these two are the ones a judge notices in the
   first 90 seconds and the one a sharp judge could actively poke a hole
   in, respectively.
2. **P0.2** (cart edit + persistence + order history) next — cheapest of
   the P0s (mostly frontend, `localStorage` + a couple of small
   endpoints) and the one most likely to visibly break *during* a live
   demo if skipped.
3. **P0.4** (inline audit trail) — reuses data you already expose via
   `/runs` and `explain_decision`; mostly a frontend rendering task.
4. **P1.1/P1.2** — both are documentation/framing fixes, an afternoon at
   most, and directly shape how a judge *reads* everything else you've
   built.
5. **P1.3/P1.4/P2** — only if time remains.

---

*Verified against the working tree as of 2026-08-26. Every claim above
was checked by reading the referenced file/line directly (grep + direct
inspection), not inferred from `PROJECT-AUDIT.md`'s narrative — that
document was in fact the thing being checked against.*
