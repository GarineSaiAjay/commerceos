# Agent Run Replay UI — Implementation Specification

## Product outcome

Build `/dashboard/runs` and `/dashboard/runs/[runId]` so a merchant or reviewer can reconstruct what an agent did, why a policy allowed/blocked it, and whether money moved. Replay is read-only forensic playback, never a mechanism to execute the original action again.

## Event model

Create a `run_id` at every buyer/growth/checkout entry point and propagate it through proposal, policy decision, authorization request, payment attempt, webhook, audit event, outbox event, and evaluation scenario. Persist ordered immutable run events:

```text
run_id, sequence, occurred_at, type, actor, correlation_id,
safe_input, safe_output, decision, policy_version, references, latency_ms
```

Store raw untrusted input only after redaction and with access controls. Keep catalog/product snapshots or content hashes, policy version/config reference, and authoritative price/cart snapshot so replay is reproducible even after catalog changes. Do not store API secrets, full card/payment details, webhook signatures, or unrestricted personal data.

## UX

The runs list supports search by run/order/cart/authorization ID, date and outcome filters, and paginated results. Each detail screen has: a summary header (outcome, amount, merchant, provider-call result); a vertical sequence timeline; a step inspector; a policy evidence card; linked audit integrity; and a copyable, redacted run ID.

Timeline events use stable names: input received, catalog search, filtering, ranking, recommendation, cart change, policy proposal, approval request, approval/rejection, authorization issued, payment attempt, webhook outcome, recovery/action blocked. Selecting a step reveals inputs, outputs, source, timestamp, latency, and related IDs. Visually distinguish untrusted input from validated command and payment truth.

## Replay API

```text
GET /runs?query=&cursor=&from=&to=&outcome=
GET /runs/{run_id}
GET /runs/{run_id}/events
GET /runs/{run_id}/integrity
```

The server authenticates merchant scope, applies redaction, returns cursor pagination, and exposes data provenance. Integrity verifies sequence continuity, referenced audit records, and snapshot hashes. A missing historical artifact yields `partially reproducible` with the exact missing dependency; it must not silently invent a step.

## Implementation stages

1. Define event taxonomy, schema migration, correlation middleware, and redaction policy.
2. Instrument existing agent, policy, payment, webhook, and audit paths.
3. Implement list/detail APIs and contract tests.
4. Build timeline and inspector with loading, empty, unauthorized, not-found, and partial-replay states.
5. Add E2E tests for successful payment, rejected policy, failed recovery, and red-team blocked runs.

## Acceptance criteria

Three arbitrary historical runs reproduce the recorded sequence in order; a reviewer can identify the exact policy version and provider outcome; replay cannot cause a payment; and tampered/missing records are visibly reported rather than masked.
