package policy

import (
	"context"
	"errors"
	"time"
)

var ErrMandateNotFound = errors.New("mandate not found")
var ErrAuthorizationNotFound = errors.New("authorization not found")
var ErrAuthorizationInvalid = errors.New("authorization invalid")
var ErrApprovalRequestNotFound = errors.New("approval request not found")

// ErrApprovalUnauthorized is returned when neither of the two legitimate
// callers can be verified: the buyer (proven by supplying the cart_id
// this request was created for) or a logged-in merchant operator
// (proven by a valid session, resolved by the HTTP layer -- see
// backend/auth). See files/AUTH.md.
var ErrApprovalUnauthorized = errors.New("not authorized to act on this approval request")

// ErrPolicyConfigNotFound means no policy_settings row exists yet for
// this merchant -- expected on a fresh database before db/seeds/
// 004_policy_settings.sql (or an operator's first save) has run.
// Service.startup handling (backend/cmd/server/main.go) falls back to
// policy.DefaultConfig() when it sees this, so the server still starts
// with correct, working defaults rather than a zero-value PolicyConfig
// that would reject every purchase (Ceiling 0) or every currency
// (AllowedCurrencies nil).
var ErrPolicyConfigNotFound = errors.New("policy config not found")

// Repository persists policy entities.
type Repository interface {
	GetMandate(ctx context.Context, id string) (Mandate, error)

	// SaveMandate persists a mandate before it can be evaluated.
	SaveMandate(ctx context.Context, m Mandate) error

	// GetAuthorization returns the authorization and its validity.
	GetAuthorization(ctx context.Context, id string) (Authorization, error)

	// SaveAuthorization persists an issued authorization.
	SaveAuthorization(ctx context.Context, a Authorization) error

	// MarkAuthorizationUsed marks an authorization as USED.
	MarkAuthorizationUsed(ctx context.Context, id string) error

	// SaveEvaluation persists a policy decision.
	SaveEvaluation(ctx context.Context, e Evaluation) error

	// SaveAgentDecision persists the agent-visible decision row.
	SaveAgentDecision(ctx context.Context, d AgentDecision) error

	// SaveAction persists the proposed action row.
	SaveAction(ctx context.Context, a ProposedAction, actionID string) error

	// ActiveAuthorizationExists reports whether a still-ACTIVE
	// authorization exists for this exact proposal (the no-duplicate
	// guard). Rejected proposals never create authorizations, so a
	// rejected-then-retried checkout is NOT blocked.
	ActiveAuthorizationExists(ctx context.Context, a ProposedAction) (bool, error)

	// GetActiveAuthorization returns the existing ACTIVE authorization
	// for this exact proposal, or ErrAuthorizationNotFound if none.
	GetActiveAuthorization(ctx context.Context, a ProposedAction) (Authorization, error)

	// SaveRiskAssessment persists the risk score for a proposed action.
	SaveRiskAssessment(ctx context.Context, assessment RiskAssessment) error

	// SaveApprovalRequest persists a durable human-approval request.
	SaveApprovalRequest(ctx context.Context, a ApprovalRequest) error

	// GetApprovalRequest fetches an approval request by ID.
	GetApprovalRequest(ctx context.Context, id string) (ApprovalRequest, error)

	// GetPendingApprovalForAction returns a PENDING approval request for an
	// identical action, or ErrApprovalRequestNotFound if none.
	GetPendingApprovalForAction(ctx context.Context, a ProposedAction) (ApprovalRequest, error)

	// ListApprovalRequests returns approval requests, newest first,
	// optionally filtered by status (empty = all).
	ListApprovalRequests(ctx context.Context, status string, limit int) ([]ApprovalRequest, error)

	// ListRuns returns the most recent agent actions as replayable runs.
	ListRuns(ctx context.Context, limit int) ([]Run, error)

	// GetRun reconstructs one run and its policy/authorization trail.
	GetRun(ctx context.Context, runID string) (Run, error)

	// SaveAgentPlan persists an agent's own reasoning trail (item 16)
	// as an independently-retrievable Run -- see agent_plan.go's doc
	// comment for why this is deliberately not a column or join target
	// on agent_actions/SaveAction. Called best-effort by agents.Handler;
	// a failure here must never block a checkout proposal from
	// reaching the buyer.
	SaveAgentPlan(ctx context.Context, p AgentPlan) error

	// LatestPlanIDForCart returns the most recently created agent_plans
	// row for cartID with created_at strictly before `before`
	// (found=false, no error, if none exists) -- the correlation
	// Service.Propose uses to connect a checkout's resulting
	// agent_actions row back to whichever agent reasoning trail (if
	// any) led the buyer to this cart, so GetRun can present both as
	// one continuous Run regardless of which ID a caller looks up. See
	// db/migrations/*_link_agent_plans_to_actions.sql for why this can
	// only be discovered at Propose time, not fabricated at plan-save
	// time.
	LatestPlanIDForCart(ctx context.Context, cartID string, before time.Time) (planID string, found bool, err error)

	// SetActionPlanID tags an already-persisted agent_actions row with
	// the agent_plans row (if any) that led to it -- best-effort,
	// called right after SaveAction by Service.Propose once
	// LatestPlanIDForCart has found a match.
	SetActionPlanID(ctx context.Context, actionID, planID string) error

	// UpdateApprovalRequestStatus transitions the request and records the
	// authorization ID (if issued) and a reason.
	UpdateApprovalRequestStatus(ctx context.Context, id, status, authorizationID, reason string) error

	// GetConfig returns the persisted policy configuration (item 25, P2,
	// PLAN-05-SELLER-DASHBOARD.md §4), or ErrPolicyConfigNotFound if no
	// row has ever been saved. AllowedProducts is never persisted here
	// (see Engine.UpdateConfig's doc comment) -- callers that need it
	// use policy.DefaultConfig().AllowedProducts instead.
	GetConfig(ctx context.Context) (PolicyConfig, error)

	// SaveConfig persists cfg as the live policy configuration,
	// attributing the change to updatedBy (the operator's email) for
	// the settings row's own updated_by/updated_at columns -- separate
	// from, and in addition to, the audit.Writer event
	// Service.UpdatePolicyConfig also writes. AllowedProducts is not
	// a column here and is silently ignored on cfg -- see
	// Engine.UpdateConfig's doc comment for why.
	SaveConfig(ctx context.Context, cfg PolicyConfig, updatedBy string) error
}

