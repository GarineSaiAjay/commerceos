# CommerceOS -- More Ways to Integrate Agents & AI Tools

This is a proposal document, not an implementation record: nothing
below is built yet. It's written from a full read of what the
codebase already does today, so every idea says explicitly how it
would plug into the existing architecture rather than inventing a
parallel one.

## What already exists (so the ideas below don't repeat it)

CommerceOS already has real agentic infrastructure, not a single
chatbot bolted onto a store front:

- **Buyer Agent** (`backend/agents`): natural-language checkout via
  `POST /agent/checkout` (single-shot: intent extraction -> catalog
  search -> a Proposed Action) and `POST /agent/loop` (a bounded,
  tool-calling agent -- `search_products`, `get_product`,
  `calculate_total`, `create_cart`/`add_item`, `recommend_bundle` --
  capped at `loopMaxToolCalls` and a `loopTimeout`). Intent extraction
  races an LLM against a deterministic fallback (`RacingExtractor`,
  a 3.5s window) so the agent degrades gracefully instead of hanging
  when the LLM is slow or unavailable -- the exact bug fixed earlier in
  this session ("context deadline exceeded" on `/agent/loop`).
- **Growth Agent** (`backend/growth`): cross-sell suggestions
  (`/growth/suggest*`), funnel/demand analytics, and a Merchant
  Simulator feeding real A/B experiments (`backend/analytics`).
- **Campaign Orchestrator** (`backend/campaign`): reads rejected-demand
  data and *proposes* discount campaigns -- it never activates one
  itself; an operator approves or rejects on the Campaigns dashboard.
- **Safety Suite** (`backend/safety`): a 14-attack red-team library run
  against the real Policy Engine, publicly surfaced (unauthenticated)
  via `/trust/run-suite` for judges.
