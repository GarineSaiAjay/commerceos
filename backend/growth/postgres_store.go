package growth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Save(ctx context.Context, r Recommendation) error {
	// cart_total_at_evaluation/budget_at_evaluation are set once, at
	// creation, from values EvaluateCandidate already had in scope --
	// they describe the conditions of the original evaluation, so they
	// are never touched by the ON CONFLICT update path (unlike
	// expected_value/decision/reason, which a re-evaluation may change).
	_, err := s.db.Exec(ctx, `
		INSERT INTO recommendations (
			id, cart_id, product_id, price, purchase_probability,
			incremental_margin, confidence, risk_cost, expected_value,
			decision, policy_version, reason,
			cart_total_at_evaluation, budget_at_evaluation
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE SET
			expected_value = EXCLUDED.expected_value,
			decision = EXCLUDED.decision,
			reason = EXCLUDED.reason
	`,
		r.ID, r.CartID, r.ProductID, r.Price, r.PurchaseProbability,
		r.IncrementalMargin, r.Confidence, r.RiskCost, r.ExpectedValue,
		r.Decision, r.PolicyVersion, r.Reason,
		nullableInt64(r.CartTotalAtEvaluation), nullableInt64(r.BudgetAtEvaluation),
	)

	if err != nil {
		return fmt.Errorf("save recommendation: %w", err)
	}

	return nil
}

// nullableInt64 turns a zero value into a SQL NULL rather than a literal
// 0 -- EvaluateCandidate always sets both fields for a real evaluation,
// so 0 only ever means "not set" (e.g. a future caller that forgets to
// populate them), and NULL is what RejectedDemandByProduct's "known
// budget" filter (backend/growth/demand.go) checks for.
func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// SaveDismissal records that a buyer said "no thanks" to a suggested
// product for a specific cart, so Suggest (suggest.go) can exclude it
// on the next call instead of proposing the exact same product again.
// ON CONFLICT DO NOTHING because a repeat dismissal of the same
// product on the same cart (e.g. a double-click, or the suggestion
// briefly reappearing before the exclusion takes effect) is a no-op,
// not an error -- the first dismissal already recorded everything a
// second one would.
func (s *PostgresStore) SaveDismissal(ctx context.Context, cartID, productID string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO suggestion_dismissals (cart_id, product_id)
		VALUES ($1, $2)
		ON CONFLICT (cart_id, product_id) DO NOTHING
	`, cartID, productID)
	if err != nil {
		return fmt.Errorf("save suggestion dismissal: %w", err)
	}
	return nil
}

// ListDismissedProductIDs returns every product ID dismissed for cartID,
// so Suggest can exclude them from candidates the same way it already
// excludes products already in the cart.
func (s *PostgresStore) ListDismissedProductIDs(ctx context.Context, cartID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT product_id FROM suggestion_dismissals WHERE cart_id = $1
	`, cartID)
	if err != nil {
		return nil, fmt.Errorf("list suggestion dismissals: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan suggestion dismissal: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate suggestion dismissals: %w", err)
	}
	return ids, nil
}

// GetByID fetches a recommendation for the explanation view.
func (s *PostgresStore) GetByID(ctx context.Context, id string) (Recommendation, error) {
	var r Recommendation

	var cartTotal, budgetAmt *int64
	err := s.db.QueryRow(ctx, `
		SELECT id, cart_id, product_id, price, purchase_probability,
			incremental_margin, confidence, risk_cost, expected_value,
			decision, policy_version, reason, created_at,
			cart_total_at_evaluation, budget_at_evaluation
		FROM recommendations
		WHERE id = $1
	`, id).Scan(
		&r.ID, &r.CartID, &r.ProductID, &r.Price, &r.PurchaseProbability,
		&r.IncrementalMargin, &r.Confidence, &r.RiskCost, &r.ExpectedValue,
		&r.Decision, &r.PolicyVersion, &r.Reason, &r.CreatedAt,
		&cartTotal, &budgetAmt,
	)
	if err != nil {
		return Recommendation{}, fmt.Errorf("get recommendation: %w", err)
	}
	if cartTotal != nil {
		r.CartTotalAtEvaluation = *cartTotal
	}
	if budgetAmt != nil {
		r.BudgetAtEvaluation = *budgetAmt
	}

	return r, nil
}
