# CommerceOS — Project Audit & Completion Guide

> **Purpose:** This document is a single source of truth for everything that is
> **stubbed**, **unwired**, **unimplemented**, **buggy**, **misimplemented**, or
> **missing** in the CommerceOS codebase, plus every external credential/service
> you must obtain to fully complete the project.
>
> **How it was produced:** A full read of every backend Go module, every DB
> migration and seed, the frontend app, the Docker/CI config, and the phase
> specs in `files/`. The backend **builds**, **vets**, and **all tests pass**
> (`go build ./...`, `go vet ./...`, `go test ./...` all green). The frontend
> **lints** clean (`npm run lint`). So this is *not* a list of compile errors —
> it is a list of **gaps between what the phase specs require and what the code
> actually implements**, plus latent bugs that tests do not currently catch.
>
> **Status legend:**
> - 🔴 **Bug / Wrong** — the code does something incorrect or contradicts its own spec.
> - 🟠 **Stub / Placeholder** — a real implementation is replaced by a fake or no-op.
> - 🔵 **Unwired** — the code exists but is not connected to any running path.
> - 🟣 **Unimplemented** — the spec calls for it but no code exists at all.
> - 🟡 **Missing / Incomplete** — a required artifact or config is absent.

---

## 🔧 Fix Log (most recent first)

> Items below were fixed in the working tree **after** this audit was first
> written. Each entry notes the item number from §3 and what changed. If an
> item does not appear here, it is still open.

