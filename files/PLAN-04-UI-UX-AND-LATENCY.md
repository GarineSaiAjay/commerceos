# Plan 04 — UI/UX Polish and Latency, Without Losing the Minimalism

Depends on: `REALITY-CHECK-2026-08-30.md` §1.2, §4.4. Two explicit user
constraints govern this whole document: **keep the minimalist UI, don't
make it slower or clumsier**, and **don't lose the architecture already
built** — every change below is additive or a same-shape refactor, never
a rewrite.

These two topics (UX polish and latency) are combined into one document
because in this codebase they're mostly the same fix wearing different
hats: skeleton loaders, component splitting, and caching are UX
improvements that are also latency improvements.

---

## Part A — UI/UX

### A1. Fix the two-palette problem first

`checkout.tsx` (buyer-facing) uses a `zinc-*` Tailwind palette;
every dashboard page uses `slate-*`. These are visually close enough
that it reads as "almost consistent," which is worse than clearly
different — it looks like a mismatch rather than a design choice. Pick
one (recommend `slate`, since 6 of 7 screens already use it) and do a
mechanical find-and-replace in `checkout.tsx`'s className strings. Zero
behavior change, meaningfully more polished, ~1 hour of work.

### A2. Split `checkout.tsx` (1,559 lines, one file) into components

Not a redesign — the same JSX, moved. Concrete split:
`ProductList`, `AgentChatPanel`, `CartPanel` (+ `SuggestionCard`),
`OrderHistoryPanel`, `AuditTrailPanel`, `CheckoutFlow` (the `step`
state machine and orchestration). Why this belongs in a UX plan, not
just a refactor: it's what makes every other change in `PLAN-01/02/03`
(chat history UI, product detail panel, cross-sell card in three new
places, variant picker) safe to add without the file becoming
unreviewable, and it directly enables the code-splitting in §B3 below.

### A3. Loading states — reuse what already exists, apply it everywhere

