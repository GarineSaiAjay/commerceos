package campaign

import (
	"context"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
	"github.com/garinesaiajay/commerceos/growth"
)

// fakeCatalog is an in-memory CatalogReader.
type fakeCatalog struct {
	products map[string]catalog.Product
}

func (c *fakeCatalog) GetProduct(ctx context.Context, id string) (catalog.Product, error) {
	p, ok := c.products[id]
	if !ok {
		return catalog.Product{}, errProductNotFound
	}
	return p, nil
}

// fakeDemand is a DemandSource returning a preset slice or error.
type fakeDemand struct {
	demand []growth.RejectedDemand
	err    error
}

func (d *fakeDemand) RejectedDemandByProduct(ctx context.Context, merchantID string, windowDays int) ([]growth.RejectedDemand, error) {
	return d.demand, d.err
}

// fakeCampaignRepo is an in-memory Repository.
type fakeCampaignRepo struct {
	campaigns    map[string]Campaign
	activeBudget int64
}

func newFakeCampaignRepo() *fakeCampaignRepo {
	return &fakeCampaignRepo{campaigns: map[string]Campaign{}}
}

func (r *fakeCampaignRepo) Save(ctx context.Context, c Campaign) error {
	r.campaigns[c.ID] = c
	return nil
}

func (r *fakeCampaignRepo) GetByID(ctx context.Context, id string) (Campaign, error) {
	c, ok := r.campaigns[id]
	if !ok {
		return Campaign{}, ErrCampaignNotFound
	}
	return c, nil
}