- **MCP surface** (`backend/mcp`): 11 tools over JSON-RPC
  (`search_products`, `get_product`, `create_cart`, `add_item`,
  `recommend_bundle`, `calculate_total`, `request_authorization`,
  `create_checkout`, `execute_authorized_checkout`,
  `get_payment_status`, `explain_decision`) plus a machine-readable
  contract at `GET /.well-known/agent-commerce.json`, for any external
  MCP-speaking agent (Claude Desktop, a judge's own tooling, ...).
- **The one rule everything above already obeys**: an agent can only
  ever produce a *proposal*. Every proposal is re-validated by the
  deterministic Policy Engine (`backend/policy`) before
  `POST /orders/{id}/payment` is allowed to touch Razorpay. No new
  integration below should be allowed to skip that gate -- each one
  says explicitly how it stays inside it.

Everything from here down is new.

## 1. Returns & Refunds Concierge Agent

**The gap:** there is no returns flow in this codebase at all --
`payments.status` has a `refunded` enum value that nothing ever sets.
A buyer today has no way to ask for a refund except contacting the
merchant outside the app.

**The idea:** a buyer describes what happened in plain language
("the AirPods case arrived scratched, I want a refund"). An extractor
(same `RacingExtractor`-style LLM-with-deterministic-fallback pattern
`agents.LLMExtractor` already uses) pulls out the order ID and reason,
looks up the order's actual `return_policy.days` from the catalog and
the order's real age, and -- only if genuinely eligible -- produces a
`REFUND` Proposed Action. That proposal goes through the *exact same*
`policy.Propose` -> `POST /orders/{id}/payment`-equivalent gate every
other action already goes through; a new `checkReturnWindow` check
in the Policy Engine (same shape as the existing `checkMandateExpired`)
is the only new guardrail needed, not a parallel money-movement path.
This is the single highest-value gap found in this audit -- everything
else on this list is additive to an already-good UX; this one is
currently just missing entirely.

## 2. WhatsApp / SMS Shopping Concierge

**Why this one specifically, for this market:** WhatsApp is the
default commerce surface for a huge share of Indian shoppers -- more
so than a web checkout page for a lot of the target audience this
catalog (AirPods, MagSafe, AirTags, and now laptop accessories) is
priced for.

**The idea:** a thin channel adapter (a webhook receiver for the
WhatsApp Business Cloud API or a Twilio number) that translates an
inbound message into the same `POST /agent/checkout` /
`POST /agent/loop` call the web UI already drives, and translates the
`CheckoutPlan`/tool responses back into a WhatsApp message with quick-
reply buttons ("Confirm ₹24,900 AirPods Pro?" / "See alternatives").
No change to `backend/agents` itself -- this is purely a new front door
onto the existing agent, the same relationship the MCP surface already
has to the same underlying logic. The only new state needed is a
buyer-phone-number -> `cart_id` mapping so a multi-turn WhatsApp
conversation can reuse `PlanCheckoutInConversation`'s existing
conversation-memory support instead of starting cold every message.

## 3. Natural-Language Merchant Copilot for the Seller Dashboard

**The gap:** every seller-facing dashboard page (Catalog, Campaigns,
Settings) is a manual form today. A merchant who wants to "drop the
price on the slow-moving AirPods Max 3-pack" or "run a 15% campaign on
laptop accessories under ₹5,000 for two weeks" has to translate that
intent into individual field edits themselves.

**The idea:** a merchant-facing counterpart to the Buyer Agent -- reads
a natural-language merchant instruction, and produces a concrete,
inspectable *diff* against catalog/campaign/policy state (using the
already-real live data: `catalog.Service`, `campaign.Engine`,
`policy.Service`), which the operator reviews and applies from the
dashboard exactly the way `CampaignAgent`'s proposals already work
today (propose -> operator approves/rejects, nothing auto-applies).
This isn't a new trust model, it's the Campaign Orchestrator's
existing propose-only pattern extended to catalog edits and policy
settings, which today can only be proposed by that one agent for that
one narrow case (discount campaigns).

## 4. LLM-Narrated Rejection Explanations

**The gap:** `explain_decision` (one of the 11 MCP tools) already
exists, but `deps.Explain` is a deterministic template, not natural
language -- it fills in `{failed_check}`/`{reason}` into a fixed
sentence shape.

**The idea:** an optional LLM rephrasing pass over the *same, already-
computed* fields (`FailedCheck`, `Reason`, `Amount`, `Merchant`) that
the deterministic explainer already produces -- the LLM only rewrites
the sentence, it is never given the ability to see or influence the
actual decision (the same one-way trust boundary
`files/trust-boundary.md` already draws around the Policy Engine).
Same fallback discipline as `RacingExtractor`: if the LLM call times
out or fails, the existing deterministic template is what ships -- this
would never be allowed to make `explain_decision` less reliable than
it is today, only occasionally more readable.

## 5. Review Summarization Agent

**The gap:** `GET /products/{id}/reviews` already returns real,
persisted reviews (seeded plus real post-purchase ones), but the
product detail panel just lists them raw -- there's no synthesis.

**The idea:** a short "buyers say ..." one-or-two-sentence summary per
product, generated from that product's real review text and cached
with the same short-TTL pattern `catalog.Service.WithCache` already
uses for `ListProducts`, invalidated the same way a variant/price
change already invalidates that cache today. Bounded, low-risk: it
only ever summarizes real review text that's already public on the
product page, never invents a claim the reviews don't support.

## 6. Proactive Reorder / Replenishment Agent

**Why now specifically:** the catalog expansion (13 -> 100 products)
added a lot of genuinely consumable items -- ear tips, cleaning kits,
cables, screen-protector kits -- that weren't well represented before.

**The idea:** extend `growth.GrowthAgent` (which already tracks
`suggestion_impressions`/`recommendations.accepted` -- the real,
honest counter pair `analytics.Service.Compute` already reports) with
a background job that looks at a buyer's real order history, estimates
a typical repurchase interval per product category, and raises a
*proactive* suggestion through the exact same `/growth/suggest*` /
impression-tracking machinery the reactive cross-sell already uses --
this is a new trigger for an existing, already-measured system, not a
new decision surface.

## 7. Fraud / Anomaly Narrator on the Audit Trail

**The gap:** `audit.Verifier` already does real cryptographic hash-
chain integrity verification (`GET /audit/verify`, and publicly via
`/trust/summary`) -- it can prove the log wasn't tampered with, but it
has no opinion on whether the *pattern* of events in an intact log
looks unusual.

**The idea:** a read-only LLM pass over recent `audit_events` (the
same merchant-scoped query `analytics.Service.Overview` already runs,
after this session's P0 merchant-scoping fix) that narrates anomalies
in plain English for the operator dashboard -- "3 policy-config changes
in the last hour is unusual for this merchant" -- strictly narration
over already-computed, already-correct data. It never blocks or flags
a transaction itself; that stays the Policy Engine's job alone.

## 8. Gifting Concierge

**Why now specifically:** this session's catalog expansion added
`gift-wrap-service` and `gift-card-sleeve` as real, purchasable
products for the first time -- today nothing in the checkout flow ever
proactively surfaces them.

**The idea:** `agents.Intent` already extracts a `Recipient` field
("sister"/"brother") from phrasing like "for my bro" -- the exact
prompt from this session's bug report. When a checkout is recognized
as a gift, the Buyer Agent's `recommend_bundle` tool (already used for
cross-sell scoring) gets a gift-specific candidate set biased toward
`gift-wrap-service`/`gift-card-sleeve`, still ranked and budget-
constrained by the same deterministic `Searcher.scoreProduct` hard
constraints as every other suggestion -- never auto-added, just
surfaced the same way today's cross-sell card already is (with an
actual Add button, unlike the bug fixed earlier this session).

