package policy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/garinesaiajay/commerceos/audit"
)

// Service is the hard chokepoint between agents and the Payment Service.
type Service struct {
	engine *Engine
	risk   *RiskEngine
	repo   Repository
	now    func() time.Time
	// auditWriter is optional (nil until WithAuditWriter is called) and
	// used only by UpdatePolicyConfig today -- every other write in this
	// service already has its own durable trail (policy_evaluations,
	// approval_requests, ...), so there was nothing else here to wire it
	// to. See UpdatePolicyConfig's doc comment for why config changes
	// specifically get a general audit_events entry too.
	auditWriter audit.Writer
}

func NewService(engine *Engine, risk *RiskEngine, repo Repository) *Service {
	return &Service{
		engine: engine,
		risk:   risk,
		repo:   repo,
		now:    time.Now,
	}
}

// WithAuditWriter wires the general hash-chained audit ledger (item 25,
// P2, PLAN-05-SELLER-DASHBOARD.md §4), same fluent-setter convention as
// order.PostgresRepository.WithAuditWriter and every other WithX(...) in
// this codebase. Existing callers that never call this keep the exact
// prior behavior -- UpdatePolicyConfig degrades to "skip the audit
// write" rather than failing the config update itself.
func (s *Service) WithAuditWriter(w audit.Writer) *Service {
	s.auditWriter = w
	return s
}

func (s *Service) CreateMandate(ctx context.Context, mandate Mandate) error {
	return s.repo.SaveMandate(ctx, mandate)
}

// GetPolicyConfig returns the engine's current LIVE configuration --
// not a fresh DB read. These are always identical while this process
// is the only writer (true today: UpdatePolicyConfig is the only path
// that ever changes either), and reading the in-memory copy is both
// cheaper and, unlike a DB round trip, can never itself disagree with
// what Evaluate is actually enforcing right now.
func (s *Service) GetPolicyConfig() PolicyConfig {
	return s.engine.Config()
}

// UpdatePolicyConfig persists cfg, then applies it to the live engine,
// then (best-effort) records the change on the general audit ledger.
// AllowedProducts on cfg is ignored either way -- see
// Engine.UpdateConfig's doc comment -- so before/after in the audit
// event always reflects what the engine is actually enforcing, not
// whatever the caller happened to leave that field as.
//
// Ordering matters: SaveConfig (durable) happens BEFORE UpdateConfig
// (in-memory). If the DB write fails, the live engine is left
// untouched and the caller gets an error -- never a config change that
// took effect immediately but silently failed to survive a restart.
// The audit write happens last and is deliberately best-effort (like
// SaveAgentPlan and the campaign_budget_exhausted write elsewhere in
// this codebase): a missing audit_events row for a settings change is
// bad, but rolling back an already-durable, already-live policy change
// because the audit ledger had a transient problem would be worse.
func (s *Service) UpdatePolicyConfig(ctx context.Context, cfg PolicyConfig, updatedBy string) (PolicyConfig, error) {
	before := s.engine.Config()

	if err := s.repo.SaveConfig(ctx, cfg, updatedBy); err != nil {
		return PolicyConfig{}, fmt.Errorf("save policy config: %w", err)
	}
	s.engine.UpdateConfig(cfg)
	after := s.engine.Config() // re-read: reflects AllowedProducts as UpdateConfig actually preserved it, not whatever cfg had

	if s.auditWriter != nil {
		detail := map[string]any{
			"before": map[string]any{
				"ceiling":            before.Ceiling,
				"budget_tolerance":   before.BudgetTolerance,
				"allowed_currencies": before.AllowedCurrencies,
				"allowed_merchants":  before.AllowedMerchants,
			},
			"after": map[string]any{
				"ceiling":            after.Ceiling,
				"budget_tolerance":   after.BudgetTolerance,
				"allowed_currencies": after.AllowedCurrencies,
				"allowed_merchants":  after.AllowedMerchants,
			},
		}
		if err := s.auditWriter.Write(ctx, updatedBy, "policy_settings_updated", "policy_config", policySettingsMerchantID, detail); err != nil {
			fmt.Printf("[policy] audit write failed for policy_settings_updated (updated_by %s): %v\n", updatedBy, err)
		}
	}

	return after, nil
}

