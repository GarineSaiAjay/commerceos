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
	_, err := s.db.Exec(ctx, `
		INSERT INTO recommendations (
			id, cart_id, product_id, price, purchase_probability,
			incremental_margin, confidence, risk_cost, expected_value,
			decision, policy_version, reason
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET
			expected_value = EXCLUDED.expected_value,
			decision = EXCLUDED.decision,
			reason = EXCLUDED.reason
	`,
		r.ID, r.CartID, r.ProductID, r.Price, r.PurchaseProbability,
		r.IncrementalMargin, r.Confidence, r.RiskCost, r.ExpectedValue,
		r.Decision, r.PolicyVersion, r.Reason,
	)

	if err != nil {
		return fmt.Errorf("save recommendation: %w", err)
	}

	return nil
}

// GetByID fetches a recommendation for the explanation view.
func (s *PostgresStore) GetByID(ctx context.Context, id string) (Recommendation, error) {
	var r Recommendation

	err := s.db.QueryRow(ctx, `
		SELECT id, cart_id, product_id, price, purchase_probability,
			incremental_margin, confidence, risk_cost, expected_value,
			decision, policy_version, reason
		FROM recommendations
		WHERE id = $1
	`, id).Scan(
		&r.ID, &r.CartID, &r.ProductID, &r.Price, &r.PurchaseProbability,
		&r.IncrementalMargin, &r.Confidence, &r.RiskCost, &r.ExpectedValue,
		&r.Decision, &r.PolicyVersion, &r.Reason,
	)

	if err != nil {
		return Recommendation{}, fmt.Errorf("get recommendation: %w", err)
	}

	return r, nil
}