func (r *fakeCampaignRepo) List(ctx context.Context, merchantID string, status string) ([]Campaign, error) {
	var out []Campaign
	for _, c := range r.campaigns {
		if c.MerchantID == merchantID && (status == "" || c.Status == status) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (r *fakeCampaignRepo) SumActiveBudget(ctx context.Context, merchantID string) (int64, error) {
	return r.activeBudget, nil
}

func (r *fakeCampaignRepo) Approve(ctx context.Context, id string, approvedBy string) (Campaign, error) {
	c, ok := r.campaigns[id]
	if !ok || c.Status != StatusProposed {
		return Campaign{}, ErrCampaignNotProposed
	}
	c.Status = StatusActive
	c.ApprovedBy = approvedBy
	r.campaigns[id] = c
	return c, nil
}

func (r *fakeCampaignRepo) Reject(ctx context.Context, id string, reason string) (Campaign, error) {
	c, ok := r.campaigns[id]
	if !ok || c.Status != StatusProposed {
		return Campaign{}, ErrCampaignNotProposed
	}
	c.Status = StatusRejected
	c.RejectedReason = reason
	r.campaigns[id] = c
	return c, nil
}

func (r *fakeCampaignRepo) FindActiveForProduct(ctx context.Context, merchantID, productID string) (Campaign, error) {
	for _, c := range r.campaigns {
		if c.MerchantID == merchantID && c.ProductID == productID && c.Status == StatusActive {
			return c, nil
		}
	}
	return Campaign{}, ErrCampaignNotFound
}

type fakeProductNotFoundError struct{}

func (fakeProductNotFoundError) Error() string { return "product not found" }

var errProductNotFound = fakeProductNotFoundError{}

func testCatalog() *fakeCatalog {
	return &fakeCatalog{products: map[string]catalog.Product{
		"airpods-case": {ID: "airpods-case", Price: catalog.Money{Amount: 199_00, Currency: "INR"}},
		"applecare":    {ID: "applecare", Price: catalog.Money{Amount: 2_500_00, Currency: "INR"}},
	}}
}

func TestProposeFromRejectedDemandPicksArgmax(t *testing.T) {
	demand := &fakeDemand{demand: []growth.RejectedDemand{
		{ProductID: "applecare", RejectCount: 4, AvgPrice: 2_500_00},
		{ProductID: "airpods-case", RejectCount: 9, AvgPrice: 199_00},
	}}
	repo := newFakeCampaignRepo()
	engine := NewEngine(DefaultConfig())
	agent := NewCampaignAgent(testCatalog(), demand, repo, engine)

	c, _, err := agent.ProposeFromRejectedDemand(context.Background(), "merchant_001", 7, 15, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ProductID != "airpods-case" {
		t.Fatalf("got product %q, want airpods-case (higher reject count)", c.ProductID)
	}
}

func TestProposeFromRejectedDemandSizesBudgetToObservedVolume(t *testing.T) {
	demand := &fakeDemand{demand: []growth.RejectedDemand{
		{ProductID: "airpods-case", RejectCount: 9, AvgPrice: 199_00},
	}}
	repo := newFakeCampaignRepo()
	engine := NewEngine(DefaultConfig())
	agent := NewCampaignAgent(testCatalog(), demand, repo, engine)

	discountPercent := 15
	c, _, err := agent.ProposeFromRejectedDemand(context.Background(), "merchant_001", 7, discountPercent, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	product := testCatalog().products["airpods-case"]
	wantDiscountPerRedemption := product.Price.Amount * int64(discountPercent) / 100
	wantBudgetCap := wantDiscountPerRedemption * 9

	if c.BudgetCap != wantBudgetCap {
		t.Fatalf("got budget_cap %d, want %d (sized to 9 observed rejections)", c.BudgetCap, wantBudgetCap)
	}
}

func TestProposeFromRejectedDemandNoDemandReturnsError(t *testing.T) {
	demand := &fakeDemand{demand: nil}
	repo := newFakeCampaignRepo()
	engine := NewEngine(DefaultConfig())
	agent := NewCampaignAgent(testCatalog(), demand, repo, engine)

	_, _, err := agent.ProposeFromRejectedDemand(context.Background(), "merchant_001", 7, 15, 7)
	if err != ErrNoRejectedDemand {
		t.Fatalf("got error %v, want ErrNoRejectedDemand", err)
	}
}

func TestProposeFromRejectedDemandStillSavesRejectedProposal(t *testing.T) {
	// Only 2 rejections observed, below DefaultConfig().MinRejectedDemandCount (3)
	// -- the engine should reject this proposal, but it's still persisted.
	demand := &fakeDemand{demand: []growth.RejectedDemand{
		{ProductID: "airpods-case", RejectCount: 2, AvgPrice: 199_00},
	}}
	repo := newFakeCampaignRepo()
	engine := NewEngine(DefaultConfig())
	agent := NewCampaignAgent(testCatalog(), demand, repo, engine)

	c, decision, err := agent.ProposeFromRejectedDemand(context.Background(), "merchant_001", 7, 15, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Decision != DecisionRejected || decision.FailedCheck != CheckSufficientDemand {
		t.Fatalf("got decision=%q failedCheck=%q, want REJECTED/%s", decision.Decision, decision.FailedCheck, CheckSufficientDemand)
	}
	if c.Status != StatusRejected {
		t.Fatalf("got status %q, want REJECTED", c.Status)
	}

	saved, ok := repo.campaigns[c.ID]
	if !ok {
		t.Fatalf("rejected proposal was not persisted")
	}
	if saved.RejectedReason == "" {
		t.Fatalf("persisted campaign has no rejected_reason recorded")
	}
}

func TestProposeFromRejectedDemandTargetProductMissingFromCatalogErrors(t *testing.T) {
	demand := &fakeDemand{demand: []growth.RejectedDemand{
		{ProductID: "does-not-exist", RejectCount: 9, AvgPrice: 500_00},
	}}
	repo := newFakeCampaignRepo()
	engine := NewEngine(DefaultConfig())
	agent := NewCampaignAgent(testCatalog(), demand, repo, engine)

	_, _, err := agent.ProposeFromRejectedDemand(context.Background(), "merchant_001", 7, 15, 7)
	if err == nil {
		t.Fatalf("expected an error for a product missing from the catalog")
	}
}
