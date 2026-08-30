# Plan 02 — World-Class Catalog (No Images, By Design)

Depends on: `REALITY-CHECK-2026-08-30.md` §2. Explicit constraint from
the user: **no product images** — text/data-driven UI only, kept
minimal and fast. Everything below is designed around that constraint,
not despite it.

## Why this matters for the track, not just UX

A 13-SKU, one-variant-each, single-brand catalog gives both the buyer
agent and the growth agent almost nothing to reason over — there's
rarely a real tradeoff to make. A judge testing "ask for X" will get the
same 2-3 products regardless of how the request is phrased, which makes
the agent look shallow even where the underlying reasoning code is fine.
Depth (variants, reviews, richer signals) is what makes agent reasoning
*visible*, which is the actual product being judged.

---

## 1. Real variants — the schema already supports this, just use it

`product_variants` (DB table) and `catalog.ProductVariant` (Go struct)
already carry `sku, price_amount, availability, attributes` per variant,
independent of the parent product. Every one of the 13 seed products
uses exactly one `-default` variant today. Concrete additions, no schema
migration needed:

- **AirPods Case, AirTag 4-Pack, Wireless Charging Pad, MagSafe
  Charger**: add color variants (white/black/starlight — matches real
  Apple accessory colorways), each its own `product_variants` row with
  its own `availability` (so "out of stock in black, 12 left in white"
  becomes a real, demoable state, not a hypothetical).
- **Lightning-to-USB-C Cable**: add length variants (0.5m / 1m / 2m)
  with real price deltas — this is the cleanest case for showing the
  agent reason about price *within* a single product, not just across
  products.
- **AppleCare**: add coverage-tier variants (1-year / 2-year) — gives
  the growth/cross-sell agent a genuine "upgrade the accessory you just
  added" case distinct from "add a different product."