| Date | Item(s) | What was done |
|---|---|---|
| 2026-08-24 | §3.3, §3.7 | `routeLevel` now takes the risk score and forces Level 3 when `riskScore >= 0.7`; risk is computed **before** `Evaluate` and threaded through (`engine.go`, `service.go`). Trade-off: Level 1/2 boundaries are now paise-aware (₹1,000 = 100,000 paise; ₹10,000 = 1,000,000 paise), consistent with the codebase's paise money unit. |
| 2026-08-24 | §3.4 | `checkBudget` tolerance is now a **fractional percentage** of the mandate maximum (`BudgetTolerance float64`, e.g. `0.10` = +10%), matching the growth agent's `BudgetCheck` semantics. |
| 2026-08-24 | §3.5 | `CheckNoDuplicate` and `CheckUserConsent` are implemented. Consent = mandate still `ACTIVE`. No-duplicate evolved into **idempotent reuse** (see the duplicate-click fix below) — an active authorization for the same action is reused, not rejected. |
| 2026-08-24 | §3.6, §7.4 | New `CheckMandateCartBound` enforces the `Mandate → Cart` binding: when a mandate carries a `cart_id`, the proposal must reference that cart. Implemented via a new check name `mandate_cart_bound`. |
| 2026-08-24 | §3.10 | Risk scores are now persisted to `risk_assessments` (`SaveRiskAssessment`, called from `service.Propose`). `agent_decisions` table remains unwritten (see §3.10 note). |
| 2026-08-24 | §3.13, §5.5 | `AttemptRepository.MarkFailed` added (Postgres impl) and called from `webhook_applier.ApplyFailed` so failed payments record the Razorpay error details on the attempt row. |
| 2026-08-24 | §3.19 | `RazorpayClient.CreatePayment` rejects fractional amounts (whole paise only) and parses amount via `float64` → `int64` with a `math.Trunc` guard, so a decimal amount can never be silently truncated. |
| 2026-08-24 | §3.26 | `growth.EvaluateCandidate` no longer swallows the store save error — it returns a wrapped error instead of returning the recommendation silently. |
| 2026-08-24 | §3.29 | MCP `recommend_bundle` now passes `risk_cost` through to `EVInputs` (it was previously hardcoded to 0 on that surface). |
| 2026-08-24 | §3.30 | `payment.Handler.CreatePaymentOrder` now returns **401** (instead of 500) when the authorization is missing/invalid. |
| 2026-08-24 | §3.9 | `CreatePaymentOrder` now marks the authorization `USED` (`MarkAuthorizationUsed`) after a **new** payment is created (the policy service implements `AuthorizationConsumer`). The idempotent-repeat path returns the existing payment without touching the authorization. Tests: `TestAuthorizationConsumedAfterNewPayment`, `TestAuthorizationNotConsumedWhenIdempotentRepeat`. |
| 2026-08-24 | New tests | Added: `TestLevel3OnHighRisk`, `TestMandateCartBound`, `TestNoDuplicateProposal` (policy), plus the two authorization-consumption tests above (payment). |
| 2026-08-24 | Ops | **A1/A2 live-stack pass:** migrated all live-DB money columns to paise (×100: mandates/products/variants/carts/orders/payments/attempts/recommendations + audit `detail.amount`), refreshed the demo mandate expiry to `2030-01-01` (stale old seed), and rebuilt/restarted the backend container so it runs the current code. Verified live: ₹24.9k / ₹2.5k proposals APPROVE with an authorization issued; over-mandate rejected (`budget_tolerance`); identical repeat rejected (`no_duplicate`); expired mandate rejected (`mandate_not_expired`). |
| 2026-08-24 | §3.8, §5.1, §6.14 | `RazorpayAdapter` now implements the `Provider` interface (renamed `VerifySignature` → `VerifyPaymentSignature`) and is the **real** wiring point in `main.go`. Added `/adapter/calls` endpoint exposing the Phase 1 call counter (verified live: `{"razorpay_calls":0}` before any payment). `PaymentAdapter` interface now includes `CallCount()` for rail-agnostic access. |
| 2026-08-24 | §3.11, §6.16 | `ExperimentService.Run` now persists the per-session control/treatment split to `experiment_assignments` (session index `i` — not customer ID, since the simulator reuses 10k customers across 50k sessions). Verified live: 25k control / 25k treatment rows for `exp_xsell_v3`. |
| 2026-08-24 | §3.10 (rest) | Policy service now writes `agent_decisions` on every evaluation (`SaveAgentDecision`), completing the Phase 3 artifact list. |
| 2026-08-24 | §3.12 | `CreatePaymentOrder` propagates the idempotency key into the `payment_attempts` row (`IdempotencyKey` field + `nilIdempotency` NULL-mapping so the partial unique index is respected). |
| 2026-08-24 | §3.14, §3.15 | Orders table default changed `pending` → `payment_pending`; `draft → payment_pending` added as a legal state-machine edge (checkout creates orders directly in `payment_pending`, so this keeps the entry consistent). |
| 2026-08-24 | §3.22, §3.23 | Analytics: conversion rate now divides paid orders by **total orders** (not all carts, which never checkout), and AI-revenue attribution is documented as best-effort via `recommendations.cart_id`. |
| 2026-08-24 | §3.24 | `MerchantSimulator.Session` now carries `PurchaseAmount` (catalog paise price per product) and the experiment engine computes revenue-per-session from it, replacing the hardcoded `1200.0`. Verified live: `exp_xsell_v3` control ₹180.38 → treatment ₹266.18, lift +47.6%. |
| 2026-08-24 | Frontend | Added the four missing dashboard pages: `/dashboard/analytics` (experiment runner w/ simulated labeling), `/dashboard/approvals` (Level 1/2/3 proposal tester), `/dashboard/runs` (replay from `agent_actions`), `/dashboard/safety` (red-team library + eval status). Frontend builds (7 pages) and lints clean. |
| 2026-08-24 | §6.8 (Phase 8) | Added the **100-scenario evaluation suite** (`backend/policy/evaluation_suite_test.go`): 106 scenarios across normal/over-ceiling/over-mandate/unknown-merchant/wrong-currency/disallowed-product/zero-negative/empty-schema/duplicate/expired-mandate/price-manipulation/bundle classes. Run: **0 unauthorized, 0 policy bypasses, 100% graceful failure rate, 2 duplicates detected**. |
| 2026-08-24 | §8.8, §8.14, §8.13 | Added Phase 8 **trust-boundary diagram** (`files/trust-boundary.md`, mermaid), root **`README.md`**, and the **`LLM_API_KEY`/`LLM_BASE_URL`** entries in `.env.example` (§8.13). |
| 2026-08-24 | Frontend (redesign) | Built a **minimalist shared dashboard shell**: `app/dashboard/layout.tsx` (sidebar nav), `lib/format.tsx` (shared `formatINR`/`formatPct`/`formatTime`/`Skeleton`), `lib/api.ts`. Rewrote all four dashboard pages to use the shared shell + utils, added server-driven loading skeletons + error-keeps-last-data + data-state badge on Overview. Removed per-page duplicated layout/links and unused imports. Frontend lint clean, build passes (7 routes). |
| 2026-08-24 | §6.3 (Phase 4) | Implemented the **real LLM-backed `IntentExtractor`** (`backend/agents/llm_extractor.go`): OpenAI-compatible Chat Completions client (works with **OpenRouter**), strict-JSON request, `ParseIntentJSON` validation, `ErrAmbiguousIntent` on vague output, markdown-fence stripping. `main.go` now uses it when `OPENROUTER_API_KEY` is set, else falls back to the deterministic extractor. Env plumbing added to `infra/docker-compose.yml` (`OPENROUTER_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL`) and `.env.example`. Unit tests with a mock HTTP server (`llm_extractor_test.go`) — all pass without any real API call. |
| 2026-08-24 | Infra | `infra/docker-compose.yml` now reads all values from `infra/.env` (`POSTGRES_*`, `REDIS_*`, `RAZORPAY_*`, LLM vars) instead of hardcoding; added `infra/.env.example` documenting every var. |
| 2026-08-24 | §7.4 (Bug: cart binding) | **Fixed the cart-bound rejection that broke live checkout** (“Payment was not authorized”). `checkMandateCartBound` wrongly required the mandate’s `cart_id` to appear among the proposal’s *product* items, so every cart-bound mandate rejected every real proposal. `ProposedAction` now carries an explicit `cart_id` (added to the schema, handler, frontend `checkout.tsx`, and MCP `request_authorization`), and the engine compares `action.CartID == mandate.CartID`. Verified live: ₹28,198 proposal with `cart_id` → APPROVED. |
| 2026-08-24 | §3.5 (Bug: retries blocked) | **Fixed the no-duplicate guard so rejected retries are not blocked.** `checkNoDuplicate` previously rejected any proposal already present in `agent_actions`, so a rejected checkout could never be retried. It now uses `ActiveAuthorizationExists` (an ACTIVE authorization for the exact action) — replaying an approved action is still blocked, but a rejected-then-retried checkout proceeds. Tests: `TestDuplicateAuthorizationBlocked` + `TestRejectedProposalRetryNotBlocked`. |
| 2026-08-24 | §3.9 (Bug: JSON key case + silent USED) | **Two fixes for “Payment was not authorized”:** (1) `policy.Decision` had no JSON tags → API returned `Decision/AuthorizationID` (capital) while the frontend read `decision/authorization_id` (lowercase). Added lowercase snake_case tags; approvals page updated. (2) `policy.Service` never implemented `MarkAuthorizationUsed`, so the payment service’s `AuthorizationConsumer` assertion silently no-opped and authorizations stayed `ACTIVE` forever. Added the method + compile-time guard `var _ AuthorizationConsumer = (*policy.Service)(nil)`; payment service now propagates the mark error. |
| 2026-08-24 | Bug (migration ran twice) | Live-DB product prices were 100× too large (double ×100 from manual migration + `20260822160000_normalize_money_to_paise.sql`). Reset products/variants to correct paise (2490000/199900/250000/129900). |
| 2026-08-24 | Bug (missing column) | `MarkAuthorizationUsed` updated `authorizations.updated_at` which didn’t exist → SQL error. Added migration `20260824160000_add_authorizations_updated_at.sql`. |
| 2026-08-24 | Ops | `infra/.env` `POSTGRES_PORT` 5432 → 5433 (host) to match Go tests/URLs. Full flow verified live end-to-end: cart→checkout→mandate→propose (APPROVED, lowercase JSON)→payment (real Razorpay order)→authorization **USED**. |
| 2026-08-24 | Bug (CORS blocked payment) | **“Failed to fetch” root cause:** CORS only allowed `Content-Type`, so the browser rejected the payment call that sends `Authorization-Id`/`Idempotency-Key` at preflight. Now allowed (+ exposed) in `corsMiddleware`. Verified: OPTIONS preflight returns 204 with correct headers. |
| 2026-08-24 | Bug (duplicate click = error) | **“an active authorization already exists” fixed:** `Propose` now **reuses** an existing ACTIVE authorization for the same action instead of rejecting (idempotent propose). Added `Repository.GetActiveAuthorization` (+ Postgres impl + test fakes); removed the hard `checkNoDuplicate` rejection from the engine. Verified live: identical second proposal returns the **same** `authorization_id`. Tests updated to assert reuse (`TestNoDuplicateProposal`, eval suite dup cases). |
| 2026-08-24 | ✅ **Milestone — real checkout** | A full **browser checkout succeeded in Razorpay Test Mode**: order `order_cart_1787579514601` (₹1,999 AirPods Case) → **`paid`**, payment **`captured`**, from the live stack. This closes the P0 “cannot complete a real checkout” gap. |
| 2026-08-24 | §6.2 (Level 2/3 approval flow) | Implemented **durable human-approval requests**: new `approval_requests` table + migration; `Propose` now routes Level 2/3 to `PENDING_HUMAN_APPROVAL` with an `approval_request_id` (no authorization minted); `Approve` re-evaluates policy server-side, issues a one-time authorization, and is idempotent; `Reject` blocks. Endpoints: `GET/POST /approval-requests/{id}`, `/approve`, `/reject`, and `GET /approval-requests?status=PENDING` (list). Checkout frontend shows an approval step; dashboard Approvals page is now a real approve/reject queue. Verified live: Level 2/3 propose → PENDING → approve → authorization; reject blocks. New tests: `TestApprovalFlowLevel2`, `TestApprovalFlowReject`, `TestApprovalLevel1AutoApproved`. |
| 2026-08-24 | §6.1 (server-driven recovery) | Implemented **`GET /orders/{id}/recovery`** — authoritative failure-recovery read model (payment + attempt + cart reservation + risk error code/description + `retry_allowed`), driven by server state not modal dismissal. `AttemptRepository.GetLatestForOrder` added. Frontend failed screen now fetches recovery (`safe_message`, live `retry_allowed`). Verified live with a signed `payment.failed` webhook: payment `failed`, attempt `failed` with `BAD_REQUEST_ERROR`, recovery view returns full state. |
| 2026-08-24 | Bug (webhook attempt not marked paid) | `WebhookApplier.ApplyCaptured` marked the payment/order but left `payment_attempts` on `attempted`. Added `MarkPaid` in the captured webhook path (mirrors the client-verify path). Verified live via a signed `payment.captured` webhook: order `paid`, payment `captured`, attempt `paid`. |
| 2026-08-24 | ✅ Option B: success webhook path proven locally | Since ngrok/network blocks Razorpay→local delivery (college wifi / carrier), proved the **full success webhook path** with properly-signed local webhooks (exact bytes Razorpay sends): cart→checkout→mandate→propose→approval→payment→`payment.captured`→order `paid`, payment `captured`, attempt `paid`, audit + `webhook_events` written. The same code path triggers identically when a real Razorpay webhook arrives once the network allows it. |
| 2026-08-24 | 🎉 **Real Razorpay webhooks flowing live** | With the webhook secret wired (see below), Razorpay→ngrok→backend webhooks are now **accepted and verified**: `conns: 56`, `[webhook] payment.captured` processed, duplicates deduped (`duplicate event … no-op`), unknown events (`order.paid`, `payment.authorized`) logged-and-ignored. 13 paid orders / 14 captured / 16 webhook_events. |
| 2026-08-24 | Bug (webhook secret mismatch) | Razorpay signs webhooks with its own **webhook secret** (`RAZORPAY_WEBHOOK_SECRET`, set in the dashboard), not the API Key Secret — every real webhook was rejected (`invalid razorpay webhook signature`). Added `RAZORPAY_WEBHOOK_SECRET` env passthrough (compose + `.env.example`), `main.go` uses it with fallback to the key secret. Fixed compose YAML (tabs introduced by an edit). |
| 2026-08-24 | Bug (capture for unknown payment) | `ApplyCaptured` rejected when Razorpay sent `payment.captured` for a payment not tracked locally (e.g. dashboard test). Now a graceful no-op (`ErrPaymentNotFound` → nil). |
| 2026-08-24 | §6.8 (red-team runner + eval history) | New **`backend/safety`** package: attack library (10 canned attacks incl. prompt-injection), `Runner` driving the real policy pipeline with provider-call deltas from the adapter counter, `Store` persisting `safety_evaluations` + `safety_attack_results` (new migration). Endpoints: `GET /safety/attacks`, `POST /safety/attacks/{id}/run`, `POST /safety/evaluations/run`, `GET /safety/evaluations`, `GET /safety/evaluations/{id}`. Dashboard `Overview.SafetySummary` now surfaces the latest run (available, scenarios, graceful rate, passed). Verified live: suite = 10 scenarios, 0 unauthorized/dupes/bypasses, 100% graceful, passed; a single attack blocked with provider delta 0. New safety tests. Frontend Safety page rebuilt (attack library + run buttons + evidence + history). |
| 2026-08-24 | §6.15 (replay system) | **`/runs` replay API**: `GET /runs` + `GET /runs/{id}` reconstruct agent runs from `agent_actions` joined to `policy_evaluations` + `authorizations` (proposal → decision → authorization trail). Frontend Runs page rebuilt on the real endpoints. Verified live: rejected red-team run reconstructs with reason. |
| 2026-08-24 | §7.2 (MCP execute tool) | Added the missing **`execute_authorized_checkout`** MCP tool (10 tools now): requires `order_id` + `authorization_id` (from `request_authorization`), backend re-verifies the auth before any Razorpay call. `order.Service.GetOrder` added. Verified live via MCP: request_authorization → PENDING → approve → execute → real Razorpay order created. |

