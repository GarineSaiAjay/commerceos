# Bugs found and fixed — 2026-08-28 usage pass

Six real bugs reported after actually clicking through the app (not read
from code), verified against the code, fixed, and committed on
`fix/checkout-ux-and-agent-catalog-bugs`. Four more doc-hygiene issues
turned up while chasing those down. Everything below is either fixed (with
the commit that fixed it) or explicitly still open — nothing in between.

## Why the fixes didn't show up after pulling this branch

All three of bugs 1, 2, and 3 below were re-reported as still happening
*after* the commits that fixed them, plus a new one: `go test ./...`
passed locally but the identical test failed twice in GitHub Actions
(`TestPostgresRepositoryListProducts: expected 5 products, got 10`). All
four turned out to have the same root cause, not four separate bugs:

- **The local Postgres container has a persistent volume**
  (`infra/docker-compose.yml`'s `postgres_data`). It survives
  `docker compose down` / `up` / restarts — only `down -v` or deleting
  the volume wipes it. So a local DB seeded once, weeks ago, keeps
  whatever it had, forever, regardless of what's in `db/seeds/*.sql` now.
- **The seed files are intentionally idempotent** (`ON CONFLICT (id) DO
  NOTHING`) so they're safe to re-run. But "do nothing on conflict" means
  exactly that: re-running `db/seeds/001_catalog.sql` against a DB that
  already has `airpods-pro-2`, `airpods-case`, `applecare`, and
  `usb-c-adapter` will **not** apply this pass's retrofitted
  `use_cases`/`features` tags to those existing rows — it only inserts
  the 5 brand-new products. The catalog fix for bug 2 needs those 4 rows
  *updated*, and a plain re-run of the seed can't do that.
- **The backend runs as a Docker image** (`infra/docker-compose.yml`'s
  `backend` service, built from `backend/Dockerfile`). `docker compose
  up -d` alone reuses whatever image was last built — it does not pick
  up new Go source until you pass `--build`. That's why the agent's
  reasoning text came back byte-for-byte identical: the container
  serving requests was still running pre-fix code.
- **CI, by contrast, always starts from nothing** — a fresh Postgres
  service container, `goose up`, then `001_catalog.sql` once, on every
  run. So CI genuinely got 10 fresh products and caught a real bug
  (`repository_test.go` hardcoded 5) that a long-lived local DB with
  only 5 rows never could.

**Fix: reset the local stack from scratch**, so it matches what CI does
and picks up both the new code and the new/updated seed data:

```bash
docker compose -f infra/docker-compose.yml down -v
docker compose -f infra/docker-compose.yml up -d --build
goose -dir db/migrations postgres "postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable" up
psql "postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable" -f db/seeds/001_catalog.sql
psql "postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable" -f db/seeds/002_operator.sql
```

`down -v` is what actually matters here — it drops the Postgres volume,
so the reseed starts from an empty `products` table instead of one
where the old 4 rows silently block the retrofit. `--build` on the way
back up is what makes the backend container run the current branch's
Go code instead of whatever was last built. Skipping either one
reproduces exactly the "still broken" symptoms reported after this
branch's first pass.

### 7. CI failing: `TestPostgresRepositoryListProducts` hardcoded 5 products

**Cause:** adding 5 products to `db/seeds/001_catalog.sql` (bug 4, below)
without checking for tests that hardcode the seed's exact size.
`repository_test.go`'s `TestPostgresRepositoryListProducts` asserted
`len(products) != 5` — true before this branch, false after. CI's fresh
DB always has exactly what the seed says (10, now), so it failed
deterministically on every run; a local DB seeded before this branch
(see above) still had 5 rows and passed, which is why `go test ./...`
looked clean locally while CI failed twice on the identical test.

**Fix:** the test now asserts each seeded product ID is present
(`t.Errorf("expected seeded product %q to be present, was missing", id)`)
instead of a bare count, so it fails with a specific missing-product
error on a real seed regression but no longer breaks every time the
catalog grows.

Commit: `7d260ea`

## Fixed

### 1. "Ask the shopping agent" input text was invisible while typing

**Cause:** `globals.css` had the default `create-next-app` dark-mode block
— on an OS/browser set to dark mode, body text color flips to near-white
(`#ededed`). The input has no explicit text-color class, so it inherited
that near-white color, while native `<input>` elements keep a white
background regardless of page theme. Near-white text on a white input =
invisible while typing. No other element on the page hit this because
every other piece of text has an explicit `text-zinc-*`/`text-slate-*`
class overriding the inherited color — this input was the one place that
didn't.

**Fix:** `frontend/app/globals.css` now forces `color-scheme: light`
instead of opting into a dark palette. The app has no actual dark-mode
design anywhere (every screen uses explicit light Tailwind classes), so
this removes the whole class of bug rather than patching one input.

Commit: `a8c7966`

### 2. Shopping agent proposed something unrelated to what was asked

**Two stacked causes, both fixed:**

- `backend/agents/search.go`'s scoring gives +1.5 for a category match and
  +3.0 for a priority match, but nothing else — everything else is
  price-proximity, which always favors the *cheapest* item in budget. The
  category vocabulary both extractors are allowed to emit
  (`"earbuds"`/`"laptop"`/`"accessories"`) didn't appear in any seeded
  product's `use_cases`, and `"battery_life"` — named directly in the
  UI's own placeholder text ("...good battery life") — was on zero
  products' `features`. So category and priority scored 0 on every
  product, for nearly any real prompt, and the agent silently degraded to
  "pick whatever's cheapest" (₹899 Wireless Charging Pad) with no
  indication that happened.
- Separately, the reasoning sentence template
  (`backend/agents/buyer_agent.go`) read `"...matching priority %s within
  budget..."` with no handling for `intent.Priority` being empty (it's
  optional — only budget + category are required) — producing the exact
  broken-looking "matching priority  within budget ₹30000" from the bug
  report.

**Fix:** Retrofitted the 4 non-fixture catalog products' `use_cases`/
`features` with the literal tags the extractors can actually emit (all
realistic claims, not just added to pass a filter), added 5 more products
so there's real differentiation to score against, and gave the reasoning
sentence two shapes — one for when a priority was recognized, one for
when it wasn't — instead of one template that assumed it always was.

`wireless-charging-pad` was deliberately left untouched: its exact fields
are a red-team fixture (`backend/safety/attacks.go` att_14).

Commits: `d30f155`, `6fcee2c`

**Not independently re-verified against a live LLM call** — I don't have
a way to run the app end-to-end from here. Worth re-testing with the
exact prompt that produced the original bad recommendation.

### 3. Order History showed "Failed to load orders" and "No orders yet" at the same time

**Cause:** `fetchOrders()`'s catch block wrote to the page-wide `message`
banner (shared with every other action), while `orders` stayed `[]` on a
failed fetch. The empty-state check only looked at `orders.length === 0`
— it had no way to know a fetch had failed vs. genuinely returned zero
orders, so both rendered together.

**Fix:** Order-loading now has its own `ordersError` state; loading,
error, empty, and populated are mutually exclusive render branches, and
the error state has a retry button.

Commit: `b7e33bf`

**The actual GET /orders failure itself wasn't reproduced or root-caused**
— `ListOrders`'s handler, service, and both SQL queries all read correctly
against the current schema (checked column-by-column against the
migrations). The most likely explanation is a stale backend process still
running old code from before the P0.2 order-history endpoints existed —
worth a full restart of the backend and retest before assuming there's a
deeper bug here.

### 4. Catalog too small to feel like a real shop

**Fix:** Added 5 products (AirPods Max, AirPods 3rd Gen, MagSafe Charger,
Lightning-to-USB-C Cable, AirPods Pro Ear Tips) alongside the original 5,
each with its own `product_variants` row matching the existing pattern.
10 products total now. All new JSON literals validated with `json.loads`
before committing (no live DB available to insert-test against directly).

Commit: `d30f155` (same commit as the tag retrofit above — same file)

### 5. Login with the AUTH.md credentials failed with "Invalid email or password"

**Three real, independent contributing bugs, all fixed:**

- **Most likely actual cause:** `README.md`'s "Getting started" step 3
  only told a fresh setup to apply `db/seeds/001_catalog.sql` — it never
  mentioned `db/seeds/002_operator.sql` at all. Follow the README exactly
  and the `operators` table stays empty forever; every login attempt then
  gets the same generic "invalid email or password" as an actually wrong
  password, because the handler intentionally doesn't distinguish "no
  such operator" from "wrong password" (that's deliberate — see
  `files/AUTH.md` — but it means a missing seed and a typo look
  identical from the UI).
