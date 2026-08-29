package growth

import (
	"testing"
)

// TestEVFormulaExact proves the EV formula reproduces the spec table:
// A: 0.21×600×0.85 = 107.10; B: 0.13×1000×0.95 = 123.50.
func TestEVFormulaExact(t *testing.T) {
	a := ExpectedValue(EVInputs{PurchaseProbability: 0.21, IncrementalMargin: 600, Confidence: 0.85})
	b := ExpectedValue(EVInputs{PurchaseProbability: 0.13, IncrementalMargin: 1000, Confidence: 0.95})

	if a != 107.10 {
		t.Fatalf("expected 107.10, got %v", a)
	}
	if b != 123.50 {
		t.Fatalf("expected 123.50, got %v", b)
	}
}

// TestSelectBestArgmax proves the engine picks B regardless of order.
func TestSelectBestArgmax(t *testing.T) {
	candidates := []Candidate{
		{ProductID: "A", EV: 107.10},
		{ProductID: "B", EV: 123.50},
	}

	best, ok := SelectBest(candidates)
	if !ok {
		t.Fatal("expected a selection")
	}
	if best.ProductID != "B" {
		t.Fatalf("expected B, got %s", best.ProductID)
	}

	// Reversed order must still pick B.
	reversed := []Candidate{
		{ProductID: "B", EV: 123.50},
		{ProductID: "A", EV: 107.10},
	}
	best, _ = SelectBest(reversed)
	if best.ProductID != "B" {
		t.Fatalf("expected B regardless of order, got %s", best.ProductID)
	}
}

// TestBudgetRejectNoTolerance proves spec §1.1: ₹24,900 cart, ₹25,000
// budget, no tolerance → ₹1,999 case is REJECTED.
func TestBudgetRejectNoTolerance(t *testing.T) {
	b := BudgetCheck{CartTotal: 24900, Budget: 25000, Tolerance: 0}

	if b.Eligible(24900 + 1999) {
		t.Fatal("expected REJECT: 26899 > 25000")
	}
}

// TestBudgetEligibleWithTolerance proves spec §1.2: +10% tolerance →
// max ₹27,500, and ₹26,899 ≤ ₹27,500 → ELIGIBLE.
func TestBudgetEligibleWithTolerance(t *testing.T) {
	b := BudgetCheck{CartTotal: 24900, Budget: 25000, Tolerance: 0.10}

	if b.MaxAllowed() != 27500 {
		t.Fatalf("expected max allowed 27500, got %d", b.MaxAllowed())
	}

	if !b.Eligible(24900 + 1999) {
		t.Fatal("expected ELIGIBLE: 26899 ≤ 27500")
	}
}

// testCatalog stands in for the real seeded catalog (db/seeds/001_catalog.sql)
// so this test doesn't need a database connection -- Generate takes the
// product list as a parameter precisely so callers like this one can
// supply their own instead of relying on a hardcoded list baked into
// the simulator itself.
var testCatalog = []ProductInfo{
	{ID: "airpods-pro-2", PriceAmount: 2_490_000},
	{ID: "airpods-case", PriceAmount: 199_900},
	{ID: "applecare", PriceAmount: 250_000},
	{ID: "usb-c-adapter", PriceAmount: 129_900},
	{ID: "wireless-charging-pad", PriceAmount: 89_900},
	{ID: "airpods-max", PriceAmount: 5_990_000},
	{ID: "airpods-3", PriceAmount: 1_890_000},
	{ID: "magsafe-charger", PriceAmount: 450_000},
	{ID: "lightning-usbc-cable", PriceAmount: 190_000},
	{ID: "airpods-eartips", PriceAmount: 150_000},
}

// TestSimulatorReproducible proves the fixed-seed dataset is identical
// across runs.
func TestSimulatorReproducible(t *testing.T) {
	a := NewMerchantSimulator(42).Generate(testCatalog)
	b := NewMerchantSimulator(42).Generate(testCatalog)

	if len(a) != 50000 || len(b) != 50000 {
		t.Fatalf("expected 50000 sessions, got %d and %d", len(a), len(b))
	}

	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("session %d differs between runs", i)
		}
	}
}

// TestSimulatorEmptyProductsReturnsNil proves Generate fails safe (nil,
// not a panic) when handed an empty catalog -- e.g. before the catalog
// has been seeded.
func TestSimulatorEmptyProductsReturnsNil(t *testing.T) {
	sessions := NewMerchantSimulator(42).Generate(nil)
	if sessions != nil {
		t.Fatalf("expected nil sessions for empty product list, got %d sessions", len(sessions))
	}
}
