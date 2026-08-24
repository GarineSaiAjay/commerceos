# CommerceOS — 100% Real Implementation: Division of Labor & Step-by-Step Process

> **Why this document exists:** You asked (a) what *I* cannot do and need *you*
> to do, (b) what "sandbox" means, and (c) for a step-by-step process to
> complete the project with 100% real implementations. This is that document.
>
> It is written to be **executed top-to-bottom**, one workstream at a time.
> Each workstream says **WHO does it** (🤖 = I do it in the repo, 🫵 = you do it,
> 🤝 = we split it) and **WHY** it is needed. Check off items as you go.

---

## 0. First: what "sandbox" means (and what I actually can/can't do)

I (the coding agent) run **inside your editor** with access to **your terminal**,
**your filesystem**, and **your local services** (Postgres, Redis, the backend on
ports 8080–8083 — all of which are currently running on your machine). When I say
"sandbox," I mean the **limits of what I can observe/operate from here**:

| I CAN do | I CANNOT do |
|---|---|
| Read/write every file in the repo | Open a **browser** (I can hit HTTP endpoints with `curl`, but cannot see/click the rendered UI) |
| Run `go build/vet/test`, `npm run lint/build` | Complete a **Razorpay checkout** (requires a human clicking the Test-Mode card form) |
| Start/stop/rebuild Docker containers, run SQL against the live Postgres | Receive **real webhook callbacks** from Razorpay's servers (needs a public URL + their delivery) |
| `curl` every backend endpoint (propose, payment, MCP, webhooks with fake signatures) | Hold and enter your **API keys / secrets** (they must never be pasted into chat) |
| Generate the 100-scenario suite, red-team UI, replay system, dashboard, docs | Launch **desktop apps** (Claude Desktop, MCP Inspector) to test MCP as an external client |
| Run the **simulated** experiment engine, audit-chain verifier, EV scoring | Take the **5-minute live demo** (needs a human at the keyboard + real Test-Mode card) |
| Rebuild and restart the whole stack after changes | Access the **internet** except to hosts you explicitly approve (I get blocked/denied otherwise) |

So: **"real implementation" for me = code + tests + local verification (HTTP,
DB, Docker, CI-ready).** The remaining "real" parts — *live Razorpay money
movement, a real LLM response, a human seeing the UI, an external MCP client,
the demo itself* — are **yours**, and this document tells you exactly how to do
each.

---

## 1. Current reality check (verified today)

- ✅ Docker stack **is running**: `commerceos-postgres` (port 5433), `commerceos-redis` (6379), `commerceos-backend` (ports 8080–8083).
- ✅ All **17 migrations** are applied (`goose_db_version` = all `t`).
- ✅ Live data exists: 16 orders (7 paid), 14 payments (8 captured, 1 failed), 1 mandate (`mnd_demo`), 4 products.
- ✅ The running container has **Razorpay Test Mode keys injected** (from a previous `docker compose up` with your `.env`).
- ⚠️ **The running backend is 16 hours old and does NOT include the recent fixes** (risk-aware routing, `MarkAuthorizationUsed`, `MarkFailed`, mandate-cart binding, no-duplicate check, 401 auth failures…).
- ⚠️ **Money-unit mismatch:** the code now uses **paise** (₹30,000 = `3_000_000`), but the **live DB seed data uses rupee-scale** (`mnd_demo` max = `25000`, products = `24900` etc.). Every amount in the live DB must be migrated to paise (`×100`) or the policy ceiling will reject everything.
- ⚠️ There is **no `.env` file in the repo** (correctly gitignored) — the keys live only in the container env.

---

## 2. Workstream A — Bring the running stack up to date with the code (🤝)

The single biggest source of "it doesn't work" right now is the 16-hour-old
container + rupee-scale DB. We fix the DB and rebuild the container.

### A1. Migrate live DB amounts to paise (🤖 — I write & run the SQL)
The easiest path is a one-time data migration (not a new goose file — this is
local test data). I will run, against the live DB:

