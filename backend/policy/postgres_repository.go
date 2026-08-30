package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) GetMandate(
	ctx context.Context,
	id string,
) (Mandate, error) {
	var m Mandate
	var categories, methods []byte
	var expiresAt time.Time
	var cartID *string

	err := r.db.QueryRow(ctx, `
		SELECT id, buyer, merchant, allowed_categories, maximum_amount,
		       currency, requires_confirmation_above, allowed_payment_methods,
		       expires_at, purpose, status, cart_id
		FROM mandates
		WHERE id = $1
	`, id).Scan(
		&m.ID, &m.Buyer, &m.Merchant, &categories, &m.MaximumAmount,
		&m.Currency, &m.RequiresConfirmationAbove, &methods,
		&expiresAt, &m.Purpose, &m.Status, &cartID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Mandate{}, ErrMandateNotFound
	}
	if err != nil {
		return Mandate{}, fmt.Errorf("get mandate: %w", err)
	}

	if cartID != nil {
		m.CartID = *cartID
	}

	m.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)

	if err := json.Unmarshal(categories, &m.AllowedCategories); err != nil {
		return Mandate{}, fmt.Errorf("unmarshal allowed categories: %w", err)
	}
	if err := json.Unmarshal(methods, &m.AllowedPaymentMethods); err != nil {
		return Mandate{}, fmt.Errorf("unmarshal allowed payment methods: %w", err)
	}

	return m, nil
}