- Cart (`commerce/cart/`) and order flows already key off `variant_id`
  end-to-end (confirmed in `cart.Service.AddItem`, MCP's `addItem` tool)
  — this is purely additive seed data plus a variant-picker UI
  component (radio/segmented control per product row — small, no new
  design language).

## 2. Reviews and ratings — real, not synthetic-and-labeled

Two options were considered:

- **(a) Synthetic seed data, labeled "Simulated"** — matches the
  pattern the codebase already uses for analytics/growth-simulator data.
  Fast, but adds a second disclosed-fake dataset to a codebase whose
  whole pitch is "nothing here is faked or asserted."
- **(b) Real reviews generated from real orders** — recommended.

**Recommendation: (b).** Design:

- New migration: `reviews` table — `id, product_id, order_id (nullable
  FK, null only for a small operator-seeded starter set),
  buyer_reference, rating smallint (1-5), comment text, verified_purchase
  bool, created_at`. `order_id` presence *is* `verified_purchase` — no
  separate flag to keep in sync.
- Post-checkout prompt: on the existing order-complete screen
  (`checkout.tsx`, `step === "complete"`), add an optional "Rate this
  order" mini-form (rating + short text) that posts to a new `POST
  /orders/{id}/review`. This is a real, first-party data source that
  grows the longer the demo/judging session runs — genuinely more
  impressive live than a static seeded set, and it's "unique, helpful
  for both buyers and sellers" per the user's ask: buyers get real
  social proof, sellers get real feedback with zero manual entry.
  A small operator-seeded starter set (5-10 reviews across the catalog,
  clearly attributed as seed data with `order_id = null`) avoids an
  empty-state catalog on first run.
- Aggregate exposure: `catalog.Service.ListProducts`/`GetProduct` gain
  `average_rating` and `review_count` via a join/subquery in
  `catalog/repository.go` — cheap (13 products, indexed on
  `product_id`), computed at read time so it's never stale.
- **Feed this into the growth agent.** `growth/suggest.go`'s
  `heuristicEVInputs` currently derives purchase probability from tag
  overlap alone — extend it to also weight by the candidate's
  `average_rating` (a `4.8`-rated accessory is a more defensible
  suggestion than a `3.1`-rated one at equal tag overlap). This is a
  genuinely better, still fully deterministic and explainable, EV input
  — and it directly improves the honesty of the "heuristic placeholder"
  gap called out in the reality check, using data this plan just made
  real.

## 3. Facets, search, and sort — text-only, fast, no new dependencies

Current browsing UI is a single unfiltered `<ul>`. Add, client-side,
over the already-fetched product list (13-50 products is trivial to
filter/sort in the browser — no server round trip needed, no latency
cost):

- A plain `<input>` search box matching against `title` +
  `features`/`use_cases` tags (substring match, no fuzzy-search library
  needed at this catalog size).
- Category chips derived from the real `use_cases` values already in
  the data (`earbuds`, `charging`, `tracking`, `accessories`,
  `protection`) — computed from the fetched product list, not
  hardcoded, so a newly added category shows up automatically.
- Sort: price asc/desc, rating (once §2 ships), availability.

This is entirely a `checkout.tsx` component change (extracted into a
`ProductList` component per `PLAN-04`'s component-split recommendation)
— zero backend changes, zero new libraries, keeps the bundle small and
the interaction instant (no network latency at all for filtering).

## 4. A real product detail view

Today `features`, `use_cases`, `compatibility`, `return_policy`, and
`shipping` are all present in the API response and never shown. Add a
lightweight inline expand (not a full page navigation — keeps the SPA
feel and avoids a router round trip) per product row: click the title to
expand a detail panel showing features as plain tags, return policy
("7-day returns"), shipping estimate, and — once §2 ships — reviews.

**This is also the second cross-sell touchpoint from
`PLAN-03-PROACTIVE-GROWTH-AGENT.md`**: the same expanded panel calls the
existing `growth/suggest.go`-style scoring (or, cheaper, a lightweight
"products sharing tags with this one" query reusing the same
overlap-scoring code already written for the cart suggestion) to show
"Frequently paired with" directly on the product itself, before it's
even in a cart. One scoring function, two call sites — no duplicated
logic.

## 5. Seller-side catalog management — closes a real security hole too

`REALITY-CHECK` §4.1 found that `POST/PATCH/DELETE /products` are fully
built, functional, and **completely unauthenticated**. This plan and
`PLAN-05-SELLER-DASHBOARD.md` share this item because it's simultaneously
a UX gap (no dashboard page) and the single highest-priority security
fix in the whole audit:

1. **Immediate, minimal fix (do this first, independent of everything
   else in this document):** wrap the three routes in
   `authService.RequireOperator(...)` in `backend/cmd/server/main.go`,
   matching every other mutating dashboard route. Two-line change,
   fully covered by the existing `backend/auth` test suite's patterns.
2. **Then** build the dashboard catalog page (full design in `PLAN-05`)
   as the *legitimate* front door to the now-gated CRUD API — including
   a variant editor for §1 and inventory adjustment (restock/mark
   out-of-stock) for real-looking demand dynamics.

## 6. Data integrity — stop the recurring allowlist staleness

`policy.DefaultConfig().AllowedProducts` has gone stale three times in
this project's own git history because it's a hand-maintained slice with
no dynamic catalog lookup, by deliberate design (the engine has no
catalog dependency, for determinism/testability). Recommended fix that
preserves that determinism property: a small **generator, not a runtime
dependency** — a `go generate` script (or a `make catalog-sync` target)
that reads `db/seeds/001_catalog.sql`'s product IDs and rewrites the
`AllowedProducts` literal in `policy/model.go` automatically, run in CI
as a diff-check (fail the build if the generated list doesn't match
what's committed) rather than at runtime. This keeps `policy.Engine`
exactly as pure and dependency-free as its own comments insist it should
stay, while making "add a product, forget the allowlist" a CI failure
instead of a production bug three commits later.

---

## Phasing

| Phase | Scope | Effort | Risk |
|---|---|---|---|
| P0 | §5.1 auth fix on product CRUD routes | 1 hour | Very low, do this regardless of anything else |
| P0 | §1 real variants (colors/lengths/tiers) via seed data + picker UI | 1 day | Low |
| P1 | §2 reviews table + post-checkout prompt + aggregate exposure | 2 days | Medium — new table, new endpoint, migration |
| P1 | §3 client-side search/sort/filter | 1 day | Low, no backend change |
| P1 | §4 product detail panel + tag-based "frequently paired with" | 1 day | Low, reuses existing scoring |
| P2 | §5.2 dashboard catalog management page | 2–3 days | Medium — see `PLAN-05` |
| P2 | §6 CI-enforced allowlist generator | 0.5 day | Low, tooling-only |
| P2 | §2 growth-agent rating-weighted EV input | 0.5 day | Low, additive to existing formula |
