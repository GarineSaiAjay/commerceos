# `orchestrator/`

This directory is intentionally empty — it is not an unfinished module.

The original Phase 1 design sketched a standalone orchestrator layer to sequence agent coordination (Buyer Agent → Growth Agent → Policy Engine handoffs). In the actual build, that sequencing lives directly in the commerce service's HTTP handlers and services (`backend/agents/buyer_agent.go`, `backend/growth/agent.go`, `backend/policy/service.go`), each calling the next stage's interface directly rather than through a separate coordination layer.

This is a deliberate simplification, not a missed step: for a system this size, an extra orchestration layer between handlers that already call each other in a straight line would add an indirection with no corresponding benefit — the same "architecture theatre" anti-pattern this project's own Phase 9 presentation guidance (`files/phase-9-presentation-demo.md` §4) explicitly warns against ("seven microservices for architecture theatre... a modular monolith is the right size for a prototype").

The actual coordination path, end to end, is:

```
Buyer Agent (intent → cart)
    → Growth Agent (cross-sell EV scoring)
        → Policy Engine (permission)
            → Authorization (consent)
                → Payment Service (execution)
```

Every arrow above is a direct Go interface call, traceable in `backend/cmd/server/main.go`'s wiring and each package's own handler/service code — not hidden behind a generic orchestrator abstraction.

See `files/PROJECT-AUDIT.md` §3.12 / Fix Log for the full history of this decision.
