package agents

import (
	"context"
	"strings"
)

// DeterministicExtractor is a test/fallback IntentExtractor that parses
// the demo prompt shape without an LLM. In production this is replaced
// by an LLM provider that emits strict structured output, which is then
// validated by ParseIntentJSON before anything else touches it.
type DeterministicExtractor struct{}

func NewDeterministicExtractor() *DeterministicExtractor {
	return &DeterministicExtractor{}
}

func (d *DeterministicExtractor) Extract(ctx context.Context, prompt string) (Intent, error) {
	lower := strings.ToLower(prompt)

	// Ambiguous input → safe no-op, never a guess.
	if strings.Contains(lower, "buy me something") ||
		strings.TrimSpace(prompt) == "" ||
		!strings.Contains(lower, "budget") {
		return Intent{Clarify: "What would you like to buy, and what is your budget?"}, nil
	}

	budget := parseBudget(prompt)
	category := parseCategory(lower)
	priority := parsePriority(lower)
	recipient := parseRecipient(lower)

	intent := Intent{
		Budget:    budget,
		Category:  category,
		Priority:  priority,
		Recipient: recipient,
	}

	if err := ValidateIntent(intent); err != nil {
		return Intent{}, err
	}

	return intent, nil
}

func parseBudget(prompt string) int64 {
	// Find the first run of digits (handles the multi-byte ₹ symbol
	// and thousands separators).
	digits := ""

	for i := 0; i < len(prompt); i++ {
		c := prompt[i]
		if c >= '0' && c <= '9' {
			digits += string(c)
		} else if c == ',' && digits != "" {
			continue
		} else if digits != "" {
			break
		}
	}

	if digits == "" {
		return 0
	}

	return int64(parseInt(digits))
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func parseCategory(lower string) string {
	switch {
	case strings.Contains(lower, "earbud") || strings.Contains(lower, "headphone"):
		return "earbuds"
	case strings.Contains(lower, "laptop"):
		return "laptop"
	// "charging" is a real use_cases tag on wireless-charging-pad,
	// magsafe-charger, and lightning-usbc-cable (db/seeds/001_catalog.sql)
	// -- without this, "I need a charger" could never rank any of them
	// above an unrelated product on category match.
	case strings.Contains(lower, "charger") || strings.Contains(lower, "charging pad") || strings.Contains(lower, "cable"):
		return "charging"
	// "accessories" is the shared use_cases tag for applecare, the
	// usb-c-adapter, and the AirPods ear tips -- same reasoning as above,
	// just for the rest of the catalog "case" alone couldn't reach.
	case strings.Contains(lower, "case") || strings.Contains(lower, "adapter") ||
		strings.Contains(lower, "warranty") || strings.Contains(lower, "applecare") ||
		strings.Contains(lower, "ear tip") || strings.Contains(lower, "eartip"):
		return "accessories"
	default:
		return ""
	}
}

func parsePriority(lower string) string {
	// More specific two-word phrases are checked first so they can't be
	// shadowed by a later single-word check that happens to be a substring
	// of one of them (none currently collide, but this keeps it that way).
	switch {
	case strings.Contains(lower, "noise isolation"):
		return "noise_isolation"
	case strings.Contains(lower, "noise cancellation"):
		return "active_noise_cancellation"
	case strings.Contains(lower, "wireless charging"):
		return "wireless_charging"
	case strings.Contains(lower, "fast charging"):
		return "fast_charging"
	case strings.Contains(lower, "spatial audio"):
		return "spatial_audio"
	case strings.Contains(lower, "battery"):
		return "battery_life"
	case strings.Contains(lower, "magnetic"):
		return "magnetic_alignment"
	case strings.Contains(lower, "braided"):
		return "durable_braided"
	case strings.Contains(lower, "comfort"):
		return "comfort_fit"
	case strings.Contains(lower, "warranty") || strings.Contains(lower, "support"):
		return "extended_warranty"
	default:
		return ""
	}
}

func parseRecipient(lower string) string {
	if strings.Contains(lower, "sister") {
		return "sister"
	}
	if strings.Contains(lower, "brother") {
		return "brother"
	}
	return ""
}