func (r *PostgresRepository) SaveMandate(
	ctx context.Context,
	m Mandate,
) error {
	categories, err := json.Marshal(m.AllowedCategories)
	if err != nil {
		return fmt.Errorf("marshal allowed categories: %w", err)
	}
	methods, err := json.Marshal(m.AllowedPaymentMethods)
	if err != nil {
		return fmt.Errorf("marshal allowed payment methods: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse mandate expiry: %w", err)
	}

	_, err = r.db.Exec(ctx, `
		INSERT INTO mandates (
			id, buyer, merchant, allowed_categories, maximum_amount, currency,
			requires_confirmation_above, allowed_payment_methods, expires_at,
			purpose, status, cart_id
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, m.ID, m.Buyer, m.Merchant, categories, m.MaximumAmount, m.Currency,
		m.RequiresConfirmationAbove, methods, expiresAt, m.Purpose, m.Status,
		nilIfEmpty(m.CartID))
	if err != nil {
		return fmt.Errorf("save mandate: %w", err)
	}
	return nil
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (r *PostgresRepository) GetAuthorization(
	ctx context.Context,
	id string,
) (Authorization, error) {
	var a Authorization
	var items []byte
	var expiresAt time.Time

	err := r.db.QueryRow(ctx, `
		SELECT id, mandate_id, action, amount, currency, merchant, items,
			policy_version, decision, risk_score, level, expires_at, status
		FROM authorizations
		WHERE id = $1
	`, id).Scan(
		&a.ID, &a.MandateID, &a.Action, &a.Amount, &a.Currency, &a.Merchant,
		&items, &a.PolicyVersion, &a.Decision, &a.RiskScore, &a.Level,
		&expiresAt, &a.Status,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return Authorization{}, ErrAuthorizationNotFound
	}
	if err != nil {
		return Authorization{}, fmt.Errorf("get authorization: %w", err)
	}

	a.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)

	if err := json.Unmarshal(items, &a.Items); err != nil {
		return Authorization{}, fmt.Errorf("unmarshal authorization items: %w", err)
	}

	return a, nil
}

func (r *PostgresRepository) SaveAuthorization(
	ctx context.Context,
	a Authorization,
) error {
	items, _ := json.Marshal(a.Items)

	expiresAt, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse authorization expiry: %w", err)
	}

	_, err = r.db.Exec(ctx, `
		INSERT INTO authorizations (
			id, mandate_id, action, amount, currency, merchant, items,
			policy_version, decision, risk_score, level, expires_at, status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`,
		a.ID, a.MandateID, a.Action, a.Amount, a.Currency, a.Merchant,
		items, a.PolicyVersion, a.Decision, a.RiskScore, a.Level,
		expiresAt, a.Status,
	)

	if err != nil {
		return fmt.Errorf("save authorization: %w", err)
	}

	return nil
}

func (r *PostgresRepository) MarkAuthorizationUsed(
	ctx context.Context,
	id string,
) error {
	_, err := r.db.Exec(ctx, `
		UPDATE authorizations
		SET status = 'USED', updated_at = NOW()
		WHERE id = $1
	`, id)

	if err != nil {
		return fmt.Errorf("mark authorization used: %w", err)
	}

	return nil
}

func (r *PostgresRepository) SaveEvaluation(
	ctx context.Context,
	e Evaluation,
) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO policy_evaluations (
			id, action_id, policy_version, decision, reason,
			authorization_id, risk_score, level
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`,
		e.ID, e.ActionID, e.PolicyVersion, e.Decision, e.Reason,
		e.AuthorizationID, e.RiskScore, e.Level,
	)

	if err != nil {
		return fmt.Errorf("save evaluation: %w", err)
	}

	return nil
}

func (r *PostgresRepository) SaveAction(
	ctx context.Context,
	a ProposedAction,
	actionID string,
) error {
	items, _ := json.Marshal(a.Items)

	_, err := r.db.Exec(ctx, `
		INSERT INTO agent_actions (id, action, amount, currency, merchant, items, proposal)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`,
		actionID, a.Action, a.Amount, a.Currency, a.Merchant, items, items,
	)

	if err != nil {
		return fmt.Errorf("save action: %w", err)
	}

	return nil
}

// ActiveAuthorizationExists returns true when a still-ACTIVE
// authorization exists for this exact proposal (action, amount, currency,
// merchant, items). Rejected proposals never issue an authorization, so a
// legitimate retry of a previously-rejected checkout is not blocked — only
// re-minting an authorization for an already-approved action is.
func (r *PostgresRepository) ActiveAuthorizationExists(
	ctx context.Context,
	a ProposedAction,
) (bool, error) {
	items, _ := json.Marshal(a.Items)

	var exists bool

	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM authorizations
			WHERE status = 'ACTIVE'
			  AND action = $1
			  AND amount = $2
			  AND currency = $3
			  AND merchant = $4
			  AND items = $5::jsonb
		)
	`, a.Action, a.Amount, a.Currency, a.Merchant, items).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check active authorization: %w", err)
	}

	return exists, nil
}

// GetActiveAuthorization returns the existing ACTIVE authorization for
// this exact proposal, or ErrAuthorizationNotFound if none exists.
func (r *PostgresRepository) GetActiveAuthorization(
	ctx context.Context,
	a ProposedAction,
) (Authorization, error) {
	items, _ := json.Marshal(a.Items)

	var auth Authorization
	var expiresAt time.Time
	err := r.db.QueryRow(ctx, `
		SELECT id, mandate_id, action, amount, currency, merchant, items,
			policy_version, decision, risk_score, level, expires_at, status
		FROM authorizations
		WHERE status = 'ACTIVE'
		  AND action = $1
		  AND amount = $2
		  AND currency = $3
		  AND merchant = $4
		  AND items = $5::jsonb
		ORDER BY created_at DESC
		LIMIT 1
	`, a.Action, a.Amount, a.Currency, a.Merchant, items).Scan(
		&auth.ID, &auth.MandateID, &auth.Action, &auth.Amount, &auth.Currency, &auth.Merchant,
		&items, &auth.PolicyVersion, &auth.Decision, &auth.RiskScore, &auth.Level,
		&expiresAt, &auth.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Authorization{}, ErrAuthorizationNotFound
	}
	if err != nil {
		return Authorization{}, fmt.Errorf("get active authorization: %w", err)
	}

	auth.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	if err := json.Unmarshal(items, &auth.Items); err != nil {
		return Authorization{}, fmt.Errorf("unmarshal authorization items: %w", err)
	}

	return auth, nil
}

// SaveRiskAssessment persists a risk score and its contributing factors.
func (r *PostgresRepository) SaveRiskAssessment(
	ctx context.Context,
	assessment RiskAssessment,
) error {
	factors, err := json.Marshal(assessment.Factors)
	if err != nil {
		return fmt.Errorf("marshal risk factors: %w", err)
	}

	_, err = r.db.Exec(ctx, `
		INSERT INTO risk_assessments (id, action_id, risk_score, factors)
		VALUES ($1, $2, $3, $4)
	`,
		assessment.ID, assessment.ActionID, assessment.RiskScore, factors,
	)
	if err != nil {
		return fmt.Errorf("save risk assessment: %w", err)
	}

	return nil
}

// SaveAgentDecision persists an agent-visible decision row.
func (r *PostgresRepository) SaveAgentDecision(
	ctx context.Context,
	d AgentDecision,
) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO agent_decisions (id, action_id, decision, reason)
		VALUES ($1, $2, $3, $4)
	`,
		d.ID, d.ActionID, d.Decision, d.Reason,
	)
	if err != nil {
		return fmt.Errorf("save agent decision: %w", err)
	}

	return nil
}