- `db/seeds/002_operator.sql`'s `INSERT INTO operators` has a
  `merchant_id` foreign key on `merchant_001`, which only
  `001_catalog.sql` creates. Running the operator seed before (or
  without) the catalog seed fails that insert — also silently from the
  operator's point of view.
- Email lookup was exact-match (`WHERE email = $1`), and Postgres `TEXT`
  equality is case-sensitive. A copy-paste of the credentials picking up
  different case or a trailing space would fail identically to a wrong
  password.

**The password hash itself was independently re-verified correct** —
recomputed PBKDF2-HMAC-SHA256(password, salt, 210000 iterations) in
Python against the exact salt stored in the seed file and it matches the
stored hash byte-for-byte. That was never the problem.

**Fix:** README now includes the missing seed step;
`db/seeds/002_operator.sql` inserts its own `merchant_001` row so seed
order can never matter; `Service.Login` now lowercases and trims the
email before lookup.

**If login still fails after re-seeding**, run this against the DB to
confirm the row actually exists:
```sql
SELECT id, email FROM operators WHERE email = 'owner@commerceos.demo';
```
Zero rows means the seed genuinely hasn't been applied to whatever
database the running backend is actually pointed at.

Commits: `4177316`, `ee0209f`, `6fea47a`

### 6. `.env.example` and `infra/.env.example` were never actually in the repository