## 9. Voice Ordering

**The idea:** a thin speech-to-text step in front of
`POST /agent/checkout` -- since intent extraction already produces
strict schema-locked JSON from a text prompt
(`agents.ParseIntentJSON`), voice input only needs to become text
before it hits the exact same pipeline; no change to intent extraction
itself. Worth calling out for this market specifically: multi-language
STT (Hindi and other Indian languages, not just English) would open
the same agent up to buyers who wouldn't type a fluent English request
today.

## 10. Merchant Weekly Digest

**The idea:** the lowest-risk item on this list -- an LLM narration
pass over `analytics.Service.Overview`'s already-real numbers
(revenue, AI-attributed revenue, conversion rate, suggestion
acceptance rate) and the Growth dashboard's funnel/demand data, turned
into a plain-English weekly summary emailed or shown on login. Read-
only narration over numbers that are already computed and already
correct -- no new data source, no new decision, just a digest.

---

## A note on scope and risk, across all ten

Every idea above was deliberately scoped to fit the pattern this
codebase already enforces everywhere else: an LLM may extract intent,
rank options, or narrate an already-computed result, but it never
gets a new, unchecked path to move money, change a real price, or
alter policy -- that stays the Policy Engine's job, gated the same way
for a new agent as for the existing ones. **#1 (Returns & Refunds)**
is the one genuine capability gap (nothing like it exists today) and
would need one new Policy Engine check (`checkReturnWindow`) -- the
smallest architectural change on this list for the largest missing
piece of real functionality. **#2 (WhatsApp)** is the highest-leverage
distribution idea for this specific market and needs zero changes to
`backend/agents` itself. **#3, #4, #6, #7, #9, #10** are all additive
narration/ranking layers over agents or data that already exist.
**#5 and #8** are small, self-contained wins that follow directly from
work already done in this session (the review system and the gift-
wrap products, respectively).

None of this is a commitment or a roadmap -- it's what a full read of
the current agent/AI surface turned up as the most concrete next
steps, written down so they don't have to be rediscovered from
scratch later.
