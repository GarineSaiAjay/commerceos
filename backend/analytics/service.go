package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Metrics are computed from real tables/events — never hardcoded.
type Metrics struct {
	Revenue           int64   `json:"revenue"`             // in paise
	AIRevenue         int64   `json:"ai_revenue"`          // attributed to accepted recommendations
	ConversionRate    float64 `json:"conversion_rate"`     // 0..1
	AverageOrderValue int64   `json:"average_order_value"` // in paise
	Simulated         bool    `json:"simulated"`           // always false here
}

// Activity is a recent, inspectable event for the merchant dashboard.
type Activity struct {
	ID         int64          `json:"id"`
	Actor      string         `json:"actor"`
	Action     string         `json:"action"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	Detail     map[string]any `json:"detail"`
	OccurredAt time.Time      `json:"occurred_at"`
}

// AgentAction summarizes an action proposed by an agent. Amounts are paise.
type AgentAction struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"`
	Merchant   string    `json:"merchant"`
	Amount     int64     `json:"amount"`
	OccurredAt time.Time `json:"occurred_at"`
}

// AuditIntegrity is the verifier result presented on the merchant dashboard.
type AuditIntegrity struct {
	Verified    bool  `json:"verified"`
	ChainBroken bool  `json:"chain_broken"`
	RowsChecked int   `json:"rows_checked"`
	BrokenAtID  int64 `json:"broken_at_id,omitempty"`
}

// SafetySummary reports the latest evaluation run from the safety suite.
type SafetySummary struct {
	Available    bool    `json:"available"`
	Message      string  `json:"message"`
	RunID        string  `json:"run_id,omitempty"`
	Scenarios    int     `json:"scenarios,omitempty"`
	Unauthorized int     `json:"unauthorized_payments,omitempty"`
	Duplicates   int     `json:"duplicate_payments,omitempty"`
	Bypasses     int     `json:"policy_bypasses,omitempty"`
	Graceful     float64 `json:"graceful_failure_rate,omitempty"`
	Passed       bool    `json:"passed"`
}

// Overview is the server-owned read model for the merchant dashboard.
type Overview struct {
	Metrics        Metrics        `json:"metrics"`
	RecentActivity []Activity     `json:"recent_activity"`
	AgentActions   []AgentAction  `json:"agent_actions"`
	AuditIntegrity AuditIntegrity `json:"audit_integrity"`
	Safety         SafetySummary  `json:"safety"`
	GeneratedAt    time.Time      `json:"generated_at"`
}

// Service computes dashboard metrics from real event data.
type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// Compute aggregates live from the orders/payments/recommendations tables.
func (s *Service) Compute(ctx context.Context) (Metrics, error) {
	var m Metrics

	// Total revenue from captured payments.
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM payments
		WHERE status IN ('captured', 'completed')
	`).Scan(&m.Revenue)
	if err != nil {
		return Metrics{}, fmt.Errorf("sum revenue: %w", err)
	}

	// AI-attributed revenue: orders whose cart has a RECOMMEND
	// recommendation. recommendations.cart_id is a real FK to carts(id)
	// (see 20260826180000_add_recommendations_cart_fk.sql), so this join
	// is over referentially-intact data, not a loose string match.
	err = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(o.subtotal), 0)
		FROM orders o
		WHERE o.cart_id IN (
			SELECT DISTINCT cart_id FROM recommendations WHERE decision = 'RECOMMEND'
		)
	`).Scan(&m.AIRevenue)
	if err != nil {
		return Metrics{}, fmt.Errorf("sum ai revenue: %w", err)
	}

	// Conversion rate: paid orders / orders that reached the checkout
	// (i.e. were converted from a cart at least once). Counting ALL carts
	// deflates the number with carts that were never checked out.
	var checkoutOrders, paid int64
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(COUNT(*), 0) FROM orders`).Scan(&checkoutOrders)
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(COUNT(*), 0) FROM orders WHERE status = 'paid'`).Scan(&paid)

	if checkoutOrders > 0 {
		m.ConversionRate = float64(paid) / float64(checkoutOrders)
	}

	// AOV: subtotal / paid orders.
	if paid > 0 {
		_ = s.db.QueryRow(ctx, `
			SELECT COALESCE(SUM(subtotal), 0) FROM orders WHERE status = 'paid'
		`).Scan(&m.AverageOrderValue)
		m.AverageOrderValue /= paid
	}

	return m, nil
}

// Overview builds the dashboard read model from persisted source-of-truth
// tables. It returns empty lists rather than fake example activity.
func (s *Service) Overview(ctx context.Context, integrity AuditIntegrity) (Overview, error) {
	metrics, err := s.Compute(ctx)
	if err != nil {
		return Overview{}, err
	}

	overview := Overview{
		Metrics:        metrics,
		RecentActivity: []Activity{},
		AgentActions:   []AgentAction{},
		AuditIntegrity: integrity,
		Safety:         SafetySummary{Available: false, Message: "No safety evaluation has been run yet.", Passed: false},
		GeneratedAt:    time.Now().UTC(),
	}

	// Surface the latest safety-evaluation run, if any.
	if err := s.db.QueryRow(ctx, `
		SELECT run_id, scenario_count, unauthorized_payments, duplicate_payments,
			policy_bypasses, graceful_failure_rate, passed
		FROM safety_evaluations
		ORDER BY created_at DESC
		LIMIT 1
	`).Scan(
		&overview.Safety.RunID, &overview.Safety.Scenarios, &overview.Safety.Unauthorized,
		&overview.Safety.Duplicates, &overview.Safety.Bypasses, &overview.Safety.Graceful,
		&overview.Safety.Passed,
	); err == nil {
		overview.Safety.Available = true
		overview.Safety.Message = "Latest safety evaluation is recorded below."
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, actor, action, entity_type, entity_id, detail, created_at
		FROM audit_events
		ORDER BY id DESC
		LIMIT 12
	`)
	if err != nil {
		return Overview{}, fmt.Errorf("load recent activity: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var activity Activity
		var detail []byte
		if err := rows.Scan(&activity.ID, &activity.Actor, &activity.Action, &activity.EntityType, &activity.EntityID, &detail, &activity.OccurredAt); err != nil {
			return Overview{}, fmt.Errorf("scan activity: %w", err)
		}
		if err := json.Unmarshal(detail, &activity.Detail); err != nil {
			return Overview{}, fmt.Errorf("decode activity detail: %w", err)
		}
		overview.RecentActivity = append(overview.RecentActivity, activity)
	}
	if err := rows.Err(); err != nil {
		return Overview{}, fmt.Errorf("iterate recent activity: %w", err)
	}

	actionRows, err := s.db.Query(ctx, `
		SELECT id, action, merchant, amount, created_at
		FROM agent_actions
		ORDER BY created_at DESC
		LIMIT 6
	`)
	if err != nil {
		return Overview{}, fmt.Errorf("load agent actions: %w", err)
	}
	defer actionRows.Close()
	for actionRows.Next() {
		var action AgentAction
		if err := actionRows.Scan(&action.ID, &action.Action, &action.Merchant, &action.Amount, &action.OccurredAt); err != nil {
			return Overview{}, fmt.Errorf("scan agent action: %w", err)
		}
		overview.AgentActions = append(overview.AgentActions, action)
	}
	if err := actionRows.Err(); err != nil {
		return Overview{}, fmt.Errorf("iterate agent actions: %w", err)
	}

	return overview, nil
}
