package agents

import (
	"regexp"
	"strings"

	"github.com/garinesaiajay/commerceos/tools"
)

// notPattern matches an explicit "not X" / "not the X" correction in a
// buyer's own words, capturing X up to the next sentence-ending
// punctuation or the end of the prompt.
//
// This is what turns "i want a airtag not airtag anti lost strap" into
// a hard exclusion instead of silently vanishing: neither extractor's
// output schema (Intent's budget/category/priority/recipient) had
// anywhere to put a correction like this, so a buyer explicitly
// rejecting a pick got the exact same wrong pick back untouched -- the
// bug reported live against this agent. See tools.SearchFilter.Exclude
// and tools/search.go's excludesProduct for how the captured phrase is
// matched against catalog titles.
//
// Deliberately narrow: only "not"/"not the", never a bare "no". "no"
// alone collides with too many ordinary phrasings that aren't a
// product correction at all ("no budget limit", "no rush", "no
// thanks") -- a false-positive exclusion silently dropping an unrelated
// product from the candidate list is a worse failure than
// under-detecting a correction the buyer can just repeat more
// explicitly ("not the strap").
var notPattern = regexp.MustCompile(`(?i)\bnot\s+(?:the\s+)?([a-z0-9][a-z0-9 \-]*?)(?:[.,!?;]|$)`)

// ParseExclusions extracts every "not X" phrase from a buyer's raw
// prompt. BuyerAgent applies this uniformly to whichever extractor's
// Intent came back (deterministic or LLM) -- negation handling is a
// catalog-agnostic operation on the buyer's own words, not something
// either extractor's structured-output schema needs to know about, so
// it lives here instead of being duplicated into both extractors (and
// into the LLM's system prompt, which would also mean trusting an LLM
// to reliably honor it).
func ParseExclusions(prompt string) []string {
	matches := notPattern.FindAllStringSubmatch(prompt, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		phrase := strings.TrimSpace(m[1])
		phrase = trimTrailingFiller(phrase)
		if phrase != "" {
			out = append(out, phrase)
		}
	}
	return out
}

// trailingFillerWords are conversational words that sometimes follow a
// "not X" correction ("not the strap please", "not the case thanks") --
// part of the buyer's sentence, but not part of the product phrase
// being excluded. notPattern's capture group is unbounded at the end of
// the prompt (it stops at sentence punctuation OR end-of-string, and a
// trailing filler word has neither after it), so without this trim
// "not the anti-lost strap please" would capture "anti-lost strap
// please" as the excluded phrase instead of "anti-lost strap".
var trailingFillerWords = map[string]bool{
	"please": true,
	"thanks": true,
	"thank":  true,
	"you":    true,
	"ok":     true,
	"okay":   true,
}

// trimTrailingFiller removes trailing filler words (see
// trailingFillerWords) from phrase one at a time, stopping once a
// single word remains so a phrase that is nothing but filler ("not
// please") is never trimmed down to empty.
func trimTrailingFiller(phrase string) string {
	words := strings.Fields(phrase)
	end := len(words)
	for end > 1 && trailingFillerWords[strings.ToLower(strings.Trim(words[end-1], ".,!?;:()\"'"))] {
		end--
	}
	return strings.Join(words[:end], " ")
}

// ExtractTerms pulls the buyer's own significant words out of the raw
// prompt for Intent.Terms / tools.SearchFilter.Terms -- see
// accessoryQualifiers' doc comment in tools/search.go for what this
// grounds. Delegates to tools.SignificantWords so the two packages
// share one tokenizer/stopword list instead of drifting apart, since
// tools/search.go independently needs the same tokenizer to match
// Exclude phrases against catalog titles.
func ExtractTerms(prompt string) []string {
	return tools.SignificantWords(prompt)
}

// mergeUnique appends items from next that base doesn't already contain
// (case-insensitively), preserving base's order, and always returns a
// freshly allocated slice so callers never risk mutating a slice a
// ConversationStore snapshot still holds a reference to.
//
// Used by mergeIntent to accumulate Intent.Exclude and Intent.Terms
// across conversation turns -- see mergeIntent's doc comment for why
// those two fields merge by union instead of "next wins" like Budget,
// Category, Priority, and Recipient.
func mergeUnique(base []string, next []string) []string {
	if len(next) == 0 {
		if base == nil {
			return nil
		}
		out := make([]string, len(base))
		copy(out, base)
		return out
	}

	seen := make(map[string]bool, len(base)+len(next))
	merged := make([]string, 0, len(base)+len(next))
	for _, b := range base {
		key := strings.ToLower(b)
		if !seen[key] {
			seen[key] = true
			merged = append(merged, b)
		}
	}
	for _, n := range next {
		key := strings.ToLower(n)
		if !seen[key] {
			seen[key] = true
			merged = append(merged, n)
		}
	}
	return merged
}
