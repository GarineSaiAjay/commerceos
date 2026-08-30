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
	Intent       Intent                `json:"intent"`
	Proposal     policy.ProposedAction `json:"proposal"`
	SelectedID   string                `json:"selected_product_id"`
	Reasoning    string                `json:"reasoning"`
	// Alternatives are the next-best matches the searcher also found for
	// this intent (Searcher.Search already ranks every match; previously
	// everything past results[0] was silently discarded the moment it was
	// computed). Never re-scored or re-ranked here -- same order
	// Search returned them in. Omitted, not an empty array, when there
	// were no other matches at all.
	Alternatives []AlternativeProduct `json:"alternatives,omitempty"`
}

// AlternativeProduct carries just enough catalog detail to render an
// alternative choice without a second round trip -- same shape as
// growth.SuggestedProduct for the same reason.
type AlternativeProduct struct {
	ProductID string `json:"product_id"`
	Title     string `json:"title"`
	Price     int64  `json:"price"`
	Currency  string `json:"currency"`
}

// maxAlternatives bounds how many next-best matches ride along with a
// plan. 2 is deliberately small: this is "here are a couple of other
// options," not a second catalog browse inside the agent panel.
const maxAlternatives = 2

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

	var alternatives []AlternativeProduct
	for _, r := range results[1:] {
		if len(alternatives) >= maxAlternatives {
			break
		}
		alternatives = append(alternatives, AlternativeProduct{
			ProductID: r.Product.ID,
			Title:     r.Product.Title,
			Price:     r.Product.Price.Amount,
			Currency:  r.Product.Price.Currency,
		})
	}

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
		Intent:       intent,
		Proposal:     proposal,
		SelectedID:   top.ID,
		Reasoning:    reasoning,
		Alternatives: alternatives,
	}, nil
}
