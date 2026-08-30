package growth

import "testing"

// TestHeuristicRatingAdjustmentIsNeutralWhenUnrated proves an unrated
// candidate (average_rating 0, i.e. review_count 0 -- catalog.Product's
// zero value) gets no probability adjustment at all. Absence of review
// data is not evidence of a bad product, so a brand-new accessory must
// never be scored worse than a middling-rated one just for lacking
// reviews yet.
func TestHeuristicRatingAdjustmentIsNeutralWhenUnrated(t *testing.T) {
	if got := heuristicRatingAdjustment(0); got != 0 {
		t.Fatalf("expected 0 adjustment for an unrated candidate, got %v", got)
	}
}

// TestHeuristicRatingAdjustmentRewardsAboveNeutral proves a
// better-than-neutral rating (PLAN-02-CATALOG-AND-COMMERCE.md §2's own
// example: 4.8 vs 3.1) pushes the estimate up, and a worse one pushes
// it down, by a small, deterministic, symmetric amount.
func TestHeuristicRatingAdjustmentRewardsAboveNeutral(t *testing.T) {
	good := heuristicRatingAdjustment(4.8)
	bad := heuristicRatingAdjustment(3.1)

	if good <= 0 {
		t.Fatalf("expected a positive adjustment for a 4.8 rating, got %v", good)
	}
	if bad >= 0 {
		t.Fatalf("expected a negative adjustment for a 3.1 rating, got %v", bad)
	}
	if good <= bad {
		t.Fatalf("expected the 4.8-rated adjustment (%v) to exceed the 3.1-rated one (%v)", good, bad)
	}
}

// TestHeuristicEVInputsRatingBreaksOverlapTies proves the actual,
// end-to-end point of item 11: at equal tag-overlap score, a
// higher-rated candidate gets a higher purchase-probability estimate --
// "a 4.8-rated accessory is a more defensible suggestion than a
// 3.1-rated one at equal tag overlap" (PLAN-02 §2), not just a rating
// number that goes unused.
func TestHeuristicEVInputsRatingBreaksOverlapTies(t *testing.T) {
	highRated := heuristicEVInputs(100_000, 2, 4.8)
	lowRated := heuristicEVInputs(100_000, 2, 3.1)
	unrated := heuristicEVInputs(100_000, 2, 0)

	if highRated.PurchaseProbability <= unrated.PurchaseProbability {
		t.Fatalf("expected the 4.8-rated candidate's probability (%v) to exceed the unrated one's (%v)",
			highRated.PurchaseProbability, unrated.PurchaseProbability)
	}
	if unrated.PurchaseProbability <= lowRated.PurchaseProbability {
		t.Fatalf("expected the unrated candidate's probability (%v) to exceed the 3.1-rated one's (%v)",
			unrated.PurchaseProbability, lowRated.PurchaseProbability)
	}

	// IncrementalMargin/Confidence/RiskCost are untouched by rating --
	// only PurchaseProbability should move.
	if highRated.IncrementalMargin != unrated.IncrementalMargin || highRated.Confidence != unrated.Confidence {
		t.Fatalf("expected only PurchaseProbability to vary with rating, got %+v vs %+v", highRated, unrated)
	}
}

// TestHeuristicEVInputsProbabilityStaysInBounds proves the combined
// overlap + rating adjustment can never push PurchaseProbability
// outside [0, heuristicMaxProbability] -- an extreme overlap score
// plus a top rating must still cap, and a zero overlap plus the worst
// possible rating must never go negative.
func TestHeuristicEVInputsProbabilityStaysInBounds(t *testing.T) {
	high := heuristicEVInputs(100_000, 10, 5.0)
	if high.PurchaseProbability > heuristicMaxProbability {
		t.Fatalf("expected probability capped at %v, got %v", heuristicMaxProbability, high.PurchaseProbability)
	}

	low := heuristicEVInputs(100_000, 0, 1.0)
	if low.PurchaseProbability < 0 {
		t.Fatalf("expected probability floored at 0, got %v", low.PurchaseProbability)
	}
}
