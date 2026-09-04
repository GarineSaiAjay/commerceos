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
	// SuggestionImpressions/SuggestionAcceptances (PLAN-03-PROACTIVE-
	// GROWTH-AGENT.md §8, item 20): how many cross-sell suggestions
	// were actually SHOWN to a buyer (suggestion_impressions -- one row
	// per real impression, across every /growth/suggest* surface, item
	// 19) versus how many resulted in the buyer adding the product
	// (recommendations.accepted, set by POST /growth/suggest/accept).
	// Previously a merchant saw a single lifetime ai_revenue figure and
	// had no way to tell whether the cross-sell agent is actually
	// engaging buyers or just occasionally getting lucky -- this is the
	// concrete, honest counter pair that closes that gap.
	SuggestionImpressions int64 `json:"suggestion_impressions"`
	SuggestionAcceptances int64 `json:"suggestion_acceptances"`
	Simulated             bool  `json:"simulated"` // always false here
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

// Compute aggregates live from the orders/payments/recommendations
// tables, scoped to ONE merchant. merchantID is a mandatory scoping
// parameter -- added as a P0 security fix (full-codebase re-audit
// 2026-09-04): every query below previously had zero merchant
// filtering at all, so GET /dashboard/metrics handed back the entire
// PLATFORM's total revenue, order count, and average order value to
// whichever operator happened to be logged in, not just their own
// merchant's. Every table here reaches merchant_id one join away
// (orders directly; payments via order_id; recommendations and
// suggestion_impressions via cart_id -> carts.merchant_id).
func (s *Service) Compute(ctx context.Context, merchantID string) (Metrics, error) {
	var m Metrics

	// Total revenue from captured payments, scoped via payments.order_id
	// -> orders.merchant_id (payments itself has no merchant_id column).
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(p.amount), 0)
		FROM payments p
		JOIN orders o ON o.id = p.order_id
		WHERE p.status IN ('captured', 'completed') AND o.merchant_id = $1
	`, merchantID).Scan(&m.Revenue)
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
		WHERE o.merchant_id = $1
		  AND o.cart_id IN (
			SELECT DISTINCT cart_id FROM recommendations WHERE decision = 'RECOMMEND'
		)
	`, merchantID).Scan(&m.AIRevenue)
	if err != nil {
		return Metrics{}, fmt.Errorf("sum ai revenue: %w", err)
	}

	// Conversion rate: paid orders / orders that reached the checkout
	// (i.e. were converted from a cart at least once). Counting ALL carts
	// deflates the number with carts that were never checked out.
	var checkoutOrders, paid int64
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(COUNT(*), 0) FROM orders WHERE merchant_id = $1`, merchantID).Scan(&checkoutOrders)
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(COUNT(*), 0) FROM orders WHERE merchant_id = $1 AND status = 'paid'`, merchantID).Scan(&paid)

	if checkoutOrders > 0 {
		m.ConversionRate = float64(paid) / float64(checkoutOrders)
	}

	// AOV: subtotal / paid orders.
	if paid > 0 {
		_ = s.db.QueryRow(ctx, `
			SELECT COALESCE(SUM(subtotal), 0) FROM orders WHERE merchant_id = $1 AND status = 'paid'
		`, merchantID).Scan(&m.AverageOrderValue)
		m.AverageOrderValue /= paid
	}

	// Suggestion impressions/acceptances (item 20) -- see the Metrics
	// doc comment above for why these are a real, honest counter pair
	// rather than a single lifetime revenue figure. Neither table has
	// its own merchant_id column, so both are scoped via cart_id ->
	// carts.merchant_id, same join shape as AI-attributed revenue above.
	if err := s.db.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0)
		FROM suggestion_impressions si
		JOIN carts c ON c.id = si.cart_id
		WHERE c.merchant_id = $1
	`, merchantID).Scan(&m.SuggestionImpressions); err != nil {
		return Metrics{}, fmt.Errorf("count suggestion impressions: %w", err)
	}
	if err := s.db.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0)
		FROM recommendations r
		JOIN carts c ON c.id = r.cart_id
		WHERE r.accepted = TRUE AND c.merchant_id = $1
	`, merchantID).Scan(&m.SuggestionAcceptances); err != nil {
		return Metrics{}, fmt.Errorf("count suggestion acceptances: %w", err)
	}

	return m, nil
}

// Overview builds the dashboard read model from persisted source-of-truth
// tables, scoped to ONE merchant (see Compute's doc comment for the P0
// fix this and every query below is part of -- full-codebase re-audit
// 2026-09-04). It returns empty lists rather than fake example activity.
func (s *Service) Overview(ctx context.Context, merchantID string, integrity AuditIntegrity) (Overview, error) {
	metrics, err := s.Compute(ctx, merchantID)
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

	// audit_events has no merchant_id column at all (it's a generic
	// actor/action/entity_type/entity_id ledger -- see
	// db/migrations/20260822122000_create_audit_events_table.sql), so
	// unlike every other query in this file there is no single join
	// that scopes it. Instead this matches against the actual, complete
	// set of entity_type values any audit.Writer.Write call in this
	// codebase produces today (verified by grepping every ".Write(ctx, ...)"
	// call site: order/postgres_repository.go's campaign_discount_
	// applied/campaign_budget_exhausted, payment/webhook_applier.go's
	// payment.captured/payment.failed, policy/service.go's
	// policy_settings_updated, agents/cost_guard.go's
	// llm_daily_cost_budget_exceeded) -- resolving each entity_id back
	// to a merchant the same way its own write call site would. This is
	// a P0 security fix (full-codebase re-audit 2026-09-04): before
	// this, GET /dashboard/overview handed every operator the raw
	// actor/action/detail JSON of the last 12 audit events PLATFORM-
	// WIDE, not just their own merchant's -- a straightforward
	// cross-tenant data leak on the exact ledger meant to prove
	// tamper-evidence, not broadcast it. llm_cost_guard is intentionally
	// NOT filtered: it's a platform-wide daily LLM spend guard with no
	// merchant dimension at all (see CostGuard.Check), so there is
	// nothing merchant-private in it to leak.
	//
	// MAINTENANCE NOTE: a future entity_type written by a NEW
	// audit.Writer.Write call site that isn't added to this WHERE
	// clause will simply never appear in this list for anyone -- safe
	// by default (fails closed, not open), but worth updating here when
	// one is added, the same static-list staleness this codebase
	// already documents for policy.PolicyConfig.AllowedProducts.
	rows, err := s.db.Query(ctx, `
		SELECT id, actor, action, entity_type, entity_id, detail, created_at
		FROM audit_events
		WHERE
			(entity_type = 'campaign' AND entity_id IN (
				SELECT id FROM campaigns WHERE merchant_id = $1
			))
			OR (entity_type = 'payment' AND entity_id IN (
				SELECT p.id FROM payments p JOIN orders o ON o.id = p.order_id WHERE o.merchant_id = $1
			))
			OR (entity_type = 'policy_config' AND entity_id = $1)
			OR entity_type = 'llm_cost_guard'
		ORDER BY id DESC
		LIMIT 12
	`, merchantID)
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
		WHERE merchant = $1
		ORDER BY created_at DESC
		LIMIT 6
	`, merchantID)
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
