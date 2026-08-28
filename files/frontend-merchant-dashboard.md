# Merchant Dashboard — Implementation Specification

**Status: overview screen done, secondary polish remaining.** `/dashboard` is built and delivers the calm, source-labelled overview this spec asks for: a header with merchant/environment badge, a live data-state indicator (`Live`/`Loading`/`Unavailable`), metric cards (revenue, AI-attributed revenue, conversion, AOV) using a shared `formatINR`, a recent-activity timeline sourced from the audit ledger, an agent-actions table sourced from policy proposals, and audit-integrity + safety-evaluation panels — all served from the real `GET /dashboard/overview` read model (`source: live|simulated`, `generated_at`, empty-state-safe). The shared shell (`app/dashboard/layout.tsx`, sidebar nav, `lib/format.tsx`, `lib/api.ts`) is in place, and all five routes in the information architecture table (Overview, Analytics, Approvals, Runs, Safety) exist.

## Remaining

- **Date range.** The overview has no date-range control; it's always "current." Add one if the demo needs to show a specific historical window.
- **Pagination.** Recent activity and agent actions are unpaginated flat lists — fine at demo scale, but add cursor-based paging if the dataset grows.
- **Accessibility pass.** Headings/captions/`role="alert"` are present in places, but there's been no explicit WCAG AA contrast check, and `aria-live` is not used anywhere for the data-state badge or refresh transitions.
- **Responsive verification.** The grid classes suggest cards collapse from four → two on smaller breakpoints, but this hasn't been visually confirmed at 320px.
- **Masking.** No payment identifiers are currently shown on this screen, so masking is moot here — but double-check the Runs/Safety pages, which do show raw IDs, against the "mask payment identifiers by default" rule.
- **Testing.** No component tests, API contract tests, or Playwright flows exist yet for loading/empty/error/populated states.

## Acceptance criteria — reverify

- A reviewer can trace every displayed metric to an API response and source label — true today.
- Live and simulated numbers cannot be mistaken for one another — true (Overview never shows simulated figures; those live only on `/dashboard/analytics`).
- The screen works at 320px and desktop widths with keyboard-only navigation — not yet verified.