```sql
-- Multiply every stored money amount by 100 (rupees → paise)
UPDATE mandates        SET maximum_amount = maximum_amount * 100,
                          requires_confirmation_above = requires_confirmation_above * 100;
UPDATE products        SET price_amount = price_amount * 100;
UPDATE product_variants SET price_amount = price_amount * 100;
UPDATE carts           SET subtotal_amount = subtotal_amount * 100;
UPDATE cart_items      SET unit_price_amount = unit_price_amount * 100,
                          total_amount = total_amount * 100;
UPDATE orders          SET subtotal = subtotal * 100;
UPDATE order_items     SET unit_price = unit_price * 100, total = total * 100;
UPDATE payments        SET amount = amount * 100;
UPDATE payment_attempts SET amount = amount * 100;
UPDATE recommendations SET price = price * 100, incremental_margin = incremental_margin * 100,
                          risk_cost = risk_cost * 100;
```

Also update the **seed file** (`db/seeds/001_catalog.sql`) to paise and fix the
demo mandate to a larger paise ceiling so the demo checkout isn't rejected:
`mandate_demo`/`mnd_demo` maximum `2500000` (₹25,000) is fine; keep it consistent.

**Result:** live data + seed agree with the code's paise assumption.

### A2. Rebuild & restart the backend with current code (🤝)
**🫵 You (or I, with your approval):**
```bash
cd /Users/garinesaiajay/projects/commerceos
docker compose up -d --build backend
```
I will **write** the fix (all my changes are in the working tree already), but
**rebuilding/restarting your Docker container is a state-changing action on your
running services** — I want your explicit go-ahead before I do it, since it
restarts the app that may be serving something.

**Checkpoint A:** after restart, `curl http://localhost:8081/health` = `200`,
`curl http://localhost:8081/dashboard/overview` returns JSON (not 404), and
`/policy/propose` approves a paise-scale ₹24,900 (= `2490000`) proposal.

---

## 3. Workstream B — The `.env` file (🫵 you)

The repo correctly gitignores `.env`, but the backend **requires** env vars to
run. The values live in your container today; to make this reproducible:

1. Create `.env` in the repo root (copy from `.env.example`):
   ```bash
   cp .env.example .env
   ```
2. Fill in:
   - `RAZORPAY_KEY_ID=rzp_test_...` ← from your Razorpay dashboard (Test Mode).
   - `RAZORPAY_KEY_SECRET=rzp_test_...` ← same place.
   - `DATABASE_URL=postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable`
   - `REDIS_ADDR=localhost:6379`, and the four ports (defaults are fine).
3. **Never paste the secret values into this chat** — they must stay in the file.
4. If you want me to be able to *test* the live backend from my side, add the
   vars to the environment I run under (or just run the backend yourself in a
   terminal and tell me "it's up").

**Why you:** the Key Secret must never reach logs, LLM context, or Git history —
which includes chat.

---

## 4. Workstream C — Razorpay: test cards, live payment, webhooks (🫵 you + 🤖 me)

This is the true "real implementation" of Phase 1–2 money movement.

### C1. Get the test cards (🫵)
Razorpay docs list **Test Mode card numbers** — you need at least:
- a **success** card (`4111 1111 1111 1111`, any future expiry, any CVV — the
  classic Visa test card works in Razorpay test mode),
- a **failure** card (Razorpay documents specific numbers per gateway, e.g. some
  `4000 0000 0000 0002` variants) — verify from Razorpay's current docs.

### C2. Configure the Razorpay webhook (🫵)
1. Razorpay Dashboard → Settings → Webhooks → Add Webhook.
2. URL: you need a **public** URL that reaches your machine. Easiest: install
   **ngrok** (`brew install ngrok`, then `ngrok http 8081`) and use the
   generated `https://xxxx.ngrok.app/webhooks/razorpay`. (ngrok is **not**
   installed — see Workstream J.)
3. Select **events**: `payment.captured`, `payment.failed` (you can enable all
   payment events).
4. Save; Razorpay will send a test event — the signature verifier must accept it.

### C3. Live checkout run (🫵 at the browser + 🤖 verifying via API/DB)
With the rebuilt backend + real `.env`:
1. Open `http://localhost:3000`, add a product, checkout, pay with the **success**
   card. Watch the webhook arrive (`docker logs commerceos-backend`).
