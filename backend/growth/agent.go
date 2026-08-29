package growth

import (
	"context"
	"fmt"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// PolicyVersion tags the cross-sell recommendation policy logic (which
// bundle/accessory to recommend), distinct from the deterministic
// checkout/authorization policy versioned as policy.PolicyVersion -- the
// two use the same "<domain>_policy_v<N>" naming scheme on purpose.
const PolicyVersion = "cross_sell_policy_v4"

// Recommendation is a persisted, explainable cross-sell decision. JSON
// tags match the recommendations table column names (postgres_store.go)
// so the same struct serializes consistently everywhere it is returned
// (/growth/evaluate, /growth/recommend/{id}, /growth/suggest, the
// recommend_bundle MCP tool) instead of falling back to Go's default
// PascalCase field-name keys.
type Recommendation struct {
	ID                  string  `json:"id"`
	CartID              string  `json:"cart_id"`
	ProductID           string  `json:"product_id"`
	Price               int64   `json:"price"`
	PurchaseProbability float64 `json:"purchase_probability"`
	IncrementalMargin   int64   `json:"incremental_margin"`
	Confidence          float64 `json:"confidence"`
	RiskCost            int64   `json:"risk_cost"`
	ExpectedValue       float64 `json:"expected_value"`
	Decision            string  `json:"decision"`
	PolicyVersion       string  `json:"policy_version"`
	Reason              string  `json:"reason"`
	// CreatedAt, CartTotalAtEvaluation, and BudgetAtEvaluation let the
	// campaign agent (backend/campaign/) recompute, for a REJECT
	// decision, whether a given discount would have brought the cart
	// under budget -- without them "N customers would now fit" is an
	// unverifiable claim. Nullable in the DB (existing rows predate this
	// column); zero value here means "unknown", not "zero budget".
	CreatedAt             time.Time `json:"created_at,omitempty"`
	CartTotalAtEvaluation int64     `json:"cart_total_at_evaluation,omitempty"`
	BudgetAtEvaluation    int64     `json:"budget_at_evaluation,omitempty"`
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
		CartTotalAtEvaluation: cartTotal,
		BudgetAtEvaluation:    budget.Budget,
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