The dashboard already has a `Skeleton` component
(`frontend/lib/format.tsx`) used consistently across dashboard pages.
`checkout.tsx` has **no skeleton states at all** — just text like
"Checking for a complementary item..." Apply the existing `Skeleton`
component (import it, don't reinvent it) to: initial catalog load,
agent "Thinking...", suggestion fetch. This is a genuine consistency and
polish win for near-zero effort, since the component already exists and
is already proven elsewhere in the same codebase.

### A4. Motion — small, purposeful, not decorative

Add a single consistent transition (150ms ease, matching Tailwind's
default) to: suggestion card enter/exit, agent plan card enter,
policy-rejection screen item removal. This is the only motion addition
recommended — anything more (page transitions, staggered lists) would
work against the "don't make it clumsy" constraint. Tailwind's built-in
`transition`/`duration-150` utilities are sufficient; no animation
library needed.

### A5. Accessibility pass

`checkout.tsx`'s buttons largely lack `aria-label`s that the dashboard
pages already consistently have (e.g. quantity +/- buttons do have them;
"Add to cart," "Ask," "No thanks" do not carry any beyond visible text,
which is usually fine, but form inputs like the agent prompt box should
gain `aria-live` on the response region so screen readers announce the
agent's proposal). Small, mechanical, and consistent with the "perfectly
wired, no hardcoding" standard the user asked for everywhere else.

---

## Part B — Latency

### B1. The LLM call is the single biggest latency risk in the app

`LLMExtractor`'s HTTP client timeout is 60 seconds
(`backend/agents/llm_extractor.go`), and every `askAgent()` call in the
frontend blocks on it synchronously (`await fetch(...)`, no
optimistic UI beyond a static "Thinking..." label). A slow OpenRouter
response is currently a worst-case ~60-second frozen "Thinking..." state
for the buyer.

The 60s ceiling itself is a reasonable *safety* bound (the code comment
correctly argues 30s was too aggressive against real provider variance,
and `FallbackExtractor` exists precisely so a timeout degrades to the
deterministic extractor rather than failing outright) — the problem is
that the fallback only kicks in *after* the full timeout elapses,
serially. Fix: **race, don't fall back after a full wait.**

- Run `LLMExtractor.Extract` and `DeterministicExtractor.Extract`
  concurrently (the deterministic one is a pure function over the
  prompt string — effectively instant, no I/O).
- If the LLM result lands within a short window (recommend 3.5s — well
  past typical provider latency, well short of a frozen UI), prefer it.
- If not, show the deterministic result immediately (better UX: an
  instant, correct answer beats a slow, marginally-better one for a
  live demo) — and if the LLM result arrives afterward and *differs*,
  this is a natural place for the agentic-loop clarification turn from
  `PLAN-01` §3 ("I found a better match once I looked closer — want to
  switch to X instead?") rather than silently discarding the slower
  answer.
- This changes zero external behavior/contracts — `FallbackExtractor`'s
  interface stays the same; this is an internal race instead of a
  sequential timeout-then-fallback, implemented inside
  `main.go`'s existing extractor-selection wiring.

### B2. Cache the catalog, don't refetch it on every mount

`checkout.tsx` fetches `GET /products` fresh on every mount with no
caching. At 13-50 products this is small, but it's still an avoidable
round trip on every navigation back to the catalog step, and it will
matter more once `PLAN-02`'s variants/reviews widen the payload. Two
independent, additive layers:

- **Client-side**: a short in-memory cache (module-level variable or
  `useRef`, TTL ~30s) so repeated navigation within one session doesn't
  refetch; this needs zero new dependencies.
- **Server-side**: Redis is already provisioned and running
  (`infra/docker-compose.yml`) but is currently used *only* for the
  Streams event bus — nothing caches through it today. Add a short-TTL
  (5-10s) cache of `GET /products`' response in
  `catalog/service.go`, invalidated on any `CreateProduct` /
  `UpdateProduct` / `DeleteProduct` (which, per `PLAN-02` §5, will be
  the dashboard catalog-management page once it's gated and built) —
  this both reduces DB load under concurrent judging traffic and
  shaves a real DB round trip off every catalog fetch.

### B3. Optimistic client-side pre-score for cross-sell

`PLAN-03`'s new suggestion surfaces (§1–§4) each trigger a
`POST /growth/suggest` round trip. The *authoritative* recommendation
(budget-gated, policy-scored, persisted) must stay server-side — that's
non-negotiable, it's the same "deterministic engine decides" principle
protecting checkout. But the product list is already fully fetched
client-side with all its `use_cases`/`features`/`compatibility` tags —
the same lightweight tag-overlap scoring `growth/suggest.go` runs can be
duplicated (a ~20-line pure function, not a service) client-side purely
as a **perceived-latency optimization**: show a skeleton-free "maybe:
AirPods Case" instantly using the client-computed guess, then swap to
the server's authoritative response (which also carries the real EV
reasoning text) the moment it arrives. If they disagree, the server
result always wins and replaces the client guess. This never affects
what's actually offered or gated — purely UI responsiveness.

### B4. Code-split the dashboard and checkout bundles

Once `checkout.tsx` is split into components (A2), lazy-load
rarely-used panels (`OrderHistoryPanel`, `AuditTrailPanel`) via
`next/dynamic` so the initial catalog-view bundle stays small. Similarly
for dashboard pages — Next.js's App Router already code-splits by route
by default (each `/dashboard/*` page is its own chunk), so this is
mostly already true on the dashboard side; the win is specifically in
`checkout.tsx`'s single large client component.

### B5. HTTP-level basics

Confirm (and if missing, add) gzip/br response compression on the Go
backend for `GET /products` and dashboard JSON endpoints — Go's
`net/http` doesn't compress by default; a small middleware
(`compress/gzip` wrap, or the standard `NewCompressor`-style pattern) on
`commerceMux` is a same-day addition with no architectural risk.
Content-Length-sensitive endpoints (webhooks, payment) should be
excluded to avoid interfering with signature verification — apply the
middleware selectively.

### B6. Don't "fix" the 4-port layout, but document it as intentional

Per `REALITY-CHECK` §4.4, `main.go` still stands up 4 ports though only
`:8081` carries real traffic today. This has **no measurable latency
cost** (the frontend only ever calls `:8081`; the other 3 ports are just
idle listeners with health checks) — recommend leaving it as-is rather
than spending effort collapsing it, but add one code comment at the top
of `main.go`'s port-setup section stating this explicitly (the existing
comment already half-says this — make it unambiguous) so a future judge
or contributor reading the code doesn't mistake 4 ports for 4 real
services and either over-credit or over-criticize the architecture.

### B7. Concrete latency budgets (so future changes are measured, not vibed)

| Interaction | Target p50 | Target p95 | Current risk |
|---|---|---|---|
| Catalog list load | < 150ms | < 400ms | Low today; matters more after Plan 02 widens payload — B2 covers it |
| Add to cart | < 250ms | < 600ms | Low, already a single write |
| Ask agent (LLM path) | < 3.5s | < 6s (race ceiling from B1) | **High today** — up to 60s worst case, B1 is the fix |
| Ask agent (deterministic fallback) | < 100ms | < 250ms | Already fast, just not reached soon enough |
| Cross-sell suggestion | < 400ms | < 900ms | Medium — one extra round trip per surface added by Plan 03 |
| Dashboard overview load | < 500ms | < 1.2s | Low, single aggregate query |

---

## Phasing

| Phase | Scope | Effort | Risk |
|---|---|---|---|
| P0 | B1 race LLM vs deterministic extractor | 1 day | Low — internal wiring change only |
| P0 | A1 unify palette | 1 hour | Very low |
| P1 | A2 split `checkout.tsx` into components | 1–2 days | Low-medium — mechanical, needs care not to regress behavior |
| P1 | A3 apply existing `Skeleton` to checkout screens | 0.5 day | Very low |
| P1 | B2 catalog caching (client TTL + Redis) | 1 day | Low-medium — cache invalidation correctness |
| P2 | A4 motion, A5 accessibility pass | 1 day combined | Very low |
| P2 | B3 optimistic client-side cross-sell pre-score | 1 day | Low, purely additive |
| P2 | B4 code-splitting rarely-used panels | 0.5 day | Low, depends on A2 |
| P2 | B5 response compression | 0.5 day | Low |