// SaveApprovalRequest persists a durable human-approval request.
func (r *PostgresRepository) SaveApprovalRequest(
	ctx context.Context,
	a ApprovalRequest,
) error {
	items, _ := json.Marshal(a.Items)

	_, err := r.db.Exec(ctx, `
		INSERT INTO approval_requests (
			id, mandate_id, action, amount, currency, merchant, items,
			cart_id, policy_version, risk_score, level, status, reason
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`,
		a.ID, a.MandateID, a.Action, a.Amount, a.Currency, a.Merchant, items,
		nilIfEmpty(a.CartID), a.PolicyVersion, a.RiskScore, a.Level, a.Status, a.Reason,
	)
	if err != nil {
		return fmt.Errorf("save approval request: %w", err)
	}

	return nil
}

// GetApprovalRequest fetches an approval request by ID.
func (r *PostgresRepository) GetApprovalRequest(
	ctx context.Context,
	id string,
) (ApprovalRequest, error) {
	var a ApprovalRequest
	var items []byte
	var cartID *string
	var authID *string

	err := r.db.QueryRow(ctx, `
		SELECT id, mandate_id, action, amount, currency, merchant, items,
			cart_id, policy_version, risk_score, level, status, authorization_id, reason
		FROM approval_requests
		WHERE id = $1
	`, id).Scan(
		&a.ID, &a.MandateID, &a.Action, &a.Amount, &a.Currency, &a.Merchant, &items,
		&cartID, &a.PolicyVersion, &a.RiskScore, &a.Level, &a.Status, &authID, &a.Reason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalRequest{}, ErrApprovalRequestNotFound
	}
	if err != nil {
		return ApprovalRequest{}, fmt.Errorf("get approval request: %w", err)
	}

	if cartID != nil {
		a.CartID = *cartID
	}
	if authID != nil {
		a.AuthorizationID = *authID
	}
	if err := json.Unmarshal(items, &a.Items); err != nil {
		return ApprovalRequest{}, fmt.Errorf("unmarshal approval request items: %w", err)
	}

	return a, nil
}

