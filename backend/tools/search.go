package tools

import (
	"context"
	"sort"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// CatalogReader is the subset of the catalog a search needs.
type CatalogReader interface {
	ListProducts(ctx context.Context) ([]catalog.Product, error)
}

// SearchFilter is the tool layer's product-search input. It is
// structurally identical to agents.Intent minus the Clarify field --
// Clarify is an intent-EXTRACTION concern (does this prompt say enough
// to search at all?), not a search concern, so it deliberately doesn't
// belong on the type actually doing the searching. See
// backend/agents/search.go for how agents.Intent adapts into this.
type SearchFilter struct {
	Budget    int64
	Category  string
	Priority  string
	Recipient string
}

// SearchResult is a ranked product with its match score.
type SearchResult struct {
	Product catalog.Product `json:"product"`
	Score   float64         `json:"score"`
}

// Searcher ranks products against a SearchFilter. Hard constraints
// (budget, availability) are enforced deterministically; soft
// preferences (features, use_cases) are scored.
//
// This lives in the shared tools package (PLAN-01-AGENTIC-CORE.md §1,
// ROADMAP-PRIORITIZED.md P1 item 17) rather than in backend/agents so
// that both the MCP tool surface (backend/mcp/tools.go's
// search_products) and the in-app agent's own tool-calling loop
// (backend/agents' bounded loop, item 18) rank products through the
// exact same code -- any future improvement to ranking benefits both
// surfaces automatically instead of needing to be built twice.
// backend/agents/search.go keeps type aliases so every existing caller
// of agents.Searcher/agents.NewSearcher/agents.SearchResult/
// agents.CatalogReader keeps compiling unchanged.
type Searcher struct {
	catalog CatalogReader
}

func NewSearcher(catalog CatalogReader) *Searcher {
	return &Searcher{catalog: catalog}
}

// Search returns products matching the filter, ranked best-first.
// The filter's budget is expressed in rupees (what the buyer said)
// while catalog prices are stored in paise, so the budget is converted
// to paise before comparing.
func (s *Searcher) Search(ctx context.Context, filter SearchFilter) ([]SearchResult, error) {
	products, err := s.catalog.ListProducts(ctx)
	if err != nil {
		return nil, err
	}

	budgetPaise := filter.Budget * 100

	var results []SearchResult

	for _, p := range products {
		// Hard constraints — deterministic, never left to the LLM.
		if p.Price.Amount > budgetPaise {
			continue
		}
		if p.Availability <= 0 {
			continue
		}

		score := s.scoreProduct(p, filter, budgetPaise)

		results = append(results, SearchResult{Product: p, Score: score})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// scoreProduct computes a soft preference score from the AI-native
// schema (features, use_cases, attributes), not title keywords alone.
func (s *Searcher) scoreProduct(p catalog.Product, filter SearchFilter, budgetPaise int64) float64 {
	score := 0.0

	// Priority feature match — the biggest soft signal.
	if filter.Priority != "" {
		for _, f := range p.Features {
			if f == filter.Priority {
				score += 3.0
			}
		}
	}

	// Category match on use_cases.
	if filter.Category != "" {
		for _, u := range p.UseCases {
			if u == filter.Category {
				score += 1.5
			}
		}
	}

	// Price proximity: cheaper within budget scores slightly higher.
	score += 1.0 - (float64(p.Price.Amount) / float64(budgetPaise) * 0.5)

	return score
}
