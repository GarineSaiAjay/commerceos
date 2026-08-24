# Merchant Dashboard — Implementation Specification

## Product outcome

Deliver one calm, operations-first `/dashboard` screen that lets a merchant answer, in under ten seconds: **what revenue happened, what the agent influenced, whether the system is safe, and what needs attention**. This is not a marketing dashboard: every number must identify its source and time range.

## Information architecture

Use a persistent left navigation on desktop and a compact top navigation on mobile:

| Area | Primary job | Route |
|---|---|---|
| Overview | Revenue, safety, and latest activity | `/dashboard` |
| Analytics | Inspect real and simulated metrics separately | `/dashboard/analytics` |
| Approvals | Resolve Level 2/3 decisions | `/dashboard/approvals` |
| Runs | Replay an agent run | `/dashboard/runs` |
| Safety | Run red-team attacks and review evaluation results | `/dashboard/safety` |

The overview must contain four sections in this order: metric cards; live activity/audit trail; agent actions; safety and audit integrity. Keep simulated experiment results visually isolated and never merge them into live revenue cards.

## Overview layout

1. Header: merchant name, environment badge (`Test mode`/`Live`), date range, refresh timestamp, and a visible data-state indicator (`Live`, `Loading`, `Unavailable`).
2. Metric cards: Revenue, AI-attributed revenue, conversion, and AOV. Display amount values in paise converted with one shared `formatINR` utility.
3. Activity timeline: intent, recommendation, policy decision, authorization, payment, webhook result. Each event links to its order/run/audit detail.
4. Agent actions: recommendations accepted/rejected, cart optimizations, and policy outcomes. Explain the action rather than implying the model moved money.
5. Integrity & safety: hash-chain verification, events checked, last evaluation run, unauthorized payments, duplicate payments, and policy bypasses.

## API contract and missing backend work

Existing APIs are `GET /dashboard/metrics`, `POST /dashboard/experiment`, and `GET /audit/verify`. Add a dashboard read model rather than making the browser join tables:

```text
GET /dashboard/overview?from=...&to=...
{
  metrics, audit_integrity, safety_summary,
  recent_activity[], agent_actions[], generated_at
}
```

Every response must include `source: live|simulated`, `generated_at`, and an empty-state-safe shape. Add pagination and cursor-based filtering for activity. The server must scope all queries by authenticated merchant; never accept a merchant ID from the browser as authority.

## UX requirements

- Amounts: use paise internally; display ₹ with Indian grouping and no ambiguous unit labels.
- Loading: skeleton cards and timeline rows; do not show `0` while a request is loading.
- Empty state: explain what will populate the area and link to a real action, e.g. “No captured payments yet — view checkout”.
- Error state: keep last known data visible, show retry, state which section failed, and never fabricate values.
- Accessibility: semantic headings, table captions, keyboard focus, text labels in addition to color, WCAG AA contrast, and `aria-live` only for genuinely changing status.
- Responsive: cards collapse from four to two to one; tables become labelled stacked rows; the safety status remains above the fold.

## Security and truthfulness rules

Do not expose Razorpay secrets, raw webhook signatures, PII, or unrestricted audit detail. Mask payment identifiers by default. A green dashboard status is only valid when the verifier and evaluation APIs independently reported success. Every simulated value carries a `Simulated` badge and links to its experiment configuration.

## Delivery plan

1. Create shared shell, routing, API client, money/date utilities, loading/error/empty components.
2. Add the overview read-model endpoint and contract tests.
3. Build cards, activity timeline, actions, and integrity/safety widgets from real data.
4. Add filters, responsive behavior, and accessibility review.
5. Add component tests, API contract tests, and Playwright flows for loading, empty, error, and populated states.

## Acceptance criteria

- A reviewer can trace every displayed metric to an API response and source label.
- Live and simulated numbers cannot be mistaken for one another.
- Audit integrity and safety failure states are unmistakable and actionable.
- The screen works at 320px and desktop widths with keyboard-only navigation.
