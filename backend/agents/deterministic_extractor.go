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

	// "buy me something" (with nothing else to go on) and an empty
	// prompt are always ambiguous, regardless of anything else.
	if strings.Contains(lower, "buy me something") || strings.TrimSpace(prompt) == "" {
		return Intent{Clarify: "What would you like to buy, and what is your budget?", Source: sourceDeterministic}, nil
	}

	budget := parseBudget(prompt)
	category := parseCategory(lower)
	priority := parsePriority(lower)
	recipient := parseRecipient(lower)

	// A real budget AND a real category are the two hard requirements
	// (see ValidateIntent) -- ask for clarification if either is
	// missing, rather than failing validation with a raw error below.
	//
	// This USED to instead require the literal substring "budget"
	// anywhere in the prompt, which rejected extremely common real
	// phrasing that expresses a budget without ever using that word --
	// "i want earbuds for my bro, under 40k" has both a clear budget
	// (40k) and a clear category (earbuds), but was clarified away
	// before parseBudget/parseCategory ever even ran, because the word
	// "budget" itself never appears. Checking the actually-extracted
	// values instead of a magic word makes this robust to "under X",
	// "below X", "less than X", "max X", "up to X", and so on.
	if budget <= 0 || category == "" {
		// Partial fields (e.g. a recognized recipient or priority) are
		// still returned alongside Clarify, not discarded. A single-shot
		// PlanCheckout call never looks past Clarify != "" so this is
		// behavior-neutral there; BuyerAgent.PlanCheckoutInConversation
		// (conversation memory, PLAN-01-AGENTIC-CORE.md §3) is what
		// actually uses them -- it's what lets "no, for my brother
		// instead" contribute its recognized recipient to the merged
		// intent instead of that information being thrown away here
		// before memory ever gets a chance to use it.
		return Intent{
			Budget:    budget,
			Category:  category,
			Priority:  priority,
			Recipient: recipient,
			Clarify:   "What would you like to buy, and what is your budget?",
			Source:    sourceDeterministic,
		}, nil
	}

	intent := Intent{
		Budget:    budget,
		Category:  category,
		Priority:  priority,
		Recipient: recipient,
		Source:    sourceDeterministic,
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
	// "30k"/"30K" is extremely common colloquial budget shorthand for
	// 30,000 -- without this, "my budget is below 30k" silently parsed
	// as a budget of 30 rupees, which then matched nothing in the
	// catalog (or the wrong thing) instead of failing loudly.
	thousands := false

	for i := 0; i < len(prompt); i++ {
		c := prompt[i]
		if c >= '0' && c <= '9' {
			digits += string(c)
		} else if c == ',' && digits != "" {
			continue
		} else if digits != "" {
			if c == 'k' || c == 'K' {
				thousands = true
			}
			break
		}
	}

	if digits == "" {
		return 0
	}

	budget := int64(parseInt(digits))
	if thousands {
		budget *= 1000
	}
	return budget
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
	// "laptop" category had zero real products until the 100-product
	// catalog expansion added MacBook/laptop accessories (sleeves,
	// stands, hubs, Magic Keyboard/Mouse/Trackpad, docking stations,
	// external SSDs, webcams, ...) -- these extra keywords are what a
	// buyer naturally types instead of the literal word "laptop" itself,
	// same reasoning as the earbuds brand-name case further below.
	case strings.Contains(lower, "laptop") || strings.Contains(lower, "macbook") ||
		strings.Contains(lower, "magic keyboard") || strings.Contains(lower, "magic mouse") ||
		strings.Contains(lower, "trackpad") || strings.Contains(lower, "docking station") ||
		strings.Contains(lower, "external ssd") || strings.Contains(lower, "webcam") ||
		strings.Contains(lower, "usb-c hub") || strings.Contains(lower, "usb c hub"):
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
	// Checked before the earbuds/brand-name case below: "ear tips for my
	// AirPods" must resolve to accessories, not earbuds, even though it
	// mentions "AirPods".
	case strings.Contains(lower, "case") || strings.Contains(lower, "adapter") ||
		strings.Contains(lower, "warranty") || strings.Contains(lower, "applecare") ||
		strings.Contains(lower, "ear tip") || strings.Contains(lower, "eartip"):
		return "accessories"
	// "tracking" is airtag-4pack's use_cases tag -- the catalog's first
	// non-audio product, added alongside airpods-pro-3 and beats-fit-pro
	// (db/seeds/001_catalog.sql) so "something to track my luggage" or
	// "find my keys" resolves to a real product instead of nothing.
	case strings.Contains(lower, "airtag") || strings.Contains(lower, "tracker") ||
		strings.Contains(lower, "track my") || strings.Contains(lower, "find my"):
		return "tracking"
	// Every earbuds SKU in the catalog is sold under a product-family
	// name ("AirPods ...", "Beats Fit Pro") that a buyer will naturally
	// type instead of the generic word "earbuds"/"headphones" -- e.g.
	// "i want beats fit pro for my sister" previously extracted an
	// empty category and was rejected with "invalid intent: category
	// required" even though budget and recipient were both given
	// correctly. Checked last (not first) because it's the broadest
	// match -- a mention of "AirPods" in an otherwise
	// accessory/charging/tracking request (an AirPods case, an AirTag)
	// must resolve to that more specific category instead.
	case strings.Contains(lower, "earbud") || strings.Contains(lower, "headphone") ||
		strings.Contains(lower, "airpods") || strings.Contains(lower, "airpod") ||
		strings.Contains(lower, "beats") || strings.Contains(lower, "buds"):
		return "earbuds"
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
	// secure_fit/sweat_resistant are beats-fit-pro's distinguishing
	// features (db/seeds/001_catalog.sql) -- without this, a workout/gym
	// request would score identically against every earbuds SKU instead
	// of preferring the one actually built for that use case.
	case strings.Contains(lower, "workout") || strings.Contains(lower, "gym") ||
		strings.Contains(lower, "exercise") || strings.Contains(lower, "run") ||
		strings.Contains(lower, "sweat"):
		return "secure_fit"
	// heart_rate_sensing is airpods-pro-3's distinguishing feature over
	// the otherwise-identical airpods-pro-2 (same price, same ANC).
	case strings.Contains(lower, "heart rate"):
		return "heart_rate_sensing"
	default:
		return ""
	}
}

func parseRecipient(lower string) string {
	if strings.Contains(lower, "sister") {
		return "sister"
	}
	// "bro" is extremely common shorthand for "brother" in a casual
	// shopping request ("earbuds for my bro, under 40k") -- without it,
	// a prompt naming no recipient word from the LLM's declared enum
	// (see llm_extractor.go's intentSystemPrompt) simply left Recipient
	// blank, which isn't validated so it never broke anything, but did
	// throw away information the buyer clearly gave.
	if strings.Contains(lower, "brother") || strings.Contains(lower, "bro") {
		return "brother"
	}
	return ""
}
