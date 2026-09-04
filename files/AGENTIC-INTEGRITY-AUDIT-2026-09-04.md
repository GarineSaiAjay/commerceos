# Agentic Integrity Audit — 2026-09-04

Triggered by a live test: prompting the shopping agent with "i want a pair of
shoes" returned a confident proposal for a "Laptop Screen Cleaning Kit"
("best-priced match in laptop within your ₹3000 budget"), with an
"Anti-Theft Cable Lock" as the alternative — and no OpenRouter calls were
observed in the provider dashboard despite heavy testing today. This document
traces both symptoms to root cause in the actual code, distinguishes them
from what is genuinely real in this codebase, and lists what to fix before
submission.

## TL;DR

Nothing here is deliberately faked or hardcoded as a shortcut. But two real,
high-severity bugs combine to make the demo *behave* exactly like a hardcoded
toy, which for a judge is indistinguishable from the real thing:

- **Finding A** — conversation memory silently reuses a stale category and
  budget from an earlier turn whenever a new message can't be parsed at all,
  instead of asking for clarification. This is exactly what produced the
  shoes → laptop-cleaning-kit answer.
- **Finding B** — `OPENROUTER_API_KEY` almost certainly never reaches the
  running backend container, because of a docker-compose `.env`
  resolution mismatch against the README's own documented startup command.
  If true, every "AI" answer today came from the keyword-matching fallback,
  never a real LLM call — which matches "no calls to OpenRouter" exactly.
- **Finding C** — there is zero UI signal telling anyone whether an answer
  came from the real LLM or the deterministic fallback. Same "AGENT
  PROPOSES" copy either way. This is what let Finding B go unnoticed.

Fix B first — until it's fixed you are not testing your LLM path at all, so
no judgment about "is the agent smart" is happening on the real system.

---

## Finding A — conversation memory substitutes an old, unrelated request (CRITICAL)

`backend/agents/conversation.go`, `mergeIntent` (lines 65–86):

```go
func mergeIntent(prev Intent, next Intent) Intent {
	merged := prev

	if next.Budget > 0 {
		merged.Budget = next.Budget
	}
	if next.Category != "" {
		merged.Category = next.Category
	}
	if next.Priority != "" {
		merged.Priority = next.Priority
	}
	if next.Recipient != "" {
		merged.Recipient = next.Recipient
	}

	merged.Clarify = ""
	return merged
}
```

`merged` starts as an exact copy of the *previous* turn's intent, and a field
is only overwritten if the *new* turn actually produced a non-empty value for
it. `backend/agents/deterministic_extractor.go`'s `parseCategory` has a fixed
keyword switch (laptop/macbook/…, charger/cable/…, case/adapter/…,
airtag/tracker/…, earbuds/airpods/…) with no entry for "shoes" or anything
outside this Apple-accessories catalog, so it returns `category = ""`.
`parseBudget` finds no digits in "i want a pair of shoes" either, so
`budget = 0`.

Walk that through `mergeIntent`: `next.Budget` is `0` (not `> 0`, skipped),
`next.Category` is `""` (skipped) — so `merged` comes out **identical to the
previous turn's intent**, e.g. `category="laptop", budget=3000` from an
earlier "laptop accessory under ₹3000" message. `ValidateIntent(merged)`
passes (budget and category are both non-empty), so
`PlanCheckoutInConversation` treats "i want a pair of shoes" as a **fully
valid, confidently-answered continuation** of the old request, and
`planFromIntent` (`backend/agents/buyer_agent.go` line ~284) prints exactly:

> "Selected Laptop Screen Cleaning Kit (₹890) — best-priced match in laptop
> within your ₹3000 budget."

There is no code path here that ever says "I didn't understand that." A
message that shares *zero* signal with the prior turn is treated the same as
a legitimate follow-up like "no, for my brother instead."

