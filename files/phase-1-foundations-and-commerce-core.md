# Phase 1 — Foundations & Commerce Core

**Project:** CommerceOS — Autonomous Revenue & Checkout Agent (built on Razorpay)
**Governing principle (never violate, even in this early phase):** *The LLM has intent authority. It never has financial authority.* Phase 1 contains no LLM code at all — it exists to prove the deterministic commerce rails work before any intelligence touches them.

---

## 0. Objective of This Phase

By the end of Phase 1, a human (or a script) must be able to: browse a seeded catalog → build a cart → check out → pay with a real Razorpay **Test Mode** card → see a webhook arrive → see the order marked complete — with **zero AI involved**. This is the foundation every later phase depends on. If this phase is shaky, every later phase inherits the instability.

Do not proceed to Phase 2 until every item in the Verification Checklist at the bottom is independently confirmed true, not assumed true.

---

## 1. Repository & Environment Setup

### 1.1 Repository structure
Create a modular monolith. Do **not** split into microservices — that is architecture theatre for a prototype of this size.

```text
/commerceos
  /frontend        (Next.js, TypeScript, Tailwind, shadcn/ui)
  /backend         (Go — modular monolith)
    /commerce      (catalog, cart, orders, payments)
    /orchestrator  (agent coordination — empty until Phase 4)
    /agents        (buyer agent, growth agent, risk agent — empty until Phase 4/5)
    /policy        (policy engine, mandates, authorization — empty until Phase 3)
    /events        (event bus, outbox, event processor)
    /audit         (tamper-evident log — basic version starts Phase 2)
    /analytics     (metrics, experiments — empty until Phase 6)
    /mcp           (agent-facing tool server — empty until Phase 7)
  /db              (migrations, schema)
  /infra           (docker-compose, env config)
```

Create every folder now, even the ones that stay empty until later phases — this keeps the module boundaries visible from day one and makes later phases additive rather than restructuring work.

### 1.2 Infrastructure
1. Provision **PostgreSQL** and **Redis** locally via docker-compose. Use Redis Streams for the event bus later — do not reach for Kafka/Redpanda unless a genuine need appears; it would be over-engineering for this scope.
2. Write `docker-compose.yml` bringing up: Postgres, Redis, and all Go services (even as empty skeletons at this stage).

### 1.3 Razorpay Test Mode account
1. Create a Razorpay account and switch to **Test Mode**.
2. Generate a **Test Key ID** and **Test Key Secret**.
3. Locate Razorpay's published Test Mode card numbers for both success and failure flows — you will need both later in this phase.

### 1.4 Secrets handling (get this right now — it never gets revisited)
The Razorpay **Key Secret** must never reach: frontend code, LLM context (relevant from Phase 4 onward), logs, Git history, or plaintext database columns.
1. Add `.env` to `.gitignore` before the first commit.
2. Create `.env.example` documenting every required variable name, with placeholder values only.
3. Load the Key Secret only inside the Payment Service process, server-side.
4. Run `git grep` for the literal secret value as a habit before every commit from here forward.

### 1.5 Service skeletons
Stand up empty HTTP servers for:
- API Gateway
- Commerce Service
- Agent API Service (empty handlers for now — filled in Phase 4)
- Dashboard API (empty handlers for now — filled in Phase 6)

Each must expose `GET /health` returning `200 OK`.

### 1.6 CI
Wire up CI (GitHub Actions or equivalent) to run lint + a placeholder test suite on every push. It doesn't need to test business logic yet — it needs to exist so later phases have somewhere to add real tests.

### Phase 1.1–1.6 Artifacts
- Full repo skeleton with all folders above
- `docker-compose.yml`
- `.env.example`
- Health-check endpoints on every service
- CI pipeline running lint + placeholder tests

### Phase 1.1–1.6 Verification
- [ ] `docker-compose up` brings up every service with no errors
- [ ] Every service's `/health` endpoint returns 200
- [ ] Razorpay Test Mode dashboard is accessible; Test Key ID/Secret exist
- [ ] `git log -p` and `git grep` for the Key Secret return **zero** matches anywhere in history
- [ ] A second person (or a clean clone) can clone the repo, copy `.env.example` to `.env`, fill in their own test keys, and run the whole stack with one command

