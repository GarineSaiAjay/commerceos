package growth

// BudgetCheck is the deterministic budget-aware eligibility check.
// The tolerance is decided by an LLM classification (a validated
// boolean/percentage); the arithmetic comparison is pure code.
type BudgetCheck struct {
	CartTotal int64
	Budget    int64
	Tolerance float64 // 0.0 = none, 0.10 = +10%
}

// MaxAllowed returns budget × (1 + tolerance).
func (b BudgetCheck) MaxAllowed() int64 {
	return b.Budget + int64(float64(b.Budget)*b.Tolerance)
}

// Eligible reports whether newTotal ≤ max allowed.
func (b BudgetCheck) Eligible(newTotal int64) bool {
	return newTotal <= b.MaxAllowed()
}

// MarginAvailable is budget − cart total (before tolerance).
func (b BudgetCheck) MarginAvailable() int64 {
	return b.Budget - b.CartTotal
}
