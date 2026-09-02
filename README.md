# CommerceOS

An **autonomous revenue & checkout agent** built on Razorpay — a modular
monolith (Go backend + Next.js frontend) where an LLM has **intent authority**
but **never financial authority**. Every money-moving action is gated by a
deterministic Policy Engine, recorded in a tamper-evident audit chain, and
explainable end-to-end.

> **Governing principle:** the LLM can recommend and reason. It never decides
> whether money moves. That belongs to the deterministic policy and
> authorization layer.

## Architecture

```
LLM (intent) ──▶ Agent (proposal) ──▶ Policy Engine ──▶ Payment Adapter ──▶ Razorpay
                                              │                     │
                                              ▼                     ▼
                                       authorizations          webhooks
                                              │                     │
                                              ▼                     ▼
                                     audit chain (tamper-evident)   order/payment state machines
```

| Layer | Role |
|---|---|
| AI (Buyer/Growth Agent) | Reasoning, intent, recommendations |
| Policy Engine | Deterministic permission checks |
| Authorization / Mandate | Consent, bound to a cart |
| Payment Adapter | The only path to Razorpay (call counter exposed) |
| Webhook / Event pipeline | Dedup + signature verification + outbox |
| Audit Ledger | Hash-chained accountability |

## Repository layout

```
/backend        Go modular monolith (catalog, cart, orders, payments, policy,
                agents, analytics, events, audit, MCP)
/frontend       Next.js app (checkout + merchant dashboard)
/db             goose migrations + seeds (amounts are paise — ₹1 = 100 paise)
/infra          docker-compose (Postgres 17 + Redis 8 + migrate + backend + frontend)
/files          Auth design, git workflow, agent contract, trust boundary,
                demo script, pitch, and this audit trail
```

## Getting started

1. **Copy and fill the environment:**
   ```bash
   cp infra/.env.example infra/.env   # add RAZORPAY_KEY_ID / RAZORPAY_KEY_SECRET (Test Mode)
   ```
   `infra/.env` is what `docker compose` in step 2 actually reads (see
   that file's own header comment) -- the backend binary itself never
   reads a `.env` file directly, it reads process environment variables.

2. **Bring up the whole stack** — Postgres, Redis, migrations + seeds,
   backend, and frontend:
   ```bash
   docker compose -f infra/docker-compose.yml up -d --build
   ```
   - Postgres `:5433`, Redis `:6379`, backend `:8080–8083`, frontend
     `:3000` (open `http://localhost:3000`; the merchant dashboard is
     at `http://localhost:3000/dashboard` -- `files/AUTH.md` has the
     demo login credentials; a public, no-login `http://localhost:3000/trust`
     page shows the same audit-chain/safety-suite evidence for a judge
     who doesn't have those credentials). The checkout page's "Guided
     demo" toggle (item 38, `files/PLAN-06-ADDITIONAL-OPPORTUNITIES.md`
     §4) turns on a persistent checklist that tracks the same six beats
     as `files/demo-script.md` -- ask the agent, accept its proposal,
     see the cross-sell, go over budget, watch the graceful rejection,
     read the audit trail -- so a judge can self-drive the whole story
     without a live presenter narrating it.
   - A one-off `migrate` service applies goose migrations and then both
     seed files automatically before `backend` starts (every seed
     `INSERT` uses `ON CONFLICT ... DO NOTHING`, so this is safe to run
     on every `up`, not just the first) -- there is no separate manual
     migration/seed step anymore. `backend` waits for `postgres` and
     `redis` to report healthy and for `migrate` to finish
     successfully before it starts, so a fresh `up` no longer races a
     not-yet-ready database.
   - That's it. Skip straight to [Testing](#testing) below.

### Running without Docker

To run the Go backend or the Next.js frontend directly on the host
instead (e.g. for faster iteration), start only the infra services and
do the rest by hand:

```bash
docker compose -f infra/docker-compose.yml up -d postgres redis migrate
```

then:

- **Backend:** the root `.env.example` is for exactly this workflow --
  copy it to `.env`, fill it in, and
  `export $(cat .env | xargs) && cd backend && go run ./cmd/server`.
- **Frontend:** `cd frontend && npm install && npm run dev` --
  `http://localhost:3000`.

## Testing

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd frontend && npm run lint && npm run build
```

> The DB-backed integration tests (`audit`, `analytics`, `order`, `cart`,
> `catalog`, `payment` repo tests) require the Postgres container on `:5433`.

## Key endpoints (Commerce Service `:8081`)

| Endpoint | Purpose |
|---|---|
| `GET /products` | Catalog |
| `POST /carts` · `POST /carts/{id}/checkout` | Cart → order |
| `POST /policy/mandates` · `POST /policy/propose` | Consent + chokepoint |
| `POST /orders/{id}/payment` | Authorized payment (needs `Authorization-Id`) |
| `POST /orders/{id}/payment/verify` | Client-side signature verification |
| `POST /webhooks/razorpay` | Razorpay webhook (signature + dedup) |
| `POST /mcp` | MCP server (JSON-RPC) |
| `GET /.well-known/agent-commerce.json` | Agent-readable manifest: MCP tools, REST endpoints, mandate/policy model, example flows |
| `GET /dashboard/overview` · `/analytics` · `/experiment` | Dashboard |
| `GET /adapter/calls` | Razorpay call counter (audit proof) |
| `GET /trust/summary` · `POST /trust/run-suite` | Public audit-chain status, call counter, and one-click 14-attack suite (no auth) |

## Docs

- `files/AUTH.md` — operator auth: demo credentials, what's gated vs. guest-accessible, and the PBKDF2 trade-off.
- `files/GIT-WORKFLOW.md` — branching, commit, and PR conventions for this repo.
- `files/agent-commerce-contract.md` — the agent API contract.
- `files/trust-boundary.md` — the untrusted → trusted data flow and what re-validates each input.
- `files/pitch-one-pager.md` — the one-page pitch.
- `files/demo-script.md` — the five-minute live demo script.

Historical, for design-history reference only (item 37, `files/PLAN-06-ADDITIONAL-OPPORTUNITIES.md` §6) -- both predate implementation and describe some capabilities that were deliberately scoped down or not built; each has an "as-built vs. as-designed" note explaining exactly where:

- `files/ORIGINAL-VISION-AUTONOMOUS-REVENUE-AGENT.md` — the original vision/planning doc.
- `files/ORIGINAL-VISION-BUILD-GUIDE.md` — the original phase-by-phase build plan.