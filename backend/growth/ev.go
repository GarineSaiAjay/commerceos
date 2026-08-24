package growth

// EVInputs are the deterministic inputs to the expected-value formula.
// These come from historical data / heuristics — never from the LLM.
type EVInputs struct {
	PurchaseProbability float64 // P(purchase | recommendation)
	IncrementalMargin   int64   // in paise
	Confidence          float64
	RiskCost            int64 // in paise
}

// ExpectedValue computes the deterministic expected incremental revenue:
//
//	EV = P(purchase) × incremental_margin × confidence − risk_cost
//
// Pure function — no LLM call, no hidden fudge factor.
func ExpectedValue(in EVInputs) float64 {
	return in.PurchaseProbability*float64(in.IncrementalMargin)*in.Confidence - float64(in.RiskCost)
}

// Candidate is a scored cross-sell candidate.
type Candidate struct {
	ProductID string
	Price     int64
	EV        float64
	Inputs    EVInputs
}

// SelectBest returns the argmax-EV candidate. Deterministic.
func SelectBest(candidates []Candidate) (Candidate, bool) {
	if len(candidates) == 0 {
		return Candidate{}, false
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.EV > best.EV {
			best = c
		}
	}

	return best, true
}
