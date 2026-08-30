# Reality Check — 2026-08-30

Ground-truth audit of CommerceOS as it actually stands on `main` at commit
`95b44db` (`fix(mcp): add add_item tool, real JSON schemas, fix
search_products`), read directly from source — not from the pitch docs,
not from memory of an earlier pass. Everything below is either a direct
code citation or a traced execution path. Where something is an opinion
rather than a fact, it's labeled as one.

This file is the shared factual baseline for the six plan documents next
to it (`PLAN-01` … `PLAN-06`) and `ROADMAP-PRIORITIZED.md`. Read this
first; the plans assume it.

Companion file for anyone new to the repo's audit history:
`files/AUDIT-2026-08-29.md` (the previous pass — mostly fixed since, see
"What's changed since the last audit" below).

---

## 0. What's changed since the last audit (2026-08-29 → now)

The previous audit's top priority items are **done**, contrary to what
project memory said going into this session:

- MCP tool surface fixed: `add_item` tool added, all 11 tools now carry
  real JSON Schema (`backend/mcp/tools.go`), `search_products` actually
  filters instead of ignoring its arguments.
- ~19 dead doc references repointed.
- docker-compose health-gating + migrate-then-seed automation + backend
  DB-connect retry, all shipped (`infra/docker-compose.yml`).
- `backend/auth` test coverage added.
- Catalog grew from 4 → 10 → 13 SKUs across several sessions, with a
  Campaign Orchestrator (`backend/campaign/`) built and merged, gated by
  its own deterministic policy engine and requiring operator approval.
- A real conversational checkout bug hunt happened (5 separate reported
  bugs, all fixed with regression tests): budget-shorthand parsing
  (`30k`), named-product → category mapping, a duplicated hardcoded
  allowlist, a broken "Remove" button on the policy-rejection screen, and
  a raised LLM timeout.

This is a **more mature codebase than the memory file suggested**. The
gaps that remain are real, but they're a different, harder tier: not
"things are broken," but "things are honest, narrow, and reactive where
the track rewards broad and proactive."

---

## 1. Is this truly a fully agentic application?

**Verdict: partially. The money-safety half is genuinely agentic-grade
engineering. The "agent" half is a single-shot classifier wearing agent
clothing.** Both halves are honest — nothing here is faked — but only one
of them is what a judge means by "agentic."

### 1.1 What's real and is the project's strongest asset

- **Policy Engine** (`backend/policy/engine.go`) is a real deterministic
  gate — ceiling, allowlist, category, currency, budget tolerance — with
  per-check explanations (`policy/explain.go`). The LLM never has
  financial authority; every proposed action passes through this before
  anything happens. This is the actual "bounded and gated" bar from the
  track brief, and it's solid.
- **Audit chain** (`backend/audit/postgres_writer.go` +
  `audit/verifier.go`): a real SHA-256 hash chain, independently
  verifiable, surfaced in the dashboard (`audit_integrity` on
  `/dashboard/overview`) and per-action (`/runs/{id}` → `RunsPage`'s
  timeline). This is a genuinely good "show the audit trail" story.
- **MCP server** (`backend/mcp/`) now exposes 11 tools with real JSON
  Schema and a working `search_products → create_cart → add_item →
  create_checkout → request_authorization → execute_authorized_checkout`
  path. A generic MCP client (Claude Desktop, an ACP-style external
  agent) can discover and complete a purchase without reading this
  repo's source. This is the actual "agent-to-agent commerce" surface
  the track cares about, and until this session's earlier fix it
  **could not complete a purchase at all** (no way to add items to a
  cart via MCP) — that's now closed.
- **Campaign Orchestrator** (`backend/campaign/`): proposes bounded
  discount campaigns from real rejected-demand data, sized to observed
  volume, gated by its own policy engine, requiring human approval
  before it can discount anything. Not a stub.
- **14-attack red-team suite + 100-scenario evaluation suite**
  (`backend/safety/`, `policy/evaluation_suite_test.go`) both pass, and
  are runnable live from the dashboard's Safety page against the real
  pipeline (not a canned demo).

### 1.2 What's NOT actually agentic, despite being called an "agent"

**`BuyerAgent.PlanCheckout` (`backend/agents/buyer_agent.go`) is a fixed
three-stage pipeline, not an agent loop:**

```
extract intent (one LLM call, strict JSON schema)
  → search catalog (deterministic scoring, agents/search.go)
    → pick results[0] (top-1, no exploration, no alternatives offered)
      → propose CREATE_ORDER
```

