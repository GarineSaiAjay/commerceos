package campaign

import (
	"context"
	"errors"
)

var (
	ErrCampaignNotFound    = errors.New("campaign not found")
	ErrCampaignNotProposed = errors.New("campaign is not in PROPOSED status")
	ErrCampaignNotApproved = errors.New("campaign is not in APPROVED status")
	ErrNoRejectedDemand    = errors.New("no rejected demand observed in the requested window")
)

// Repository persists campaigns and answers the queries the policy
// engine and checkout hook need. Mirrors policy.Repository's shape.
type Repository interface {
	Save(ctx context.Context, c Campaign) error
	GetByID(ctx context.Context, id string) (Campaign, error)

	// List returns campaigns for a merchant, optionally filtered by
	// status (empty = all), newest first.
	List(ctx context.Context, merchantID string, status string) ([]Campaign, error)

	// SumActiveBudget returns the sum of budget_cap across every ACTIVE
	// campaign for this merchant -- the input to
	// Engine.checkMerchantBudgetCeiling.
	SumActiveBudget(ctx context.Context, merchantID string) (int64, error)

	// Approve/Reject transition a PROPOSED campaign. Approve also
	// activates it immediately (PROPOSED -> APPROVED -> ACTIVE in one
	// call, setting starts_at/ends_at from duration_days) -- this
	// project has no separate "approved but not yet started" scheduling
	// concept, so keeping it as two DB writes behind one repository call
	// avoids a caller forgetting the second step.
	Approve(ctx context.Context, id string, approvedBy string) (Campaign, error)
	Reject(ctx context.Context, id string, reason string) (Campaign, error)

	// FindActiveForProduct returns the ACTIVE campaign for this merchant
	// + product, or ErrCampaignNotFound if none. This is a plain read
	// (no lock) for the dashboard/handler layer; the actual checkout-time
	// budget guard is the atomic UPDATE in
	// commerce/order/postgres_repository.go, not this call.
	FindActiveForProduct(ctx context.Context, merchantID, productID string) (Campaign, error)
}
