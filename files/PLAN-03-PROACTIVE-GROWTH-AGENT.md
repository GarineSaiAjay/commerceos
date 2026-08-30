# Plan 03 — Fixing the Silent Upsell Agent

Depends on: `REALITY-CHECK-2026-08-30.md` §3, which traces the exact
root cause. Read that first — this plan is the fix design, not a repeat
of the diagnosis.

## Recap of the bug in one line

`fetchSuggestion()` in `frontend/app/checkout.tsx` only ever runs when
`useEffect(..., [step, cart?.cart_id, cart?.items.length])` sees
`step === "cart"` — and the normal buyer flow (accept the agent's
product → "Keep shopping") sets `step` back to `"catalog"` and leaves it
there, so the real, working, policy-gated suggestion engine
(`backend/growth/suggest.go`) never fires again for the rest of the
session.

## Design principles for the fix

The user explicitly does **not** want a nagging, dark-pattern shopping
experience — "helpful for clients and also for sellers," minimal UI
preserved. Every surface below is designed to be low-frequency,
dismissible, and to respect a dismissal once given. None of it changes
`growth/suggest.go`'s actual decision logic (budget gate, EV scoring) —
this plan is entirely about *where the existing, already-correct
recommendation gets a chance to be seen*.

---

## 1. Decouple suggestion-fetching from `step`

The one-line root cause fix: trigger `fetchSuggestion()` on cart
mutation, not on screen navigation.

```tsx
useEffect(() => {
  if (cart && cart.items.length > 0) {
    fetchSuggestion();
  }
}, [cart?.cart_id, cart?.items.length]);
```

(Drop the `step === "cart"` condition entirely.) This alone fixes the
literal reported bug: the suggestion is now computed the moment an item
lands in the cart, regardless of which screen the buyer navigates to
next.

**Rendering it while browsing** needs a small, deliberately unobtrusive
surface — not a full copy of the cart's suggestion card floating over
the catalog (that would be exactly the nagging pattern to avoid). Add a
persistent, small badge on the existing "Cart"/checkout affordance
(wherever the cart total or item count is currently shown in the
catalog-step header) — e.g. "Cart (2) · 1 suggestion" — that expands the
existing suggestion card only on click. This keeps the catalog screen's
information density unchanged for a buyer who ignores it, while making
the suggestion discoverable without requiring a full navigation back to
the cart screen.

## 2. Cross-sell inside the agent chat itself

Today, accepting an agent-proposed product (`acceptAgentPlan`) calls
`addToCart` and stops — the growth agent and the shopping agent chat
have zero connection. Fix: after `acceptAgentPlan` successfully adds an
item, call `fetchSuggestion()` immediately and, if available, render it
*inline in the same chat panel* ("Since you added AirPods Pro, buyers
often add an AirPods Case too — add it?") rather than requiring the
buyer to notice a cart badge. This is the single highest-impact fix for
the reported experience specifically, since it puts the second
suggestion in the exact place the buyer was already looking.

## 3. Cross-sell on the product detail view (pre-cart)

From `PLAN-02-CATALOG-AND-COMMERCE.md` §4: the same tag-overlap scoring
`growth/suggest.go` already computes for "what pairs with what's in the
cart" generalizes cleanly to "what pairs with this one product" — same
function, different input (score against one product's tags instead of
the cart's aggregate tag set). Surfaced as a small "Frequently paired
with" line in the product detail panel. This reaches buyers who never
even open the agent chat, which today get zero cross-sell exposure
whatsoever.

## 4. Post-checkout cross-sell ("complete the set")

The order-complete screen (`step === "complete"`) currently shows the
receipt and audit trail and nothing else. Add one more suggestion slot
here, scored against the *just-purchased* items rather than a live cart
(same `growth/suggest.go` logic, called once against the completed
order's line items). This is a real, low-risk revenue surface most
e-commerce checkouts use and this app currently has zero equivalent of.

## 5. Respect dismissal properly — persist it, don't just hide it locally

Today `dismissedProductId` is component state: it resets on reload and
on a new cart. Fix: when a buyer dismisses a suggestion
("No thanks"), record it server-side against the `cart_id` (the
`recommendations` table already has a `REJECT`-shaped row concept for
budget rejections — extend the same table with a `DISMISSED` decision
value distinct from `REJECT`, so it's queryable the same way
`RejectedDemandByProduct` already queries `REJECT` rows for the
Campaign Orchestrator). `SuggestHandler.Suggest` excludes any product
already dismissed for that cart. This means a buyer who says no to the
AirPods Case once won't see it proposed again three screens later —
which is *also* what prevents the multi-surface approach above (§1–§4)
from turning into the exact nagging pattern the user wants avoided:
more surfaces, but each one respects the same no.

## 6. Frequency cap, not just per-product dismissal

Even with per-product dismissal, a buyer who keeps adding items could
theoretically see a new suggestion at every one of the four surfaces
above in quick succession. Add a simple session-scoped cap: at most 2
suggestion impressions shown per cart per 10-minute window (a small
`shown_count`/`last_shown_at` pair alongside the recommendation row is
enough — no new infrastructure). This is a deliberate quality-over-
quantity choice matching the user's "helpful, not annoying" framing.

## 7. Event-driven path (stretch, not required for the fix)

The Redis Stream event bus is already provisioned
(`backend/events/redis_stream_bus.go`) but its only consumer
(`stream_consumer.go`) just logs and acks — a documented placeholder.
Once real downstream consumers exist for other reasons (analytics,
notifications), a `cart.item_added` event could drive suggestion
computation server-side/asynchronously instead of the client polling on
mutation — this is the "proactive, not reactive" version of the fix and
is worth doing eventually, but §1–§6 already solve the reported problem
with far less risk and no new infrastructure to keep healthy for
judging day. Recommended as a `PLAN-06`-tier stretch goal, not part of
the core fix.

## 8. Make the fix's impact measurable

Add two counters to the `recommendations` table's existing row shape (or
a small companion table): `shown_at` (already implicit via
`created_at`) and an `accepted` boolean set when `acceptSuggestion()`
succeeds. Surface **suggestion impressions vs. acceptances** on the
merchant dashboard's Overview page, next to the existing `ai_revenue`
metric (`PLAN-05-SELLER-DASHBOARD.md` §3 covers the dashboard side).
Today a merchant sees a single lifetime revenue number attributed to AI
and no way to tell whether the cross-sell agent is actually engaging
buyers or just occasionally getting lucky — this closes that visibility
gap and gives judges a concrete, honest metric to point at instead of a
single aggregate figure.

---

## Phasing

| Phase | Scope | Effort | Risk |
|---|---|---|---|
| P0 | §1 decouple fetch from `step`, add cart badge | 2–3 hours | Very low — the exact bug fix |
| P0 | §2 inline suggestion after agent-chat accept | 3–4 hours | Low |
| P0 | §5 persist dismissal server-side | 3–4 hours | Low — extends existing table |
| P1 | §3 product-detail "frequently paired with" | 1 day | Low, reuses scoring (shared with `PLAN-02`) |
| P1 | §4 post-checkout cross-sell | 0.5 day | Low |
| P1 | §6 frequency cap | 0.5 day | Low |
| P1 | §8 impression/acceptance metrics on dashboard | 1 day | Low |
| P2 | §7 event-driven suggestion pipeline via existing Redis Streams | 2–3 days | Medium — real infra work, only worth it once other consumers exist |

Total for a fully fixed, multi-surface, non-annoying growth agent
(P0+P1): roughly 4–5 focused days, almost entirely additive to existing,
already-tested backend logic.
