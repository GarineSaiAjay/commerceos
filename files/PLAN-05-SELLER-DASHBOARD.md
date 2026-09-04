> **Historical document.** This plan was written against the dashboard as it stood on 2026-08-30 (six nav pages, catalog/orders/growth/settings/team all unbuilt). A fresh audit against the real codebase (2026-09-04) found every item in "What's missing" below has since shipped -- Catalog, Orders, Growth, Settings, notification badges, CSV export, and even the "stretch, not required" multi-operator invite flow are all live, authenticated, and wired to real data. Kept in full below for design-history reference, with a **Shipped:** note added under each item pointing at the actual files, rather than deleted -- see `README.md` for what the dashboard nav looks like today.

# Plan 05 — Seller/Merchant Dashboard: Reality Check + V2

Depends on: `REALITY-CHECK-2026-08-30.md` §4.1–4.2.

## Current state, verified against source

`frontend/app/dashboard/layout.tsx`'s nav lists exactly six pages:
Overview, Analytics, Approvals, Campaigns, Runs, Safety. All are gated
by real operator auth (`AuthGate`, `RequireOperator` middleware) and all
render real, live data — this dashboard has no fake screens, which is a
genuinely good foundation to build on.

> **Shipped (2026-09-04):** the nav now lists ten pages -- the six
> above plus Catalog, Orders, Growth, and Settings, all built out per
> the "What's missing" sections below. See
> `frontend/app/dashboard/dashboard-nav.tsx` for the current list.

What each page actually does today:

- **Overview** (`merchant-dashboard.tsx`): revenue, AI-attributed
  revenue, conversion, AOV; recent audit activity feed; audit chain
  integrity status; safety-eval status link; latest agent actions table.
  Solid, real, well-built.
- **Analytics** (`analytics/page.tsx`): a *manual, single-shot*
  A/B experiment runner (name/seed/multiplier → run → see one result).
  **No history/list view** — unlike every other list-shaped page in this
  dashboard (Campaigns, Approvals, Runs, Safety all show a persisted
  list), Analytics shows only the just-run result and loses it on
  refresh. This is a real inconsistency in an otherwise consistent
  dashboard.
  **Shipped (2026-09-04):** `GET /dashboard/experiments` now backs a
  persisted history list in `analytics/page.tsx`, matching the Safety
  page's pattern -- this inconsistency is fixed.
- **Approvals**: pending Level 2/3 proposals, approve/reject. Real,
  works.
- **Campaigns**: propose + list + approve/reject discount campaigns
  from the real Campaign Orchestrator. Real, works, well-designed
  (budget bar, reasoning text, rejection audit trail).
- **Runs**: forensic replay of every policy-reviewed agent action, full
  step timeline. Real, works, arguably the single best page in the
  dashboard.
- **Safety**: run the red-team attack library and the 100-scenario
  evaluation suite live, see pass/fail + provider-call-delta proof.
  Real, works, genuinely impressive for a judge to click through.

## What's missing — verified against what the backend can already do

This is the concrete "unwired, not missing" case the user asked to have
documented precisely:

### 1. Catalog management — backend exists, zero UI, currently unauthenticated

> **Shipped:** `frontend/app/dashboard/catalog/page.tsx` +
> `VariantEditor.tsx` is a full CRUD UI, matching the existing list-page
> pattern. `POST/PATCH/DELETE /products` and the variant routes are
> wrapped in `RequireOperator` in `backend/cmd/server/main.go` (the
> auth fix shipped alongside the page, as this plan required). The
> `ErrProductInUse`/`ErrVariantInUse` 409s surface verbatim in the UI.
> One spec deviation: the "quick +/- availability control per variant,
> distinct from a full edit" below was NOT built as a separate stepper
> -- `VariantEditor.tsx` has a single inline edit form (SKU/price/
> availability/Save) instead. Functionally equivalent (availability is
> editable), just not the specific UX pattern this section asked for.

`backend/commerce/catalog/handler.go` fully implements
`CreateProduct`/`UpdateProduct`/`DeleteProduct`, wired into `main.go` at
`POST/PATCH/DELETE /products`. **No dashboard page calls any of them.**
A merchant literally cannot add, edit, or remove a product except by
hand-editing `db/seeds/001_catalog.sql` and restarting the stack. Per
`REALITY-CHECK` §4.1, these routes are also currently unauthenticated —
**that fix (wrap in `RequireOperator`) must ship before or alongside
this page, not after**, since building a dashboard UI for an
unauthenticated write path doesn't fix the hole, it just gives it a
nicer front door.

