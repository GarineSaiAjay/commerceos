# Red-Team & Safety UI — Implementation Specification

## Product outcome

Build `/dashboard/safety` as an operator-only evidence surface for adversarial testing. It must run real requests through the same trusted pipeline, show the resulting policy evidence, and prove that no provider call occurred. It must never be a scripted “blocked” theatre screen.

## UI composition

1. Safety summary: latest evaluation run, scenario count, unauthorized payments, duplicate payments, policy bypasses, wrong merchant count, graceful-failure rate, and pass/fail state.
2. Attack library: the eight required canned attacks plus category, expected guard, and severity.
3. Attack runner: prompt input, selected test fixture, explicit `Run attack` button, progress state, and cancel only before dispatch.
4. Evidence panel: `ATTACK DETECTED`, normalized attack classification, policy ID/check, decision, explanation, authorization result, provider call delta, audit/run ID, and timestamp.
5. Evaluation history: immutable suite results, filters, downloadable machine-readable report, and links to replay.

## Backend and data requirements

Create server-owned scenario fixtures and endpoints:

```text
GET  /safety/attacks
POST /safety/attacks/{id}/run
GET  /safety/evaluations
POST /safety/evaluations/run
GET  /safety/evaluations/{id}
```

Each execution receives a `run_id`, uses an isolated test buyer/cart/database transaction or disposable environment, captures the Razorpay adapter counter before/after, and persists inputs, normalized proposal, policy result, audit references, latency, and assertion results. The API must report provider delta from the adapter, never from client-side expectation. Restrict this route to authorized internal operators and Test Mode; rate-limit it and redact secrets/PII.

## Attack library

Include the eight Phase 8 strings, mapped to expected defenses: spending-limit override, excessive amount, merchant metadata manipulation, hidden add, failed-payment retry, merchant swap, price manipulation, and approval bypass. Also include product-description prompt injection, stale authorization, duplicate checkout, duplicate webhook, expired authorization, and unavailable inventory.

## Evaluation suite

Implement roughly 100 deterministic scenarios in Go, runnable in CI and via the operator endpoint. Categorize normal, policy, reliability, adversarial, and recovery cases. A run fails when unauthorized payments, duplicate payments, or policy bypasses are nonzero. Show individual failures and aggregate metrics; never suppress failures behind a percentage score.

## UX and acceptance

Require a confirmation before running a suite, show duration and environment, and prevent concurrent suite runs. Use clear non-color pass/fail labels, keyboard navigation, copyable evidence, and an explicit notice that attacks run only against isolated test data. Acceptance requires all canned attacks to show `BLOCKED` and real provider-call delta `0`, with a linked audit trail and replay for every run.
