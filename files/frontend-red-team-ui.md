# Red-Team & Safety UI — Implementation Specification

**Status: mostly done.** `/dashboard/safety` exists and is a real operator surface, not a scripted "blocked" screen: the attack library (10 canned attacks including prompt injection), the attack runner (per-attack "Run" button hitting `POST /safety/attacks/{id}/run`), the evidence panel (blocked/not-blocked, policy check, reason, provider-call delta), and evaluation history (`GET /safety/evaluations`, `POST /safety/evaluations/run`) are all wired to the real pipeline and the real adapter call counter — no hardcoded "BLOCKED" text.

## Remaining UI polish

- No confirmation dialog before running the full suite, and no visible guard against a second concurrent run.
- No duration/environment display on a suite run (how long it took, that it ran against Test Mode / isolated data).
- No "isolated test data" notice reassuring an operator this can't touch real orders.
- No downloadable machine-readable report for a past evaluation.
- No links from an evaluation or attack result to its replay (`/dashboard/runs/{run_id}`) — the evidence panel shows a `run_id` conceptually but the current UI doesn't surface one per attack result, so there's nothing to link yet (see the attack-runner backend note below).

## Remaining backend/data gap

Each attack execution should receive its own `run_id` and persist inputs, the normalized proposal, the policy result, audit references, latency, and assertion results as a discrete, retrievable record — today `RunAttack` returns the evidence inline but doesn't appear to hand back a stable `run_id` for later lookup via `/runs/{id}`. Add that so "click through from a red-team result to its replay" is possible.

## Remaining threat coverage

See `files/phase-8-red-team-security.md` — three threat categories (tool injection, data exfiltration, goal hijacking) have no dedicated attack in the library, and the prompt-injection attack should also be planted in an actual catalog product description, not just sent as a user prompt.

## Acceptance (verify once the above lands)

Require a confirmation before running a suite, show duration and environment, and prevent concurrent suite runs. Acceptance requires all canned attacks to show `BLOCKED` and real provider-call delta `0` (already true today), with a linked audit trail and replay for every run (not yet true).
