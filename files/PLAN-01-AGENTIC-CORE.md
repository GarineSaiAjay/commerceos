# Plan 01 — Making the Shopping Agent Actually Agentic

Depends on: `REALITY-CHECK-2026-08-30.md` §1. Read that first — this plan
only touches the *shopping/buyer agent* half; the policy/audit/mandate
half is explicitly out of scope and must not be weakened by anything
here.

## Goal

Turn `BuyerAgent` from a fixed three-stage classifier
(extract → search → propose top-1) into a bounded, memoried, tool-using
loop — while keeping the exact same invariant the rest of the codebase is
built around: **the agent proposes, it never decides whether money
moves.** Every new capability below terminates in the same
`policy.ProposedAction` → `policy.Engine` chokepoint that already exists.

Why this is the highest-leverage change for the track: the brief's "why
now" is explicitly about agent-to-agent and conversational commerce
protocols (ACP, AP2, x402, NPCI's UAP). A judge evaluating against that
brief will look for *planning and tool use*, not just intent
classification. This project already built the hard, boring, correct
part (the safety rail). This plan makes the agent worth safety-railing.

---

## 1. Unify the two agent surfaces

Today there are two disconnected implementations of "the agent":

- **In-app**: `BuyerAgent.PlanCheckout` calls `IntentExtractor.Extract`
  and `Searcher.Search` directly (Go function calls).
- **External (MCP)**: `backend/mcp/tools.go` exposes `search_products`,
  `get_product`, `create_cart`, `add_item`, `recommend_bundle`,
  `request_authorization`, `create_checkout`,
  `execute_authorized_checkout`, `get_payment_status`,
  `explain_decision` — 11 tools, real schemas, genuinely usable by an
  external MCP client end-to-end.

**Step 1: make the in-app agent a client of its own MCP tools**, not a
separate code path. Concretely: `agents.Searcher.Search` and
`agents.BuyerAgent` should call the same tool functions
`backend/mcp/tools.go` calls (`searchProducts`, `getProduct`, etc.) —
today those already delegate to the underlying services
(`deps.Catalog`, `deps.Cart`…), so this is a refactor toward one shared
tool layer, not a rewrite. This matters for two reasons: (a) any
improvement to tool behavior (schema, filtering, cross-sell awareness)
now benefits both surfaces automatically instead of needing to be built
twice, and (b) it makes the in-app agent's own step-by-step tool calls
loggable in exactly the shape the MCP protocol already uses, which is
the natural trace format for the audit log extension in §4.

## 2. A bounded tool-calling loop, not a fixed pipeline

Replace the fixed `extract → search → propose` sequence with a real
(small, capped) agent loop:

```
loop (max 4 iterations, hard timeout budget e.g. 12s total):
  1. LLM sees: system prompt + conversation history + tool results so far
  2. LLM either:
     a. calls a tool (search_products, get_product, recommend_bundle, ...)
     b. asks a clarifying question (same ErrAmbiguousIntent path, but now
        mid-conversation, not a dead end)
     c. proposes a final CheckoutPlan (may name up to 3 ranked options,
        not just 1)
  3. tool result is appended to history, loop continues unless (b) or (c)
```

Concretely:
- Reuse the OpenAI-compatible function-calling shape already spoken by
  `LLMExtractor` (`backend/agents/llm_extractor.go` already POSTs to
  `/chat/completions` with `response_format: json_object` — extend this
  to `tools`/`tool_choice` instead of a single fixed JSON schema).
  `LLM_MODEL` (`openai/gpt-4o-mini` by default) supports function calling
  natively via OpenRouter with no provider change needed.
- The tool palette offered to the loop is exactly the **read-only and
  cart-building** subset of the existing MCP tools:
  `search_products`, `get_product`, `create_cart`, `add_item`,
  `calculate_total`, `recommend_bundle`. Deliberately **excluded**:
  `request_authorization`, `create_checkout`, `execute_authorized_checkout`
  — those stay behind the explicit human "Add to cart" / "Checkout"
  button clicks that already exist in `checkout.tsx`. This is the
  concrete embodiment of "LLM has intent authority, never financial
  authority": the loop can build a cart proposal through real tool use,
  but the actual authorization/payment tools are structurally
  unreachable from inside the loop.
- Hard bounds, all server-side and unconditional: max tool calls per
  turn (4), max wall-clock budget per turn (reuse and tighten the
  existing `llmRequestTimeout` pattern — see `PLAN-04` for the latency
  argument for lowering this), and a max-cost/day guard if the
  OpenRouter key is metered (simple in-memory counter is enough for a
  demo; log to the audit chain if it trips).

## 3. Real conversation memory

Today, `ErrAmbiguousIntent` throws away everything the buyer already
said. Fix:

- Add a `conversation_id` (can literally be the `cart_id`, since a
  buyer's cart already anchors their session — no new identity system
  needed) and a small `agent_conversations` table: `id, cart_id,
  role, content, tool_calls jsonb, created_at`. Each turn appends to
  it; each new `askAgent()` call from the frontend sends the
  `conversation_id` and the backend replays the stored history into the
  loop's message list before adding the new turn.
- Frontend change is small: `checkout.tsx`'s `askAgent`/`agentPrompt`
  state becomes a real (short) message list instead of a single
  input-and-clear box — same visual footprint (a chat-style scroll
  inside the existing "Ask the shopping agent" panel), no layout
  redesign needed, keeps the minimalist UI the user wants (see
  `PLAN-04`).
- This alone fixes a real reported-style failure mode: "actually, make
  it cheaper" or "no, for my brother instead" becomes a valid follow-up
  instead of a from-scratch re-extraction.

## 4. Explainability: extend the existing audit trail, don't build a new one

`RunsPage` (`frontend/app/dashboard/runs/page.tsx`) already renders a
per-run step timeline (`proposed → risk_assessed → policy_evaluated →
authorized → authorization_consumed`) reconstructed from the audit log.
Extend the same `steps` shape with agent-reasoning stages that happen
*before* `proposed`:

```
intent_extracted    -- what the model understood, verbatim
tool_called          -- e.g. "search_products(budget=25000, category=earbuds)"
tool_result_summary   -- e.g. "4 candidates, top: AirPods Pro 3 (score 4.5)"
alternatives_considered -- the other ranked results, not just the winner
proposed              -- unchanged, existing stage
```

This does two things at once: it satisfies "every money action
explainable" *and* the buyer/judge sees genuine multi-step reasoning
instead of a single opaque "Selected X" sentence. No new UI surface
needed — `RunsPage` and the buyer-facing `renderAuditTrail()` in
`checkout.tsx` both already render an arbitrary step list; this is a
backend-only change to what gets logged.

## 5. Offer real alternatives, not just the top-1

`Searcher.Search` already computes and ranks every matching product —
`BuyerAgent.PlanCheckout` throws away everything except `results[0]`.
Minimal, low-risk change: return the top 3 as `alternatives` on
`CheckoutPlan`, unchanged `SelectedID` remains the agent's actual
recommendation (so the "Add to cart" button behavior doesn't change),
but the panel gains a "or: AirPods 3 (₹18,900), Beats Fit Pro
(₹15,990)" line the buyer can tap to swap before accepting. This is a
half-day frontend change with zero backend risk (the data already
exists in the response, it's currently discarded before the JSON is
even built).

## 6. Proactive agent turns

Everything above is still buyer-initiated. The single biggest gap
against "agentic" is that the agent never speaks first. Two concrete,
scoped additions (deliberately small — an agent that talks too much is
worse than one that's quiet):

- **Cart-mutation trigger, not step-navigation trigger**: fold into
  `PLAN-03`'s fix directly — once cross-sell suggestions fire on cart
  changes regardless of which screen the buyer is on, that *is* a
  proactive agent turn (see `PLAN-03-PROACTIVE-GROWTH-AGENT.md`).
- **Policy-rejection recovery is already semi-agentic** (the existing
  `policy_rejected` screen with "Remove" buttons rebuilding a smaller
  cart) — extend it one step further: when a proposal is rejected for
  budget reasons, have the agent loop (§2) proactively re-run
  `search_products` with a lower implied budget and propose a
  substitute, instead of just asking the buyer to manually remove
  items. This reuses machinery that already exists on both sides
  (`RemoveItemAndRecheckout` on the backend, the rejection screen on the
  frontend) and turns a dead end into a second agent turn.

## 7. Protocol alignment (track-specific, do this even if scoped small)

The track brief names ACP, AP2, and x402 explicitly. This project
already has the right seam to plug into any of them without a rewrite:

- **MCP is the substrate to keep building on** — it's the closest
  existing thing to ACP's tool-call shape, and it's already real (§1
  makes it the *only* substrate, used by both surfaces).
- **x402** (HTTP 402 Payment Required as a payment rail) fits directly
  behind `commerce/payment/adapter.go`'s `Adapter` interface — its own
  doc comment literally says *"swapping the real rail (Razorpay) for a
  synthetic one (mock, x402, …) is a one-line change."* A stretch-goal
  `x402Adapter` implementing that interface (even a minimal one that
  only handles the 402 challenge/response handshake in test mode) is a
  concrete, demo-able artifact that directly answers "why now" — see
  `PLAN-06-ADDITIONAL-OPPORTUNITIES.md` for scoping.
- **Agent-readable catalog manifest**: publish a machine-fetchable
  `/.well-known/agent-commerce.json` describing the MCP endpoint, the
  11 tools, and the mandate/policy model in structured form — this is
  the literal "Agent-readable catalog" example direction from the
  track brief, and it's cheap: it's mostly `files/agent-commerce-
  contract.md`'s content, machine-readable instead of prose. Detailed
  in `PLAN-06`.

---

## Phasing

| Phase | Scope | Effort | Risk |
|---|---|---|---|
| P0 | §3 conversation memory (fixes the "retype everything" dead end) | 1 day | Low — additive table + small frontend state change |
| P0 | §5 show alternatives, not just top-1 | 0.5 day | Low — data already computed |
| P1 | §4 extend audit trail with reasoning stages | 1 day | Low — reuses existing `steps` rendering |
| P1 | §1 unify in-app + MCP tool layer | 1–2 days | Medium — refactor, needs the existing test suites (`agents_test.go`, `mcp_test.go`) kept green |
| P1 | §2 bounded tool-calling loop | 2–3 days | Medium-high — new LLM interaction shape, needs careful timeout/cost bounds (feeds `PLAN-04`'s latency budget) |
| P2 | §6 proactive rejection-recovery turn | 1 day | Low, depends on P1 |
| P2 | §7 x402 adapter stub + agent-readable manifest | 1–2 days | Low — additive, doesn't touch existing paths |

Nothing in this plan changes `policy.Engine`, `audit`, or the
authorization model. Every new code path still terminates at the same
`policy.Propose` chokepoint the rest of the system already trusts.
