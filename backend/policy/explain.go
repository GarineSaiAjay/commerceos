package policy

import "fmt"

// ExplainRejection renders a plain-language explanation for a failed
// policy check. Every rejection in the system uses this shared function.
func ExplainRejection(failedCheck string, action ProposedAction, mandate Mandate) string {
	switch failedCheck {
	case CheckMerchantAllowlisted:
		return fmt.Sprintf(
			"The merchant %s is not allowlisted. I did not proceed, and no payment action was attempted.",
			action.Merchant,
		)

	case CheckCurrencyAllowed:
		return fmt.Sprintf(
			"The currency %s is not supported. I did not proceed, and no payment action was attempted.",
			action.Currency,
		)

	case CheckAmountCeiling:
		return fmt.Sprintf(
			"The amount ₹%d exceeds the configured ceiling of ₹%d. I did not proceed, and no payment action was attempted.",
			action.Amount,
			3_000_000,
		)

	case CheckProductPermitted:
		return fmt.Sprintf(
			"One of the requested items is not permitted. I did not proceed, and no payment action was attempted.",
		)

	case CheckBudgetTolerance:
		return fmt.Sprintf(
			"The cart total ₹%s exceeds your authorized maximum of ₹%s (difference ₹%s). I did not add it, and no payment action was attempted.",
			formatINR(action.Amount),
			formatINR(mandate.MaximumAmount),
			formatINR(action.Amount-mandate.MaximumAmount),
		)

	case CheckMandateNotExpired:
		return fmt.Sprintf(
			"Your mandate has expired or is no longer active. I did not proceed, and no payment action was attempted.",
		)

	case CheckMandateBound:
		return fmt.Sprintf(
			"The mandate is no longer bound to this cart/merchant. I did not proceed, and no payment action was attempted.",
		)

	default:
		return "The action was rejected by policy. No payment action was attempted."
	}
}

// formatINR inserts thousands separators into a paise amount (₹26,900).
func formatINR(amount int64) string {
	s := fmt.Sprintf("%d", amount)
	if len(s) <= 3 {
		return s
	}

	// Indian grouping: last 3 digits, then groups of 2.
	out := s[len(s)-3:]
	s = s[:len(s)-3]
	for len(s) > 2 {
		out = s[len(s)-2:] + "," + out
		s = s[:len(s)-2]
	}
	if len(s) > 0 {
		out = s + "," + out
	}
	return out
}
