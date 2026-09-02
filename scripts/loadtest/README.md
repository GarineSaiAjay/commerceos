# Load/chaos readiness pass (item 41, P3)

`PLAN-06-ADDITIONAL-OPPORTUNITIES.md` §5, "Load and chaos readiness for
judging day": at buildathon scale, concurrent load on the single
Postgres/Redis pair backing this project is a real risk that hadn't
been load-tested. This directory is the "concrete, cheap prep" §5
asks for. It has three parts; only the first is a script you run --
the other two were findings, already true before this item touched
anything, recorded here rather than left implicit.

## 1. Run the load test

`k6_load_test.js` hits the three endpoints §5 names -- `/products`,
`/agent/checkout`, `/growth/suggest` -- concurrently, using k6
(https://k6.io). It is deliberately **not** a Go program even though
the plan allows either: a load generator has no reason to live inside
`backend/go.mod` or be picked up by `go build ./...`, so it can never
be the thing that breaks the backend build.

**Install k6** (not part of this repo's own dependencies -- a
load-testing tool, not something the app needs at runtime):
```bash
brew install k6            # macOS
# or see https://k6.io/docs/get-started/installation/ for your platform
```

**Bring the stack up first** (a fresh `docker compose up` applies
`db/seeds/001_catalog.sql`, which this script's cart/agent scenarios
depend on -- `merchant_001` and variant `airpods-pro-2-default`
specifically):
```bash
docker compose -f infra/docker-compose.yml up -d --build
```

**Run it** at the default (10 VUs per scenario, 1 minute):
```bash
k6 run scripts/loadtest/k6_load_test.js
```

**Then escalate** -- a single run at the default tells you almost
nothing about where the actual breaking point is. Increase `VUS` and
`DURATION` until something in the results (see below) actually
degrades, and note the point where it did:
```bash
k6 run -e VUS=25 -e DURATION=2m  scripts/loadtest/k6_load_test.js
k6 run -e VUS=50 -e DURATION=2m  scripts/loadtest/k6_load_test.js
k6 run -e VUS=100 -e DURATION=2m scripts/loadtest/k6_load_test.js
```
Point it at a non-`localhost` target with `-e BASE_URL=...` if judging
uses a deployed URL rather than a local stack -- see §7 in this same
plan doc ("CORS / production readiness") for the other half of that
concern (this script doesn't touch CORS at all).

### Reading the results

k6 prints a summary per scenario. What to actually look at:

- **`products_ok` / `products_duration_ms`** -- the read path every
  buyer hits on every page load. `products_ok` should stay at 100%;
  watch `p(95)` duration climb as `VUS` increases -- that climb, not a
  hard failure, IS the breaking point you're looking for. A sudden
  cliff (latency flat, then a wall) rather than a gradual climb
  usually means something pooled and finite (the Postgres connection
  pool -- see §2 below) just ran out.

- **`agent_checkout_acceptable` and `agent_checkout_server_error`** --
  **expect the large majority of `/agent/checkout` requests to come
  back HTTP 429 once `VUS` is above roughly 1-2, and that is correct,
  not a bug this script found.** Item 34's rate limiter
  (`backend/ratelimit/limiter.go`) is a 10-burst, 1-request-per-6-
  seconds token bucket **per client IP**, and every k6 VU in one run
  shares this test runner's one IP -- so the limiter collapses the
  whole load test down to "one client" almost immediately. That's
  the limiter doing exactly its job (a public judging URL with an
  unmetered path to a paid LLM API was the exact gap item 34 closed).
  The metric that actually matters here is
  `agent_checkout_server_error`, which must stay at **0%** -- any 5xx
  there is a real bug (a panic, a DB pool exhaustion surfacing as an
  error, something) that 429s from the rate limiter are not.

  If you deliberately want to load-test `/agent/checkout`'s own
  handler logic at real concurrency (not just confirm the limiter
  blocks a flood), run k6 with multiple runners behind different
  source IPs, or temporarily raise `llmLimiter`'s burst/refill in
  `backend/cmd/server/main.go` for that one test run and put it back
  -- don't leave it raised, since that's the exact cost-control gap
  item 34 exists to close.

  Also: with `OPENROUTER_API_KEY` set, whatever gets *through* the
  limiter still calls the real OpenRouter LLM API, which has its own
  cost and its own rate limits independent of ours. Know which mode
  you're load-testing in -- unset the key to force the deterministic
  extractor fallback (`agents/llm_extractor.go` →
  `agents/deterministic_extractor.go`) if you want to load-test the
  checkout pipeline without spending real LLM API budget.

- **`growth_suggest_ok` / `growth_suggest_duration_ms`** -- each
  iteration creates a real cart, adds the seeded item (which, since
  item 42, also publishes `cart.item_added` and triggers
  `growth.CartEventConsumer`'s async precompute), then asks for a
  suggestion -- the full real path, not a shortcut. Should stay above
  95% success; watch duration the same way as `products_duration_ms`.

A backend crash, a `context deadline exceeded acquiring connection`-
shaped error in the backend's own logs, or Postgres logging connection
refusals under some `VUS` level is the actual "breaking point" this
script exists to find before a judge does. Write down the `VUS` level
where it first appears -- that number, not a clean bill of health, is
the useful output of this exercise.

### Cleanup

Every run leaves real `cart_loadtest_agent_*`/`cart_loadtest_growth_*`
rows in Postgres (`agent_checkout` and `growth_suggest` both create
real carts to exercise the real path, not mocks) -- harmless, but if
you're running this shortly before a live demo or judging session,
recreate the stack (`docker compose down -v && docker compose up -d
--build`) afterward rather than demoing on top of thousands of
load-test carts.

## 2. Connection pool sizing (already explicit, checked, not changed)

§5 also asks to "confirm connection pool sizing in
`backend/infra/db/postgres.go` is set explicitly rather than left at
pgx defaults." It already was, before this item touched anything:

```go
config.MaxConns = 10
config.MinConns = 2
config.MaxConnLifetime = time.Hour
```

(`NewPostgresPool`, `backend/infra/db/postgres.go`.) pgx's own default
without an explicit `MaxConns` is 4 -- a real landmine for concurrent
load -- but this project never had that landmine; it was already
overridden. `infra/docker-compose.yml` doesn't override Postgres's own
`max_connections` (the official `postgres:17` image's default, 100),
and this compose file runs exactly one backend process, so `MaxConns =
10` leaves ample headroom under that ceiling -- there was no
conflicting constraint to reconcile either.

**Deliberately not bumped as part of this item.** Raising `MaxConns`
without load-test evidence that 10 is actually the bottleneck would be
a guess dressed up as a fix, and Postgres's own `max_connections` is a
shared ceiling across every real consumer (this backend, `goose`
migrations, anything else pointed at the same database) -- change it
based on what §1's load test actually shows at whatever `VUS` level
things start to degrade, not preemptively here.

The Redis client (`redis.NewClient(&redis.Options{Addr: redisAddr})`,
`backend/cmd/server/main.go`) has no explicit `PoolSize` either, but
unlike pgx's default of 4, go-redis v9's default (`10 *
runtime.GOMAXPROCS(0)`) already scales with the container's own CPU
allocation and isn't the kind of fixed, easy-to-outgrow number pgx's
default is -- checked, and left alone for the same "don't tune what
the evidence hasn't flagged" reason as `MaxConns` above. §5 also
doesn't name it specifically -- only `backend/infra/db/postgres.go`.

## 3. Rate limiting the two LLM-backed endpoints (already done, item 34)

§5's third ask -- "Rate-limit the two LLM-backed endpoints
(`/agent/checkout`, and the future agentic-loop endpoint from
`PLAN-01`)" -- shipped earlier in this project's own history as item
34 (`backend/ratelimit/`), before this load-test script existed to
exercise it. See "Reading the results" above for what that means for
this script's own `agent_checkout` scenario.
