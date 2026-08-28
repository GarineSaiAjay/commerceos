# Analytics & Experimentation UI — Implementation Specification

**Status: core done, polish remaining.** `/dashboard/analytics` exists, runs a real control/treatment experiment against the Merchant Simulator population (population split, lift, 95% CI), and renders it in a visually distinct amber "Simulated" panel — a judge cannot mistake it for live data. The backend semantics repair called for below is also done: AI attribution requires an accepted recommendation linked to a completed order, conversion has a documented denominator, AOV/revenue use captured/completed payments, and the hardcoded `1200` mean was replaced by the simulator's real purchase amounts.

What's below is the remaining polish work — a richer, dual-tab analytics surface — not yet built.

## Remaining screen design

Add a date-range toolbar, a `Live commerce` tab, and a clearly separate `Experiments (simulated)` tab (today there is one screen with an experiment-runner form only). The live tab should show revenue, AI-attributed revenue, conversion, AOV, a time-series chart, and a labelled definition drawer for each metric. The experiment tab should show population split, control/treatment values, lift, 95% CI, seed, assumptions, and run time (the current single-run form has most of these but nothing persists between runs).

For every chart provide a table equivalent, tooltips with exact values, and an empty state. Do not use a line chart to imply continuity when data is sparse; use daily bars or a table instead.

## Remaining data contract

```text
GET /dashboard/analytics/timeseries?from=&to=&granularity=day
GET /dashboard/experiments?cursor=
GET /dashboard/experiments/{id}
POST /dashboard/experiments
```

`POST /dashboard/experiment` (singular) already accepts `treatment_multiplier` and returns a real report, but nothing is persisted — there's no way to list or re-open a past run. Add the plural, persisted `/dashboard/experiments` endpoints above, backed by the `experiment_assignments` rows already being written, and expose assignment counts (never personal session data).

## Remaining interaction model

- Date range defaults to the last 30 complete days and is shown in every exported/screenshot-ready view.
- Metric cards open a definition panel with numerator, denominator, exclusions, source, and last refresh.
- Confidence interval crossing zero renders "inconclusive", not a positive/negative recommendation.
- Filters update URL query parameters for shareability; server validates allowed ranges and granularity.

## Quality and safety (still to verify)

Guard against divide-by-zero, sparse samples, partial API failure, timezone boundary errors, and stale responses. Never use color alone for improvement/decline.

## Testing and acceptance (not yet built)

- Unit-test all formatters, metric definitions, interval/zero/inconclusive states.
- Contract-test source labels and paise values.
- E2E-test live empty/populated/error states and a simulated experiment run.
