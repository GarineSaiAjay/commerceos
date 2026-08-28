package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/garinesaiajay/commerceos/policy"
)

// BuyerAgent assembles checkout proposals. It NEVER calls the Payment
// Service — it only produces a policy.ProposedAction and hands it to
// the Policy Engine. It has no financial authority.
type BuyerAgent struct {
	extractor IntentExtractor
	searcher  *Searcher
}

func NewBuyerAgent(extractor IntentExtractor, searcher *Searcher) *BuyerAgent {
	return &BuyerAgent{
		extractor: extractor,
		searcher:  searcher,
	}
}

// CheckoutPlan is the agent's proposal plus the reasoning.
type CheckoutPlan struct {
	Intent     Intent                `json:"intent"`
	Proposal   policy.ProposedAction `json:"proposal"`
	SelectedID string                `json:"selected_product_id"`
	Reasoning  string                `json:"reasoning"`
}

// ErrNoSuitableProduct is returned when nothing matches the intent.
var ErrNoSuitableProduct = errors.New("no suitable product for intent")

// PlanCheckout turns a natural-language prompt into a Proposed Action.
// The agent names a product_id; it never writes price/quantity itself.
func (a *BuyerAgent) PlanCheckout(
	ctx context.Context,
	prompt string,
	merchant string,
) (CheckoutPlan, error) {
	intent, err := a.extractor.Extract(ctx, prompt)
	if err != nil {
		return CheckoutPlan{}, err
	}

	// Ambiguous intent → safe no-op (clarification), never a guess.
	if intent.Clarify != "" {
		return CheckoutPlan{}, ErrAmbiguousIntent
	}

	results, err := a.searcher.Search(ctx, intent)
	if err != nil {
		return CheckoutPlan{}, err
	}

	if len(results) == 0 {
		return CheckoutPlan{}, ErrNoSuitableProduct
	}

	// The agent selects the top-ranked product by name only.
	top := results[0].Product

	proposal := policy.ProposedAction{
		Action:   "CREATE_ORDER",
		Amount:   top.Price.Amount,
		Currency: top.Price.Currency,
		Merchant: merchant,
		Items:    []string{top.ID},
	}

	// intent.Priority is optional (only budget + category are required --
	// see ValidateIntent), so the sentence needs two honest shapes: most
	// requests never name a specific priority, and "matching priority
	// within budget ₹X" (empty %s) read as a broken sentence rather than
	// explaining anything.
	var reasoning string
	if intent.Priority != "" {
		reasoning = fmt.Sprintf(
			"Selected %s (₹%d) — best match for your %s priority in %s, within budget ₹%d.",
			top.Title,
			top.Price.Amount/100, // paise → rupees for display
			intent.Priority,
			intent.Category,
			intent.Budget, // already rupees
		)
	} else {
		reasoning = fmt.Sprintf(
			"Selected %s (₹%d) — best-priced match in %s within your ₹%d budget.",
			top.Title,
			top.Price.Amount/100,
			intent.Category,
			intent.Budget,
		)
	}

	return CheckoutPlan{
		Intent:     intent,
		Proposal:   proposal,
		SelectedID: top.ID,
		Reasoning:  reasoning,
	}, nil
}
