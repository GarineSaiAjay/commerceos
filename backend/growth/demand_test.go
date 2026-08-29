package growth

import "testing"

func TestReinstatableCountNoDiscountNoneClear(t *testing.T) {
	rows := []evaluationContext{
		{CartTotal: 28_000_00, Budget: 30_000_00, Price: 2_490_00}, // 30,490 > 30,000
	}
	if got := reinstatableCount(rows, 0); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func TestReinstatableCountDiscountClearsSome(t *testing.T) {
	// All three rows get the SAME 15%-off hypothetical applied (that's
	// what a single reinstatableCount call means: "if this discount had
	// existed, how many of these rejections would have cleared?").
	rows := []evaluationContext{
		// 28,000 + 15% off 2,490 (=2,116.50, paise-truncated to 2,116.50
		// exactly since 2,490*85 divides evenly) = 30,116.50 > 30,000 --
		// still rejected even at 15% off.
		{CartTotal: 28_000_00, Budget: 30_000_00, Price: 2_490_00},
		// 25,000 + 15% off 2,490 (=2,116.50) = 27,116.50 <= 30,000 -- clears.
		{CartTotal: 25_000_00, Budget: 30_000_00, Price: 2_490_00},
		// 20,000 + 15% off 1,299 (=1,104.15 -> 1,104 int division) = 21,104 <= 30,000 -- clears.
		{CartTotal: 20_000_00, Budget: 30_000_00, Price: 1_299_00},
	}
	if got := reinstatableCount(rows, 15); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}

func TestReinstatableCountFullDiscountClearsWhenPriceAloneFitsBudget(t *testing.T) {
	rows := []evaluationContext{
		{CartTotal: 29_000_00, Budget: 30_000_00, Price: 500_00},
	}
	// 100% off: discounted price is 0, so cartTotal (29,000) <= budget (30,000) clears.
	if got := reinstatableCount(rows, 100); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
}

func TestReinstatableCountEmptyRows(t *testing.T) {
	if got := reinstatableCount(nil, 15); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}