2. Repeat with the **failure** card; confirm the recovery screen appears and no
   duplicate charge occurs.
3. **I verify** afterward via SQL: `payments`/`orders`/`audit_events` rows,
   and via `curl` on `/dashboard/overview`.

**Why you:** only a human can complete the Razorpay-hosted card form, and only
Razorpay's servers can deliver a real webhook.

---

## 5. Workstream D — Real LLM provider (🫵 key + 🤖 implementation)

The `IntentExtractor` is a **deterministic stub** today. To make Phase 4/5/8
"real," we plug in a real LLM behind that interface.

### D1. You provide:
- An **API key** for **OpenAI** (`OPENAI_API_KEY`), or **Anthropic**
  (`ANTHROPIC_API_KEY`), or **any OpenAI-compatible endpoint** (base URL + key).
  Put it in `.env` (e.g. `LLM_API_KEY=...`). Never paste it into chat.

### D2. I implement (🤖 — no external access needed to write it):
- A new `backend/agents/llm_extractor.go` implementing `IntentExtractor` using
  the provider's **structured-output / JSON-schema** mode (strict schema; the
  existing `ParseIntentJSON` validation stays as the safety net).
- Plumb `LLM_API_KEY` (and `LLM_BASE_URL` if needed) through env.
- Keep `DeterministicExtractor` as **fallback when no key is set** (so tests and
  CI don't require a paid API).
- Unit tests with a **mocked** LLM client (no real call) + an integration test
  that is **skipped unless `LLM_API_KEY` is set**.

### D3. You run the live check (🫵):
```bash
cd backend && LLM_API_KEY=sk-... go run ./cmd/server
# then curl a real prompt:
curl -s -X POST http://localhost:8081/agent/checkout \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"I need wireless earbuds for my sister. Budget ₹25,000. I want good noise cancellation.","merchant":"merchant_001"}'
```
Expect a real structured `Intent` + a `CREATE_ORDER` proposal. Compare against
the deterministic extractor's output; they must agree on the happy path.

**Why you:** only you hold the paid API key; I won't fabricate one and won't put
yours into any file that could leak.

---

## 6. Workstream E — Phase 8 Red Team (🤖 mostly, 🫵 browser + key for real LLM)

Phase 8 is currently **entirely unimplemented** (per §6.8 of the audit). I can
build all of it:

1. **Canned attack library + "Attack the Agent" red-team UI** (a frontend page
   hitting the real agent pipeline with the 8 scripted attack strings).
2. **Backend enforcement** that each attack is genuinely rejected by the Policy
   Engine and reports `Razorpay API calls: 0` (we already have the call
   counter; expose it via an endpoint).
3. **Replay system**: add `run_id` at the start of an agent run, persist
   search→filter→rank→propose→authorize→payment steps, add a "Replay Agent Run"
   dashboard view.
4. **100-scenario evaluation suite**: a Go test (or a CLI) that runs ~100
   scenarios (normal/adversarial), asserts `0 unauthorized`, `0 duplicate`,
   `0 policy bypass`, and writes a summary row the dashboard can read.
5. **Trust-boundary diagram** (Markdown/mermaid file) — pure docs, I can write it.

**🫵 You:** (a) run the suite with a real LLM key once to generate the live
safety numbers; (b) click through the red-team UI in a browser to confirm the
visuals; (c) confirm 3 arbitrary past runs replay identically.

---

## 7. Workstream F — Phase 9: dashboard polish + demo (🤝)

- **🫵 You:** give me the product a judge will see (or let me pick the seeded
  catalog). Confirm the demo scenario (budget, accessory, ceiling) matches the
  spec numbers.
- **🤖 I:** build the single merchant dashboard screen (revenue / AI-attributed /
  conversion / AOV / AI actions / audit trail / audit integrity / safety eval),
  wire every number to the real endpoints, add "Simulated" labeling.
- **🤝 Demo script:** I write the 5-minute script (the spec has a template). **🫵
  You** must rehearse and present it — I can't be at the keyboard.

---

## 8. Workstream G — Remaining P2 correctness items (🤖 — I can do all of these)

These are code-level, verifiable by tests; no external input needed. I'll knock
them out in this order:

1. **§3.8/§5.1** — Wire `PaymentAdapter`/`RazorpayAdapter` in `main.go` (or delete
   the dead interface) so the adapter layer is the real single call path.
2. **§3.10 (rest)** — Write `agent_decisions` on every evaluation.
3. **§3.11/§6.16** — Persist `experiment_assignments` in the experiment service.
4. **§3.12** — Propagate the idempotency key into `payment_attempts`.
5. **§3.14/§3.15/§7.2** — Make order creation start at a legal state-machine
   state (decide `draft → authorized → payment_pending` vs. documenting the
   `payment_pending` entry) and fix the orders table default.
6. **§3.16–3.18** — Finish the frontend recovery/change-method/currency-display
   fixes correctly for paise.
7. **§3.22/§3.23/§3.24** — Fix analytics attribution (join recommendations→orders
   properly), conversion definition, and replace the hardcoded experiment base.
8. **§3.27/§7.6** — Unify the policy-version constants.
9. **§6.13** — Add product CRUD endpoints (create/update/delete/restock).
10. **§6.14** — Expose the Razorpay adapter call counter (`GET /adapter/calls`).
11. **§6.12/§5.10** — Make MCP `explain_decision` work for approved decisions and
    by transaction ID, not just rejected-with-params.
12. **§4.2/§5.8** — Turn `StreamConsumer` into a real analytics/audit consumer
    that writes to the dashboard tables.

Each gets tests + a live `curl` verification against the rebuilt stack.

---

## 9. Workstream H — Phase 2 recovery UX & Level 2/3 UI (🤖 code, 🫵 visual)

- **🤖 I** build: the "Remove ₹1,999 accessory" recovery option, real failure-
  reason surfacing from the webhook payload, and the Level 2 "Approve" / Level 3
  "hard gate" screens wired to the authorization levels.
- **🫵 You** confirm visually in the browser (I cannot click through Razorpay's
  modal or verify layout rendering).

---

## 10. Workstream I — MCP external client test (🫵)

The MCP server is at `POST http://localhost:8081/mcp` (JSON-RPC). I can verify
it with `curl` (`tools/list`, `tools/call search_products`, `explain_decision`,
forged-authz rejection). **You** should additionally connect a **real MCP
client** — **Claude Desktop** or the **MCP Inspector** — point it at
`http://localhost:8081/mcp`, and call `search_products` + `get_product`. That's
a desktop app I can't launch.

---

## 11. Workstream J — Infrastructure fixes (🤖 code, 🫵 one-time installs)

- **🤖 I** fix: CI to provision Postgres/Redis (add a `services:` block in the
  GitHub Actions workflow, or a `docker-compose.ci.yml` + spin-up step) so the
  integration tests pass in CI; add the root `README.md`; add the
  `.env.example` entry for `LLM_API_KEY`.
- **🫵 You** (one-time): `brew install ngrok` (for the Razorpay webhook, §C2).
  Optionally `brew install --cask claude` / MCP Inspector (Workstream I).

---

## 12. Final live-verification matrix (what "done" means)

| Capability | How verified | Who |
|---|---|---|
| Backend builds & tests | `go build/vet/test ./...` green | 🤖 (done today, will re-run after G) |
| Frontend builds & lints | `npm run build && npm run lint` | 🤖 (done today) |
| Real Razorpay checkout (success card) | Browser + `payments`/`orders` rows + `audit_events` | 🫵 click, 🤖 SQL verify |
| Real Razorpay failure + recovery | Failure card → recovery screen, no double charge | 🫵 click, 🤖 SQL verify |
| Webhook signature + dedup live | Real Razorpay webhook via ngrok; duplicate delivery no-op | 🫵 configure, 🤖 verify |
| Real LLM intent extraction | Live prompt → structured Intent | 🫵 key + run, 🤖 code |
| 100-scenario eval suite | `0 unauthorized / 0 duplicate / 0 bypass` | 🤖 code, 🫵 run with key |
| Red-team live attacks | 8 attacks all BLOCKED, `Razorpay calls: 0` | 🤖 code, 🫵 browser |
| Replay 3 runs by `run_id` | Identical reconstructed steps | 🤖 code, 🫵 click |
| MCP external client | Tools callable from Claude Desktop/Inspector | 🫵 |
| Merchant dashboard | Real numbers, simulated labeled | 🤖 code, 🫵 see it |
| 5-minute demo | Live, twice in a row, no manual DB edits | 🫵 present (🤖 script) |

---

## 13. The step-by-step execution order (checklist)

**Phase A – Stabilize ✅:**
- [x] 🤖 A1: paise data migration on live DB; seed paise-correct.
- [x] 🤝 A2: backend rebuilt & restarted with current code; live policy smoke test passed.

**Phase B – Credentials ✅:**
- [x] 🫵 B: `infra/.env` has real Razorpay keys + `OPENROUTER_API_KEY` (+ `LLM_MODEL`); `POSTGRES_PORT` aligned to 5433.
- [ ] 🫵 C2: install ngrok + configure Razorpay webhook for `payment.captured`/`failed` (still open — webhook path verified via route, not live callback).

**Phase C – Live money ✅ (success card):**
- [x] 🫵 C3: success-card checkout completed — order `order_cart_1787579514601` (₹1,999) **paid**, payment **captured**, from the browser.
- [ ] 🫵 C3b: failure-card checkout (recovery UX) — the failed screen now fetches `GET /orders/{id}/recovery`; **a real browser failure-card run still to confirm visually**.
- [x] 🤖 Verified rows/audit/no-double-charge for the success run.

**Phase D – Real AI ✅:**
- [x] 🫵 D1: `OPENROUTER_API_KEY` + `LLM_MODEL=qwen/qwen3.5-9b` added to `infra/.env`.
- [x] 🤖 D2: `LLMExtractor` implemented (OpenRouter / OpenAI-compatible), env-plumbed, unit-tested (mock server).
- [x] 🫵 D3: live prompt test passed — real LLM extracted earbuds/₹25k/ANC/sister → `airpods-pro-2` proposal.

**Phase E–I – Remaining build-out:**
- [x] 🤖 G: all P2 correctness items (3.8, 6.14, 3.10, 3.11, 3.12, 3.14/3.15, 3.22–3.24).
- [x] 🤖 Dashboard pages + frontend redesign (shared shell), 7 routes, build+lint clean.
- [x] 🤖 E: 100-scenario eval suite (106 scenarios, 0 unauthorized/bypass), trust-boundary diagram, red-team page shell.
- [x] 🤖 J (docs): root `README.md`, `.env.example` LLM entries.
- [ ] 🤖 E (next): red-team **attack runner** endpoints (`POST /safety/attacks/{id}/run`), **evaluation history** API.
- [x] 🤖 E: red-team attack runner + evaluation history **done** (`backend/safety` + `/safety/*` endpoints, dashboard safety page rebuilt, suite verified live: 10 attacks, 0 failures).
- [ ] 🤖 E (next): **replay** system (`run_id` propagation + `/runs` APIs).
- [x] 🤖 E: **replay `/runs` API** done (list + detail from persisted records; Runs page rebuilt).
- [x] 🤖 Phase 7: `execute_authorized_checkout` MCP tool added (10 tools; verified live via MCP).
- [x] 🤖 H: server-driven recovery (`GET /orders/{id}/recovery`) + Level 2/3 `approval-requests` backend state — **done** (approval queue dashboard + checkout approval step + recovery endpoint, all verified live).
- [ ] 🫵 H: visual confirmation of recovery + approval flows in browser.
- [ ] 🤖 F: demo script + final dashboard polish.
- [ ] 🫵 I: MCP via Claude Desktop/Inspector.
- [ ] 🤖 J: CI Postgres provisioning (remaining — integration tests need `localhost:5433` in CI).

**Phase J/K – Remaining from the audit:**
- [ ] 🤖 Product CRUD endpoints (6.13), `run_id` in agent/policy traces (6.15), unified policy-version constants (7.6), `recommendations.cart_id` FK (7.7).
- [ ] 🫵 Rehearse the 5-minute demo twice with a stopwatch (script to be written in Workstream F).
- [ ] 🤖 Final full-suite pass; keep `PROJECT-AUDIT.md` fix log current.

---

*End of action plan. This document will be updated as workstreams complete.*