---

## 2. Catalog Domain

### 2.1 Product schema
Products are **not** flat `{name, price}` records. Every product carries structured, agent-readable semantics — this is what makes the merchant "AI-native" later, so build it correctly now rather than retrofitting it in Phase 4.

```json
{
  "product_id": "airpods-pro-2",
  "title": "AirPods Pro",
  "price": { "amount": 24900, "currency": "INR" },
  "availability": 12,
  "features": ["active_noise_cancellation", "transparency_mode"],
  "compatibility": ["ios", "macos"],
  "use_cases": ["travel", "music", "calls"],
  "merchant": { "id": "merchant_001" },
  "return_policy": { "days": 7 },
  "shipping": { "estimated_days": 3 },
  "attributes": { "anc": true, "battery_hours": 30, "wireless": true },
  "purchase_constraints": { "max_quantity": 2 }
}
```

### 2.2 Implementation tasks
1. Create `merchants`, `products`, `product_variants` tables in Postgres. Store `features`, `attributes`, `purchase_constraints`, `compatibility`, `use_cases` as JSONB columns (or normalized side-tables if you prefer relational purity — JSONB is simpler and sufficient here).
2. Implement CRUD for products at the Commerce Service layer.
3. Seed a demo electronics catalog:
   - AirPods Pro — ₹24,900
   - AirPods Case — ₹1,999
   - AppleCare — ₹2,500
   - USB-C Adapter — ₹1,299
   - Add enough additional SKUs (10–20) so later search/ranking work (Phase 4) has something meaningful to filter over — vary `features`, `attributes`, and price ranges deliberately.
4. Write a round-trip test: create a product with nested `features`/`attributes`/`purchase_constraints` → fetch it → assert deep equality.

---

## 3. Cart Domain

1. Implement cart creation, add item, remove item, recompute totals.
2. Give every cart a **reservation TTL** (e.g., 9 minutes). This is not cosmetic — Phase 2's failed-payment recovery flow depends on carts staying "held" for a bounded window after a failed payment, so build the TTL mechanism now even though nothing consumes it yet.
3. Store `carts`, `cart_items` tables.
4. Cart totals must be recomputed server-side on every mutation — never trust a client-supplied total.

---

## 4. Order Domain

1. Implement order creation **from a cart snapshot** — never a live reference to a mutable cart. Freeze the amount and item list at order-creation time by copying the values, not by foreign-keying into the live cart row.
2. Store `orders`, `order_items` tables.
3. Write a test proving this explicitly: create an order from a cart, then mutate the original cart (change quantity/remove item), then re-fetch the order — the order must be unchanged.

---

## 5. Razorpay Adapter

This is the single most structurally important piece of Phase 1, because Phase 3's entire authorization guarantee depends on it being the *only* code path that can reach Razorpay.

1. Build a thin, isolated Go module/package (e.g. `/backend/commerce/razorpay_adapter`) wrapping the Razorpay Orders API.
2. **Nothing else in the codebase may import the Razorpay SDK or call `api.razorpay.com` directly.** Enforce this as a rule now — every future phase's "policy engine gates everything" guarantee is built on this constraint existing structurally, not by convention.
3. Add an internal call counter/logger inside the adapter (a simple incrementing counter is fine) — later phases' verification steps repeatedly need to confirm "zero calls were made to Razorpay," and this counter is how you prove it, not by eyeballing logs.
4. Expose adapter methods: `CreateOrder(amount, currency, receipt)`, and whatever else Standard Checkout requires.

---

## 6. Payment Service

1. Given an **approved** order (for now, "approved" just means "exists" — Phase 3 adds real approval), call the Razorpay Adapter to create a Razorpay order.
2. Return the checkout payload (order ID, amount, currency, key ID) to the frontend.
3. Expose an endpoint the frontend calls after the Razorpay Checkout UI completes, to reconcile the client-side result with server state.
4. Store `payments`, `payment_attempts` tables.

---

