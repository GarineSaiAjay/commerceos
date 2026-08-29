package campaign

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Save(ctx context.Context, c Campaign) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO campaigns (
			id, merchant_id, product_id, discount_percent, budget_cap, spent,
			duration_days, status, policy_version, rejected_demand_count,
			reasoning, rejected_reason
		)
		VALUES ($1,$2,$3,$4,$5,0,$6,$7,$8,$9,$10,$11)
	`,
		c.ID, c.MerchantID, c.ProductID, c.DiscountPercent, c.BudgetCap,
		c.DurationDays, c.Status, c.PolicyVersion, c.RejectedDemandCount,
		c.Reasoning, nullableString(c.RejectedReason),
	)
	if err != nil {
		return fmt.Errorf("save campaign: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id string) (Campaign, error) {
	c, err := scanCampaign(r.db.QueryRow(ctx, campaignSelectSQL+` WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Campaign{}, ErrCampaignNotFound
	}
	if err != nil {
		return Campaign{}, fmt.Errorf("get campaign: %w", err)
	}
	return c, nil
}

func (r *PostgresRepository) List(ctx context.Context, merchantID string, status string) ([]Campaign, error) {
	var rows pgx.Rows
	var err error
	if status == "" {
		rows, err = r.db.Query(ctx, campaignSelectSQL+` WHERE merchant_id = $1 ORDER BY created_at DESC`, merchantID)
	} else {
		rows, err = r.db.Query(ctx, campaignSelectSQL+` WHERE merchant_id = $1 AND status = $2 ORDER BY created_at DESC`, merchantID, status)
	}
	if err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	defer rows.Close()

	var campaigns []Campaign
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, fmt.Errorf("scan campaign: %w", err)
		}
		campaigns = append(campaigns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate campaigns: %w", err)
	}
	return campaigns, nil
}

func (r *PostgresRepository) SumActiveBudget(ctx context.Context, merchantID string) (int64, error) {
	var sum int64
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(budget_cap), 0) FROM campaigns
		WHERE merchant_id = $1 AND status = 'ACTIVE'
	`, merchantID).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("sum active campaign budget: %w", err)
	}
	return sum, nil
}

// Approve transitions a PROPOSED campaign directly to ACTIVE (this
// project has no separate "approved but not yet started" scheduling
// step), recording who approved it and setting starts_at/ends_at from
// duration_days. Runs in one transaction so a concurrent Approve/Reject
// on the same campaign can't interleave between the read and the write.
func (r *PostgresRepository) Approve(ctx context.Context, id string, approvedBy string) (Campaign, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Campaign{}, fmt.Errorf("begin approve transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var durationDays int
	err = tx.QueryRow(ctx, `
		SELECT duration_days FROM campaigns WHERE id = $1 AND status = 'PROPOSED' FOR UPDATE
	`, id).Scan(&durationDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return Campaign{}, ErrCampaignNotProposed
	}
	if err != nil {
		return Campaign{}, fmt.Errorf("read campaign for approval: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE campaigns
		SET status = 'ACTIVE', approved_by = $1, starts_at = NOW(),
		    ends_at = NOW() + ($2 || ' days')::interval, updated_at = NOW()
		WHERE id = $3
	`, approvedBy, durationDays, id)
	if err != nil {
		return Campaign{}, fmt.Errorf("approve campaign: %w", err)
	}

	c, err := scanCampaign(tx.QueryRow(ctx, campaignSelectSQL+` WHERE id = $1`, id))
	if err != nil {
		return Campaign{}, fmt.Errorf("read approved campaign: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Campaign{}, fmt.Errorf("commit approve transaction: %w", err)
	}
	return c, nil
}

func (r *PostgresRepository) Reject(ctx context.Context, id string, reason string) (Campaign, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE campaigns
		SET status = 'REJECTED', rejected_reason = $1, updated_at = NOW()
		WHERE id = $2 AND status = 'PROPOSED'
	`, reason, id)
	if err != nil {
		return Campaign{}, fmt.Errorf("reject campaign: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return Campaign{}, ErrCampaignNotProposed
	}
	return r.GetByID(ctx, id)
}

// FindActiveForProduct is a plain read (no lock) for the dashboard/
// handler layer -- the actual checkout-time budget guard is the atomic
// UPDATE in commerce/order/postgres_repository.go, not this call.
func (r *PostgresRepository) FindActiveForProduct(ctx context.Context, merchantID, productID string) (Campaign, error) {
	c, err := scanCampaign(r.db.QueryRow(ctx, campaignSelectSQL+`
		WHERE merchant_id = $1 AND product_id = $2 AND status = 'ACTIVE'
		  AND (ends_at IS NULL OR ends_at > NOW())
		ORDER BY created_at DESC LIMIT 1
	`, merchantID, productID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Campaign{}, ErrCampaignNotFound
	}
	if err != nil {
		return Campaign{}, fmt.Errorf("find active campaign: %w", err)
	}
	return c, nil
}

// campaignSelectSQL is the shared column list for every read path above
// -- GetByID, List, FindActiveForProduct, and the post-Approve re-read
// -- so a future column addition only needs updating (and its scan
// function) in one place.
const campaignSelectSQL = `
	SELECT id, merchant_id, product_id, discount_percent, budget_cap, spent,
		duration_days, starts_at, ends_at, status, policy_version,
		rejected_demand_count, COALESCE(reasoning, ''), COALESCE(approved_by, ''),
		COALESCE(rejected_reason, ''), created_at, updated_at
	FROM campaigns`

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows (Query
// iteration, which also has a Scan method) so every read path above
// shares one field list and one scan function.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCampaign(row rowScanner) (Campaign, error) {
	var c Campaign
	err := row.Scan(
		&c.ID, &c.MerchantID, &c.ProductID, &c.DiscountPercent, &c.BudgetCap, &c.Spent,
		&c.DurationDays, &c.StartsAt, &c.EndsAt, &c.Status, &c.PolicyVersion,
		&c.RejectedDemandCount, &c.Reasoning, &c.ApprovedBy,
		&c.RejectedReason, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return Campaign{}, err
	}
	return c, nil
}

// nullableString turns an empty string into a SQL NULL.
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
