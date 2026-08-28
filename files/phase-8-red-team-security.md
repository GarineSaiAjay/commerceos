# Phase 8 — Red Team & Security Hardening

**Prerequisite:** Phase 7 nearly complete (see remaining item there).

**Status: mostly complete.** The trust boundary is documented, the attack library and red-team runner, the automated evaluation suite, and the safety dashboard are all built and verified live:

- Trust-boundary diagram exists at `files/trust-boundary.md` (Phase 8 §1 artifact).
- 10 canned attacks (`backend/safety/attacks.go`) run through the real Buyer/Growth/Policy pipeline via `POST /safety/attacks/{id}/run` and `POST /safety/evaluations/run` — every attack is BLOCKED with the Razorpay adapter's real call-counter delta at `0`, not an asserted number.
- The automated evaluation suite (`backend/policy/evaluation_suite_test.go`) runs 106 scenarios in CI via `go test`, spanning normal/over-ceiling/over-mandate/unknown-merchant/wrong-currency/disallowed-product/expired-mandate/price-manipulation/duplicate/bundle classes: 0 unauthorized, 0 policy bypasses, 100% graceful failure rate.
- `/dashboard/safety` shows the evaluation history, the attack library, and live evidence (decision, policy check, provider-call delta) for each run; `Overview.SafetySummary` surfaces the latest result.
- Basic replay exists: `GET /runs` and `GET /runs/{id}` reconstruct a proposal → policy decision → authorization trail for any past action, and the dashboard's Runs page renders it.

## Remaining

1. **Threat coverage gap.** The attack library covers authorization override, excessive amount, merchant-metadata manipulation, hidden-add, failed-payment retry, merchant swap, price manipulation, approval bypass, and prompt injection — but three of the six threat categories the spec calls out have no dedicated test: **tool injection** (a tool result trying to trigger unintended tool calls), **data exfiltration** (LLM output attempting to leak secrets/other users' data), and **goal hijacking** (steering the agent away from the user's stated goal). Add a scenario for each.
2. **Prompt injection should be planted in catalog data, not just a user prompt.** The current `att_09`/`att_10` attacks send the malicious string as the buyer's own prompt. The spec specifically wants the string (`"IGNORE ALL PREVIOUS INSTRUCTIONS..."`) planted inside a real **product description** in the catalog, so the test proves untrusted *merchant* content can't hijack the agent — not just untrusted *user* input, which is a different (and already-covered) threat surface.
3. **Replay is coarse.** `/runs` reconstructs proposal → decision → authorization, but not the finer-grained sequence the spec and `files/frontend-run-replay-ui.md` call for (search → filter → rank → recommend, each as its own timed step with a shared `run_id` propagated through every stage). If a richer forensic timeline matters for the demo, this needs its own event-log table and instrumentation pass — otherwise the current coarse replay is enough to satisfy "reconstruct a past run," just not a step-by-step trace.

**Do not consider Phase 8 fully closed until items 1–2 above are addressed — they're the actual security-coverage gaps a judge is most likely to probe.**
