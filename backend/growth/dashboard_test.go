package growth

import (
	"context"
	"fmt"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// fakeDashboardStore is an in-memory DashboardStore for
// GrowthDashboardHandler.computeOverview tests -- the "agent/engine
// layer has the tests, the thin HTTP handler on top does not" split
// this file's doc comment describes, so these exercise computeOverview
// directly rather than going through HTTP + auth at all.
type fakeDashboardStore struct {
	funnel      FunnelSummary
	funnelErr   error
	topProducts []ProductAcceptance
	topErr      error
	demand      []RejectedDemand
	demandErr   error
}

func (f *fakeDashboardStore) Funnel(ctx context.Context, merchantID string, windowDays int) (FunnelSummary, error) {
	if f.funnelErr != nil {
		return FunnelSummary{}, f.funnelErr
	}
	return f.funnel, nil
}

func (f *fakeDashboardStore) TopProductsByAcceptance(ctx context.Context, merchantID string, windowDays int, limit int) ([]ProductAcceptance, error) {
	if f.topErr != nil {
		return nil, f.topErr
	}
	return f.topProducts, nil
}

func (f *fakeDashboardStore) RejectedDemandByProduct(ctx context.Context, merchantID string, windowDays int) ([]RejectedDemand, error) {
	if f.demandErr != nil {
		return nil, f.demandErr
	}
	return f.demand, nil
}

func TestComputeOverviewAssemblesFunnelTopProductsAndDemand(t *testing.T) {
	store := &fakeDashboardStore{
		funnel: FunnelSummary{Shown: 10, Accepted: 3, Dismissed: 2},
		topProducts: []ProductAcceptance{
			{ProductID: "case", Shown: 5, Accepted: 3, AcceptanceRate: 0.6},
		},
		demand: []RejectedDemand{
			{ProductID: "headphones", RejectCount: 4, AvgPrice: 500_000},
		},
	}
	catalogReader := &fakeCatalog{products: map[string]catalog.Product{
		"case":       testProduct("case", "m1", 50_000, 5, "earbuds"),
		"headphones": testProduct("headphones", "m1", 500_000, 5, "audio"),
	}}
	h := NewGrowthDashboardHandler(store, catalogReader)

	overview, err := h.computeOverview(context.Background(), "m1", 7)
	if err != nil {
		t.Fatal(err)
	}

	if overview.WindowDays != 7 {
		t.Fatalf("expected window_days 7, got %d", overview.WindowDays)
	}
	if overview.Funnel != (FunnelSummary{Shown: 10, Accepted: 3, Dismissed: 2}) {
		t.Fatalf("expected funnel to pass through unchanged, got %+v", overview.Funnel)
	}
	if len(overview.TopProducts) != 1 || overview.TopProducts[0].Title != "case" {
		t.Fatalf("expected top product 'case' with its catalog title filled in, got %+v", overview.TopProducts)
	}
	if len(overview.RejectedDemand) != 1 || overview.RejectedDemand[0].Title != "headphones" {
		t.Fatalf("expected rejected-demand product 'headphones' with its catalog title filled in, got %+v", overview.RejectedDemand)
	}
	if overview.RejectedDemand[0].RejectCount != 4 {
		t.Fatalf("expected reject_count 4 to pass through, got %d", overview.RejectedDemand[0].RejectCount)
	}
}

func TestComputeOverviewToleratesAProductMissingFromCatalog(t *testing.T) {
	store := &fakeDashboardStore{
		topProducts: []ProductAcceptance{{ProductID: "deleted-product", Shown: 2, Accepted: 1, AcceptanceRate: 0.5}},
	}
	// Empty catalog -- "deleted-product" is nowhere in it, simulating a
	// product removed since the impressions/acceptances were recorded.
	h := NewGrowthDashboardHandler(store, &fakeCatalog{products: map[string]catalog.Product{}})

	overview, err := h.computeOverview(context.Background(), "m1", 7)
	if err != nil {
		t.Fatalf("expected a missing catalog title to degrade gracefully, not error, got: %v", err)
	}
	if len(overview.TopProducts) != 1 {
		t.Fatalf("expected the historical shown/accepted numbers to still appear, got %+v", overview.TopProducts)
	}
	if overview.TopProducts[0].Title != "" {
		t.Fatalf("expected an empty title for a product no longer in the catalog, got %q", overview.TopProducts[0].Title)
	}
	if overview.TopProducts[0].Shown != 2 || overview.TopProducts[0].Accepted != 1 {
		t.Fatalf("expected shown/accepted counts to survive a missing title, got %+v", overview.TopProducts[0])
	}
}

func TestComputeOverviewPropagatesStoreErrors(t *testing.T) {
	cases := []struct {
		name  string
		store *fakeDashboardStore
	}{
		{"funnel error", &fakeDashboardStore{funnelErr: fmt.Errorf("db down")}},
		{"top products error", &fakeDashboardStore{topErr: fmt.Errorf("db down")}},
		{"rejected demand error", &fakeDashboardStore{demandErr: fmt.Errorf("db down")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewGrowthDashboardHandler(tc.store, &fakeCatalog{products: map[string]catalog.Product{}})
			if _, err := h.computeOverview(context.Background(), "m1", 7); err == nil {
				t.Fatal("expected the store error to propagate")
			}
		})
	}
}

func TestComputeOverviewEmptyResultsAreEmptySlicesNotNil(t *testing.T) {
	// An empty catalog with no suggestion activity yet must render as a
	// real empty state on the dashboard, not a null that a naive
	// frontend .map() would crash on.
	h := NewGrowthDashboardHandler(&fakeDashboardStore{}, &fakeCatalog{products: map[string]catalog.Product{}})

	overview, err := h.computeOverview(context.Background(), "m1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if overview.RejectedDemand == nil {
		t.Fatal("expected RejectedDemand to be an empty slice, not nil")
	}
	if len(overview.RejectedDemand) != 0 {
		t.Fatalf("expected zero rejected-demand entries, got %d", len(overview.RejectedDemand))
	}
}
