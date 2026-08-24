package policy

// RiskEngine computes a first-pass risk score (expanded in Phase 8).
// Simple weighted heuristic: higher amount, unknown merchant, and
// budget pressure raise the score.
type RiskEngine struct{}

func NewRiskEngine() *RiskEngine {
	return &RiskEngine{}
}

// Score returns a risk score in [0, 1].
func (r *RiskEngine) Score(
	amount int64,
	merchant string,
	budgetMax int64,
) float64 {
	score := 0.0

	// Amount factor: up to 0.4 for amounts near the ceiling.
	score += 0.4 * (float64(amount) / 3_000_000.0)

	// Merchant factor: unknown merchant adds 0.5.
	if merchant != "merchant_001" {
		score += 0.5
	}

	// Budget pressure: approaching the mandate ceiling adds up to 0.1.
	if budgetMax > 0 {
		ratio := float64(amount) / float64(budgetMax)
		if ratio > 0.9 {
			score += 0.1
		}
	}

	if score > 1.0 {
		return 1.0
	}

	return score
}