// RiskAssessment is a persisted risk score for a proposed action.
type RiskAssessment struct {
	ID        string
	ActionID  string
	RiskScore float64
	Factors   map[string]any
}

// ApprovalRequest is a durable human-approval request for a Level 2/3
// proposal. A one-time authorization is issued only on approval.
type ApprovalRequest struct {
	ID              string   `json:"approval_request_id"`
	MandateID       string   `json:"mandate_id"`
	Action          string   `json:"action"`
	Amount          int64    `json:"amount"`
	Currency        string   `json:"currency"`
	Merchant        string   `json:"merchant"`
	Items           []string `json:"items"`
	CartID          string   `json:"cart_id"`
	PolicyVersion   string   `json:"policy_version"`
	RiskScore       float64  `json:"risk_score"`
	Level           int      `json:"level"`
	Status          string   `json:"status"` // PENDING | APPROVED | REJECTED | EXPIRED | REVOKED
	AuthorizationID string   `json:"authorization_id"`
	Reason          string   `json:"reason"`
	// ActionID carries the original Propose call's agent_actions.id
	// through the human-approval detour -- a Level 2/3 proposal has no
	// Authorization yet (see Authorization.ActionID's doc comment) at
	// the moment this request is created, so the run identity has to
	// be captured here instead and copied onto the Authorization once
	// Service.Approve issues it.
	ActionID string `json:"action_id,omitempty"`
}
type AgentDecision struct {
	ID       string
	ActionID string
	Decision string
	Reason   string
}

// Authorization is an issued authorization bound to a mandate.
type Authorization struct {
	ID            string
	MandateID     string
	Action        string
	Amount        int64
	Currency      string
	Merchant      string
	Items         []string
	PolicyVersion string
	Decision      string
	RiskScore     float64
	Level         int
	ExpiresAt     string
	Status        string
	// ActionID is the agent_actions.id (the run_id GET /runs/{id}
	// takes) whose proposal ultimately produced this authorization --
	// set once at Propose/Approve time, never changed afterward. Added
	// so payment.Service can tag an order with the run that authorized
	// it (PLAN-05-SELLER-DASHBOARD.md §2's "Orders -> Runs audit-trail
	// link") the moment the authorization is verified and consumed,
	// without payment needing any policy-internal knowledge beyond
	// this one field.
	ActionID string
	// CartID binds this authorization to the specific cart (and, via
	// order.Order.CartID, the specific order) it was proposed for --
	// copied from ProposedAction.CartID/ApprovalRequest.CartID at
	// Propose/Approve time, never changed afterward. Added as a P0
	// security fix (full-codebase re-audit 2026-09-04, see
	// db/migrations/20260904090100_add_authorizations_cart_id.sql):
	// without this, VerifyAuthorization only proved an authorization
	// was ACTIVE and unexpired, never that it was actually issued for
	// the order being paid, so a cheap Level-1 authorization could be
	// spent against an arbitrarily expensive order. Empty for a
	// cart-less ("generalized") proposal -- see
	// Engine.checkMandateCartBound's doc comment -- which
	// payment.Service.CreatePaymentOrder now refuses to accept for
	// paying an order at all (an order always has a real cart_id, so
	// an authorization with none can never legitimately match it).
	CartID string
}

// Evaluation is a persisted policy decision.
type Evaluation struct {
	ID              string
	ActionID        string
	PolicyVersion   string
	Decision        string
	Reason          string
	AuthorizationID string
	RiskScore       float64
	Level           int
}
