# CommerceOS — One-Page Pitch

## The line

**We built the trust layer for agentic commerce.**

Not "an AI shopping assistant." Not "an AI payment agent." The product is the boundary between an AI's *reasoning* and an AI's *authority to move money* — and making that boundary real, auditable, and impossible to talk your way around, even from inside the conversation with the agent itself.

## The problem

AI agents will increasingly be the buyer, not just the recommender. But an agent with unrestricted access to a payment method is a liability, not a feature — a wrong amount, a wrong merchant, a duplicated charge, or a cleverly-worded prompt injection are all one bad inference away. Most "agentic commerce" demos paper over this with a thin wrapper: `LLM → create_order()`. That's not a control, it's a hope.

## The architecture, in one line each

| Component | Represents | What actually enforces it |
|---|---|---|
| AI (Buyer/Growth Agent) | Reasoning | LLM intent extraction (OpenRouter) + deterministic expected-value cross-sell scoring — proposals only, never authority |
| Policy Engine | Permission | Deterministic checks (merchant allowlist, currency, ceiling, product, budget tolerance, duplicate, consent, mandate binding) — every proposal passes through here, no exceptions |
| Authorization / Mandate | Consent | A cart-bound mandate with a spending ceiling and confirmation threshold; three levels (auto-approve, confirm, hard gate) route by amount **and** computed risk score |
| Payment Adapter | Execution | The only code path that ever calls Razorpay — swappable behind one interface, call count exposed live for verification |
| Webhook / Event Pipeline | Truth | Signature-verified, deduplicated Razorpay webhooks drive order state — the system never trusts its own optimism about payment status |
| Audit Ledger | Accountability | Hash-chained event log; a tampered row is detectable, not just theoretically detectable |

## What makes this defensible, not just demoable

- **The LLM never touches money.** It can recommend a product or an amount; the Policy Engine is the only thing that can turn a proposal into an authorization, and it re-evaluates server-side even at approval time — a stale or drifted request is rejected, not rubber-stamped.
- **Three real authorization levels**, not one modal reused for everything: Level 1 auto-approves small, low-risk actions; Level 2 requires an explicit confirm; Level 3 is a distinct, non-dismissible hard-gate screen for anything above ₹10,000 or high computed risk.
- **Failure is a first-class state, not an afterthought.** A failed payment never double-charges (idempotency-key reuse on retry), and the buyer gets three genuine recovery paths — retry, change payment method, or remove an item and re-checkout through the full policy pipeline again.
- **Red-teamed against 14 distinct attack categories** — authorization override, excessive amount, merchant-metadata manipulation, hidden cart additions, failed-payment replay, merchant swap, price manipulation (×2), approval bypass, prompt injection (as a user message *and* planted in real catalog data an external LLM client would read), tool injection, data exfiltration, and goal hijacking — plus a 100-scenario evaluation suite. Every attack is blocked with **zero** Razorpay API calls made, verified live via an exposed call counter, not asserted.
- **Every dashboard number is real.** Revenue, AI-attributed revenue, conversion, AOV, the audit trail, and the safety evaluation summary are all computed from persisted tables at request time — nothing is hardcoded for the demo.
- **A real external protocol surface.** An MCP server exposes the same commerce tools, with real per-tool JSON Schemas (not a bare `{"type":"object"}` stub), over plain JSON-RPC 2.0 to any MCP client that speaks simple request/response (Claude Desktop, the MCP Inspector both do) — this isn't a bespoke API only the demo frontend can talk to. One honestly-documented gap: the server doesn't implement the Streamable-HTTP transport's server-initiated SSE half, so a client that specifically requires that won't work here (`files/agent-commerce-contract.md`).

## Why a modular monolith, not microservices

Four logical services (API Gateway, Commerce, Agent API, Dashboard) inside one deployable Go binary. Enough separation to reason about boundaries; none of the operational overhead a hackathon build doesn't need to fake. Architecture theatre is its own anti-pattern.

## The one sentence a judge should be able to repeat after watching the demo once

*"The LLM never directly moved money."*