## 7. Webhook Receiver (basic — hardened in Phase 2)

1. Build an endpoint that receives Razorpay webhook events (`payment.captured`, `payment.failed`, etc.).
2. For now, just log them — signature verification and deduplication are Phase 2 work. Do not skip building this endpoint now; Phase 2 upgrades it, it doesn't create it from scratch.

---

## 8. Frontend Checkout Flow

1. Build a minimal Next.js page: view catalog → add to cart → checkout → Razorpay Standard Checkout UI → completion screen.
2. **Order matters:** Razorpay's Standard Checkout expects the **server** to create the order before the checkout UI is shown. Build it server-first, not client-first — do not call Razorpay from the browser to create the order.
3. Use TypeScript, Tailwind, shadcn/ui per the recommended stack.

---

## 9. End-to-End Manual Test

Run the full lifecycle manually in Test Mode, using **both** a success and a failure test card (Razorpay documents both explicitly — use both, don't only demo the happy path):

```text
create order → checkout → test payment → webhook → verification → order completion
```

Do this at least twice: once ending in success, once ending in failure. Confirm both appear correctly and distinctly in your logs and in the Razorpay Test Mode dashboard.

---

## Phase 1 — Full Artifact List

- `merchants`, `products`, `product_variants` tables
- `carts`, `cart_items` tables
- `orders`, `order_items` tables
- `payments`, `payment_attempts` tables
- Razorpay Adapter module (the *only* code path touching the Razorpay API) with an internal call counter
- Basic webhook receiver endpoint (unverified — Phase 2 hardens it)
- Working checkout UI (Next.js)
- `docker-compose.yml`, `.env.example`, CI pipeline, health checks

---

## Phase 1 — Verification Checklist (must all be true before starting Phase 2)

> **Progress note (updated after a full observed run of the docker-compose stack against the real Razorpay Test Mode API):**
> - ✅ **Item 1** — `docker compose up` brings up Postgres, Redis, and the backend container (built from `backend/Dockerfile`); all four `/health` endpoints return 200.
> - ✅ **Item 2** — `git grep` returns zero matches for the Key Secret/ID; secrets live only in gitignored `.env` files.
> - ✅ **Item 4** — `docker logs commerceos-backend` shows `[webhook] payment.failed — payment failed` and `[webhook] payment.captured — payment succeeded` distinctly.
> - ✅ **Item 5** — DB order amounts (₹2,500 / ₹1,999) match the Razorpay dashboard amounts exactly for both success and failure runs.
> - ✅ **Items 6, 7, 8** — verified by grep and explicit tests.
> - ✅ **Item 3** — a full purchase (browse → cart → checkout → pay) completed in Razorpay **Test Mode** using a real test card, and the resulting payment showed up in the Razorpay dashboard.
> - ⚠️ **Item 9** — the stack is docker-compose-ready (Dockerfile + compose service + `.env.example`), but a clean-clone one-command run was not performed. **Marked as not required by the user.**

- [x] `docker-compose up` brings up every service with no errors; every `/health` returns 200
- [x] Razorpay Test Mode dashboard accessible; Key ID/Secret generated and never committed to Git (`git grep` confirms zero matches)
- [x] A full purchase (browse → cart → checkout → pay) completes in Razorpay **Test Mode** using a real test card, and the resulting payment shows up in the Razorpay dashboard
- [x] A **failed** test payment (using Razorpay's failure test card) is received and logged distinctly from a success
- [x] The order amount stored in the DB matches the amount actually charged in Razorpay, for both the success and failure runs
- [x] No code outside the Razorpay Adapter module makes any call to `api.razorpay.com` (verify by grepping the codebase for the SDK import / API host)
- [x] The cart's reserved amount, once converted to an order, is immutable — mutating the cart after order creation does not change the order (proven by an explicit test, not inspection)
- [x] Product schema round-trips correctly (create → fetch → matches exactly, including nested `features`, `attributes`, `purchase_constraints`)
- [x] A teammate/clean environment can clone, configure `.env`, and run the whole stack with one command — **not required** (user decision)

**Do not start Phase 2 until every box above is checked against an actual observed run.**