There is no loop. The "agent" cannot:
- **Ask a follow-up question and remember the answer.** `ErrAmbiguousIntent`
  just returns a static frontend string ("Say a bit more…") and the
  buyer has to retype the *entire* prompt from scratch — none of what
  they already said is retained. There is no conversation state anywhere
  (no `conversation_id`, no message history, nothing keyed by session).
  Compare this to any real chat-based shopping agent, where "actually,
  make it cheaper" is a valid follow-up. Here it is not — it's a brand
  new, context-free extraction.
- **Use more than one tool per turn.** The MCP tools exist and are
  well-specified, but nothing in this codebase actually chains them in
  a reasoning loop. `BuyerAgent` calls its own Go functions directly
  (`Extract` → `Search`), not the MCP tools at all — the MCP surface is
  a *parallel*, disconnected interface for external clients, not the
  substrate the in-app agent itself runs on. Two separate "agents"
  exist (in-app `BuyerAgent`, external MCP tool palette) with no shared
  loop or shared reasoning.
- **Offer alternatives or negotiate.** `results[0]` is taken
  unconditionally; the other ranked results are computed and discarded.
  A buyer who doesn't like the one proposed product has no way to see
  "here are 2 other options" — they can only reject and retype.
- **Act without being asked.** Every agent behavior in this app is
  triggered by an explicit buyer action (typing a prompt, landing on the
  cart step). Nothing is agent-initiated. See §3 for the concrete
  instance of this (the cross-sell engagement gap).
- **Explain its own reasoning process, only its conclusion.** The
  `reasoning` string on `CheckoutPlan` is a templated sentence
  (`fmt.Sprintf`), not a trace of what the agent considered and rejected.
  Compare this to the *policy* side, which has genuine per-check
  explanations (`policy/explain.go`) — the money-safety half explains
  itself far better than the shopping half does.

**Growth Agent** (`backend/growth/`) is honestly documented as
deterministic and heuristic, not agentic: `GrowthAgent.EvaluateCandidate`
is pure arithmetic (EV formula over caller-supplied inputs), and the one
endpoint that picks its own candidate, `SuggestHandler.Suggest`
(`growth/suggest.go`), does *content-overlap tag scoring* — no LLM
call, no reasoning, no negotiation. That's a legitimate, auditable design
choice (a heuristic is easier to bound and explain than an LLM), but it
means "the agent recommends" is doing a lot of marketing work for what is,
mechanically, a scored SQL-adjacent join.