**Fix direction:** when the newly-extracted intent has *no* signal at all
(budget `0` **and** category `""` **and** priority `""` **and** recipient
`""`), that is not a follow-up to merge — it's an unparseable new message,
and should return `ErrAmbiguousIntent` / a clarify prompt instead of falling
through to a silent full-previous-intent reuse. A regression test belongs
alongside it: "an off-topic prompt after a valid prior turn must ask for
clarification, never silently answer with the old intent."

---

## Finding B — `OPENROUTER_API_KEY` most likely never reaches the backend container (CRITICAL — explains "no calls to OpenRouter")

- `infra/.env` has a real-looking key (`OPENROUTER_API_KEY=sk-or-v1-...`).
- README's documented "Getting started" command (the one meant to be run,
  from the repo root):
  ```bash
  docker compose -f infra/docker-compose.yml up -d --build
  ```
- `infra/docker-compose.yml`'s `backend` service reads
  `OPENROUTER_API_KEY: ${OPENROUTER_API_KEY:-}`. Docker Compose resolves
  `${VAR}` interpolation from the shell environment or from a `.env` file —
  and by default it looks for that `.env` in the **current working
  directory the command is run from** (or `--project-directory` if passed),
  **not** automatically in the directory of the `-f` compose file. The
  repo root (where the README tells you to run this from) has no `.env` —
  only `.env.example` — confirmed via `ls -la` on the repo root.
  There's also no `env_file:` line in `docker-compose.yml` pointing at
  `infra/.env` to force it.
- Net effect: run the README's own documented command from the repo root,
  and `${OPENROUTER_API_KEY:-}` almost certainly resolves to an **empty
  string** inside the `backend` container — even though a real key sits
  right there in `infra/.env`, unused.
- Downstream (`backend/cmd/server/main.go` ~line 322):
  `agents.NewLLMExtractorFromEnv()` returns `nil` when the key is unset, so
  `agentExtractor` falls straight to `DeterministicExtractor` — permanently,
  silently, with **zero log line or UI indicator** that this happened. Same
  applies to `rejectionNarrator` and `reviewSummarizer`, which share the
  same env var.
- This is a documentation/wiring mismatch, not a code bug in the LLM path
  itself — the LLM extractor, racing logic, and cost guard all look
  correctly built (see `backend/agents/llm_extractor.go`,
  `racing_extractor.go`, `cost_guard.go`). The key just never arrives.

