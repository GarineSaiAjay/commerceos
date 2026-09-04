package analytics

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestComputeAndOverviewAreMerchantScoped proves the P0 fix (full-
// codebase re-audit 2026-09-04): Compute and Overview both require a
// merchantID and every number/row they return is scoped to it. Before
// this fix, neither method took a merchant parameter at all -- GET
// /dashboard/metrics and GET /dashboard/overview handed back the
// entire PLATFORM's revenue, order count, AOV, and recent agent
// actions to whichever operator happened to be logged in, not just
// their own merchant's.
func TestComputeAndOverviewAreMerchantScoped(t *testing.T) {
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
		merchantA = "merchant_001" // already seeded (db/seeds/001_catalog.sql)
		merchantB = "merchant_analytics_isolation_test"
	)

	if _, err := pool.Exec(ctx, `INSERT INTO merchants (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, merchantB); err != nil {
		t.Fatal(err)
	}

	// Clean up any prior run's fixtures (distinctive id prefix).
	for _, merchant := range []string{merchantA, merchantB} {
		_, _ = pool.Exec(ctx, `
			DELETE FROM payments WHERE order_id IN (
				SELECT id FROM orders WHERE id = $1
			)
		`, "order_analytics_scope_"+merchant)
		_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, "order_analytics_scope_"+merchant)
		_, _ = pool.Exec(ctx, `DELETE FROM carts WHERE id = $1`, "cart_analytics_scope_"+merchant)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_actions WHERE id = $1`, "action_analytics_scope_"+merchant)
	}

	svc := NewService(pool)

	// merchant_001 is shared by many other integration tests in this
	// codebase's real-Postgres suite, so its revenue is never zero and
	// never predictable in absolute terms here. Every assertion below
	// therefore compares DELTAS around each insert, not absolute totals
	// -- that's what makes this test correct regardless of what else
	// has run against the shared dev database, while still being an
	// unmistakable failure if merchantA's Compute ever includes so much
	// as one paisa of merchantB's revenue (a fresh, test-only merchant
	// id nothing else in this codebase writes to).
	amounts := map[string]int64{merchantA: 111_00, merchantB: 222_00}

	baselineA, err := svc.Compute(ctx, merchantA)
	if err != nil {
		t.Fatalf("baseline Compute(merchantA): %v", err)
	}
	baselineB, err := svc.Compute(ctx, merchantB)
	if err != nil {
		t.Fatalf("baseline Compute(merchantB): %v", err)
	}

	insertOrderFixture := func(merchant string) {
		cartID := "cart_analytics_scope_" + merchant
		orderID := "order_analytics_scope_" + merchant
		paymentID := "payment_analytics_scope_" + merchant

		if _, err := pool.Exec(ctx, `
			INSERT INTO carts (id, merchant_id, currency, status, expires_at)
			VALUES ($1, $2, 'INR', 'checked_out', NOW() + interval '1 day')
		`, cartID, merchant); err != nil {
			t.Fatalf("insert cart for %s: %v", merchant, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO orders (id, merchant_id, cart_id, currency, subtotal, status)
			VALUES ($1, $2, $3, 'INR', $4, 'paid')
		`, orderID, merchant, cartID, amounts[merchant]); err != nil {
			t.Fatalf("insert order for %s: %v", merchant, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO payments (id, order_id, provider, provider_order_id, amount, currency, status)
			VALUES ($1, $2, 'razorpay', $1, $3, 'INR', 'captured')
		`, paymentID, orderID, amounts[merchant]); err != nil {
			t.Fatalf("insert payment for %s: %v", merchant, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO agent_actions (id, action, amount, currency, merchant, items, proposal)
			VALUES ($1, 'CREATE_ORDER', $2, 'INR', $3, '[]'::jsonb, '{}'::jsonb)
		`, "action_analytics_scope_"+merchant, amounts[merchant], merchant); err != nil {
			t.Fatalf("insert agent_actions for %s: %v", merchant, err)
		}
	}

	// Insert ONLY merchantA's fixture first, and prove merchantB's view
	// is completely unaffected by it (the actual cross-tenant leak this
	// fix closes: merchantB seeing merchantA's revenue, or vice versa).
	insertOrderFixture(merchantA)

	afterA, err := svc.Compute(ctx, merchantA)
	if err != nil {
		t.Fatalf("Compute(merchantA) after its own insert: %v", err)
	}
	if afterA.Revenue != baselineA.Revenue+amounts[merchantA] {
		t.Fatalf("expected merchantA revenue to increase by exactly %d, went from %d to %d", amounts[merchantA], baselineA.Revenue, afterA.Revenue)
	}

	crossCheckB, err := svc.Compute(ctx, merchantB)
	if err != nil {
		t.Fatalf("Compute(merchantB) after merchantA's insert: %v", err)
	}
	if crossCheckB.Revenue != baselineB.Revenue {
		t.Fatalf("merchantB Compute leaked merchantA's revenue: expected unchanged %d, got %d", baselineB.Revenue, crossCheckB.Revenue)
	}

	// Now insert merchantB's fixture and prove the reverse: merchantA's
	// already-measured total must not move again.
	insertOrderFixture(merchantB)

	afterB, err := svc.Compute(ctx, merchantB)
	if err != nil {
		t.Fatalf("Compute(merchantB) after its own insert: %v", err)
	}
	if afterB.Revenue != baselineB.Revenue+amounts[merchantB] {
		t.Fatalf("expected merchantB revenue to increase by exactly %d, went from %d to %d", amounts[merchantB], baselineB.Revenue, afterB.Revenue)
	}

	finalA, err := svc.Compute(ctx, merchantA)
	if err != nil {
		t.Fatalf("Compute(merchantA) after merchantB's insert: %v", err)
	}
	if finalA.Revenue != afterA.Revenue {
		t.Fatalf("merchantA Compute leaked merchantB's revenue: expected unchanged %d, got %d", afterA.Revenue, finalA.Revenue)
	}

	// Overview's AgentActions must be scoped the same way: merchantA's
	// list must contain merchantA's action and must NOT contain
	// merchantB's.
	overviewA, err := svc.Overview(ctx, merchantA, AuditIntegrity{})
	if err != nil {
		t.Fatalf("Overview(merchantA): %v", err)
	}
	sawOwnAction, sawOtherMerchantsAction := false, false
	for _, a := range overviewA.AgentActions {
		if a.ID == "action_analytics_scope_"+merchantA {
			sawOwnAction = true
		}
		if a.ID == "action_analytics_scope_"+merchantB {
			sawOtherMerchantsAction = true
		}
	}
	if !sawOwnAction {
		t.Fatal("expected merchantA's Overview.AgentActions to include its own action")
	}
	if sawOtherMerchantsAction {
		t.Fatal("merchantA's Overview.AgentActions leaked merchantB's action")
	}
}