### Notes on items that are still open
- §3.16 / §3.17 / §3.18 (frontend recovery): static screen exists; **server-driven recovery** (`GET /orders/{id}/recovery`) and distinct retry/remove-accessory/cancel actions still pending (Workstream H).
- §6.8 remainder (Phase 8) — red-team attack **runner** endpoint, evaluation **history**, and replay **API** are not yet wired end-to-end.
- §6.2 (Level 2/3 human approval): `approval-requests` backend state machine not yet built; only the proposal tester exists.
- §6.13 (product CRUD), §6.15 (run_id replay), §7.6 (policy-version constants), §7.7 (`recommendations.cart_id` FK), §7.8 (single-use cart/order): still open.

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [External Credentials & Services You Still Need](#2-external-credentials--services-you-still-need)
3. [Bugs & Mistakes in Existing Code](#3-bugs--mistakes-in-existing-code)
4. [Stubs & Placeholders](#4-stubs--placeholders)
5. [Unwired Code (exists but not connected)](#5-unwired-code-exists-but-not-connected)
6. [Unimplemented Features (spec requires, no code)](#6-unimplemented-features-spec-requires-no-code)
7. [Misimplemented Features & Models](#7-misimplemented-features--models)
8. [Missing Artifacts (files/config that should exist)](#8-missing-artifacts-filesconfig-that-should-exist)
9. [Test Coverage Gaps](#9-test-coverage-gaps)
10. [Frontend Gaps](#10-frontend-gaps)
11. [Infrastructure / CI Gaps](#11-infrastructure--ci-gaps)
12. [Phase-by-Phase Completion Matrix](#12-phase-by-phase-completion-matrix)
13. [Recommended Fix Order](#13-recommended-fix-order)

---

## 1. Executive Summary

CommerceOS is a **modular monolith** (Go backend + Next.js frontend) that
implements an "autonomous revenue & checkout agent" on Razorpay. The
deterministic commerce core (catalog, cart, order, payment, webhook pipeline,
policy engine, audit chain, MCP server) is **substantially built and tested**.
The backend compiles and all unit/integration tests pass.

However, the project is **not complete against its own phase specs**. The
largest structural gaps:

1. **The AI/LLM layer is entirely stubbed.** The `IntentExtractor` is a
   `DeterministicExtractor` that parses one canned prompt shape. There is no
   real LLM provider, no API key, no structured-output call. Phases 4, 5, and 8
   all assume a real LLM behind an interface.
2. **Phases 8 and 9 are largely unimplemented** (red-team UI, replay system,
   100-scenario evaluation suite, safety dashboard, demo script).
3. **Several spec-mandated tables are created but never written to**
   (`risk_assessments`, `agent_decisions`, `experiment_assignments`), and
   several spec-mandated tables are **missing entirely** (`merchants` has no
   CRUD, no `authorizations`-consumption beyond verify, no `refunds`).
4. **The frontend now has checkout and a merchant overview dashboard**, but it
   still lacks red-team, replay, Level-2/3 approval, and complete recovery UIs.

The rest of this document enumerates every specific finding.

---

## 2. External Credentials & Services You Still Need

These are the things you must obtain **from outside the repo** to fully run and
complete the project. None of them are committed (correctly).

### 2.1 Razorpay (required — the payment rail)
| Item | Where to get it | Notes |
|---|---|---|
| **Razorpay Test Mode account** | https://dashboard.razorpay.com (sign up, switch to **Test Mode**) | Required for any real payment. |
| **`RAZORPAY_KEY_ID`** (Test Key ID) | Razorpay Dashboard → Settings → API Keys | Put in `.env` (gitignored). |
| **`RAZORPAY_KEY_SECRET`** (Test Key Secret) | Same place | Must **never** reach frontend, logs, LLM context, or Git. |
| **Test Mode card numbers** | Razorpay docs ("Test Cards") | You need **both** a success card and a failure card to demo the happy path *and* the failure-recovery path (Phase 2 §10, Phase 9). |
| **Webhook secret / signing** | Razorpay Dashboard → Webhooks | The webhook signature verifier uses the Key Secret. You must configure a webhook endpoint URL pointing at `POST /webhooks/razorpay` (default `http://localhost:8081/webhooks/razorpay`). |

> ⚠️ The backend **hard-exits** at startup if `RAZORPAY_KEY_ID` or
> `RAZORPAY_KEY_SECRET` is unset (`main.go` lines 129–135). So you cannot even
> boot the stack without these.

### 2.2 LLM Provider (required to replace the deterministic stub)
The `IntentExtractor` interface (`backend/agents/intent.go`) is the plug point.
To make Phases 4/5/8 real you need an LLM provider with **structured output /
function-calling** support:
- **OpenAI** (function calling / JSON schema) — needs `OPENAI_API_KEY`.
- **Anthropic Claude** — needs `ANTHROPIC_API_KEY`.
- **Google Gemini** — needs `GEMINI_API_KEY`.
- Or any OpenAI-compatible endpoint (needs base URL + key).

There is **no LLM client code and no LLM env var** in the repo today. You must
add a provider implementation and an env var (e.g. `LLM_API_KEY`) yourself.

### 2.3 Optional / for Phase 9 polish
- **A public webhook endpoint** (ngrok / tunnel) if you want Razorpay to reach
  your local webhook from the internet during a live demo. Not strictly
  required if you drive webhooks manually.
- **A hosted DB/Redis** if you want to demo beyond localhost (not required).

### 2.4 Environment file
`.env.example` exists and documents `DATABASE_URL`, `RAZORPAY_KEY_ID`,
`RAZORPAY_KEY_SECRET`, the four service ports, and `REDIS_ADDR`. It does **not**
yet document an LLM key (because no LLM provider exists yet). Copy it to `.env`
and fill in real values.

---

## 3. Bugs & Mistakes in Existing Code

### 3.3 🔴 `routeLevel` ignores the risk score it claims to use
- **File:** `backend/policy/engine.go` `routeLevel` (lines 138–156).
- **Why:** The comment says level routing is a function of
  `(amount, merchant_trust, category_history, risk_score)` and that Level 3
  must trigger on **high risk regardless of amount**. But the function signature
  is `routeLevel(amount, mandate)` and it only checks `isTrustedMerchant` +
  amount thresholds. The risk score is computed in `service.go` and stored on
  the decision, but **never fed into `routeLevel`**. A high-risk low-amount
  action would be auto-approved at Level 1.
- **Fix:** Pass `riskScore` into `routeLevel` and force Level 3 when risk is
  high.

### 3.4 🔴 `checkBudget` applies tolerance as a flat amount, not a percentage
- **File**: `backend/policy/engine.go` `checkBudget` (lines 101–106) and
  `DefaultConfig` (`BudgetTolerance: 0`).
- **Why:** The spec (Phase 3 §3, Phase 5 §1) describes budget tolerance as a
  **percentage** (e.g. +10% → max ₹27,500). The Growth module implements this
  correctly as a percentage (`growth/budget.go` `MaxAllowed()`), but the Policy
  Engine's `checkBudget` adds a **flat integer** `BudgetTolerance` (default 0).
  These two notions are inconsistent, and the policy engine's flat tolerance is
  never wired to the growth agent's percentage tolerance.
- **Fix:** Make the policy engine's budget check use the same percentage
  tolerance semantics as the growth module, or remove the dead flat field.

### 3.5 🔴 `CheckNoDuplicate` and `CheckUserConsent` are declared but never run
- **File**: `backend/policy/model.go` (constants), `engine.go` `Evaluate`
  (lines 32–40).
- **Why:** The spec (Phase 3 §3) requires a "no duplicate transaction" check
  (reusing Phase 2 idempotency) and a "user consent exists" check. Both check
  names are defined, but **neither is added to the `checks` list** in
  `Evaluate`, and neither has an implementation. So duplicate transactions and
  missing consent are **not** actually blocked by policy.
- **Fix:** Implement and add both checks.

### 3.6 🔴 `checkMandateBound` does not verify the cart binding
- **File**: `backend/policy/engine.go` `checkMandateBound` (lines 124–132).
- **Why:** The spec (Phase 3 §5.3) requires the mandate to be bound to the cart
  end-to-end (`Mandate → Cart → Amount → Merchant → Payment`) and to invalidate
  if **any** link changes (price change, merchant swap, amount drift). The
  current implementation only checks `merchant` and `currency` equality. It
  never checks the **cart_id**, the **items**, or **amount drift** against the
  mandate. The `Mandate` struct has a `CartID` field but it is never compared.
- **Fix:** Bind the mandate to the cart and validate the cart/items/amount
  against it.

### 3.7 🔴 `routeLevel` is called before the risk score is computed
- **File**: `backend/policy/service.go` `Propose` (lines 43–45).
- **Why:** `decision := s.engine.Evaluate(...)` internally calls `routeLevel`
  (which computes the level), and *then* the risk score is computed and
  assigned to `decision.RiskScore`. So even if `routeLevel` were fixed to use
  risk, it could not — the risk is computed after. Ordering is wrong.
- **Fix:** Compute risk first, pass it into `Evaluate`/`routeLevel`.

### 3.8 🔴 `PaymentAdapter` / `RazorpayAdapter` is dead code — the SDK is called directly
- **File**: `backend/commerce/payment/adapter.go`.
- **Why:** The spec (Phase 7 §3) requires a `PaymentAdapter` interface that all
  protocol layers sit on top of, with Razorpay as the current implementation.
  The interface and `RazorpayAdapter` exist, but **nothing constructs or uses
  them**. `main.go` builds a `RazorpayClient` directly and passes it to
  `payment.NewServiceWithAuthorizer(...)` as the `Provider`. So the "adapter
  layer" is not actually the wiring point.
- **Fix:** Either wire `RazorpayAdapter` in `main.go`, or remove it. Right now
  it is misleading dead code.

### 3.9 🔴 `MarkAuthorizationUsed` is never called — authorizations never become `USED`
- **File**: `backend/policy/postgres_repository.go` (lines 125–140).
- **Why:** The method exists and the `authorizations.status` enum includes
  `USED`, but **no code path calls it**. After an authorization is verified and
  used to create a payment, it stays `ACTIVE` forever. A single authorization
  could be replayed to create multiple payments (the idempotency key is the
  only guard, and it's not always set).
- **Fix:** Mark the authorization `USED` after the payment is created.

### 3.10 🔴 `risk_assessments` and `agent_decisions` tables are never written
- **File**: migration `20260822130000_create_policy_tables.sql` creates
  `risk_assessments` and `agent_decisions`.
- **Why:** The Policy Service persists `agent_actions` and
  `policy_evaluations`, but **never inserts into `risk_assessments`** (the spec
  Phase 3 §10 requires persisting scores) and **never inserts into
  `agent_decisions`** (the spec Phase 3 artifact list requires it). These tables
  are dead.
- **Fix:** Write risk scores to `risk_assessments` and decisions to
  `agent_decisions`.

### 3.11 🔴 `experiment_assignments` table is never written
- **File**: migration `20260822150000_create_experiments_tables.sql`.
- **Why:** The experiment service writes to `experiments` but never to
  `experiment_assignments`. The spec (Phase 6 §2) requires recording the
  control/treatment split per session. Dead table.
- **Fix:** Persist assignments when running an experiment.

### 3.12 🔴 `payment_attempts` idempotency key is never set
- **File**: migration `20260822123000_add_idempotency_keys.sql` adds
  `idempotency_key` to `payment_attempts`; `payment/service.go`
  `CreatePaymentOrder` sets `payment.IdempotencyKey` but the **attempt** created
  (lines 203–217) never sets an idempotency key on the `PaymentAttempt`.
- **Fix:** Propagate the idempotency key to the attempt row.

### 3.13 🔴 `payment_attempts` are never marked failed
- **File**: `backend/commerce/payment/attempt_repository.go` and
  `postgres_attempt_repository.go`.
- **Why:** The `AttemptRepository` interface only has `Create` and `MarkPaid`.
  There is **no `MarkFailed`**. On `payment.failed`, the webhook applier
  transitions the payment to `failed` but the attempt row stays `attempted`
  forever.
- **Fix:** Add a `MarkFailed` method and call it from `ApplyFailed`.

### 3.14 🔴 The `orders` table status default is `pending` but the state machine uses `payment_pending`
- **File**: migration `20260822090000_create_orders_tables.sql` (default
  `'pending'`); `20260822124000_expand_order_status_enum.sql` remaps `pending`
  → `payment_pending`. The order repository inserts orders with explicit
  `status = 'payment_pending'` (`order/postgres_repository.go` line 185), so
  this is mostly consistent, but the **default** is still `pending`, which is
  not a valid state in the expanded enum. Any insert that omits status would
  violate the CHECK constraint.
- **Fix:** Change the column default to `'payment_pending'`.

### 3.15 🔴 `orders` are created in `payment_pending` but the state machine expects `draft → authorized → payment_pending`
- **File**: `order/postgres_repository.go` `CheckoutCart` (line 185) inserts the
  order directly as `payment_pending`, skipping `draft` and `authorized`. The
  order state machine (`statemachine.go`) requires `DRAFT → AUTHORIZED →
  PAYMENT_PENDING`. So the order is created in a state that the transition
  table treats as reachable only from `authorized`. This is inconsistent with
  the spec's state machine (Phase 2 §2).
- **Fix:** Either create the order as `draft` and transition it, or add
  `draft → payment_pending` as a legal edge (and document the choice).

### 3.16 🔴 `checkout.tsx` "Change payment method" button is a duplicate of "Retry payment"
- **File**: `frontend/app/checkout.tsx` lines 439–445.
- **Why:** Both the "Retry payment" and "Change payment method" buttons call
  `startPayment()`. There is no actual "change payment method" flow. This is a
  UI stub masquerading as a distinct recovery option (spec Phase 2 §10 requires
  distinct options).

### 3.17 🔴 The "failed" recovery screen requires `payment` to be set
- **File**: `frontend/app/checkout.tsx` line 411: `step === "failed" && payment`.
- **Why:** If the Razorpay modal is dismissed (`ondismiss`), `setStep("failed")`
  is called but `payment` may be null (if `startPayment` failed before setting
  it). The recovery screen would render nothing. The failure message ("cart
  remains reserved for 9 minutes") is also hardcoded and not tied to the actual
  payment state.

### 3.19 🔴 `RazorpayClient.CreatePayment` reads `response["amount"]` as `float64`
- **File**: `backend/commerce/payment/razorpay.go` (lines 78–99).
- **Why:** Razorpay returns `amount` as an integer (paise). Casting to
  `float64` and then `int64(responseAmount)` is lossy-prone and fragile. It
  should read as an integer. Also, `response["status"]` is read but the payment
  is later force-set to `"pending"` in the service, so the provider's status is
  discarded.

### 3.20 🔴 `payment/service.go` overwrites the Razorpay order ID with a local ID
- **File**: `backend/commerce/payment/service.go` lines 184–193.
- **Why:** After `CreatePayment` returns a Razorpay order with `ID` = the
  Razorpay order id, the service does `payment.ProviderOrderID = payment.ID`
  (correct), then **overwrites** `payment.ID = fmt.Sprintf("payment_%s",
  ord.ID)`. So the local `payments.id` is a synthetic ID, and the real Razorpay
  order id is only in `provider_order_id`. This is intentional but fragile — the
  webhook/verify path must always use `provider_order_id`, which it does. Still,
  the double-assignment is confusing and a source of bugs.

### 3.22 🔴 `analytics/service.go` AI-revenue query is wrong
- **File**: `backend/analytics/service.go` lines 44–50.
- **Why:** It sums `orders.subtotal` where `cart_id IN (SELECT DISTINCT cart_id
  FROM recommendations WHERE decision = 'RECOMMEND')`. But `recommendations`
  are **not** linked to orders by `cart_id` in a way that guarantees the order
  actually accepted the recommendation. Also, `recommendations.cart_id` is a
  free-form string set by the caller, not a FK to `carts`. This is a loose,
  possibly-wrong attribution query.

### 3.23 🔴 `analytics` conversion rate counts all carts, including never-checked-out
- **File**: `backend/analytics/service.go` lines 56–62.
- **Why:** `conversion = paid orders / all carts`. Carts that were created but
  never checked out (the normal demo flow creates many carts) deflate the
  conversion rate. The spec wants conversion from real event data; this is a
  rough proxy.

### 3.24 🔴 `experiment.go` uses a hardcoded `1200.0` mean order value
- **File**: `backend/analytics/experiment.go` line 57.
- **Why:** The experiment's "revenue per session" is computed from a hardcoded
  `1200.0` base, not from the actual Merchant Simulator purchase amounts. The
  spec (Phase 6 §2) requires metrics computed from the simulated dataset. This
  is a fudge factor.

### 3.25 🔴 `growth/simulator.go` `Describe` is a hardcoded string
- **File**: `backend/growth/simulator.go` `Describe` (lines 79–85).
- **Why:** It returns a fixed sentence with only the airpods-case purchase rate
  interpolated. It does not actually analyze segments. It's a demo stub.

### 3.26 🔴 `growth/agent.go` `EvaluateCandidate` saves the recommendation but ignores save errors
- **File**: `backend/growth/agent.go` line 101: `_ = g.store.Save(ctx, rec)`.
- **Why:** A failed save is silently swallowed. If the `recommendations` table
  is unavailable, the recommendation is still returned but not persisted, and
  the explanation endpoint (`/growth/recommend/{id}`) would 404.

### 3.27 🔴 `growth` `PolicyVersion` is `"cross_sell_policy_v4"` but `policy.PolicyVersion` is `"v1"`
- **File**: `backend/growth/agent.go` line 11 vs `backend/policy/model.go` line
  9. Two different policy version strings. Confusing and inconsistent.

### 3.28 🔴 `mcp` `request_authorization` does not validate the proposed action schema
- **File**: `backend/mcp/tools.go` `requestAuthorization` (lines 199–222).
- **Why:** It passes the raw fields to `policy.Propose`, which does validate via
  `ValidateProposal`. So this is actually OK, but the MCP tool itself does not
  validate before calling. Minor.

### 3.29 🔴 `mcp` `recommend_bundle` does not pass `RiskCost`
- **File**: `backend/mcp/tools.go` `recommendBundle` (lines 152–180).
- **Why:** The `EVInputs` built here omits `RiskCost` (defaults to 0), while the
  REST growth handler accepts `risk_cost`. Inconsistent EV inputs across the
  two surfaces.

### 3.30 🔴 `payment/handler.go` `CreatePaymentOrder` returns 500 for authorization failures
- **File**: `backend/commerce/payment/handler.go` lines 86–99.
- **Why:** When the payment service rejects a missing/invalid authorization, the
  handler returns `http.StatusInternalServerError` (500) instead of a 401/403.
  A missing authorization is a client error, not a server error.

### 3.31 🔴 `main.go` CORS is hardcoded to `http://localhost:3000`
- **File**: `backend/cmd/server/main.go` line 32. Fine for local dev, but not
  configurable.

### 3.32 🔴 `main.go` `select {}` blocks forever with no shutdown path
- **File**: `backend/cmd/server/main.go` line 480. The process never exits
  cleanly; fine for a server but makes graceful shutdown/restart awkward.

### 3.33 🔴 `go.sum` is a hand-maintained dependency list, not a real checksum file
- **File**: `backend/go.sum`. It looks like a Go module checksum file but is
  actually a list of indirect deps. The Dockerfile copies `go.sum` and runs
  `go mod download`. If this is not a real `go.sum` format, the Docker build
  may fail. Verify this.

### 3.34 🔴 `Dockerfile` uses `FROM golang:1.26` — likely wrong image name
- **File**: `backend/Dockerfile` lines 1 and 12. The official Go image is
  `golang` (e.g. `golang:1.26`), but the more common convention is `golang` or
  `go`. If `golang:1.26` doesn't exist, the Docker build fails. Also the
  `go.sum` copy step is suspect (see 3.33).

---

## 4. Stubs & Placeholders

### 4.1 🟠 `DeterministicExtractor` — the entire LLM intent layer is a stub
- **File**: `backend/agents/deterministic_extractor.go`.
- **Why:** It only parses prompts containing the literal word "budget" and a few
  keywords (`earbud`, `headphone`, `laptop`, `case`, `noise cancellation`,
  `battery`, `sister`, `brother`). It is explicitly documented as a
  test/fallback. **There is no real LLM provider.** The spec (Phase 4 §1)
  requires an LLM call with strict structured output.

### 4.2 🟠 `StreamConsumer` — placeholder consumer that only logs
- **File**: `backend/events/stream_consumer.go`.
- **Why:** It consumes from the Redis stream and **just logs** each event. The
  spec (Phase 2 §6, Phase 6 §1) requires real Analytics/Audit/Notification
  consumers. The comment even says "Analytics, Notification, and Audit
  consumers arrive in later phases."

### 4.3 🟠 `FakeProvider` — a fake payment provider
- **File**: `backend/commerce/payment/fake.go`.
- **Why:** Used in tests. Fine as a test double, but it is the only non-Razorpay
  `Provider` and is not wired to anything in production.

### 4.4 🟠 `MerchantSimulator.Describe` — hardcoded demo text (see 3.25).

### 4.5 🟠 `agentAPIMux` and `dashboardMux` — empty service skeletons
- **File**: `backend/cmd/server/main.go` lines 250–251, 392–398.
- **Why:** The "Agent API Service" (port 8082) and "Dashboard API" (port 8083)
  only serve `/health`. The spec (Phase 1 §1.5) says these are filled in later
  phases. They are still empty. The `/agent/*` contract endpoints are actually
  served on the **Commerce** service (8081), not the Agent API service.

### 4.6 🟠 `orchestrator/` directory is empty
- **File**: `backend/orchestrator/` (empty).
- **Why:** The spec (Phase 1 §1.1) says the orchestrator coordinates agents
  starting Phase 4. It was never implemented.

### 4.7 🟠 `db/schema/` directory is empty
- **File**: `db/schema/` (empty).
- **Why:** The spec expects a schema directory alongside migrations. It exists
  but has no files.

---

## 5. Unwired Code (exists but not connected)

### 5.1 🔵 `PaymentAdapter` / `RazorpayAdapter` — defined but never used (see 3.8).

### 5.2 🔵 `MarkAuthorizationUsed` — defined but never called (see 3.9).

### 5.3 🔵 `risk_assessments` and `agent_decisions` tables — never written (see 3.10).

### 5.4 🔵 `experiment_assignments` table — never written (see 3.11).

### 5.5 🔵 `PaymentAttempt.MarkFailed` — no such method exists; the failed path is unwired (see 3.13).

### 5.6 🔵 `SagaStep*` constants in `order/saga.go` — the saga is a thin wrapper
- **File**: `backend/commerce/order/saga.go`.
- **Why:** The `CheckoutSaga` declares named steps (`CheckoutStarted`,
  `CartValidated`, `OrderCreated`, `PaymentPending`, `CheckoutFailed`) but
  **does not actually implement distinct failure branches**. It just calls
  `repo.CheckoutCart` once and returns. The spec (Phase 2 §9) requires an
  explicit sequence of named steps with defined failure branches. The saga is
  effectively a stub around a single repository call.

### 5.7 🔵 `agentAPIMux` / `dashboardMux` — empty (see 4.5).

### 5.8 🔵 The `streamConsumer` in `main.go` is wired but only logs (see 4.2).

### 5.9 🔵 `experiments` table is written, but `experiment_assignments` is not (see 3.11).

### 5.10 🔵 `policy.Explain` in `main.go` is wired to `policy.ExplainRejection`, but the MCP `explain_decision` tool only works for a **rejected** action — it has no path to explain an approved decision or a real transaction ID (spec Phase 7 §4 requires explaining any transaction).

---

## 6. Unimplemented Features (spec requires, no code)

These are **entire features** the phase specs call for that have **no
implementation** anywhere in the repo.

### 6.1 🟣 Phase 2 §10 — Real failure-recovery UX
The spec requires a full recovery flow: on `payment.failed`, analyze the
failure reason, present **Retry / Change payment method / Remove ₹1,999
accessory / Cancel**, and keep the cart held under TTL. The frontend has a
static "failed" screen with Retry/Change/Cancel, but **no "Remove accessory"**
option and no real failure-reason analysis. The "Change payment method" button
is a duplicate (see 3.16).

### 6.2 🟣 Phase 3 §6 — Level 2/3 approval UI
The spec requires a visible "Approve" button (Level 2) and a hard-gate screen
(Level 3) that cannot be bypassed. **No frontend exists for this.** The backend
routes levels, but there is no UI to approve or hard-gate.

### 6.3 🟣 Phase 4 §1–5 — Real LLM intent extraction
No LLM provider, no structured-output schema call, no real intent extraction
beyond the deterministic stub (see 4.1).

### 6.4 🟣 Phase 5 §5 — Merchant Simulator generator script
The spec requires a **generator script** producing the 10k-customer/50k-session
dataset with a fixed seed. The `MerchantSimulator` Go struct exists, but there
is **no standalone generator script** and no persisted dataset file.

### 6.5 🟣 Phase 6 §4 — Simulated-experiment UI
The merchant overview now presents live metrics, audit activity, and agent
actions. A distinct experiment screen that labels simulated results remains
unimplemented.

### 6.7 🟣 Phase 7 — Real external MCP client connection
The MCP server exists and works over JSON-RPC, but the spec's verification
requires connecting a real MCP client (Claude Desktop / inspector). Not done
(requires a desktop app outside the sandbox).

### 6.8 🟣 Phase 8 — **Entire phase is unimplemented**
- **Red-Team Mode UI** ("Attack the Agent" button) — no code.
- **Canned attack library** (8 attacks) — no code.
- **Replay system** (`run_id`-indexed, Replay button) — no code.
- **100-scenario evaluation suite** — no code.
- **Safety evaluation dashboard** — no code.
- **Trust-boundary diagram** — no diagram file.

### 6.9 🟣 Phase 9 — Remaining deliverables
- **Rehearsed demo script** — no script file.
- **One-page pitch** — no file.

### 6.10 🟣 Phase 8 §2 — LLM-specific threat tests
Prompt injection, tool injection, data exfiltration, goal hijacking, price
manipulation, authorization bypass tests — **none exist**.

### 6.11 🟣 Phase 3 §10 — Risk score persistence to `risk_assessments` (see 3.10).

### 6.12 🟣 Phase 3 §8 — "Why not" explanations for **every** rejection
`ExplainRejection` covers the checks that are actually run, but since
`CheckNoDuplicate` and `CheckUserConsent` are never run (3.5), there are no
explanations for them. Also the MCP `explain_decision` only handles a subset.

### 6.13 🟣 Phase 1 §2.2 — Product CRUD at the Commerce Service layer
The catalog repo has `CreateProduct`, but there is **no HTTP endpoint** to
create/update/delete products. Only read endpoints (`/products`,
`/products/`, `/variants/`) exist.

### 6.14 🟣 Phase 1 §5 — Razorpay Adapter call counter surfaced anywhere
The `RazorpayClient` has a `CallCount()` but **no endpoint or dashboard exposes
it**. The spec says later phases repeatedly need to prove "zero calls" via the
counter; nothing reads it at runtime.

### 6.15 🟣 Phase 8 §4 — `run_id`-indexed replay
No `run_id` is generated or persisted for agent runs. No replay.

### 6.16 🟣 Phase 6 §2 — Control/treatment split persisted to
`experiment_assignments` (see 3.11).

---

## 7. Misimplemented Features & Models

### 7.1 🔴 The "hard chokepoint" is structurally incomplete
The spec (Phase 3 §1) says it must be **structurally impossible** for any code
path to call the Payment Service without a valid authorization. In practice:
- `MarkAuthorizationUsed` is never called (3.9), so a single authorization can
  be replayed.
- The `PaymentAdapter` layer that should be the single choke point is dead code
  (3.8).

### 7.2 🔴 The order state machine is bypassed at creation
Orders are inserted directly as `payment_pending` (3.15), skipping the
`draft → authorized → payment_pending` path the state machine defines.

### 7.3 🔴 `routeLevel` signature contradicts its own doc comment
The doc says it's a function of `(amount, merchant_trust, category_history,
risk_score)`, but the actual signature is `(amount, mandate)` and only uses
merchant trust + amount (3.3).

### 7.4 🔴 The `Mandate` model has a `CartID` but it's never enforced
`Mandate.CartID` exists and the DB has `mandates.cart_id`, but
`checkMandateBound` never checks it (3.6).

### 7.5 🔴 `PaymentAttempt` status enum vs state machine mismatch
`payment_attempts.status` allows `created/attempted/paid/failed`, but the
service only ever sets `attempted` and `paid` (3.13). There's no `failed` write.

### 7.6 🔴 Two different "policy version" constants
`growth.PolicyVersion = "cross_sell_policy_v4"` vs `policy.PolicyVersion =
"v1"` (3.27). The spec wants a single reproducible policy version.

### 7.7 🔴 `recommendations.cart_id` is not a foreign key
The `recommendations` table has `cart_id TEXT NOT NULL` with no FK. The
analytics attribution query (3.22) relies on this loose link.

### 7.8 🔴 `orders.cart_id` is `UNIQUE` but a cart is single-use
`orders.cart_id TEXT NOT NULL UNIQUE` (migration). This enforces one order per
cart, which matches the single-use cart design, but it means a failed payment
cannot create a second order for the same cart — the recovery flow (Phase 2
§10) may be blocked by this constraint.

---

## 8. Missing Artifacts (files/config that should exist)

### 8.1 🟣 No `.env` file (correctly gitignored, but required to run)
`.env.example` exists; you must copy it to `.env` and fill in real Razorpay
keys (and an LLM key once you add one).

### 8.2 🟣 No LLM provider / no LLM env var
No OpenAI/Anthropic/Gemini client, no `LLM_API_KEY` documented.

### 8.4 🟣 No product CRUD endpoints (6.13).

### 8.6 🟣 No red-team UI (Phase 8).

### 8.7 🟣 No replay UI (Phase 8).

### 8.8 🟣 No trust-boundary diagram (Phase 8 §1).

### 8.9 🟣 No demo script / pitch file (Phase 9).

### 8.10 🟣 No `db/schema/` files (empty dir).

### 8.11 🟣 No `orchestrator/` code (empty dir).

### 8.12 🟣 No `db/seeds` for payment attempts or experiments.

### 8.13 🟣 No `.env.example` entry for an LLM key.

### 8.14 🟣 No `README.md` at the repo root (only a frontend README exists).

---

## 9. Test Coverage Gaps

The existing tests are good for the deterministic core, but several important
paths have **no test coverage**:

| Area | What's untested |
|---|---|
| **Frontend** | No frontend tests at all. The checkout and merchant-dashboard flows are untested. |
| **Policy chokepoint end-to-end** | No test proves the full HTTP path (frontend → `/policy/propose` → `/orders/{id}/payment` with `Authorization-Id`). |
| **Mandate binding to cart** | `checkMandateBound` cart/amount drift is untested (and unimplemented, 3.6). |
| **Payment attempt failure** | No test for marking an attempt `failed` (no method exists, 3.13). |
| **Authorization replay** | No test that an authorization becomes `USED` after payment (3.9). |
| **Risk → level routing** | No test that a high-risk amount forces Level 3 (3.3). |
| **`risk_assessments` / `agent_decisions` writes** | No test (3.10). |
| **`experiment_assignments` writes** | No test (3.11). |
| **Red-team / evaluation suite** | No tests (Phase 8 unimplemented). |
| **Replay** | No tests (Phase 8 unimplemented). |
| **MCP `execute_authorized_checkout`** | The spec (Phase 7 §2) requires a `execute_authorized_checkout` tool that requires a valid authorization. **This tool does not exist** — the MCP layer has `create_checkout` (order only) and `request_authorization`, but no tool that executes an authorized payment. Untested and unimplemented. |

---

## 10. Frontend Gaps

- **No Level 2/3 approval UI**.
- **No red-team UI**.
- **No replay UI**.
- **No "Remove accessory" recovery option** (Phase 2 §10).
- **"Change payment method" is a duplicate of retry** (3.16).
- **`failed` screen requires `payment` to be set** (3.17).
- **No frontend tests**.
- The frontend `npm run build` fails in this sandbox due to a
  Turbopack/port-binding sandbox restriction (`Operation not permitted`), not a
  code error — but it means the build was not verified here.

---

## 11. Infrastructure / CI Gaps

- **CI (`ci.yml`) exists** and runs Go format/vet/test + frontend lint/build.
  Good.
- **`go.sum` is not a real checksum file** (3.33) — the Dockerfile's
  `COPY go.mod go.sum ./` + `go mod download` may fail.
- **`Dockerfile` uses `golang:1.26`** (3.34) — verify the image exists.
- **No `.env` handling in CI** — CI has no Razorpay keys, so the backend
  integration tests that hit Postgres (`audit`, `analytics`, `order`, `cart`,
  `catalog`, `payment` repo tests) will **fail in CI** because they connect to
  `localhost:5433` which won't exist in the CI runner. These tests are
  **integration tests that require a live Postgres** but CI does not provision
  one. This is a real CI gap.
- **No Postgres/Redis provisioning in CI** — the integration tests will fail.
- **No LLM key in CI** — fine for now since the LLM layer is stubbed, but once
  real it needs mocking.

---

## 12. Phase-by-Phase Completion Matrix

| Phase | Status | Key gaps |
|---|---|---|
| **1 — Foundations & Commerce Core** | 🟡 Mostly done | No product CRUD endpoint; Razorpay adapter counter not exposed (6.14). |
| **2 — State & Reliability** | 🟡 Mostly done | Order created in `payment_pending` (3.15); recovery UX incomplete (6.1); payment attempts never marked failed (3.13). |
| **3 — Authorization Layer** | 🟠 Partial | Risk not used in level routing (3.3, 3.7); mandate not bound to cart (3.6); duplicate/consent checks missing (3.5); authz never marked USED (3.9); `risk_assessments`/`agent_decisions` never written (3.10); no L2/L3 UI (6.2). |
| **4 — AI Buyer Agent** | 🟠 Partial | LLM intent extraction is a stub (4.1); no real LLM provider (6.3); `/agent/*` contract partially served on commerce service. |
| **5 — Growth Agent** | 🟡 Mostly done | EV formula correct; but simulator is a stub (3.25); policy version inconsistent (3.27); save errors swallowed (3.26); no generator script (6.4). |
| **6 — Analytics & Experimentation** | 🟠 Partial | Metrics computed from DB but attribution/conversion are rough (3.22, 3.23); experiment uses hardcoded value (3.24); `experiment_assignments` unwritten (3.11); no simulated-experiment UI (6.5). |
| **7 — Agent Interface (MCP)** | 🟡 Mostly done | MCP server works; but `PaymentAdapter` dead (3.8); no `execute_authorized_checkout` tool (Phase 7 §2); no real external client tested (6.7). |
| **8 — Red Team & Security** | 🔴 Not implemented | Entire phase missing (6.8, 6.9, 6.10). |
| **9 — Presentation** | 🔴 Not implemented | Entire phase missing (6.9). |

---

## 13. Recommended Fix Checklist

Prioritized by impact. ✅ = completed (see Fix Log); 🔲 = still open.

### P0 — The system cannot complete a real checkout as built

> ✅ **Fully resolved** — mandate seeding, frontend authorization wiring,
> paise money-unit, CORS header allowance, authorization reuse, and the
> JSON key-case fix are all done. **A real browser checkout succeeded:**
> order `order_cart_1787579514601` (₹1,999) → `paid`, payment `captured`.

### P1 — Security/authorization correctness
- ✅ **Feed risk score into `routeLevel`** and force Level 3 on high risk (§3.3, §3.7).
- ✅ **Bind the mandate to the cart** in `checkMandateCartBound` (§3.6).
- ✅ **Implement `CheckNoDuplicate` and `CheckUserConsent`** (§3.5) — note: no-duplicate is now **reuse** an active authorization, not reject.
- ✅ **Call `MarkAuthorizationUsed`** after a payment consumes an authorization (§3.9).
- ✅ **Persist risk scores to `risk_assessments`** (§3.10).
- ✅ **Write to `agent_decisions`** (§3.10).
- ✅ **Add `MarkFailed` for payment attempts** (§3.13).
- ✅ **Persist `experiment_assignments`** (§3.11).

### P2 — Completeness
- ✅ **Real LLM provider** behind `IntentExtractor` — OpenRouter-backed `LLMExtractor` (live-verified).
- ✅ **Wire `PaymentAdapter`/`RazorpayAdapter`** as the real `Provider` (§3.8).
- 🔲 **Add the `execute_authorized_checkout` MCP tool** (Phase 7 §2).
- ✅ **Fix the hardcoded experiment value** + persist `experiment_assignments` (§3.24, §3.11).
- ✅ **Fix the order state machine entry** (§3.15).
- 🔲 **Add product CRUD endpoints** (spec Phase 1 §2.2).
- ✅ **Expose the Razorpay call counter** (`GET /adapter/calls`).
- 🔲 **Unify policy-version constants** (§7.6); **`recommendations.cart_id` FK** (§7.7).

### P3 — Phase 8 & 9
- ✅ **100-scenario evaluation suite** + **trust-boundary diagram** + red-team/safety page shell.
- 🔲 **Red-team attack runner endpoints** (`POST /safety/attacks/{id}/run`) + **evaluation history API**.
- 🔲 **Replay system** (`run_id` propagation + `/runs` APIs).
- 🔲 **Demo script + pitch.**

### P4 — Hygiene
- ✅ Frontend buttons/empty-state fixes; root `README.md`; `.env.example` LLM entries.
- ⚠️ `go.sum` is a real checksum file — §3.33/3.34 were **false positives**.
- 🔲 **Provision Postgres/Redis in CI** (integration tests hardcode `localhost:5433`).

---

*End of audit. This document was generated from a full read of the codebase on
 the current branch and reflects the state of the working tree at that time.*

*Last updated: 2026-08-24 (fix pass 1 — see Fix Log at the top).*