Design for `/dashboard/catalog`:
- List view matching the existing list-page pattern (Campaigns/
  Approvals/Runs all share a visual shape — reuse it exactly): product
  title, price, availability, variant count, review aggregate (once
  `PLAN-02` ships).
- Create/edit form: title, price, features/use_cases/compatibility as
  tag inputs (comma-entry, matching the JSON array shape the backend
  already expects), return policy days, shipping estimate.
- Variant sub-editor (once `PLAN-02` §1 ships real variants): add/edit/
  remove variants under a product, each with its own price/availability.
- Inventory adjustment: a quick +/- availability control per variant,
  distinct from a full edit (matches how a real merchant actually
  manages stock day to day).
- Delete with the existing `ErrProductInUse` guard surfaced as a clear
  message ("can't delete — referenced by N existing orders") rather than
  a raw error, since the backend already distinguishes this case.

### 2. Orders / fulfillment — merchant has no view of their own orders

> **Shipped:** `frontend/app/dashboard/orders/page.tsx` -- list/detail
> split, linked payment record, and a working deep link into Runs
> (`/dashboard/runs?run_id=...`). No refund/cancel path exists yet,
> exactly as this section anticipated ("note this explicitly as a
> future capability") -- that part of the gap is still open, correctly
> left unbuilt rather than faked.

`GET /orders?merchant_id=` already exists and is used by the *buyer's*
own order history in `checkout.tsx`. The merchant dashboard — the
"merchant command center," per its own tagline — has **no orders page
at all.** Add `/dashboard/orders`:
- List: order id, items, subtotal, status, payment status, created at
  — same visual pattern as Runs' list/detail split.
- Detail: full line items, linked payment record, linked audit
  trail (reuse `RunsPage`'s step-timeline rendering directly — the data
  shape is compatible), and — if/when a refund path exists — the
  action to trigger it. (No refund/cancel backend path currently exists
  in `commerce/payment/` beyond the recovery/retry flow for failed
  payments; note this explicitly as a **future capability**, not
  something to fabricate a button for today.)

### 3. Growth-agent performance — one aggregate number today, should be a real page

> **Shipped:** `frontend/app/dashboard/growth/page.tsx` +
> `backend/growth/dashboard.go` -- suggestion funnel (shown/accepted/
> dismissed), top products by acceptance rate, and a rejected-demand
> list linking into Campaigns, all backed by real SQL joins over
> `suggestion_impressions`/`recommendations`/`suggestion_dismissals`,
> not mocked.

Overview shows a single lifetime `ai_revenue` figure. There is no visibility
into: how many suggestions were shown, how many were accepted, which
products the growth agent recommends most/most-successfully, or how the
Campaign Orchestrator's rejected-demand data is trending. Add
`/dashboard/growth`:
- Suggestion funnel: shown vs. accepted vs. dismissed (needs
  `PLAN-03` §8's impression/acceptance tracking — this page is the
  natural home for that data once it exists).
- Top recommended products by acceptance rate, not just by volume.
- A direct link into Campaigns for any product with high rejected-
  demand (the two features are already causally connected in the
  backend — `CampaignAgent` literally reads `RejectedDemandByProduct`
  — this page just makes that connection visible instead of implicit).

This single page most directly answers the user's complaint #3 from the
merchant's side: today a merchant has *no way to tell* whether the
cross-sell agent is working at all, which makes it impossible to trust
or tune.

### 4. Mandate / policy settings — currently invisible and hardcoded

> **Shipped:** `frontend/app/dashboard/settings/page.tsx` +
> `backend/policy/handler.go`'s `GetSettings`/`UpdateSettings`, gated
> at `/dashboard/settings/policy`. Config is persisted before being
> applied live, then best-effort audit-written -- exactly the read-
> first, gated-edit posture this section called for.

The mandate a buyer checks out against
(`mandate_demo`, ceiling ₹30,000, allowed categories `["electronics"]`)
is a single seeded DB row with zero dashboard visibility. Add
`/dashboard/settings` (or fold into a `Policy` tab): view (and, gated
appropriately, edit) the mandate's ceiling, allowed categories/products,
budget tolerance, and confirmation threshold. Still validated
deterministically server-side by `policy.Engine` exactly as today — this
page is a window into existing config, not a new authority.

### 5. Notification / alerting surface

> **Shipped:** all three sub-items are built -- an approvals badge and
> a persistent chain-broken/budget-exhausted banner
> (`dashboard-nav.tsx`, `dashboard-banners.tsx`) driven by a shared
> poller in `alerts.tsx` rendered above every `/dashboard/*` page.

Nothing in the current nav communicates "something needs your
attention" without navigating into that specific page. Add:
- A pending-approvals count badge on the "Approvals" nav item (the data
  already exists — `GET /approval-requests?status=PENDING` — just needs
  a count surfaced in the nav, not just the page body).
- A campaign-budget-exhaustion indicator (campaigns already expose
  `spent`/`budget_cap` — surface any `exhausted` campaign as a small
  Overview banner, not just visible after navigating into Campaigns).
- Elevate `audit_integrity.chain_broken` from a card on Overview to a
  persistent top-of-dashboard banner across every page if it's ever
  true — this is the one condition serious enough that a merchant
  should see it no matter which page they're on.

### 6. Reporting / export

> **Shipped:** `backend/commerce/order/handler.go`'s
> `ExportOrdersCSV` and `backend/campaign/handler.go`'s `ExportCSV`,
> both `RequireOperator`-gated and wired to real export buttons in the
> UI.

CSV export of orders and campaigns — useful for both a real merchant
and for a judge who wants to inspect data outside the UI. Backend-side
this is a thin CSV-serialization endpoint over data the existing
handlers already query; no new business logic.

### 7. Multi-operator (stretch, not required)

> **Shipped anyway:** `backend/auth/invite.go` (invite/accept/list/
> revoke, hashed tokens, TTL, can't-remove-self/can't-remove-last-
> operator guards, enforced server-side) + `frontend/app/dashboard/
> settings/team.tsx` + `frontend/app/accept-invite`. Built despite
> being flagged here as stretch/not-required.

Exactly one hardcoded operator account exists (`db/seeds/002_operator.sql`),
with the PBKDF2 trade-off already documented in `files/AUTH.md`. A real
operator-invite flow (second operator per merchant, each with their own
login) is a reasonable stretch goal but not required for the track's
bar — flagged here for completeness, detailed further in `PLAN-06`.

---

## UX consistency notes (apply while building the above, not a separate pass)

- Every new list page should match the established shape (header +
  description, list of cards with a status badge, empty state matching
  the existing "No X yet" pattern already used identically across
  Campaigns/Approvals/Runs/Safety) — this dashboard already has a
  consistent design language; the job is extending it, not inventing a
  new one.
- Analytics' missing history list (noted above) should be fixed as part
  of this pass regardless of which new page ships first — it's the one
  existing inconsistency, cheap to fix (persist each run's report row,
  list them the same way Safety lists evaluations). **Shipped** -- see
  the Analytics bullet above.

---

## Phasing

> **Status (2026-09-04): every phase below has shipped**, including
> the P3 multi-operator stretch item. Kept as a record of the original
> plan, not a live tracker.

| Phase | Scope | Effort | Risk |
|---|---|---|---|
| P0 | Auth-gate the product CRUD routes (shared with `PLAN-02` §5.1) | 1 hour | Very low, do first |
| P0 | `/dashboard/catalog` — list + create/edit (no variants yet) | 2 days | Medium |
| P0 | Fix Analytics' missing history list | 0.5 day | Low |
| P1 | `/dashboard/orders` list + detail | 1.5 days | Low-medium |
| P1 | Catalog page: variant sub-editor + inventory adjust (needs `PLAN-02` §1) | 1.5 days | Medium |
| P1 | `/dashboard/growth` funnel page (needs `PLAN-03` §8 tracking) | 1.5 days | Low-medium |
| P2 | `/dashboard/settings` mandate view/edit | 1.5 days | Medium — touches policy config surface, keep read-first, edit gated carefully |
| P2 | Notification badges + persistent integrity banner | 1 day | Low |
| P2 | CSV export | 1 day | Low |
| P3 | Multi-operator invite flow | 2–3 days | Medium-high, security-sensitive |