Found while fixing bug 5: `.gitignore`'s `.env.*` line (meant to catch
`.env.local` and similar) also matched `.env.example` and
`infra/.env.example` — the two files `README.md`'s own setup
instructions tell a fresh clone to `cp`. `git log --all` had zero commits
touching either path before this pass. A genuinely fresh clone had
nothing to copy from at all.

While fixing this, also found: `backend/agents/llm_extractor.go` reads
`OPENROUTER_API_KEY`, but the root `.env.example` named the variable
`LLM_API_KEY` — which the code never reads. `infra/.env` (a real,
gitignored file already on this machine) had the right name; the
template everyone else would actually copy from didn't. Silent
consequence: an empty `OPENROUTER_API_KEY` and an unannounced fallback to
the keyword-only `DeterministicExtractor` — directly feeding into bug 2.

**Fix:** `.gitignore` now un-ignores both example files explicitly;
`.env.example` corrected to `OPENROUTER_API_KEY` and given the missing
`LLM_MODEL` variable; both files committed for real.

Commits: `2ccbb65`

## Also fixed: dead documentation links

A recent cleanup deleted `files/PROJECT-AUDIT.md`, `files/COMPLETION-PLAN.md`,
the `files/phase-*.md` specs, and `files/JUDGE-FACING-GAPS.md` — but
`README.md`, `backend/orchestrator/README.md`, and `files/AUTH.md` still
pointed at them by name. Repointed each reference at what actually
carries that information now.

Commit: `d4d61a1`

## Still open

- **Go changes not compiled.** No Go toolchain is available in either
  sandbox this project has been built through (this pass or earlier
  ones). `backend/agents/buyer_agent.go` and `backend/auth/service.go`
  were verified by brace/paren balance and manual review, not `go
  build`/`go vet`/`go test`. Run those locally before trusting this branch.
- **`.env` vs `infra/.env` is genuinely confusing.** Docker Compose reads
  its `.env` from the directory of the first `-f` file given
  (`infra/docker-compose.yml` → `infra/.env`), while the root `.env`
  (from the root `.env.example` this pass fixed) is a second,
  overlapping copy of some of the same variables, used when running the
  backend directly without Docker. Both now have correct variable names,
  but having two files for overlapping purposes is itself worth
  simplifying at some point — not done here, since it's a bigger
  restructuring than a bug fix.
- **The `DeterministicExtractor` fallback is still narrow.** It only
  recognizes a handful of literal words ("earbud", "laptop", "case",
  "battery", "noise cancellation") — fine as the documented "no LLM key
  configured" fallback, but worth broadening if this project is ever
  demoed somewhere `OPENROUTER_API_KEY` might not be set.
- **"Many parts of the UI can be done better."** Fixed the one concrete,
  reproducible bug (input contrast). The catalog step is still a plain
  list with no images or search (a known gap from before this pass) — a
  real UI polish pass is a bigger, more subjective effort than a bug fix
  and needs its own scope, not a guess bundled into this one.
- **Bugs 2 and 3 above have fixes for every root cause found by reading
  the code, but neither was re-verified by actually running the app** —
  I don't have a way to run this stack end-to-end from here. Please
  re-test both after the full reset above (`down -v`, `--build`,
  re-seed) — a restart alone isn't enough for either the code or the
  data to actually change; see "Why the fixes didn't show up" at the
  top of this doc.
