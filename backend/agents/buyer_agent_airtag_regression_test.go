package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// airtagFakeCatalog mirrors the real AirTag-family rows in
// db/seeds/001_catalog.sql exactly (id, title, price in paise,
// use_cases) -- a separate fixture from agents_test.go's fakeCatalog so
// this file doesn't risk perturbing its unrelated earbuds tests.
type airtagFakeCatalog struct{}

func (airtagFakeCatalog) ListProducts(ctx context.Context) ([]catalog.Product, error) {
	return []catalog.Product{
		{ID: "airtag-single", Title: "AirTag (Single)", Price: catalog.Money{Amount: 329000, Currency: "INR"}, Availability: 80, UseCases: []string{"tracking", "travel", "accessories"}},
		{ID: "airtag-2pack", Title: "AirTag (2 Pack)", Price: catalog.Money{Amount: 629000, Currency: "INR"}, Availability: 50, UseCases: []string{"tracking", "travel", "accessories"}},
		{ID: "airtag-4pack", Title: "AirTag (4 Pack)", Price: catalog.Money{Amount: 1290000, Currency: "INR"}, Availability: 40, UseCases: []string{"tracking", "travel", "accessories"}},
		{ID: "airtag-anti-lost-strap", Title: "AirTag Anti-Lost Strap", Price: catalog.Money{Amount: 129000, Currency: "INR"}, Availability: 55, UseCases: []string{"accessories", "tracking", "travel"}},
		{ID: "airtag-leather-case", Title: "Premium Leather AirTag Case", Price: catalog.Money{Amount: 169000, Currency: "INR"}, Availability: 30, UseCases: []string{"accessories", "tracking", "protection"}},
		{ID: "airtag-loop-leather", Title: "AirTag Loop (Leather)", Price: catalog.Money{Amount: 390000, Currency: "INR"}, Availability: 30, UseCases: []string{"tracking", "accessories", "travel"}},
	}, nil
}

// TestPlanCheckout_BareAirtagRequestPicksTrackerNotAccessory is the
// regression test for the live-reported bug: "i want a airtag for my
// sister, under 20k" returned "AirTag Anti-Lost Strap" -- an accessory
// FOR an AirTag, not an AirTag -- because category-match-plus-
// cheapest-wins scoring can't tell the two apart when both carry the
// same "tracking" use_cases tag (db/seeds/001_catalog.sql). It must now
// pick an actual AirTag.
func TestPlanCheckout_BareAirtagRequestPicksTrackerNotAccessory(t *testing.T) {
	agent := NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(airtagFakeCatalog{}))

	plan, err := agent.PlanCheckout(context.Background(), "i want a airtag for my sister, under 20k", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}

	if plan.SelectedID == "airtag-anti-lost-strap" {
		t.Fatalf("regression: picked the accessory again, reasoning: %q", plan.Reasoning)
	}
	if plan.SelectedID != "airtag-single" {
		t.Fatalf("expected airtag-single (cheapest real tracker), got %s -- reasoning: %q", plan.SelectedID, plan.Reasoning)
	}
}

// TestPlanCheckoutInConversation_ExplicitNegationOverridesStalePick is
// the regression test for the second half of the same live bug report:
// after the wrong pick, "i want a airtag not airtag anti lost strap"
// got the EXACT SAME wrong pick back, because neither extractor's
// schema had anywhere to record an explicit "not X" correction. The
// second turn must now exclude the named product and say so.
func TestPlanCheckoutInConversation_ExplicitNegationOverridesStalePick(t *testing.T) {
	store := newMemoryConversationStore()
	agent := NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(airtagFakeCatalog{})).
		WithConversationStore(store)

	first, err := agent.PlanCheckoutInConversation(context.Background(), "cart_1", "i want a airtag for my sister, under 20k", "merchant_001")
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if first.SelectedID != "airtag-single" {
		t.Fatalf("first turn: expected airtag-single, got %s", first.SelectedID)
	}

	second, err := agent.PlanCheckoutInConversation(context.Background(), "cart_1", "i want a airtag not airtag anti lost strap", "merchant_001")
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}

	if second.SelectedID == "airtag-anti-lost-strap" {
		t.Fatalf("regression: correction was ignored, picked the excluded product again")
	}
	if second.SelectedID != "airtag-single" {
		t.Fatalf("expected airtag-single to still win after excluding the strap, got %s", second.SelectedID)
	}
	if len(second.Intent.Exclude) != 1 || second.Intent.Exclude[0] != "airtag anti lost strap" {
		t.Fatalf("expected the correction recorded on Intent.Exclude, got %v", second.Intent.Exclude)
	}
	if !strings.Contains(second.Reasoning, "Excluded as requested") {
		t.Fatalf("expected the reasoning sentence to surface the honored correction, got %q", second.Reasoning)
	}
	// Budget must still carry forward from the first turn even though
	// the second turn's prompt never repeats it (mergeIntent).
	if second.Intent.Budget != 20000 {
		t.Fatalf("expected budget carried forward from turn 1, got %d", second.Intent.Budget)
	}
}

// TestPlanCheckout_ExplicitAccessoryRequestStillFindsIt guards against
// the accessory-demotion fix overcorrecting: asking for the case/strap
// specifically must still surface it, not just the bare tracker.
func TestPlanCheckout_ExplicitAccessoryRequestStillFindsIt(t *testing.T) {
	agent := NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(airtagFakeCatalog{}))

	plan, err := agent.PlanCheckout(context.Background(), "i want an airtag case, under 5000", "merchant_001")
	if err != nil {
		t.Fatal(err)
	}
	if plan.SelectedID != "airtag-leather-case" {
		t.Fatalf("expected the case to win when explicitly requested, got %s -- reasoning: %q", plan.SelectedID, plan.Reasoning)
	}
}
