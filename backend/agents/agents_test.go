package agents

import (
	"context"
	"testing"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

// fakeCatalog returns a fixed product set.
type fakeCatalog struct{}

func (f fakeCatalog) ListProducts(ctx context.Context) ([]catalog.Product, error) {
	// Prices are paise, matching the real catalog and the paise money unit.
	return []catalog.Product{
		{
			ID:           "airpods-pro-2",
			Title:        "AirPods Pro",
			Price:        catalog.Money{Amount: 2_490_000, Currency: "INR"},
			Availability: 12,
			Features:     []string{"active_noise_cancellation", "transparency_mode"},
			UseCases:     []string{"travel", "music", "calls"},
		},
		{
			ID:           "airpods-case",
			Title:        "AirPods Case",
			Price:        catalog.Money{Amount: 199_900, Currency: "INR"},
			Availability: 25,
			Features:     []string{"protective", "wireless_charging"},
			UseCases:     []string{"protection", "travel"},
		},
		{
			ID:           "usb-c-adapter",
			Title:        "USB-C Adapter",
			Price:        catalog.Money{Amount: 129_900, Currency: "INR"},
			Availability: 30,
			Features:     []string{"usb_c", "plug_and_play"},
			UseCases:     []string{"charging", "connectivity"},
		},
	}, nil
}

// TestIntentExtractionConsistency proves spec: the demo prompt extracts
// budget/category/priority/recipient reliably across 5 runs.
func TestIntentExtractionConsistency(t *testing.T) {
	extractor := NewDeterministicExtractor()
	prompt := "I need wireless earbuds for my sister. Budget ₹25,000. I want good noise cancellation."

	for i := 0; i < 5; i++ {
		intent, err := extractor.Extract(context.Background(), prompt)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}

		if intent.Budget != 25000 {
			t.Fatalf("run %d: expected budget 25000, got %d", i, intent.Budget)
		}
		if intent.Category != "earbuds" {
			t.Fatalf("run %d: expected category earbuds, got %s", i, intent.Category)
		}
		if intent.Priority != "active_noise_cancellation" {
			t.Fatalf("run %d: expected priority anc, got %s", i, intent.Priority)
		}
		if intent.Recipient != "sister" {
			t.Fatalf("run %d: expected recipient sister, got %s", i, intent.Recipient)
		}
	}
}

