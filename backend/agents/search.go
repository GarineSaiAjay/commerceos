package agents

import (
	"context"
	"sort"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// CatalogReader is the subset of the catalog the agent needs.
type CatalogReader interface {
	ListProducts(ctx context.Context) ([]catalog.Product, error)
}

// SearchResult is a ranked product with its match score.
type SearchResult struct {
	Product catalog.Product `json:"product"`
	Score   float64         `json:"score"`
}

// Searcher ranks products against an Intent. Hard constraints (budget,
// availability) are enforced deterministically; soft preferences
// (features, use_cases) are scored.
type Searcher struct {
	catalog CatalogReader
}

func NewSearcher(catalog CatalogReader) *Searcher {
	return &Searcher{catalog: catalog}
}

// Search returns products matching the intent, ranked best-first.
// The intent budget is expressed in rupees (what the buyer said) while
// catalog prices are stored in paise, so the budget is converted to
// paise before comparing.
func (s *Searcher) Search(ctx context.Context, intent Intent) ([]SearchResult, error) {
	products, err := s.catalog.ListProducts(ctx)
	if err != nil {
		return nil, err
	}

	budgetPaise := intent.Budget * 100

	var results []SearchResult

	for _, p := range products {
		// Hard constraints — deterministic, never left to the LLM.
		if p.Price.Amount > budgetPaise {
			continue
		}
		if p.Availability <= 0 {
			continue
		}

		score := s.scoreProduct(p, intent, budgetPaise)

		results = append(results, SearchResult{Product: p, Score: score})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// scoreProduct computes a soft preference score from the AI-native
// schema (features, use_cases, attributes), not title keywords alone.
func (s *Searcher) scoreProduct(p catalog.Product, intent Intent, budgetPaise int64) float64 {
	score := 0.0

	// Priority feature match — the biggest soft signal.
	if intent.Priority != "" {
		for _, f := range p.Features {
			if f == intent.Priority {
				score += 3.0
			}
		}
	}

	// Category match on use_cases.
	if intent.Category != "" {
		for _, u := range p.UseCases {
			if u == intent.Category {
				score += 1.5
			}
		}
	}

	// Price proximity: cheaper within budget scores slightly higher.
	score += 1.0 - (float64(p.Price.Amount) / float64(budgetPaise) * 0.5)

	return score
}
