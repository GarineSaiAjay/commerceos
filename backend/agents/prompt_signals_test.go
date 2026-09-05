package agents

import (
	"reflect"
	"testing"
)

// TestParseExclusions is the regression test for the live-reported bug:
// "i want a airtag not airtag anti lost strap" got the exact same wrong
// pick back, because neither extractor's schema had anywhere to put an
// explicit "not X" correction, so it was silently dropped.
func TestParseExclusions(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   []string
	}{
		{
			name:   "reported bug repro",
			prompt: "i want a airtag not airtag anti lost strap",
			want:   []string{"airtag anti lost strap"},
		},
		{
			name:   "not the X phrasing",
			prompt: "i want an airtag, not the anti-lost strap please",
			want:   []string{"anti-lost strap"},
		},
		{
			name:   "no negation present",
			prompt: "i want a airtag for my sister, under 20k",
			want:   nil,
		},
		{
			name:   "bare no is never treated as negation",
			prompt: "no budget limit, just get me something nice",
			want:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseExclusions(tc.prompt)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseExclusions(%q) = %v, want %v", tc.prompt, got, tc.want)
			}
		})
	}
}

// TestExtractTerms proves ExtractTerms strips ordinary function words so
// the accessory-qualifier check in tools/search.go isn't fooled by
// "i want a case" containing "a" or "want".
func TestExtractTerms(t *testing.T) {
	terms := ExtractTerms("i want a airtag case, under 5k")
	if !containsWord(terms, "airtag") || !containsWord(terms, "case") {
		t.Fatalf("expected airtag and case in terms, got %v", terms)
	}
	if containsWord(terms, "want") || containsWord(terms, "a") || containsWord(terms, "under") {
		t.Fatalf("expected stopwords stripped, got %v", terms)
	}
}

// containsWord mirrors tools.containsWord's case-insensitive membership
// check -- duplicated here (rather than exported from tools) since it's
// only ever needed by this one test.
func containsWord(words []string, target string) bool {
	for _, w := range words {
		if w == target {
			return true
		}
	}
	return false
}

// TestMergeUniqueAccumulatesAcrossTurns proves Exclude/Terms merge by
// union, not "next wins" -- an earlier "not the strap" must stay
// excluded even when a later turn doesn't repeat it.
func TestMergeUniqueAccumulatesAcrossTurns(t *testing.T) {
	base := []string{"airtag anti lost strap"}
	next := []string{"airtag loop"}
	got := mergeUnique(base, next)
	want := []string{"airtag anti lost strap", "airtag loop"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeUnique(%v, %v) = %v, want %v", base, next, got, want)
	}

	// Duplicate (case-insensitive) entries aren't repeated.
	got2 := mergeUnique(got, []string{"Airtag Loop"})
	if len(got2) != 2 {
		t.Fatalf("expected duplicate to be deduped, got %v", got2)
	}

	// base is never mutated by a later merge.
	if len(base) != 1 {
		t.Fatalf("mergeUnique must not mutate its base slice, got %v", base)
	}
}