// Propose validates and evaluates a proposed action against a mandate.
// It persists the action, its risk assessment, and the evaluation
// (approved or rejected), then issues an authorization when approved.
func (s *Service) Propose(
	ctx context.Context,
	action ProposedAction,
	mandateID string,
) (Decision, error) {
	if err := ValidateProposal(action); err != nil {
		return Decision{}, err
	}

	mandate, err := s.repo.GetMandate(ctx, mandateID)
	if err != nil {
		return Decision{}, err
	}

	riskScore := s.risk.Score(action.Amount, action.Merchant, mandate.MaximumAmount)

	decision := s.engine.Evaluate(ctx, action, mandate, riskScore)
	decision.RiskScore = riskScore

	// Persist the action, then the risk assessment and evaluation
	// (approved or rejected).
	actionID := fmt.Sprintf("action_%d", s.now().UnixNano())
	if err := s.repo.SaveAction(ctx, action, actionID); err != nil {
		return Decision{}, err
	}
	decision.ActionID = actionID

	assessment := RiskAssessment{
		ID:        fmt.Sprintf("risk_%d", s.now().UnixNano()),
		ActionID:  actionID,
		RiskScore: riskScore,
		Factors: map[string]any{
			"amount":   action.Amount,
			"merchant": action.Merchant,
		},
	}
	if err := s.repo.SaveRiskAssessment(ctx, assessment); err != nil {
		return Decision{}, err
	}

	eval := Evaluation{
		ID:            fmt.Sprintf("eval_%d", s.now().UnixNano()),
		ActionID:      actionID,
		PolicyVersion: decision.PolicyVersion,
		Decision:      decision.Decision,
		Reason:        decision.Reason,
		RiskScore:     riskScore,
		Level:         decision.Level,
	}
	if err := s.repo.SaveEvaluation(ctx, eval); err != nil {
		return Decision{}, err
	}

	// Persist the agent-visible decision row (spec Phase 3 artifact list).
	if err := s.repo.SaveAgentDecision(ctx, AgentDecision{
		ID:       fmt.Sprintf("agentdecision_%d", s.now().UnixNano()),
		ActionID: actionID,
		Decision: decision.Decision,
		Reason:   decision.Reason,
	}); err != nil {
		return Decision{}, err
	}

	if decision.Decision == DecisionApproved {
		// Level 2/3 proposals require durable human approval before an
		// authorization is issued. Create/return the pending approval
		// request and do NOT issue an authorization yet.
		if decision.Level >= 2 {
			return s.requireApproval(ctx, decision, action, mandateID, riskScore)
		}

		// Reuse an existing active authorization for this exact action
		// rather than minting a duplicate (idempotent proposal).
		existing, err := s.repo.GetActiveAuthorization(ctx, action)
		if err == nil {
			decision.AuthorizationID = existing.ID
			decision.ExpiresAt = existing.ExpiresAt
			decision.Level = existing.Level
			return decision, nil
		}
		if err != ErrAuthorizationNotFound {
			return Decision{}, err
		}

		auth := Authorization{
			ID:            fmt.Sprintf("auth_%d", s.now().UnixNano()),
			MandateID:     mandateID,
			Action:        action.Action,
			Amount:        action.Amount,
			Currency:      action.Currency,
			Merchant:      action.Merchant,
			Items:         action.Items,
			PolicyVersion: decision.PolicyVersion,
			Decision:      decision.Decision,
			RiskScore:     riskScore,
			Level:         decision.Level,
			ExpiresAt:     s.now().Add(10 * time.Minute).Format("2006-01-02T15:04:05Z"),
			Status:        "ACTIVE",
		}
		if err := s.repo.SaveAuthorization(ctx, auth); err != nil {
			return Decision{}, err
		}
		decision.AuthorizationID = auth.ID
		decision.ExpiresAt = auth.ExpiresAt
	}

	return decision, nil
}

// MarkAuthorizationUsed marks an authorization as consumed. It is called
// by the Payment Service after a new payment is created, so a single
// authorization cannot be replayed to create a second payment. This makes
// policy.Service satisfy payment.AuthorizationConsumer.
func (s *Service) MarkAuthorizationUsed(ctx context.Context, id string) error {
	return s.repo.MarkAuthorizationUsed(ctx, id)
}