// GetPendingApprovalForAction returns a PENDING approval request for an
// identical action, or ErrApprovalRequestNotFound if none.
func (r *PostgresRepository) GetPendingApprovalForAction(
	ctx context.Context,
	a ProposedAction,
) (ApprovalRequest, error) {
	items, _ := json.Marshal(a.Items)

	req := ApprovalRequest{}
	var rawItems []byte

	err := r.db.QueryRow(ctx, `
		SELECT id, mandate_id, action, amount, currency, merchant, items,
			cart_id, policy_version, risk_score, level, status, authorization_id, reason
		FROM approval_requests
		WHERE status = 'PENDING'
		  AND action = $1
		  AND amount = $2
		  AND currency = $3
		  AND merchant = $4
		  AND items = $5::jsonb
		ORDER BY created_at DESC
		LIMIT 1
	`, a.Action, a.Amount, a.Currency, a.Merchant, items).Scan(
		&req.ID, &req.MandateID, &req.Action, &req.Amount, &req.Currency, &req.Merchant, &rawItems,
		&req.CartID, &req.PolicyVersion, &req.RiskScore, &req.Level, &req.Status, &req.AuthorizationID, &req.Reason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalRequest{}, ErrApprovalRequestNotFound
	}
	if err != nil {
		return ApprovalRequest{}, fmt.Errorf("get pending approval: %w", err)
	}

	if err := json.Unmarshal(rawItems, &req.Items); err != nil {
		return ApprovalRequest{}, fmt.Errorf("unmarshal approval request items: %w", err)
	}

	return req, nil
}

// UpdateApprovalRequestStatus transitions the request and records the
// authorization ID (if issued) and a reason.
func (r *PostgresRepository) UpdateApprovalRequestStatus(
	ctx context.Context,
	id, status, authorizationID, reason string,
) error {
	_, err := r.db.Exec(ctx, `
		UPDATE approval_requests
		SET status = $1,
		    authorization_id = $2,
		    reason = $3,
		    updated_at = NOW()
		WHERE id = $4
	`, status, nilIfEmpty(authorizationID), reason, id)
	if err != nil {
		return fmt.Errorf("update approval request: %w", err)
	}

	return nil
}

