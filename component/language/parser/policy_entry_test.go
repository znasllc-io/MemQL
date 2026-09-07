package parser

import (
	"strings"
	"testing"
)

// TestValidatePolicyEntry_AcceptsEveryFormOfTheClosedGrammar walks the whole
// accepted surface. The grammar is CLOSED, so a form missing from this table is
// a form the loader will refuse, and the table is what says which is which.
func TestValidatePolicyEntry_AcceptsEveryFormOfTheClosedGrammar(t *testing.T) {
	for _, tc := range []struct {
		entry string
		why   string
	}{
		{"streamClaudeSonnet", "a bare name is a provider registry entry"},
		{"chat54Mini", "digits inside an identifier are legal"},

		{"fleet:strongest", "the strongest model on the person's own machines"},
		{"fleet:fastest", "the quickest one"},
		{"fleet:qwen3.8:27b", "a model id may itself contain a colon, which is why the split takes the FIRST one"},
		{"fleet:llama3.3-70b", "a model id carries dots and dashes; they are somebody else's naming convention"},

		{"app:*", "any signed-in local app that can run the call"},
		{"app:claude-code", "one named app"},
		{"app:codex", "the other one"},

		{"federation:cheapest", "the cheapest qualifying federated record"},
		{"federation:strongest", "the strongest one"},
		{"federation:streamClaudeSonnet", "one named federated provider"},

		{"policy:localFirst", "another policy, expanded at load"},
	} {
		if err := ValidatePolicyEntry(tc.entry); err != nil {
			t.Errorf("ValidatePolicyEntry(%q) refused it (%s): %v", tc.entry, tc.why, err)
		}
	}
}

// TestValidatePolicyEntry_RefusesEverythingElse is the other half: the forms
// that must NOT load.
//
// The failure this prevents is quiet. An unrecognised entry that passed would
// reach the router as a provider name, miss the registry, and be reported as
// "no such provider" -- which reads as a missing model rather than as the
// spelling mistake it is.
func TestValidatePolicyEntry_RefusesEverythingElse(t *testing.T) {
	for _, tc := range []struct {
		entry string
		why   string
	}{
		{"", "empty"},
		{"   ", "whitespace only"},

		{"fleet:", "a scheme with nothing after it names no model"},
		{"app:", "a scheme with nothing after it names no app"},
		{"federation:", "a scheme with nothing after it names no provider"},
		{"policy:", "a scheme with nothing after it names no policy"},

		{"cloud:cheapest", "there is no cloud door; the paid one is federation"},
		{"local:strongest", "the local door is spelled fleet"},

		// A model id may contain a colon, so `fleet:strongest:extra` would read
		// as a model literally named "strongest:extra" and fail at resolution
		// as "no such model". A selector word is reserved, so a colon after one
		// is a malformed selector rather than an id.
		{"fleet:strongest:extra", "a selector takes nothing after it"},
		{"fleet:fastest:7b", "the same, for the other selector"},

		{"9provider", "an identifier does not start with a digit"},
		{"provider name", "an identifier carries no space"},
		{"policy:local first", "a policy name is an identifier"},
		{"federation:local first", "a federated provider name is an identifier"},
		{"fleet:qwen 27b", "a model id carries no whitespace"},
	} {
		if err := ValidatePolicyEntry(tc.entry); err == nil {
			t.Errorf("ValidatePolicyEntry(%q) was accepted (%s); the grammar is closed", tc.entry, tc.why)
		}
	}
}

// TestValidatePolicyEntry_FleetStarIsRetiredAndSaysWhatToWrite pins the one
// refusal whose MESSAGE is load-bearing.
//
// An author who wrote fleet:* meant "the best local model". The spelling for
// that is fleet:strongest, and a refusal saying only "invalid entry" sends them
// to the engine source to work out what changed. The wildcard was retired
// because it could not say WHICH of a person's machines it meant -- the choice
// fell to whichever registered first.
func TestValidatePolicyEntry_FleetStarIsRetiredAndSaysWhatToWrite(t *testing.T) {
	err := ValidatePolicyEntry("fleet:*")
	if err == nil {
		t.Fatal("ValidatePolicyEntry(\"fleet:*\") was accepted; it is retired")
	}
	if !strings.Contains(err.Error(), "fleet:strongest") {
		t.Fatalf("the refusal does not name the replacement: %v", err)
	}
}

// TestValidatePolicyEntry_UnknownSchemeNamesTheAcceptedForms checks the other
// message a person reads. An error that says an entry is wrong without saying
// what right looks like costs a trip to the source every time.
func TestValidatePolicyEntry_UnknownSchemeNamesTheAcceptedForms(t *testing.T) {
	err := ValidatePolicyEntry("cloud:cheapest")
	if err == nil {
		t.Fatal("ValidatePolicyEntry(\"cloud:cheapest\") was accepted")
	}
	for _, want := range []string{"fleet:strongest", "app:*", "federation:cheapest", "policy:<name>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestIsSelectorEntry_DecomposesDoorsAndOnlyDoors pins the split between the
// two helper predicates.
//
// A `policy:` entry is deliberately NOT a selector: it is expanded away at
// load and the router never walks one. A caller that treated it as a door
// would try to resolve a provider literally named after the policy.
func TestIsSelectorEntry_DecomposesDoorsAndOnlyDoors(t *testing.T) {
	for _, tc := range []struct {
		entry        string
		wantScheme   string
		wantSelector string
	}{
		{"fleet:strongest", "fleet", "strongest"},
		{"fleet:qwen3.8:27b", "fleet", "qwen3.8:27b"},
		{"app:*", "app", "*"},
		{"federation:cheapest", "federation", "cheapest"},
	} {
		scheme, selector, ok := IsSelectorEntry(tc.entry)
		if !ok {
			t.Errorf("IsSelectorEntry(%q) reported not-a-selector", tc.entry)
			continue
		}
		if scheme != tc.wantScheme || selector != tc.wantSelector {
			t.Errorf("IsSelectorEntry(%q) = (%q, %q), want (%q, %q)",
				tc.entry, scheme, selector, tc.wantScheme, tc.wantSelector)
		}
	}

	for _, entry := range []string{"streamClaudeSonnet", "policy:localFirst", ""} {
		if _, _, ok := IsSelectorEntry(entry); ok {
			t.Errorf("IsSelectorEntry(%q) reported a selector; it is not a door", entry)
		}
	}
}

// TestIsPolicyEntry_NamesTheReferencedPolicy pins the composition half.
func TestIsPolicyEntry_NamesTheReferencedPolicy(t *testing.T) {
	name, ok := IsPolicyEntry("policy:localFirst")
	if !ok || name != "localFirst" {
		t.Fatalf("IsPolicyEntry(\"policy:localFirst\") = (%q, %v), want (localFirst, true)", name, ok)
	}

	for _, entry := range []string{"fleet:strongest", "app:*", "streamClaudeSonnet", "policy:", ""} {
		if _, ok := IsPolicyEntry(entry); ok {
			t.Errorf("IsPolicyEntry(%q) reported a policy reference", entry)
		}
	}
}