// VerifyAuthorization is called by the Payment Service before any money
// movement. It returns the authorization if valid, or an error.
func (s *Service) VerifyAuthorization(
	ctx context.Context,
	authorizationID string,
) (Authorization, error) {
	if authorizationID == "" {
		return Authorization{}, fmt.Errorf("authorization_id is required")
	}

	auth, err := s.repo.GetAuthorization(ctx, authorizationID)
	if err != nil {
		return Authorization{}, err
	}

	if auth.Status != "ACTIVE" {
		return Authorization{}, fmt.Errorf("%v: status is %s", ErrAuthorizationInvalid, auth.Status)
	}

	expires, err := time.Parse(time.RFC3339, auth.ExpiresAt)
	if err != nil {
		return Authorization{}, fmt.Errorf("%v: unparseable expiry", ErrAuthorizationInvalid)
	}
	if s.now().After(expires) {
		return Authorization{}, fmt.Errorf("%v: expired", ErrAuthorizationInvalid)
	}

	return auth, nil
}

// requireApproval creates (or returns an existing) PENDING human-approval
// request for a Level 2/3 proposal. No authorization is issued here.
func (s *Service) requireApproval(
	ctx context.Context,
	decision Decision,
	action ProposedAction,
	mandateID string,
	riskScore float64,
) (Decision, error) {
	// Reuse an existing PENDING request for the same action (idempotent).
	existing, err := s.repo.GetPendingApprovalForAction(ctx, action)
	if err == nil {
		decision.Decision = DecisionPendingApproval
		decision.ApprovalRequestID = existing.ID
		decision.Level = existing.Level
		decision.RiskScore = existing.RiskScore
		return decision, nil
	}
	if err != ErrApprovalRequestNotFound {
		return Decision{}, err
	}

	req := ApprovalRequest{
		ID:            fmt.Sprintf("apr_%d", s.now().UnixNano()),
		MandateID:     mandateID,
		Action:        action.Action,
		Amount:        action.Amount,
		Currency:      action.Currency,
		Merchant:      action.Merchant,
		Items:         action.Items,
		CartID:        action.CartID,
		PolicyVersion: decision.PolicyVersion,
		RiskScore:     riskScore,
		Level:         decision.Level,
		Status:        "PENDING",
	}
	if err := s.repo.SaveApprovalRequest(ctx, req); err != nil {
		return Decision{}, err
	}

	decision.Decision = DecisionPendingApproval
	decision.ApprovalRequestID = req.ID
	return decision, nil
}

// GetApprovalRequest fetches a durable approval request for the UI.
func (s *Service) GetApprovalRequest(ctx context.Context, id string) (ApprovalRequest, error) {
	return s.repo.GetApprovalRequest(ctx, id)
}

// ListApprovalRequests returns approval requests, newest first, optionally
// filtered by status (e.g. PENDING for the approvals queue).
func (s *Service) ListApprovalRequests(ctx context.Context, status string, limit int) ([]ApprovalRequest, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.ListApprovalRequests(ctx, status, limit)
}

// ListRuns returns the most recent agent actions as replayable runs.
func (s *Service) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListRuns(ctx, limit)
}

// GetRun reconstructs one run and its policy/authorization trail.
func (s *Service) GetRun(ctx context.Context, runID string) (Run, error) {
	return s.repo.GetRun(ctx, runID)
}

// SaveAgentPlan persists an agent's own reasoning trail as an
// independently-retrievable Run (item 16, agent_plan.go). Unlike
// Propose (which mints its own "action_<unixnano>" ID internally), id
// here is minted by the caller -- agents.Handler mints
// "plan_<unixnano>" before calling this, since Handler is what needs
// to know the ID isn't going to collide with an agent_actions row
// (the "plan_" vs "action_" prefix is what GetRun routes on).
func (s *Service) SaveAgentPlan(ctx context.Context, id string, action ProposedAction, steps []RunStep) error {
	return s.repo.SaveAgentPlan(ctx, AgentPlan{
		ID:        id,
		Proposal:  action,
		Steps:     steps,
		CreatedAt: s.now(),
	})
}

// resolveApprover decides who is allowed to approve or reject req, and
// what identity to record. There are exactly two legitimate callers:
// the buyer who created this approval request (proven by knowing its
// cart_id -- only the browser that ran the cart through mandate/propose
// has that) and a logged-in merchant operator (proven by operatorEmail,
// which the HTTP layer only sets after validating a session -- see
// backend/auth.Service.OptionalOperator). Anyone else is rejected. See
// files/AUTH.md: this replaces an approver/by field
// the client could set to any string at all, with zero verification.
func resolveApprover(req ApprovalRequest, cartID, operatorEmail string) (string, error) {
	if operatorEmail != "" {
		return "operator:" + operatorEmail, nil
	}
	if cartID != "" && cartID == req.CartID {
		return "buyer (cart " + cartID + " verified)", nil
	}
	return "", ErrApprovalUnauthorized
}