**Campaign Agent** is explicitly documented as never calling an LLM
(`campaign/agent.go`'s own doc comment: *"never calls an LLM and never
chooses its own discount percent or duration"*) — again, a legitimate
determinism choice, but it means three of the four things branded
"agent" in this codebase (Growth, Campaign, and half of Buyer) do not
involve a model making a decision at all.

### 1.3 Scorecard

| Capability | Present? | Where |
|---|---|---|
| Perceives state (catalog, cart, mandate) | Yes | catalog/cart services |
| Reasons over unstructured input | Yes (one-shot only) | `LLMExtractor` |
| Plans multi-step | **No** | fixed 3-stage pipeline |
| Uses tools dynamically / tool-calling loop | **No** (tools exist, unused by the in-app agent) | `backend/mcp/tools.go` vs `buyer_agent.go` |
| Remembers across turns | **No** | no conversation/session state |
| Acts proactively (not just reactive) | **No** | see §3 |
| Explains its own decision process | Partial (policy: yes; shopping: templated only) | `policy/explain.go` vs `buyer_agent.go` |
| Bounded / gated before money moves | **Yes — strong** | `policy.Engine` |
| Externally discoverable/usable by another agent | **Yes — strong, recently fixed** | `backend/mcp/` |

**Bottom line:** the *trust and safety architecture* is what a top-tier
submission in this track needs, and it's already excellent — don't touch
it. The *shopping agent* is the weaker half and is architecturally a
single-shot NLU classifier, not an agent. `PLAN-01-AGENTIC-CORE.md`
designs the fix: a bounded tool-calling loop with real memory, built on
top of (not replacing) the existing MCP tool palette and policy gate.

---

## 2. Catalog reality check

**13 products, all Apple/Beats audio accessories** (`db/seeds/001_catalog.sql`):
AirPods Pro 2 & 3, AirPods 3, AirPods Max, Beats Fit Pro, AirPods Case,
AppleCare, USB-C Adapter, Wireless Charging Pad, MagSafe Charger,
Lightning-to-USB-C Cable, AirPods Ear Tips, AirTag 4-Pack.

Concrete gaps, all verified in code:

- **Every product has exactly one variant.** The schema
  (`product_variants` table, `catalog.ProductVariant` struct) fully
  supports multiple variants per product (SKU, price, availability,
  attributes all per-variant) — it is simply never used that way. Every
  one of the 13 seed inserts creates exactly one `-default` variant.
  There is no color, size, or capacity choice anywhere in the catalog
  despite the data model being built for it.
- **No reviews or ratings table exists at all.** Not stubbed, not
  hidden — absent from the schema entirely (`db/migrations/` has no
  `reviews` migration).
- **No product detail view.** The catalog browsing UI
  (`frontend/app/checkout.tsx`, the `step === "catalog"` block) renders
  a flat `<ul>` of title / price / stock / "Add to cart" — features,
  use_cases, compatibility, and return policy are all present in the
  API response (`GET /products`) and never rendered anywhere on the
  buyer side.
- **No search, sort, or filter UI.** The full catalog is fetched once
  and listed in insertion order; there is no way for a buyer to narrow
  13 items by category, price, or feature except by typing a prompt at
  the agent.
- **No images** — this is explicitly not a gap; the user has asked for
  this to stay text/data-driven, and `PLAN-02` follows that.
- **Single-tenant, single-brand by construction**, not by data volume:
  `merchant_001` is hardcoded across seeds, policy config, and
  `MERCHANT_ID` in the frontend. Fine for a demo; worth naming
  explicitly so it isn't mistaken for a scale limitation of the
  architecture (it isn't — merchant_id is a real column throughout).

`PLAN-02-CATALOG-AND-COMMERCE.md` designs variants, reviews, and
browse/search/detail UX depth — deliberately without images.

---

## 3. Why the upsell/cross-sell agent goes quiet after the first add-to-cart

**Reported symptom:** buyer asks the agent for a product, accepts it,
keeps browsing — and never hears from an agent again.

**Root cause, traced exactly:**

`frontend/app/checkout.tsx`, the suggestion-fetch effect:

```tsx
useEffect(() => {
  if (step === "cart" && cart && cart.items.length > 0) {
    fetchSuggestion();
  }
}, [step, cart?.cart_id, cart?.items.length]);
```

`fetchSuggestion` — which calls `POST /growth/suggest`
(`backend/growth/suggest.go`, a real, policy/budget-gated, scored
recommendation) — **only ever runs when `step === "cart"`.** The buyer's
reported flow (ask agent → accept → "keep shopping", which sets
`step` back to `"catalog"`, per the button at the bottom of the cart
view) leaves `step` on `"catalog"` for the rest of the session unless
they manually click back into the cart. The suggestion engine is real
and working — it is simply **never invoked** outside one specific screen.

There is no other cross-sell touchpoint anywhere in the app:
- The agent chat (`askAgent`/`acceptAgentPlan`) never calls
  `/growth/suggest` after adding an item — it's a completely separate
  code path with zero connection to the growth agent.
- The catalog browsing list has no "customers also considered" or
  "frequently bought with" surfaced per product.
- The order-complete screen has no post-purchase cross-sell.
- Dismissal (`dismissedProductId`) is component state only — it resets
  on page reload and isn't persisted server-side, so nothing about past
  interactions carries forward even within the one place suggestions
  *do* show up.

This is a **wiring gap, not a broken feature.** The backend growth
engine works exactly as designed and is unit-tested
(`growth/growth_test.go`). It's connected to exactly one of the many
places a real cross-sell agent needs to live. `PLAN-03` fixes this with
concrete, minimal, non-annoying multi-surface wiring.

---

## 4. Full hardcoded / stub / unwired inventory

Ordered by how much it matters, not by where it lives in the repo.

### 4.1 Critical — found this session, not in any prior audit

**`POST /products`, `PATCH /products/{id}`, `DELETE /products/{id}` have
no authentication at all.** (`backend/cmd/server/main.go`, lines ~380–412:
the `/products` and `/products/` handlers dispatch directly to
`catalogHandler.CreateProduct` / `UpdateProduct` / `DeleteProduct` with
no `authService.RequireOperator(...)` wrapper — contrast with
`/campaigns`, `/safety/*`, `/runs`, `/dashboard/*`, all of which are
wrapped.) The full CRUD handlers exist and work
(`backend/commerce/catalog/handler.go`, `repository.go`) — this is not a
missing feature, it's a **shipped, unauthenticated write path onto the
catalog**. Anyone who can reach the Commerce Service port can rewrite any
product's price, delete the entire catalog, or — worse, given this repo
already documents a planted prompt-injection payload in one product's
`attributes.description` (`wireless-charging-pad`, see
`db/seeds/001_catalog.sql`'s own comment and `safety/attacks.go`'s
`att_14`) — **plant a new prompt-injection payload into any product's
attributes, targeting the LLM buyer agent or any external MCP client
that reads `get_product`/`search_products`.** This is the single highest-
priority fix in this whole document; it is a two-line change
(`RequireOperator` wrap) with an existing, tested auth layer to reuse.

### 4.2 High — real functionality, real gap in reach

- **Catalog CRUD has zero dashboard UI.** The backend fully supports
  merchant catalog management; the merchant dashboard has no page for
  it. This is the textbook "unwired, not missing" case the user asked
  to have documented. See `PLAN-05`.
- **No orders/fulfillment view for the merchant.** `GET
  /orders?merchant_id=` exists and is used by the *buyer's own* order
  history (`checkout.tsx`'s `viewOrderHistory`) but the merchant
  dashboard has no orders page at all — a merchant cannot see their own
  order list from their own command center today.
- **Mandate policy knobs (ceiling, allowed categories, tolerance) have
  no dashboard UI.** They're a single hardcoded seed row
  (`mandate_demo`, `db/seeds/001_catalog.sql`) and a Go constant
  (`policy.DefaultConfig()`). A merchant cannot see or adjust their own
  policy limits without editing source.
- **`policy.DefaultConfig().AllowedProducts` is a hand-maintained
  string slice** that has already gone stale three times in this
  project's own git history (per the code's own comments) because
  nothing keeps it in sync with the catalog table. Consolidated to one
  copy this session (`campaign` now reuses `policy`'s list), which
  halves the risk but doesn't remove it.
- **`growth.DemoBudgetCeiling`/`DemoBudgetTolerance`** are package
  constants standing in for "the buyer's real mandate," because there is
  no per-buyer mandate lookup wired to the growth agent. Documented
  honestly in `growth/suggest.go`'s own comment.

### 4.3 Medium — honestly disclosed, worth knowing anyway

- `backend/events/stream_consumer.go`: consumes the Redis Stream event
  bus and only logs + acks. No downstream effect. The comment calls it a
  "Placeholder stream consumer proving the event bus is wired" — accurate
  and not misrepresented anywhere in the UI, but it means the event bus
  infrastructure (already provisioned, Redis is running) is currently
  decorative.
- `growth/suggest.go`'s `heuristicEVInputs`: a documented,
  non-machine-learned placeholder for purchase probability/margin,
  because there's no purchase-history table yet. Fine for a demo; the EV
  *formula* itself is real, only its inputs are heuristic.
- `backend/analytics/experiment.go` + the Analytics dashboard page: a
  real statistical A/B computation (population split, lift, confidence
  interval — not asserted numbers) run over a **simulated** population
  (`growth/simulator.go`), and it is labeled "Simulated" prominently in
  the UI. Not a dishonesty issue. Worth noting: the Analytics page has
  no history list (unlike Safety's evaluation history) — only the most
  recently run experiment is ever visible; refreshing loses it.
- Single hardcoded operator account (`db/seeds/002_operator.sql`), no
  registration/invite/password-reset flow. Documented trade-off in
  `files/AUTH.md`.
- `FRONTEND_ORIGIN` CORS default is a single origin
  (`http://localhost:3000`), now env-overridable (fixed this session)
  but still single-value — a public judging URL needs exactly one
  origin configured correctly, no fallback list.

### 4.4 Low / cosmetic

- `backend/cmd/server/main.go` still stands up 4 separate ports/muxes
  (API Gateway `:8080`, Commerce `:8081`, Agent API `:8082`, Dashboard
  `:8083`) from an original microservice-shaped design, but — per the
  code's own comment — **every real route today lives on
  `commerceMux`/`:8081`.** The other three ports exist and have health
  checks but carry no real traffic; the frontend only ever talks to
  `:8081`. This is architecturally inert, not broken, but it's worth
  knowing it's not really "four services" today — see `PLAN-04` for
  whether to formalize or collapse this.
- `backend/Dockerfile`'s final stage uses the full `golang:1.26` SDK
  image rather than a slim runtime base (image size only, no runtime
  latency effect).
- Visual inconsistency: `checkout.tsx` uses a `zinc-*` Tailwind palette;
  every dashboard page uses `slate-*`. Not a bug, but the two halves of
  the same product currently look like two different products.

---

## 5. What NOT to change

Explicitly calling this out so nothing downstream "fixes" something that
isn't broken:

- The Policy Engine, audit hash chain, authorization/mandate model, and
  the "LLM proposes, deterministic engine decides" separation are the
  project's actual competitive edge for this track. Every plan document
  in this set treats these as load-bearing and only extends them
  (e.g. logging more agent steps into the *existing* audit trail),
  never bypasses or duplicates them.
- The MCP tool surface's "thin wrapper, no business logic duplication"
  rule (`backend/mcp/tools.go`'s own doc comment) is correct and should
  hold for any new tool added.
- Simulated/heuristic components (analytics, growth EV inputs) are
  fine to keep simulated as long as they stay labeled — the fix is
  never "make it fake data look real," it's "make the real parts reach
  further."
