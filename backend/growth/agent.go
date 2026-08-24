package growth

import (
	"context"
	"fmt"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// PolicyVersion tags the cross-sell policy logic.
const PolicyVersion = "cross_sell_policy_v4"

// Recommendation is a persisted, explainable cross-sell decision.
type Recommendation struct {
	ID                  string
	CartID              string
	ProductID           string
	Price               int64
	PurchaseProbability float64
	IncrementalMargin   int64
	Confidence          float64
	RiskCost            int64
	ExpectedValue       float64
	Decision            string
	PolicyVersion       string
	Reason              string
}

// CatalogReader is the catalog surface the growth agent needs.
type CatalogReader interface {
	GetProduct(ctx context.Context, id string) (catalog.Product, error)
}

// Store persists recommendations.
type Store interface {
	Save(ctx context.Context, r Recommendation) error
}

// GrowthAgent proposes cross-sell items. It computes EV deterministically
// and routes the final proposal through the Policy Engine — no shortcut.
type GrowthAgent struct {
	catalog CatalogReader
	store   Store
}

func NewGrowthAgent(catalog CatalogReader, store Store) *GrowthAgent {
	return &GrowthAgent{catalog: catalog, store: store}
}

// EvaluateCandidate runs budget check + EV scoring for one candidate.
// Returns the recommendation and whether it is eligible.
func (g *GrowthAgent) EvaluateCandidate(
	ctx context.Context,
	cartID string,
	cartTotal int64,
	budget BudgetCheck,
	productID string,
	inputs EVInputs,
) (Recommendation, error) {
	product, err := g.catalog.GetProduct(ctx, productID)
	if err != nil {
		return Recommendation{}, err
	}

	newTotal := cartTotal + product.Price.Amount
	ev := ExpectedValue(inputs)

	rec := Recommendation{
		ID:                  fmt.Sprintf("rec_%s_%s", cartID, productID),
		CartID:              cartID,
		ProductID:           productID,
		Price:               product.Price.Amount,
		PurchaseProbability: inputs.PurchaseProbability,
		IncrementalMargin:   inputs.IncrementalMargin,
		Confidence:          inputs.Confidence,
		RiskCost:            inputs.RiskCost,
		ExpectedValue:       ev,
		PolicyVersion:       PolicyVersion,
	}

	if !budget.Eligible(newTotal) {
		rec.Decision = "REJECT"
		rec.Reason = fmt.Sprintf(
			"new total ₹%d exceeds max allowed ₹%d (budget ₹%d, tolerance %d%%)",
			newTotal,
			budget.MaxAllowed(),
			budget.Budget,
			int(budget.Tolerance*100),
		)
	} else {
		rec.Decision = "RECOMMEND"
		rec.Reason = fmt.Sprintf(
			"eligible: new total ₹%d ≤ max allowed ₹%d; EV ₹%.2f",
			newTotal,
			budget.MaxAllowed(),
			ev,
		)
	}

	if g.store != nil {
		if err := g.store.Save(ctx, rec); err != nil {
			return Recommendation{}, fmt.Errorf("save recommendation: %w", err)
		}
	}

	return rec, nil
}
