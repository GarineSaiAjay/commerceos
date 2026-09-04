package growth

import (
	"context"
	"fmt"
)

// RejectedDemand summarizes how much REJECT volume a product accumulated
// in a time window -- the campaign orchestrator's input signal. Real,
// observed data from the recommendations table (backend/growth/agent.go
// EvaluateCandidate), not synthetic.
type RejectedDemand struct {
	ProductID string
	// RejectCount is how many REJECT recommendations this product got
	// in the window.
	RejectCount int
	// AvgPrice is the average catalog price (paise) across those
	// recommendations.
	AvgPrice int64
	// KnownBudgetCount is how many of those RejectCount rows have both
	// cart_total_at_evaluation and budget_at_evaluation populated (rows
	// written before that migration landed have neither) -- only these
	// rows can feed ReinstatableAtDiscount's honest count.
	KnownBudgetCount int
	// ReinstatableAtDiscount reports, for a given discount percent, how
	// many of the KnownBudgetCount rows would have cleared their budget
	// ceiling with that discount applied to this product's price. nil
	// if KnownBudgetCount is 0 (nothing to recompute).
	ReinstatableAtDiscount func(discountPercent int) int
}

// evaluationContext is one REJECT recommendation's known cart total,
// budget, and candidate price at evaluation time -- enough to redo the
// budget comparison EvaluateCandidate made, at a hypothetical discount.
type evaluationContext struct {
	CartTotal int64
	Budget    int64
	Price     int64
}

// reinstatableCount counts how many rows would now clear budget --
// cartTotal + (price discounted by discountPercent) <= budget -- at the
// given discount. Pure function, no DB dependency, so the one genuinely
// new calculation in the campaign orchestrator has a fast, DB-free unit
// test (demand_test.go). Uses budget_at_evaluation as the ceiling
// directly (not a tolerance-adjusted "max allowed") -- the per-row
// tolerance percentage isn't persisted, and using the stricter raw
// budget makes this count conservative (an undercount, never an
// overcount) rather than optimistic.
func reinstatableCount(rows []evaluationContext, discountPercent int) int {
	count := 0
	for _, row := range rows {
		discountedPrice := row.Price * int64(100-discountPercent) / 100
		if row.CartTotal+discountedPrice <= row.Budget {
			count++
		}
	}
	return count
}

// RejectedDemandByProduct groups REJECT recommendations by product over
// the last windowDays, for merchantID -- scoped via a JOIN to carts
// since recommendations has no merchant_id column of its own
// (carts.merchant_id, recommendations.cart_id -> carts.id are both
// FK-enforced). Returns products ordered by reject count descending, so
// the caller's argmax pick is just demand[0].
//
// Both queries below use make_interval(days => $2), not
// ($2 || ' days')::interval -- see Funnel's doc comment in
// dashboard.go for exactly why the || form fails against pgx v5. This
// function backs both GET /dashboard/growth and the Campaign
// Orchestrator's POST /campaigns/propose, so the bug affected both
// surfaces. See TestPostgresStoreQueriesAgainstLiveDB for a live-DB
// regression test.
func (s *PostgresStore) RejectedDemandByProduct(
	ctx context.Context,
	merchantID string,
	windowDays int,
) ([]RejectedDemand, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			r.product_id,
			COUNT(*) AS reject_count,
			AVG(r.price)::bigint AS avg_price,
			COUNT(*) FILTER (
				WHERE r.cart_total_at_evaluation IS NOT NULL
				  AND r.budget_at_evaluation IS NOT NULL
			) AS known_budget_count
		FROM recommendations r
		JOIN carts c ON c.id = r.cart_id
		WHERE c.merchant_id = $1
		  AND r.decision = 'REJECT'
		  AND r.created_at >= NOW() - make_interval(days => $2)
		GROUP BY r.product_id
		ORDER BY reject_count DESC
	`, merchantID, windowDays)
	if err != nil {
		return nil, fmt.Errorf("query rejected demand: %w", err)
	}
	defer rows.Close()

	var demand []RejectedDemand
	for rows.Next() {
		var d RejectedDemand
		if err := rows.Scan(&d.ProductID, &d.RejectCount, &d.AvgPrice, &d.KnownBudgetCount); err != nil {
			return nil, fmt.Errorf("scan rejected demand row: %w", err)
		}
		demand = append(demand, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rejected demand rows: %w", err)
	}
	if len(demand) == 0 {
		return demand, nil
	}

	// Second query: the raw known-budget rows for every product in the
	// result, in one round trip, used to build each RejectedDemand's
	// ReinstatableAtDiscount closure.
	rawRows, err := s.db.Query(ctx, `
		SELECT r.product_id, r.cart_total_at_evaluation, r.budget_at_evaluation, r.price
		FROM recommendations r
		JOIN carts c ON c.id = r.cart_id
		WHERE c.merchant_id = $1
		  AND r.decision = 'REJECT'
		  AND r.created_at >= NOW() - make_interval(days => $2)
		  AND r.cart_total_at_evaluation IS NOT NULL
		  AND r.budget_at_evaluation IS NOT NULL
	`, merchantID, windowDays)
	if err != nil {
		return nil, fmt.Errorf("query rejected demand evaluation context: %w", err)
	}
	defer rawRows.Close()

	rawByProduct := map[string][]evaluationContext{}
	for rawRows.Next() {
		var productID string
		var row evaluationContext
		if err := rawRows.Scan(&productID, &row.CartTotal, &row.Budget, &row.Price); err != nil {
			return nil, fmt.Errorf("scan rejected demand evaluation context: %w", err)
		}
		rawByProduct[productID] = append(rawByProduct[productID], row)
	}
	if err := rawRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rejected demand evaluation context: %w", err)
	}

	for i := range demand {
		productRows := rawByProduct[demand[i].ProductID]
		if len(productRows) == 0 {
			continue // no known-budget rows for this product; leave nil
		}
		demand[i].ReinstatableAtDiscount = func(discountPercent int) int {
			return reinstatableCount(productRows, discountPercent)
		}
	}

	return demand, nil
}
