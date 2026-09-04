package growth

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresStoreQueriesAgainstLiveDB reproduces, end to end against a
// real Postgres, the bug reported live: GET /dashboard/growth failing
// with "Could not load growth data." on every real request.
//
// Root cause (see Funnel's and RejectedDemandByProduct's own doc
// comments): every time-windowed query in dashboard.go and demand.go
// built its WHERE clause with ($N || ' days')::interval. That form
// makes Postgres resolve $N's type as text (because of the ||
// operator), and pgx v5 has no encode plan for a Go int against a
// server-declared text OID in the extended protocol -- so every one of
// these queries failed with "unable to encode N into text format for
// text (OID 25): cannot find encode plan", which GrowthDashboardHandler
// .Overview turned into a plain 500, which the frontend turned into
// "Could not load growth data." None of this package's existing tests
// caught it because they all exercise DashboardStore through a fake
// (dashboard_test.go) or a pure function (demand_test.go) -- never
// *PostgresStore's real SQL. This is the exact same bug
// campaign/postgres_repository.go's Approve hit and fixed (see that
// query's own doc comment) -- this package had the identical pattern
// in seven more places, all fixed alongside this test to use
// make_interval(days => $N) instead, which pgx encodes correctly since
// $N then resolves unambiguously to int4/int8.
//
// Requires a live Postgres reachable at this project's standard dev/CI
// connection string (infra/docker-compose.yml's postgres service
// publishes 5433 for exactly this reason), with migrations and at
// least db/seeds/001_catalog.sql applied -- both already prerequisites
// for this repo's other live-DB tests, so this doesn't add a new
// environment requirement, only a new test that uses one that already
// exists. Run with the dev stack up
// (`docker compose -f infra/docker-compose.yml up -d`) via
// `cd backend && go test ./growth/... -run TestPostgresStoreQueriesAgainstLiveDB -v`.
func TestPostgresStoreQueriesAgainstLiveDB(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	const (
		merchantID = "merchant_growth_live_test"
		cartID     = "cart_growth_live_test"
		productID  = "airpods-pro-2" // already seeded, db/seeds/001_catalog.sql
	)

	if _, err := pool.Exec(ctx, `INSERT INTO merchants (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, merchantID); err != nil {
		t.Fatal(err)
	}

	// Clean slate for a re-run. recommendations/suggestion_impressions/
	// suggestion_dismissals carry no FK to carts at all (dashboard.go's
	// own doc comments explain why: they're scoped to a merchant only
	// via a JOIN, not a foreign key), so each needs its own explicit
	// cleanup rather than relying on carts' cascade.
	_, _ = pool.Exec(ctx, `DELETE FROM suggestion_impressions WHERE cart_id = $1`, cartID)
	_, _ = pool.Exec(ctx, `DELETE FROM suggestion_dismissals WHERE cart_id = $1`, cartID)
	_, _ = pool.Exec(ctx, `DELETE FROM recommendations WHERE cart_id = $1`, cartID)
	_, _ = pool.Exec(ctx, `DELETE FROM carts WHERE id = $1`, cartID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO carts (id, merchant_id, currency, expires_at)
		VALUES ($1, $2, 'INR', NOW() + interval '1 day')
	`, cartID, merchantID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO suggestion_impressions (cart_id, product_id, shown_at)
		VALUES ($1, $2, NOW())
	`, cartID, productID); err != nil {
		t.Fatal(err)
	}

	// cart_total_at_evaluation/budget_at_evaluation populated so
	// ReinstatableAtDiscount's closure gets built and exercised too,
	// not just the reject-count aggregate.
	if _, err := pool.Exec(ctx, `
		INSERT INTO recommendations (
			id, cart_id, product_id, price, purchase_probability,
			incremental_margin, confidence, risk_cost, expected_value,
			decision, policy_version, created_at,
			cart_total_at_evaluation, budget_at_evaluation
		) VALUES (
			'rec_growth_live_test', $1, $2, 190000, 0.5,
			50000, 0.8, 0, 1000,
			'REJECT', 'v1', NOW(), 2900000, 3000000
		)
	`, cartID, productID); err != nil {
		t.Fatal(err)
	}

	store := NewPostgresStore(pool)

	funnel, err := store.Funnel(ctx, merchantID, 7)
	if err != nil {
		t.Fatalf("Funnel: %v", err)
	}
	if funnel.Shown < 1 {
		t.Errorf("Funnel.Shown = %d, want >= 1 (the impression inserted above)", funnel.Shown)
	}

	top, err := store.TopProductsByAcceptance(ctx, merchantID, 7, 10)
	if err != nil {
		t.Fatalf("TopProductsByAcceptance: %v", err)
	}
	foundTop := false
	for _, p := range top {
		if p.ProductID == productID {
			foundTop = true
		}
	}
	if !foundTop {
		t.Errorf("TopProductsByAcceptance did not include %s: %+v", productID, top)
	}

	demand, err := store.RejectedDemandByProduct(ctx, merchantID, 7)
	if err != nil {
		t.Fatalf("RejectedDemandByProduct: %v", err)
	}
	if len(demand) != 1 || demand[0].ProductID != productID || demand[0].RejectCount != 1 {
		t.Errorf("RejectedDemandByProduct = %+v, want exactly one row for %s with RejectCount=1", demand, productID)
	}
	if len(demand) == 1 && demand[0].ReinstatableAtDiscount == nil {
		t.Error("ReinstatableAtDiscount closure is nil, want it set since a known-budget row was inserted")
	}
}