// ListApprovalRequests returns approval requests, newest first, optionally
// filtered by status (empty = all).
func (r *PostgresRepository) ListApprovalRequests(
	ctx context.Context,
	status string,
	limit int,
) ([]ApprovalRequest, error) {
	query := `
		SELECT id, mandate_id, action, amount, currency, merchant, items,
			cart_id, policy_version, risk_score, level, status, authorization_id, reason
		FROM approval_requests
	`
	var args []any
	if status != "" {
		query += " WHERE status = $" + strconv.Itoa(len(args)+1)
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += " LIMIT $" + strconv.Itoa(len(args)+1)
		args = append(args, limit)
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list approval requests: %w", err)
	}
	defer rows.Close()

	var out []ApprovalRequest
	for rows.Next() {
		var a ApprovalRequest
		var items []byte
		var cartID, authID *string
		if err := rows.Scan(
			&a.ID, &a.MandateID, &a.Action, &a.Amount, &a.Currency, &a.Merchant, &items,
			&cartID, &a.PolicyVersion, &a.RiskScore, &a.Level, &a.Status, &authID, &a.Reason,
		); err != nil {
			return nil, fmt.Errorf("scan approval request: %w", err)
		}
		if cartID != nil {
			a.CartID = *cartID
		}
		if authID != nil {
			a.AuthorizationID = *authID
		}
		if err := json.Unmarshal(items, &a.Items); err != nil {
			return nil, fmt.Errorf("unmarshal approval request items: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approval requests: %w", err)
	}

	return out, nil
}

// ListRuns returns the most recent agent actions as replayable runs,
// joined with their policy evaluation + issued authorization when
// present, merged with the most recent agent_plans rows (item 16 --
// see agent_plan.go's doc comment for why these are a separate source
// rather than a join target on agent_actions). Two queries, merged and
// re-sorted in Go rather than one UNION: the two sources have
// meaningfully different shapes (a plan never has a decision/
// authorization to report), and this keeps the existing, already-
// tested agent_actions query completely untouched.
func (r *PostgresRepository) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	rows, err := r.db.Query(ctx, `
		SELECT aa.id, aa.action, aa.amount, aa.currency, aa.merchant, aa.items,
			COALESCE(pe.decision, ''), COALESCE(pe.reason, ''),
			COALESCE(a.id, ''), COALESCE(a.status, ''), aa.created_at
		FROM agent_actions aa
		LEFT JOIN policy_evaluations pe ON pe.action_id = aa.id
		LEFT JOIN authorizations a ON a.id = pe.authorization_id
		ORDER BY aa.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		var run Run
		var items []byte
		if err := rows.Scan(
			&run.ID, &run.Action, &run.Amount, &run.Currency, &run.Merchant, &items,
			&run.Decision, &run.Reason,
			&run.Authorization, &run.AuthStatus, &run.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		if err := json.Unmarshal(items, &run.Items); err != nil {
			return nil, fmt.Errorf("unmarshal run items: %w", err)
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}

	// Deliberately doesn't SELECT steps here -- Run.Steps's own doc
	// comment establishes ListRuns as a flat summary for every source,
	// GetRun as the only place a timeline gets built, and that contract
	// stays uniform across both agent_actions and agent_plans even
	// though the agent_plans query has no N+1 reason to hold back.
	planRows, err := r.db.Query(ctx, `
		SELECT id, action, amount, currency, merchant, items, created_at
		FROM agent_plans
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent plans: %w", err)
	}
	defer planRows.Close()

	for planRows.Next() {
		var p AgentPlan
		var items []byte
		if err := planRows.Scan(
			&p.ID, &p.Proposal.Action, &p.Proposal.Amount, &p.Proposal.Currency, &p.Proposal.Merchant,
			&items, &p.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent plan: %w", err)
		}
		if err := json.Unmarshal(items, &p.Proposal.Items); err != nil {
			return nil, fmt.Errorf("unmarshal agent plan items: %w", err)
		}
		out = append(out, p.toRun())
	}
	if err := planRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent plans: %w", err)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

// GetRun reconstructs one run and its policy/authorization trail, plus a
// step-by-step timeline built from the same persisted rows (agent_actions,
// risk_assessments, policy_evaluations, authorizations) -- or, when runID
// is an agent_plans row (item 16, always prefixed "plan_"), delegates to
// getAgentPlanRun instead. The prefix check routes the lookup to the
// right table without a wasted query against the other one first.
func (r *PostgresRepository) GetRun(ctx context.Context, runID string) (Run, error) {
	if strings.HasPrefix(runID, "plan_") {
		return r.getAgentPlanRun(ctx, runID)
	}

	var run Run
	var items []byte
	var policyVersion string
	var evaluatedAt *time.Time
	err := r.db.QueryRow(ctx, `
		SELECT aa.id, aa.action, aa.amount, aa.currency, aa.merchant, aa.items,
			COALESCE(pe.decision, ''), COALESCE(pe.reason, ''),
			COALESCE(pe.policy_version, ''), pe.created_at,
			COALESCE(a.id, ''), COALESCE(a.status, ''), aa.created_at
		FROM agent_actions aa
		LEFT JOIN policy_evaluations pe ON pe.action_id = aa.id
		LEFT JOIN authorizations a ON a.id = pe.authorization_id
		WHERE aa.id = $1
	`, runID).Scan(
		&run.ID, &run.Action, &run.Amount, &run.Currency, &run.Merchant, &items,
		&run.Decision, &run.Reason, &policyVersion, &evaluatedAt,
		&run.Authorization, &run.AuthStatus, &run.CreatedAt,
	)
	if err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}
	if err := json.Unmarshal(items, &run.Items); err != nil {
		return Run{}, fmt.Errorf("unmarshal run items: %w", err)
	}

	run.Steps = append(run.Steps, RunStep{
		Stage:     "proposed",
		Detail:    fmt.Sprintf("%s proposed for %s %d %s: %s", run.Action, run.Merchant, run.Amount, run.Currency, strings.Join(run.Items, ", ")),
		Timestamp: run.CreatedAt,
	})

	var riskScore float64
	var riskCreatedAt time.Time
	if err := r.db.QueryRow(ctx, `
		SELECT risk_score, created_at FROM risk_assessments
		WHERE action_id = $1 ORDER BY created_at ASC LIMIT 1
	`, runID).Scan(&riskScore, &riskCreatedAt); err == nil {
		run.Steps = append(run.Steps, RunStep{
			Stage:     "risk_assessed",
			Detail:    fmt.Sprintf("risk score %.2f", riskScore),
			Timestamp: riskCreatedAt,
		})
	}

	if evaluatedAt != nil {
		detail := fmt.Sprintf("%s (policy %s)", run.Decision, policyVersion)
		if run.Reason != "" {
			detail += ": " + run.Reason
		}
		run.Steps = append(run.Steps, RunStep{
			Stage:     "policy_evaluated",
			Detail:    detail,
			Timestamp: *evaluatedAt,
		})
	}

	if run.Authorization != "" {
		var authStatus string
		var authCreatedAt, authUpdatedAt time.Time
		if err := r.db.QueryRow(ctx, `
			SELECT status, created_at, updated_at FROM authorizations WHERE id = $1
		`, run.Authorization).Scan(&authStatus, &authCreatedAt, &authUpdatedAt); err == nil {
			run.Steps = append(run.Steps, RunStep{
				Stage:     "authorized",
				Detail:    fmt.Sprintf("authorization %s issued", run.Authorization),
				Timestamp: authCreatedAt,
			})
			if authStatus == "USED" && authUpdatedAt.After(authCreatedAt) {
				run.Steps = append(run.Steps, RunStep{
					Stage:     "authorization_consumed",
					Detail:    "authorization used to create a payment",
					Timestamp: authUpdatedAt,
				})
			}
		}
	}

	return run, nil
}