**How to confirm on your machine right now:**
```bash
docker compose -f infra/docker-compose.yml exec backend printenv OPENROUTER_API_KEY
```
Empty output = confirmed root cause. (I couldn't run this myself — `docker`
wasn't on PATH in the shell I have access to your machine through.)

**Fix options (pick one):**
1. Add `env_file: ../infra/.env` under the `backend` service (and any other
   service that needs it) in `infra/docker-compose.yml`. Most robust — works
   regardless of cwd.
2. Change the documented command to `cd infra && docker compose up -d --build`
   (Compose then finds `infra/.env` automatically because it matches cwd).
3. Change the documented command to explicitly pass
   `docker compose -f infra/docker-compose.yml --env-file infra/.env up -d --build`.

Option 1 is safest because it doesn't depend on how someone invokes the
command.

---

## Finding C — no UI signal distinguishes a real LLM answer from the fallback (transparency risk)

`frontend/app/checkout.tsx` and `AgentChatPanel.tsx` render the identical
"Agent proposes" copy and reasoning sentence regardless of whether
`agentExtractor` was the LLM or `DeterministicExtractor`. Nothing in the
`CheckoutPlan` JSON currently threads through *which* extractor answered, so
even the backend itself doesn't retain that distinction past the single
request.

This is exactly why Finding B could run undetected through "many AI things"
of testing today — a fully rule-based answer looks pixel-identical to an
LLM-reasoned one.

**Fix direction (cheap, and arguably a good demo beat):** thread the
extractor's identity (`"llm"` vs `"deterministic"`) into `CheckoutPlan` /
`ReasoningTrail`, and surface a small badge in `AgentChatPanel`
("AI-reasoned" vs "rule-based fallback"). Given the buildathon bar is "every
money action explainable, bounded, and gated... show ... one failure handled
gracefully," an honest, visible fallback indicator is a feature, not just a
bug fix — it's proof the system degrades gracefully instead of pretending.

---

## Already-known findings, current status (from `files/REALITY-CHECK-2026-08-30.md`)

Re-verified against the current code today, not just carried over:

- **RESOLVED** — unauthenticated product CRUD. `POST /products` and the
  `/products/{id}` PATCH/DELETE routes are now wrapped in
  `authService.RequireOperator` (`backend/cmd/server/main.go` ~lines
  824–904). No action needed.
- **RESOLVED** — the "cross-sell/upsell goes silent after first add-to-cart"
  bug. `checkout.tsx`'s suggestion-fetch effect no longer gates on
  `step === "cart"`; it now re-checks on every cart mutation regardless of
  screen (see the comment block above the `optimisticSuggestion`/`useEffect`
  pair, ~line 979).
- **RESOLVED** — catalog is no longer single-variant. It now has 100 SKUs
  (`db/seeds/001_catalog.sql`) with real per-product variants (colorways,
  cable lengths, AppleCare coverage tiers), not the one-variant-per-product
  state the Aug 30 audit flagged.
- **STILL OPEN** — the in-app shopping agent (`BuyerAgent.PlanCheckout`) is
  a fixed 3-stage pipeline (extract → search → pick top-1), not a real
  multi-turn tool-calling agent loop. The 11-tool MCP surface
  (`backend/mcp/tools.go`) is real and well-specified but runs as a second,
  disconnected implementation from the in-app buyer agent. See
  `files/PLAN-01-AGENTIC-CORE.md`.
- **STILL OPEN** — no rate limiting on the LLM-backed endpoints
  (`backend/ratelimit/limiter.go` exists but isn't clearly applied to the
  agent/LLM routes) — a real cost-control gap for a public judging URL. See
  `files/PLAN-06-ADDITIONAL-OPPORTUNITIES.md`.

---

## What is genuinely real here (so the fear is calibrated, not blanket)

- **Policy Engine, mandate/authorization model, hash-chained audit ledger**
  (`backend/policy`, `backend/audit`) — real deterministic checks and a real
  tamper-evident chain, not decorative UI.
- **Search/ranking** (`backend/tools/search.go`) — a real scoring function
  over budget, priority-feature match, and `use_cases` tags; not a hardcoded
  lookup table. It correctly returned zero-signal-driven results for
  "shoes" because there genuinely is no matching product — the bug is in
  what happened *after* that null result (Finding A), not the search itself.
- **Growth/cross-sell EV logic** (`backend/growth/suggest.go`) — a real,
  policy-gated engine; it was previously under-wired to a single screen,
  which is already fixed (see above).
- **Catalog data** — 100 real SKUs with dated, sourced India prices in the
  seed file's own comments (e.g. Apple's India store listings, Aug 2026).
  It's a genuine single-vertical (Apple/electronics-accessories) catalog by
  design — there was never going to be a "shoes" match, which is fine; the
  problem is only that the agent didn't say so.

---

## Suggested order of operations

1. **Fix Finding B** (docker-compose `env_file`, ~2 lines) — until this is
   fixed, every "is the agent smart" judgment you make is happening against
   the fallback, not the real LLM path.
2. **Fix Finding A** (`mergeIntent` guard against zero-signal new turns, plus
   a regression test) — small, contained change in `conversation.go`.
3. Re-run the exact repro (e.g. "laptop stand under ₹3000" then "i want a
   pair of shoes") after both fixes and confirm you now get a clarification
   question, not a confident wrong product.
4. Consider Finding C (extractor-source badge) — cheap, and turns an
   embarrassing gap into a demonstrable "graceful failure handled honestly"
   moment, which is literally called out in the track's own bar.

Follow `files/GIT-WORKFLOW.md` (branch per change, atomic Conventional
Commits, PR + merge) for all of the above, same as every other change in
this repo.