// TestSearchRanksANCAboveNonANC proves spec: an ANC-tagged product ranks
// above a non-ANC product at similar price.
func TestSearchRanksANCAboveNonANC(t *testing.T) {
	searcher := NewSearcher(fakeCatalog{})

	results, err := searcher.Search(context.Background(), Intent{
		Budget:   25000,
		Category: "earbuds",
		Priority: "active_noise_cancellation",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// AirPods Pro (ANC) must rank above AirPods Case (no ANC).
	if results[0].Product.ID != "airpods-pro-2" {
		t.Fatalf("expected airpods-pro-2 first, got %s", results[0].Product.ID)
	}
}

// TestSearchRespectsBudget proves hard constraints are deterministic.
func TestSearchRespectsBudget(t *testing.T) {
	searcher := NewSearcher(fakeCatalog{})

	// Budget ₹1,500 (150_000 paise) excludes AirPods Pro (₹24,900) but
	// includes cheaper items.
	results, err := searcher.Search(context.Background(), Intent{
		Budget:   1500,
		Category: "earbuds",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Product.Price.Amount > 150_000 {
			t.Fatalf("product %s exceeds budget", r.Product.ID)
		}
	}
}

// TestWellFormedProposal proves the agent always emits a valid Proposed
// Action with a product_id, never a price it invented.
func TestWellFormedProposal(t *testing.T) {
	agent := NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(fakeCatalog{}))

	plan, err := agent.PlanCheckout(
		context.Background(),
		"I need wireless earbuds for my sister. Budget ₹25,000. I want good noise cancellation.",
		"merchant_001",
	)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Proposal.Action != "CREATE_ORDER" {
		t.Fatalf("expected CREATE_ORDER, got %s", plan.Proposal.Action)
	}
	if plan.Proposal.Merchant != "merchant_001" {
		t.Fatalf("unexpected merchant %s", plan.Proposal.Merchant)
	}
	if len(plan.Proposal.Items) != 1 || plan.Proposal.Items[0] != "airpods-pro-2" {
		t.Fatalf("expected airpods-pro-2 item, got %v", plan.Proposal.Items)
	}
	if plan.Proposal.Amount != 2_490_000 {
		t.Fatalf("expected amount 2490000 paise (authoritative price), got %d", plan.Proposal.Amount)
	}
}

// TestAmbiguousIntentDegradesGracefully proves spec: "buy me something"
// yields a clarifying question, never a wrong-amount cart.
func TestAmbiguousIntentDegradesGracefully(t *testing.T) {
	agent := NewBuyerAgent(NewDeterministicExtractor(), NewSearcher(fakeCatalog{}))

	_, err := agent.PlanCheckout(context.Background(), "buy me something", "merchant_001")
	if err != ErrAmbiguousIntent {
		t.Fatalf("expected ErrAmbiguousIntent, got %v", err)
	}
}

// TestDeterministicExtractorCoversFullCatalog proves the no-LLM fallback
// extractor (used whenever OPENROUTER_API_KEY is unset) isn't blind to
// the products db/seeds/001_catalog.sql added in commit d30f155 --
// category must equal a real use_cases entry and priority a real
// features entry (agents.Searcher.scoreProduct compares by exact
// string equality), or a keyword parses into an intent that can never
// actually move a product's rank.
func TestDeterministicExtractorCoversFullCatalog(t *testing.T) {
	extractor := NewDeterministicExtractor()

	cases := []struct {
		prompt        string
		wantCategory  string
		wantPriority  string
		wantBudget    int64
		wantRecipient string
	}{
		{
			prompt:       "I need a MagSafe charger, budget ₹5,000, fast charging and magnetic alignment please.",
			wantCategory: "charging",
			wantPriority: "fast_charging",
			wantBudget:   5000,
		},
		{
			prompt:       "Looking for AppleCare, budget ₹3,000, want the extended warranty.",
			wantCategory: "accessories",
			wantPriority: "extended_warranty",
			wantBudget:   3000,
		},
		{
			prompt:       "I want new ear tips for my AirPods, budget ₹2,000, need a comfortable fit.",
			wantCategory: "accessories",
			wantPriority: "comfort_fit",
			wantBudget:   2000,
		},
		// The three products added alongside this test's original three
		// cases in the airpods-pro-3/airtag-4pack/beats-fit-pro expansion
		// (db/seeds/001_catalog.sql) -- same "keyword must resolve to a
		// real features/use_cases string" requirement as above.
		{
			prompt:       "I need something to track my luggage, budget ₹2,000.",
			wantCategory: "tracking",
			wantPriority: "",
			wantBudget:   2000,
		},
		{
			prompt:       "I want new AirPods with heart rate sensing, budget ₹25,000, earbuds for calls.",
			wantCategory: "earbuds",
			wantPriority: "heart_rate_sensing",
			wantBudget:   25000,
		},
		{
			prompt:       "I need workout earbuds for the gym, budget ₹16,000.",
			wantCategory: "earbuds",
			wantPriority: "secure_fit",
			wantBudget:   16000,
		},
		// Regression: this exact phrasing (a named product, not the
		// generic word "earbuds"/"headphones") previously extracted an
		// empty category and was rejected by ValidateIntent with
		// "invalid intent: category required", even though budget and
		// recipient both parsed correctly. Also exercises the "30k"
		// budget shorthand (== 30000, not 30).
		{
			prompt:        "i want beats fit pro for my sister, my budget is below 30k",
			wantCategory:  "earbuds",
			wantPriority:  "",
			wantBudget:    30000,
			wantRecipient: "sister",
		},
		// Regression: no literal word "budget" anywhere in this prompt
		// ("under 40k" expresses it instead) -- previously the ambiguity
		// check required that exact substring and clarified this away
		// before parseBudget/parseCategory ever ran, even though both
		// are trivially extractable. Also exercises "bro" as shorthand
		// for the "brother" recipient.
		{
			prompt:        "i want earbuds for my bro, under 40k",
			wantCategory:  "earbuds",
			wantPriority:  "",
			wantBudget:    40000,
			wantRecipient: "brother",
		},
	}

	for _, c := range cases {
		intent, err := extractor.Extract(context.Background(), c.prompt)
		if err != nil {
			t.Fatalf("prompt %q: %v", c.prompt, err)
		}
		if intent.Category != c.wantCategory {
			t.Fatalf("prompt %q: expected category %s, got %s", c.prompt, c.wantCategory, intent.Category)
		}
		if intent.Priority != c.wantPriority {
			t.Fatalf("prompt %q: expected priority %s, got %s", c.prompt, c.wantPriority, intent.Priority)
		}
		if intent.Budget != c.wantBudget {
			t.Fatalf("prompt %q: expected budget %d, got %d", c.prompt, c.wantBudget, intent.Budget)
		}
		if intent.Recipient != c.wantRecipient {
			t.Fatalf("prompt %q: expected recipient %s, got %s", c.prompt, c.wantRecipient, intent.Recipient)
		}
	}
}
