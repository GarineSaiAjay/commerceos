# Phase 6 — Analytics & Experimentation

**Status: ✅ COMPLETE — fully verified against a live observed run.**

The Analytics service, the experimentation engine, and the dashboard widgets are built and verified against real data:

- `/dashboard/metrics` and `/dashboard/overview` compute revenue, AI-attributed revenue, conversion, and AOV from real `payments`/`orders`/`carts` rows — no hardcoded metric anywhere in the service.
- `/dashboard/experiment` runs a real control/treatment split over the Merchant Simulator population and returns lift + a 95% confidence interval; the same seed reproduces the same lift/CI (unit test + two live runs).
- The AI Actions feed (`agent_actions`) and audit trail timeline (`audit_events`) are both populated with real timestamps and match DB rows in order.
- The frontend (`/dashboard/analytics`) renders the experiment result in a visually distinct amber "Simulated" panel, separate from the live metric cards — a judge cannot mistake one for the other.

A richer analytics surface (a persisted, listable experiment history with `GET /dashboard/experiments`, a `/dashboard/analytics/timeseries` endpoint, and a dedicated Live/Experiments tab layout) is tracked separately in `files/frontend-analytics-ui.md` as forward-looking polish, not a Phase 6 gap — the phase's own verification checklist is fully satisfied.

No remaining tasks for this phase. See `PROJECT-AUDIT.md` for the full history of what was built and fixed.