// Approve issues the one-time authorization for a PENDING approval request
// after re-validating that its binding (amount/items/cart/merchant/version)
// has not drifted. It is idempotent: approving again returns the same
// authorization. The verified approver identity is recorded in the reason
// -- see resolveApprover.
func (s *Service) Approve(ctx context.Context, approvalRequestID, cartID, operatorEmail string) (Decision, error) {
	req, err := s.repo.GetApprovalRequest(ctx, approvalRequestID)
	if err != nil {
		return Decision{}, err
	}

	approver, err := resolveApprover(req, cartID, operatorEmail)
	if err != nil {
		return Decision{}, err
	}

	if req.Status == "APPROVED" {
		// Idempotent: already approved returns the issued authorization.
		if req.AuthorizationID != "" {
			if auth, err := s.repo.GetAuthorization(ctx, req.AuthorizationID); err == nil {
				return Decision{Decision: DecisionApproved, AuthorizationID: auth.ID, ExpiresAt: auth.ExpiresAt, Level: auth.Level}, nil
			}
		}
		return Decision{Decision: DecisionApproved, ApprovalRequestID: req.ID, Reason: "already approved"}, nil
	}

	if req.Status == "REJECTED" || req.Status == "REVOKED" {
		return Decision{}, fmt.Errorf("approval request %s is %s", req.ID, req.Status)
	}
	if req.Status == "EXPIRED" {
		return Decision{}, fmt.Errorf("approval request %s has expired", req.ID)
	}

	// Recompute policy against the CURRENT state — never trust the browser.
	mandate, err := s.repo.GetMandate(ctx, req.MandateID)
	if err != nil {
		return Decision{}, err
	}
	action := ProposedAction{
		Action: req.Action, Amount: req.Amount, Currency: req.Currency,
		Merchant: req.Merchant, Items: req.Items, CartID: req.CartID,
	}
	decision := s.engine.Evaluate(ctx, action, mandate, req.RiskScore)
	if decision.Decision != DecisionApproved {
		return Decision{}, fmt.Errorf("policy re-evaluation failed at approval time: %s (%s)", decision.FailedCheck, decision.Reason)
	}
	if decision.Level != req.Level {
		return Decision{}, fmt.Errorf("approval level changed (%d -> %d); re-propose required", req.Level, decision.Level)
	}

	// Issue the one-time authorization.
	auth := Authorization{
		ID:            fmt.Sprintf("auth_%d", s.now().UnixNano()),
		MandateID:     req.MandateID,
		Action:        req.Action,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Merchant:      req.Merchant,
		Items:         req.Items,
		PolicyVersion: req.PolicyVersion,
		Decision:      DecisionApproved,
		RiskScore:     req.RiskScore,
		Level:         req.Level,
		ExpiresAt:     s.now().Add(10 * time.Minute).Format("2006-01-02T15:04:05Z"),
		Status:        "ACTIVE",
	}
	if err := s.repo.SaveAuthorization(ctx, auth); err != nil {
		return Decision{}, err
	}

	reason := "approved by " + approver
	if err := s.repo.UpdateApprovalRequestStatus(ctx, req.ID, "APPROVED", auth.ID, reason); err != nil {
		return Decision{}, err
	}

	return Decision{
		Decision: DecisionApproved, ApprovalRequestID: req.ID,
		AuthorizationID: auth.ID, ExpiresAt: auth.ExpiresAt, Level: auth.Level,
	}, nil
}

// Reject marks a PENDING approval request as rejected. See resolveApprover
// for who is allowed to do this.
func (s *Service) Reject(ctx context.Context, approvalRequestID, cartID, operatorEmail, reason string) error {
	req, err := s.repo.GetApprovalRequest(ctx, approvalRequestID)
	if err != nil {
		return err
	}
	by, err := resolveApprover(req, cartID, operatorEmail)
	if err != nil {
		return err
	}
	if req.Status != "PENDING" {
		return fmt.Errorf("approval request %s is %s (cannot reject)", req.ID, req.Status)
	}
	return s.repo.UpdateApprovalRequestStatus(ctx, req.ID, "REJECTED", "", "rejected by "+by+": "+reason)
}

var _ = errors.New
