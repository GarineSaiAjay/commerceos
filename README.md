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
/infra          docker-compose (Postgres 17 + Redis 8 + backend)
/files          Phase specs, audit, completion plan, agent contract
```

## Getting started

1. **Copy and fill the environment:**
   ```bash
   cp .env.example .env   # add RAZORPAY_KEY_ID / RAZORPAY_KEY_SECRET (Test Mode)
   ```

2. **Bring up the stack:**
   ```bash
   docker compose -f infra/docker-compose.yml up -d --build
   ```
   - Postgres `:5433`, Redis `:6379`, backend `:8080–8083`, frontend `:3000`.

3. **Apply migrations + seed:**
   ```bash
   goose -dir db/migrations postgres "postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable" up
   psql "postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable" -f db/seeds/001_catalog.sql
   ```

4. **Frontend:**
   ```bash
   cd frontend && npm install && npm run dev   # http://localhost:3000
   ```

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
| `GET /dashboard/overview` · `/analytics` · `/experiment` | Dashboard |
| `GET /adapter/calls` | Razorpay call counter (audit proof) |

## Docs

- `files/PROJECT-AUDIT.md` — full gap analysis + fix log.
- `files/COMPLETION-PLAN.md` — step-by-step plan for 100% real completion (what needs a human vs. what's automated).
- `files/phase-1..9-*.md` — the original phase specs.
- `files/agent-commerce-contract.md` — the agent API contract.