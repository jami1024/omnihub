package provider

import "testing"

func TestApplyModelRedirects(t *testing.T) {
	rules := []ModelRedirect{
		{MatchType: MatchExact, Source: "gpt-4o", Target: "gpt-4o-mini"},
		{MatchType: MatchPrefix, Source: "claude-3-", Target: "claude-3-5-sonnet"},
		{MatchType: MatchSuffix, Source: "-preview", Target: "stable"},
		{MatchType: MatchContains, Source: "turbo", Target: "fast"},
		{MatchType: MatchRegex, Source: `^o(\d)-mini$`, Target: "o$1"},
	}

	cases := []struct {
		model   string
		want    string
		matched bool
	}{
		{"gpt-4o", "gpt-4o-mini", true},               // exact
		{"claude-3-haiku", "claude-3-5-sonnet", true}, // prefix
		{"gemini-preview", "stable", true},            // suffix
		{"some-turbo-x", "fast", true},                // contains
		{"o3-mini", "o3", true},                       // regex capture
		{"unknown-model", "unknown-model", false},
	}
	for _, tc := range cases {
		got, ok := ApplyModelRedirects(rules, tc.model)
		if got != tc.want || ok != tc.matched {
			t.Errorf("ApplyModelRedirects(%q) = (%q,%v), want (%q,%v)",
				tc.model, got, ok, tc.want, tc.matched)
		}
	}
}

func TestModelRedirectValid(t *testing.T) {
	valid := []ModelRedirect{
		{MatchType: MatchExact, Source: "a", Target: "b"},
		{MatchType: MatchRegex, Source: `^x(\d+)$`, Target: "y$1"},
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected valid: %+v", r)
		}
	}
	invalid := []ModelRedirect{
		{MatchType: MatchExact, Source: "", Target: "b"},       // empty source
		{MatchType: MatchExact, Source: "a", Target: ""},       // empty target
		{MatchType: "fuzzy", Source: "a", Target: "b"},         // unknown type
		{MatchType: MatchRegex, Source: `^x(\d+`, Target: "y"}, // bad regex
	}
	for _, r := range invalid {
		if r.Valid() {
			t.Errorf("expected invalid: %+v", r)
		}
	}
}

// First-match-wins ordering: an earlier broad rule shadows a later one.
func TestApplyModelRedirectsFirstMatchWins(t *testing.T) {
	rules := []ModelRedirect{
		{MatchType: MatchContains, Source: "gpt", Target: "first"},
		{MatchType: MatchExact, Source: "gpt-4o", Target: "second"},
	}
	if got, _ := ApplyModelRedirects(rules, "gpt-4o"); got != "first" {
		t.Errorf("expected first-match-wins 'first', got %q", got)
	}
}
