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

	// GetByID, Approve, and Reject all take merchantID as a mandatory
	// scoping parameter -- added as a P0 security fix (full-codebase
	// re-audit 2026-09-04). Before this, all three took only a campaign
	// id: Handler.Get/Approve/Reject is authenticated (RequireOperator,
	// see main.go) but was never checking that the campaign belonged to
	// the CALLING operator's own merchant, so any authenticated operator
	// of ANY merchant could read, approve, or reject any other
	// merchant's campaign just by knowing or guessing its id
	// ("campaign_<productID>_<unixnano>" -- not a secret). Approving a
	// rival's PROPOSED campaign activates real ad spend against their
	// budget without their consent; rejecting kills a legitimate one.
	// This interface's own doc comment ("mirrors policy.Repository's
	// shape") and Handler's ("every handler below can assume ... scope
	// every read/write to that operator's own merchant") were simply
	// false for these three methods until this fix. A cross-merchant id
	// now behaves exactly like a nonexistent one -- see
	// PostgresRepository's implementations for why that's a deliberate
	// choice, not just a side effect.
	GetByID(ctx context.Context, merchantID, id string) (Campaign, error)

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
	Approve(ctx context.Context, merchantID, id string, approvedBy string) (Campaign, error)
	Reject(ctx context.Context, merchantID, id string, reason string) (Campaign, error)

	// FindActiveForProduct returns the ACTIVE campaign for this merchant
	// + product, or ErrCampaignNotFound if none. This is a plain read
	// (no lock) for the dashboard/handler layer; the actual checkout-time
	// budget guard is the atomic UPDATE in
	// commerce/order/postgres_repository.go, not this call.
	FindActiveForProduct(ctx context.Context, merchantID, productID string) (Campaign, error)
}
