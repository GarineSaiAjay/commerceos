# Analytics & Experimentation UI — Implementation Specification

## Product outcome

Create `/dashboard/analytics`, a trustworthy decision surface for commerce performance. It must prioritize provenance over attractive charts: real captured-payment metrics are live facts; experiment output is simulated evidence and must be labelled as such everywhere.

## Screen design

Use a date-range toolbar, a `Live commerce` tab, and a clearly separate `Experiments (simulated)` tab. The live tab shows revenue, AI-attributed revenue, conversion, AOV, a time-series chart, and a labelled definition drawer for each metric. The experiment tab shows population split, control/treatment values, lift, 95% confidence interval, seed, assumptions, and run time.

For every chart provide a table equivalent, tooltips with exact values, and an empty state. Do not use a line chart to imply continuity when data is sparse; use daily bars or a table instead.

## Data contract

Keep `GET /dashboard/metrics` as the summary endpoint, but add:

```text
GET /dashboard/analytics/timeseries?from=&to=&granularity=day
GET /dashboard/experiments?cursor=
GET /dashboard/experiments/{id}
POST /dashboard/experiments
```

`POST /dashboard/experiments` must return a persisted report plus assumptions. Its request must use `treatment_multiplier`, not an ambiguous `treatment` field. Persist each session's control/treatment assignment in `experiment_assignments`; expose assignment counts, never personal session data.

Before UI work, repair the current backend semantics: AI attribution must require an accepted recommendation linked to the completed order; conversion needs a documented eligible-cart denominator; AOV and revenue use captured/completed payments; the hard-coded 1200 mean must be replaced by a named, versioned simulator assumption or actual observed values.

## Interaction model

- Date range defaults to the last 30 complete days and is shown in every exported/screenshot-ready view.
- Metric cards open a definition panel with numerator, denominator, exclusions, source, and last refresh.
- An experiment cannot be represented as a live uplift. The experiment tab header says `Simulated experiment`; cards and exports repeat that label.
- Confidence interval crossing zero renders “inconclusive”, not a positive/negative recommendation.
- Filters update URL query parameters for shareability; server validates allowed ranges and granularity.

## Quality and safety

All money is paise at the API boundary. Use a single locale-aware INR formatter. Never use color alone for improvement/decline. Guard against divide-by-zero, sparse samples, partial API failure, timezone boundary errors, and stale responses. Merchant authorization is enforced server-side.

## Testing and acceptance

- Unit-test all formatters, metric definitions, interval/zero/inconclusive states.
- Contract-test source labels and paise values.
- E2E-test live empty/populated/error states and a simulated experiment run.
- A reviewer can identify a number's source, time range, calculation, and simulation status without reading code.
