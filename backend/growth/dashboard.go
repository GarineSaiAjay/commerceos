package growth

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/garinesaiajay/commerceos/auth"
)

// GrowthOverview is the server-owned read model for /dashboard/growth
// (item 20's data with somewhere real to live -- PLAN-05-SELLER-
// DASHBOARD.md §3 / PLAN-03-PROACTIVE-GROWTH-AGENT.md §8, item 24).
// Before this, a merchant had exactly one lifetime ai_revenue figure on
// Overview and no way to tell whether the cross-sell agent is actually
// engaging buyers, which products it recommends most successfully (not
// just most often), or whether budget-rejected demand is piling up
// anywhere worth acting on.
type GrowthOverview struct {
	WindowDays     int                     `json:"window_days"`
	Funnel         FunnelSummary           `json:"funnel"`
	TopProducts    []ProductAcceptance     `json:"top_products"`
	RejectedDemand []RejectedDemandSummary `json:"rejected_demand"`
	GeneratedAt    time.Time               `json:"generated_at"`
}

// FunnelSummary is the shown/accepted/dismissed funnel across every
// /growth/suggest* surface combined, over the requested window.
// Accepted and Dismissed both necessarily undercount relative to Shown
// (a shown suggestion the buyer just ignored is neither) -- that gap
// IS the signal a merchant reads this page to see.
type FunnelSummary struct {
	Shown     int `json:"shown"`
	Accepted  int `json:"accepted"`
	Dismissed int `json:"dismissed"`
}

// ProductAcceptance ranks a product by how often it converts when
// shown, not just by how often it's shown -- "top recommended products
// by acceptance rate, not just by volume" (PLAN-03 §8's own framing).
type ProductAcceptance struct {
	ProductID string `json:"product_id"`
	// Title is filled in by GrowthDashboardHandler from a live catalog
	// lookup -- the underlying store query only has product_id, the
	// same "id now, title enriched by the caller" split
	// SuggestedProduct/RejectedDemand already use elsewhere in this
	// package.
	Title          string  `json:"title"`
	Shown          int     `json:"shown"`
	Accepted       int     `json:"accepted"`
	AcceptanceRate float64 `json:"acceptance_rate"`
}

// RejectedDemandSummary is the dashboard-facing, title-enriched view of
// growth.RejectedDemand -- "a direct link into Campaigns for any
// product with high rejected demand" (PLAN-05 §3). The two features
// (this page and the Campaign Orchestrator) already read the exact
// same underlying query (RejectedDemandByProduct); this just makes
// that connection visible on the dashboard instead of only implicit in
// campaign.CampaignAgent's own argmax pick.
type RejectedDemandSummary struct {
	ProductID   string `json:"product_id"`
	Title       string `json:"title"`
	RejectCount int    `json:"reject_count"`
	AvgPrice    int64  `json:"avg_price"`
}

