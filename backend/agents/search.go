package agents

import "github.com/garinesaiajay/commerceos/tools"

// Searcher, SearchResult, CatalogReader, and NewSearcher are now
// implemented in the shared tools package (backend/tools/search.go --
// see its doc comment for why: PLAN-01-AGENTIC-CORE.md §1,
// ROADMAP-PRIORITIZED.md P1 item 17, unifying the MCP tool surface and
// the in-app agent's own tool-calling loop onto one search
// implementation). These are kept as aliases here so every existing
// caller of agents.Searcher / agents.NewSearcher / agents.SearchResult /
// agents.CatalogReader keeps compiling unchanged -- only Search's
// parameter type actually changed, from agents.Intent to
// tools.SearchFilter (structurally identical minus the Clarify field),
// so callers that build an Intent for extraction and then search
// separately convert at the call site (see BuyerAgent.planFromIntent).
type Searcher = tools.Searcher
type SearchResult = tools.SearchResult
type CatalogReader = tools.CatalogReader

var NewSearcher = tools.NewSearcher
