package campaign

import (
	"context"
	"fmt"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/growth"
)

// timeNow is a package-level var (not a bare time.Now() call) so tests
// can substitute a fixed clock -- same pattern policy.Engine uses for
// its own `now func() time.Time` field.
var timeNow = time.Now

// CatalogReader is the catalog surface the campaign agent needs,
// mirroring growth.CatalogReader.
type CatalogReader interface {
	GetProduct(ctx context.Context, id string) (catalog.Product, error)
}

// DemandSource is the growth-side read the agent needs -- implemented by
// growth.PostgresStore.RejectedDemandByProduct. campaign imports growth
// (not the reverse), so this can name growth.RejectedDemand directly.
type DemandSource interface {
	RejectedDemandByProduct(ctx context.Context, merchantID string, windowDays int) ([]growth.RejectedDemand, error)
}

// CampaignAgent proposes discount campaigns from observed rejected
// cross-sell demand. It never calls an LLM and never chooses its own
// discount percent or duration -- those are operator-supplied inputs
// (see ProposeFromRejectedDemand's parameters), keeping the whole
// pipeline deterministic and testable, matching policy.Engine's "never
// delegates to the LLM" posture.
type CampaignAgent struct {
	catalog CatalogReader
	demand  DemandSource
	repo    Repository
	engine  *Engine
}

func NewCampaignAgent(catalog CatalogReader, demand DemandSource, repo Repository, engine *Engine) *CampaignAgent {
	return &CampaignAgent{catalog: catalog, demand: demand, repo: repo, engine: engine}
}

// ProposeFromRejectedDemand scans REJECT recommendations over the last
// windowDays for merchantID, and proposes ONE campaign for the product
// with the most rejected demand (deterministic argmax by RejectCount).
// The campaign is persisted either way -- PROPOSED if it clears the
// policy engine, REJECTED (with FailedCheck's reason recorded) if it
// doesn't -- so even a rejected proposal is an auditable, explainable
// record, the same posture policy.Engine takes for a rejected checkout.
func (a *CampaignAgent) ProposeFromRejectedDemand(
	ctx context.Context,
	merchantID string,
	windowDays int,
	discountPercent int,
	durationDays int,
) (Campaign, Decision, error) {
	demand, err := a.demand.RejectedDemandByProduct(ctx, merchantID, windowDays)
	if err != nil {
		return Campaign{}, Decision{}, fmt.Errorf("query rejected demand: %w", err)
	}
	if len(demand) == 0 {
		return Campaign{}, Decision{}, ErrNoRejectedDemand
	}

	best := demand[0]
	for _, d := range demand[1:] {
		if d.RejectCount > best.RejectCount {
			best = d
		}
	}

	// Verify the target product actually exists in the catalog -- the
	// engine's own product check is a static allowlist (engine.go), this
	// is the "does it still exist at all" check only the agent, with a
	// CatalogReader, can make.
	product, err := a.catalog.GetProduct(ctx, best.ProductID)
	if err != nil {
		return Campaign{}, Decision{}, fmt.Errorf("target product %s not found: %w", best.ProductID, err)
	}

	reinstatable := 0
	if best.ReinstatableAtDiscount != nil {
		reinstatable = best.ReinstatableAtDiscount(discountPercent)
	}

	// Size the spend cap to OBSERVED volume: worst case, every rejected
	// customer redeems once at this discount on this product's price.
	// Bounded to actual observed demand, not an arbitrary round number.
	discountPerRedemption := product.Price.Amount * int64(discountPercent) / 100
	budgetCap := discountPerRedemption * int64(best.RejectCount)

	reasoning := fmt.Sprintf(
		"%d customers were offered %s and rejected on budget in the last %d days (avg price ₹%d). "+
			"%d of those rejections have known cart/budget context; %d would clear their ceiling at %d%% off. "+
			"Capped spend ₹%d assumes all %d redeem once at %d%% off.",
		best.RejectCount, best.ProductID, windowDays, best.AvgPrice/100,
		best.KnownBudgetCount, reinstatable, discountPercent,
		budgetCap/100, best.RejectCount, discountPercent,
	)

	c := Campaign{
		ID:                  fmt.Sprintf("campaign_%s_%d", best.ProductID, timeNow().UnixNano()),
		MerchantID:          merchantID,
		ProductID:           best.ProductID,
		DiscountPercent:     discountPercent,
		BudgetCap:           budgetCap,
		DurationDays:        durationDays,
		Status:              StatusProposed,
		PolicyVersion:       PolicyVersion,
		RejectedDemandCount: best.RejectCount,
		Reasoning:           reasoning,
	}

	activeBudget, err := a.repo.SumActiveBudget(ctx, merchantID)
	if err != nil {
		return Campaign{}, Decision{}, fmt.Errorf("sum active budget: %w", err)
	}

	decision := a.engine.Evaluate(ctx, c, activeBudget)
	if decision.Decision == DecisionRejected {
		c.Status = StatusRejected
		c.RejectedReason = decision.Reason
	}

	if err := a.repo.Save(ctx, c); err != nil {
		return Campaign{}, Decision{}, fmt.Errorf("save campaign proposal: %w", err)
	}

	return c, decision, nil
}
