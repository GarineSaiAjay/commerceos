package tools

import (
	"context"
	"sort"
	"strings"

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
	// Exclude holds literal phrases the buyer explicitly ruled out this
	// turn (agents.ParseExclusions, from a "not X" / "not the X"
	// correction). This is a HARD constraint, enforced in Search
	// exactly like Budget and Availability below: a product whose title
	// matches an excluded phrase (excludesProduct) is removed from the
	// candidate list entirely, never merely down-scored. An explicit
	// "not X" is the buyer overriding whatever the soft category/
	// priority signals alone would otherwise pick, so it must never be
	// outweighed by them the way a mere score penalty could be.
	Exclude []string
	// Terms holds the buyer's own significant words from the raw prompt
	// (agents.ExtractTerms). Used only as a soft signal in scoreProduct
	// below, via accessoryQualifiers, to tell an accessory FOR a
	// product apart from the product itself -- something bare
	// use_cases/category matching cannot do on its own. A nil or empty
	// Terms disables that check entirely, so existing callers/tests
	// that never set it are unaffected.
	Terms []string
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
		if excludesProduct(p.Title, filter.Exclude) {
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

	// Accessory demotion. A title naming an accessory-for-a-product noun
	// the buyer's own words never mentioned is probably not what they
	// meant by a bare category request -- see accessoryQualifiers' doc
	// comment. This is what stops "AirTag Anti-Lost Strap" (₹1,290,
	// use_cases ["accessories","tracking","travel"] -- db/seeds/
	// 001_catalog.sql) from outranking the actual "AirTag (Single)"
	// (₹3,290, same "tracking" use_cases tag) on category-match-plus-
	// cheapest-wins scoring alone, which is exactly the bug reported
	// live: "i want a airtag" returned the strap, an accessory FOR an
	// AirTag, not an AirTag. Skipped entirely when Terms is empty
	// (older callers/tests that never set it), so this is purely
	// additive.
	if len(filter.Terms) > 0 && hasAccessoryQualifier(p.Title, filter.Terms) {
		score -= accessoryPenalty
	}

	return score
}

// accessoryPenalty is large enough to overcome the category match
// (+1.5) and price-proximity (≤1.0) an accessory would otherwise win on
// being the cheapest item tagged with the same use_cases as the actual
// product -- see scoreProduct's doc comment above.
const accessoryPenalty = 2.5

// accessoryQualifiers are nouns that mark a catalog title as an add-on
// FOR a product rather than the product itself -- a case, a strap, a
// cable, and so on. The catalog tags an accessory with the SAME
// use_cases as the product it's for (db/seeds/001_catalog.sql: "AirTag
// Anti-Lost Strap" and "Premium Leather AirTag Case" both carry
// "tracking" exactly like "AirTag (Single)" does), which is correct for
// search recall ("something to protect my AirTag" should find the
// case) but means category-match-plus-price-proximity alone can never
// tell "the AirTag" from "something for an AirTag" -- and an accessory
// is almost always the cheaper of the two, so it always won.
//
// Hand-maintained and intentionally small: a soft per-title penalty in
// scoreProduct, not a hard filter, so a search that can *only* find
// accessories in budget still returns them rather than nothing.
var accessoryQualifiers = []string{
	"case", "cover", "strap", "loop", "sleeve", "skin", "wrap",
	"adapter", "cable", "stand", "mount", "holder", "dock", "kit", "band",
}

// hasAccessoryQualifier reports whether title names one of
// accessoryQualifiers that terms (the buyer's own words) never
// mentioned -- i.e. the buyer didn't ask for a case/strap/etc.
// specifically, so a title that is one probably isn't the match.
func hasAccessoryQualifier(title string, terms []string) bool {
	for _, q := range accessoryQualifiers {
		if strings.Contains(normalizeText(title), q) && !containsWord(terms, q) {
			return true
		}
	}
	return false
}

// containsWord reports whether words contains target, case-insensitively.
func containsWord(words []string, target string) bool {
	for _, w := range words {
		if strings.EqualFold(w, target) {
			return true
		}
	}
	return false
}

// excludesProduct reports whether title matches any of the buyer's
// explicitly excluded phrases (SearchFilter.Exclude) -- a phrase
// matches when EVERY one of its significant words (SignificantWords)
// appears in title, so "not airtag anti lost strap" matches the
// catalog title "AirTag Anti-Lost Strap" regardless of casing or the
// hyphen in "Anti-Lost" (normalizeText flattens both before comparing).
func excludesProduct(title string, exclude []string) bool {
	if len(exclude) == 0 {
		return false
	}
	normTitle := normalizeText(title)
	for _, phrase := range exclude {
		words := SignificantWords(phrase)
		if len(words) == 0 {
			continue
		}
		allPresent := true
		for _, w := range words {
			if !strings.Contains(normTitle, w) {
				allPresent = false
				break
			}
		}
		if allPresent {
			return true
		}
	}
	return false
}

// normalizeText lowercases s and flattens hyphens to spaces so
// "Anti-Lost" and "anti lost" compare equal -- catalog titles use the
// former, buyer prompts almost always the latter.
func normalizeText(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "-", " ")
}

// searchStopwords are common function words stripped out by
// SignificantWords -- kept short and specific to how buyers phrase
// shopping requests and corrections ("i want X", "not the Y") rather
// than attempting a general-purpose English stopword list.
var searchStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "i": true, "me": true,
	"my": true, "for": true, "of": true, "to": true, "is": true,
	"with": true, "and": true, "or": true, "in": true, "on": true,
	"at": true, "not": true, "no": true, "want": true, "need": true,
	"looking": true, "please": true, "instead": true, "that": true,
	"this": true, "it": true, "be": true, "buy": true, "get": true,
	"under": true, "below": true, "budget": true,
}

// SignificantWords tokenizes s into lowercase words with punctuation
// and hyphens stripped, dropping searchStopwords. Exported and shared
// by both this package (excludesProduct, matching an Exclude phrase
// against a catalog title) and agents.ExtractTerms (grounding
// Intent.Terms in the buyer's raw prompt), so the two call sites can
// never drift onto two different notions of "significant word."
func SignificantWords(s string) []string {
	fields := strings.Fields(normalizeText(s))
	words := make([]string, 0, len(fields))
	for _, w := range fields {
		w = strings.Trim(w, ".,!?;:()\"'")
		if w == "" || searchStopwords[w] {
			continue
		}
		words = append(words, w)
	}
	return words
}