// getAgentPlanRun reconstructs one agent_plans-backed run (item 16).
// Unlike GetRun's agent_actions path, there is no risk_assessments/
// policy_evaluations/authorizations trail to layer on -- an AgentPlan's
// Steps column already carries its complete reasoning trail as
// persisted, so this is a single lookup and unmarshal, not a
// multi-query timeline build.
func (r *PostgresRepository) getAgentPlanRun(ctx context.Context, runID string) (Run, error) {
	var p AgentPlan
	var items, steps []byte

	err := r.db.QueryRow(ctx, `
		SELECT id, action, amount, currency, merchant, items, steps, created_at
		FROM agent_plans
		WHERE id = $1
	`, runID).Scan(
		&p.ID, &p.Proposal.Action, &p.Proposal.Amount, &p.Proposal.Currency, &p.Proposal.Merchant,
		&items, &steps, &p.CreatedAt,
	)
	if err != nil {
		return Run{}, fmt.Errorf("get agent plan: %w", err)
	}
	if err := json.Unmarshal(items, &p.Proposal.Items); err != nil {
		return Run{}, fmt.Errorf("unmarshal agent plan items: %w", err)
	}
	if err := json.Unmarshal(steps, &p.Steps); err != nil {
		return Run{}, fmt.Errorf("unmarshal agent plan steps: %w", err)
	}

	return p.toRun(), nil
}

// SaveAgentPlan persists an agent's own reasoning trail as an
// independently-retrievable Run (item 16). See agent_plan.go and
// db/migrations/*_create_agent_plans_table.sql for the full design
// rationale. Callers (agents.Handler) treat a failure here as
// best-effort and log-only -- this must never block a checkout
// proposal from reaching the buyer.
func (r *PostgresRepository) SaveAgentPlan(ctx context.Context, p AgentPlan) error {
	items, err := json.Marshal(p.Proposal.Items)
	if err != nil {
		return fmt.Errorf("marshal agent plan items: %w", err)
	}
	steps, err := json.Marshal(p.Steps)
	if err != nil {
		return fmt.Errorf("marshal agent plan steps: %w", err)
	}

	_, err = r.db.Exec(ctx, `
		INSERT INTO agent_plans (id, action, amount, currency, merchant, items, steps)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`,
		p.ID, p.Proposal.Action, p.Proposal.Amount, p.Proposal.Currency, p.Proposal.Merchant, items, steps,
	)
	if err != nil {
		return fmt.Errorf("save agent plan: %w", err)
	}

	return nil
}
