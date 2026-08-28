# Agent Run Replay UI — Implementation Specification

**Status: basic version done, the full forensic event model is not.** `/dashboard/runs` and its detail view exist and work: a list of past agent actions with decision/amount/merchant, and a detail panel showing the proposed action, policy outcome, reason, and authorization — reconstructed read-only from `agent_actions` joined to `policy_evaluations` and `authorizations`. This satisfies "reconstruct a past run at the proposal/decision/authorization level" and cannot execute anything.

What's below — the fine-grained, step-by-step event model — is not built yet.

## Remaining — event model

There is no dedicated `run_id` created at every buyer/growth/checkout entry point and propagated through proposal → policy decision → authorization request → payment attempt → webhook → audit event → outbox event → evaluation scenario as its own correlation ID; today the "run" is really just the underlying agent-action row. Building the full model means a new ordered, immutable run-events table:

```text
run_id, sequence, occurred_at, type, actor, correlation_id,
safe_input, safe_output, decision, policy_version, references, latency_ms
```

with raw untrusted input stored only after redaction, and catalog/product snapshots or content hashes kept so replay stays reproducible even after catalog changes.

## Remaining — UX

Search by run/order/cart/authorization ID, date and outcome filters, and pagination don't exist yet (today it's a flat, unfiltered list capped at 50). The detail screen needs a vertical sequence timeline and step inspector (input received → catalog search → filtering → ranking → recommendation → cart change → policy proposal → approval request → approval/rejection → authorization issued → payment attempt → webhook outcome → recovery/action blocked) — today it shows only the start and end of that chain, not the middle steps, because those steps aren't persisted as discrete events yet.

## Remaining — API

```text
GET /runs?query=&cursor=&from=&to=&outcome=
GET /runs/{run_id}/events
GET /runs/{run_id}/integrity
```

`GET /runs` and `GET /runs/{run_id}` exist; the query/filter parameters, the `/events` sub-resource, and the `/integrity` endpoint (sequence continuity, referenced audit records, snapshot hashes, and a `partially reproducible` response when a historical artifact is missing) do not.

## Acceptance criteria (partially met)

Three arbitrary historical runs can already be pulled up and show a consistent decision/authorization outcome — but "reproduce the recorded sequence of steps" in the fuller sense (the granular timeline above) still needs the event model built first.
