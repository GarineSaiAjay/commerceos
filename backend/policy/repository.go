package policy

import (
	"context"
	"errors"
)

var ErrMandateNotFound = errors.New("mandate not found")
var ErrAuthorizationNotFound = errors.New("authorization not found")
var ErrAuthorizationInvalid = errors.New("authorization invalid")
var ErrApprovalRequestNotFound = errors.New("approval request not found")

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

	// UpdateApprovalRequestStatus transitions the request and records the
	// authorization ID (if issued) and a reason.
	UpdateApprovalRequestStatus(ctx context.Context, id, status, authorizationID, reason string) error
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