// Funnel computes the shown/accepted/dismissed counts for merchantID
// over the last windowDays. All three tables are scoped to a merchant
// via a JOIN to carts (none of suggestion_impressions/recommendations/
// suggestion_dismissals carry their own merchant_id column) -- the
// same join pattern RejectedDemandByProduct below already uses.
func (s *PostgresStore) Funnel(ctx context.Context, merchantID string, windowDays int) (FunnelSummary, error) {
	var f FunnelSummary

	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM suggestion_impressions si
		JOIN carts c ON c.id = si.cart_id
		WHERE c.merchant_id = $1
		  AND si.shown_at >= NOW() - ($2 || ' days')::interval
	`, merchantID, windowDays).Scan(&f.Shown); err != nil {
		return FunnelSummary{}, fmt.Errorf("count shown suggestions: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM recommendations r
		JOIN carts c ON c.id = r.cart_id
		WHERE c.merchant_id = $1
		  AND r.accepted = TRUE
		  AND r.created_at >= NOW() - ($2 || ' days')::interval
	`, merchantID, windowDays).Scan(&f.Accepted); err != nil {
		return FunnelSummary{}, fmt.Errorf("count accepted suggestions: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM suggestion_dismissals sd
		JOIN carts c ON c.id = sd.cart_id
		WHERE c.merchant_id = $1
		  AND sd.dismissed_at >= NOW() - ($2 || ' days')::interval
	`, merchantID, windowDays).Scan(&f.Dismissed); err != nil {
		return FunnelSummary{}, fmt.Errorf("count dismissed suggestions: %w", err)
	}

	return f, nil
}

// TopProductsByAcceptance ranks products by acceptance rate (accepted /
// shown) over the last windowDays, highest rate first, ties broken
// toward higher shown-count (more evidence behind the rate) -- so a
// product shown once and accepted once doesn't outrank one shown 20
// times and accepted 15. "Accepted" here means at least one
// cart+product recommendation row was marked accepted for that
// product; "shown" is the real impression count from
// suggestion_impressions.
func (s *PostgresStore) TopProductsByAcceptance(ctx context.Context, merchantID string, windowDays int, limit int) ([]ProductAcceptance, error) {
	shown := map[string]int{}
	rows, err := s.db.Query(ctx, `
		SELECT si.product_id, COUNT(*)
		FROM suggestion_impressions si
		JOIN carts c ON c.id = si.cart_id
		WHERE c.merchant_id = $1
		  AND si.shown_at >= NOW() - ($2 || ' days')::interval
		GROUP BY si.product_id
	`, merchantID, windowDays)
	if err != nil {
		return nil, fmt.Errorf("query shown counts: %w", err)
	}
	for rows.Next() {
		var productID string
		var count int
		if err := rows.Scan(&productID, &count); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan shown count: %w", err)
		}
		shown[productID] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate shown counts: %w", err)
	}
	rows.Close()

	accepted := map[string]int{}
	acceptedRows, err := s.db.Query(ctx, `
		SELECT r.product_id, COUNT(*)
		FROM recommendations r
		JOIN carts c ON c.id = r.cart_id
		WHERE c.merchant_id = $1
		  AND r.accepted = TRUE
		  AND r.created_at >= NOW() - ($2 || ' days')::interval
		GROUP BY r.product_id
	`, merchantID, windowDays)
	if err != nil {
		return nil, fmt.Errorf("query accepted counts: %w", err)
	}
	for acceptedRows.Next() {
		var productID string
		var count int
		if err := acceptedRows.Scan(&productID, &count); err != nil {
			acceptedRows.Close()
			return nil, fmt.Errorf("scan accepted count: %w", err)
		}
		accepted[productID] = count
	}
	if err := acceptedRows.Err(); err != nil {
		acceptedRows.Close()
		return nil, fmt.Errorf("iterate accepted counts: %w", err)
	}
	acceptedRows.Close()

	results := make([]ProductAcceptance, 0, len(shown))
	for productID, shownCount := range shown {
		acceptedCount := accepted[productID]
		var rate float64
		if shownCount > 0 {
			rate = float64(acceptedCount) / float64(shownCount)
		}
		results = append(results, ProductAcceptance{
			ProductID:      productID,
			Shown:          shownCount,
			Accepted:       acceptedCount,
			AcceptanceRate: rate,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].AcceptanceRate != results[j].AcceptanceRate {
			return results[i].AcceptanceRate > results[j].AcceptanceRate
		}
		return results[i].Shown > results[j].Shown
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// DashboardStore is the read surface GrowthDashboardHandler needs --
// satisfied by *PostgresStore (Funnel/TopProductsByAcceptance are new
// above; RejectedDemandByProduct already existed for the Campaign
// Orchestrator). A separate interface, rather than depending on
// *PostgresStore directly, so handler-level tests (auth gate, window_
// days parsing, title enrichment, partial-failure handling) can use an
// in-memory fake instead of a real Postgres -- the same pattern
// suggest_test.go already established for SuggestHandler.
type DashboardStore interface {
	Funnel(ctx context.Context, merchantID string, windowDays int) (FunnelSummary, error)
	TopProductsByAcceptance(ctx context.Context, merchantID string, windowDays int, limit int) ([]ProductAcceptance, error)
	RejectedDemandByProduct(ctx context.Context, merchantID string, windowDays int) ([]RejectedDemand, error)
}

// GrowthDashboardHandler serves GET /dashboard/growth (item 24). Kept
// separate from Handler (Evaluate/Explain) above -- this is a
// merchant-facing analytics read model, not part of the
// propose/evaluate pipeline, the same separation
// analytics.Service/Handler already draws from the rest of this
// codebase's write paths.
type GrowthDashboardHandler struct {
	store   DashboardStore
	catalog CatalogReader
}

func NewGrowthDashboardHandler(store DashboardStore, catalog CatalogReader) *GrowthDashboardHandler {
	return &GrowthDashboardHandler{store: store, catalog: catalog}
}

// computeOverview does the real work of assembling a GrowthOverview,
// with no HTTP/auth concerns at all -- deliberately split out from
// Overview below so it can be tested directly with fakes (dashboard_
// test.go), the same "agent/engine layer has the tests, the thin HTTP
// handler on top does not" split this codebase already uses for
// campaign.CampaignAgent vs. campaign.Handler.Propose (neither
// campaign.Handler nor analytics.Handler has its own HTTP-level test
// in this codebase; the logic underneath them does).
func (h *GrowthDashboardHandler) computeOverview(ctx context.Context, merchantID string, windowDays int) (GrowthOverview, error) {
	funnel, err := h.store.Funnel(ctx, merchantID, windowDays)
	if err != nil {
		return GrowthOverview{}, err
	}

	topProducts, err := h.store.TopProductsByAcceptance(ctx, merchantID, windowDays, 10)
	if err != nil {
		return GrowthOverview{}, err
	}
	for i := range topProducts {
		if product, err := h.catalog.GetProduct(ctx, topProducts[i].ProductID); err == nil {
			topProducts[i].Title = product.Title
		}
		// A product that's since been deleted (catalog.ErrProductNotFound)
		// still shows its historical shown/accepted numbers -- Title just
		// stays empty rather than failing the whole page over one stale row.
	}

	rejectedDemand, err := h.store.RejectedDemandByProduct(ctx, merchantID, windowDays)
	if err != nil {
		return GrowthOverview{}, err
	}
	rejectedSummaries := make([]RejectedDemandSummary, 0, len(rejectedDemand))
	for _, d := range rejectedDemand {
		summary := RejectedDemandSummary{ProductID: d.ProductID, RejectCount: d.RejectCount, AvgPrice: d.AvgPrice}
		if product, err := h.catalog.GetProduct(ctx, d.ProductID); err == nil {
			summary.Title = product.Title
		}
		rejectedSummaries = append(rejectedSummaries, summary)
	}

	return GrowthOverview{
		WindowDays:     windowDays,
		Funnel:         funnel,
		TopProducts:    topProducts,
		RejectedDemand: rejectedSummaries,
		GeneratedAt:    time.Now().UTC(),
	}, nil
}

// Overview handles GET /dashboard/growth?window_days=N (default 7,
// matching campaign.Handler.Propose's own default window so the two
// pages read as the same time horizon by default).
func (h *GrowthDashboardHandler) Overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	operator, ok := auth.OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return
	}

	windowDays := 7
	if raw := r.URL.Query().Get("window_days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			windowDays = n
		}
	}

	overview, err := h.computeOverview(r.Context(), operator.MerchantID, windowDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, overview)
}
