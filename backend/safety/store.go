package safety

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists safety evaluations and their attack results.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// SaveEvaluation persists an evaluation and all its attack results.
func (s *Store) SaveEvaluation(ctx context.Context, e Evaluation) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin safety tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO safety_evaluations (
			id, run_id, scenario_count, unauthorized_payments, duplicate_payments,
			policy_bypasses, wrong_merchant, invalid_authorization,
			graceful_failure_rate, passed, source
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'suite')
	`,
		e.ID, e.RunID, e.ScenarioCount, e.UnauthorizedPayments, e.DuplicatePayments,
		e.PolicyBypasses, e.WrongMerchant, e.InvalidAuthorization,
		e.GracefulFailureRate, e.Passed,
	)
	if err != nil {
		return fmt.Errorf("save safety evaluation: %w", err)
	}

	for _, r := range e.Results {
		_, err = tx.Exec(ctx, `
			INSERT INTO safety_attack_results (
				evaluation_id, attack_id, attack_string, attack_kind, blocked,
				decision, reason, policy_check, provider_call_delta, run_id
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`,
			e.ID, r.AttackID, r.AttackString, r.AttackKind, r.Blocked,
			r.Decision, r.Reason, r.PolicyCheck, r.ProviderCallDelta, r.RunID,
		)
		if err != nil {
			return fmt.Errorf("save attack result: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit safety evaluation: %w", err)
	}
	return nil
}

// GetEvaluation fetches an evaluation with its results.
func (s *Store) GetEvaluation(ctx context.Context, id string) (Evaluation, error) {
	var e Evaluation
	err := s.db.QueryRow(ctx, `
		SELECT id, run_id, scenario_count, unauthorized_payments, duplicate_payments,
			policy_bypasses, wrong_merchant, invalid_authorization,
			graceful_failure_rate, passed
		FROM safety_evaluations
		WHERE id = $1
	`, id).Scan(
		&e.ID, &e.RunID, &e.ScenarioCount, &e.UnauthorizedPayments, &e.DuplicatePayments,
		&e.PolicyBypasses, &e.WrongMerchant, &e.InvalidAuthorization,
		&e.GracefulFailureRate, &e.Passed,
	)
	if err != nil {
		return Evaluation{}, err
	}

	rows, err := s.db.Query(ctx, `
		SELECT attack_id, attack_string, attack_kind, blocked, decision, reason,
			policy_check, provider_call_delta, run_id
		FROM safety_attack_results
		WHERE evaluation_id = $1
		ORDER BY id
	`, id)
	if err != nil {
		return Evaluation{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var r AttackResult
		if err := rows.Scan(&r.AttackID, &r.AttackString, &r.AttackKind, &r.Blocked,
			&r.Decision, &r.Reason, &r.PolicyCheck, &r.ProviderCallDelta, &r.RunID); err != nil {
			return Evaluation{}, err
		}
		e.Results = append(e.Results, r)
	}
	return e, rows.Err()
}

// ListEvaluations returns recent evaluations (newest first).
func (s *Store) ListEvaluations(ctx context.Context, limit int) ([]Evaluation, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, run_id, scenario_count, unauthorized_payments, duplicate_payments,
			policy_bypasses, wrong_merchant, invalid_authorization,
			graceful_failure_rate, passed
		FROM safety_evaluations
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Evaluation
	for rows.Next() {
		var e Evaluation
		if err := rows.Scan(&e.ID, &e.RunID, &e.ScenarioCount, &e.UnauthorizedPayments, &e.DuplicatePayments,
			&e.PolicyBypasses, &e.WrongMerchant, &e.InvalidAuthorization,
			&e.GracefulFailureRate, &e.Passed); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
