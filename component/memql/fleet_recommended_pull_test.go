package memql

import (
	"strings"
	"testing"
)

// The act's own decisions, as pure functions over the rows it reads. What is
// NOT tested here is the engine round trip -- planModelPull's refusals are
// covered in fleet_model_pull_test.go and RecommendedSet's ordering in
// fleet_recommend_test.go, and re-asserting either through a fake engine would
// record a query string rather than exercise a decision.

func TestCatalogRowParsingKeepsWhatTheSetNeeds(t *testing.T) {
	// The projection from a catalog row to a CatalogProfile is a field list, and
	// a field list is exactly the kind of code that goes wrong by omission --
	// a name dropped here does not error, it reads as zero, and a zero
	// minMachineClass makes a profile unrecommendable on every machine while a
	// zero params makes it sort last on all of them.
	rows := []map[string]any{{
		"modelId":         "qwen3.5:9b",
		"category":        "text",
		"runtime":         "ollama",
		"family":          "qwen3.5",
		"minMachineClass": "24",
		"recommendedFor":  []any{"fast", "strong"},
		"offeredOn":       []any{"macos", "linux"},
		"flags":           []any{"structured", "tools"},
		"params":          float64(9_000_000_000),
		"contextWindow":   float64(128000),
		"sizeBytes":       float64(5_400_000_000),
		"notes":           "the balanced pick",
	}}
	got := catalogProfilesFromRows(rows)
	if len(got) != 1 {
		t.Fatalf("expected one profile, got %d", len(got))
	}
	p := got[0]
	if p.ModelId != "qwen3.5:9b" || p.Runtime != "ollama" || p.MinMachineClass != "24" {
		t.Fatalf("scalar fields: %+v", p)
	}
	if p.Params != 9_000_000_000 || p.ContextWindow != 128000 || p.SizeBytes != 5_400_000_000 {
		t.Fatalf("numeric fields: params=%d ctx=%d size=%d", p.Params, p.ContextWindow, p.SizeBytes)
	}
	if len(p.RecommendedFor) != 2 || len(p.OfferedOn) != 2 || len(p.Flags) != 2 {
		t.Fatalf("list fields: %+v", p)
	}
}

func TestAnInactiveOrUnavailableCatalogEntryIsNotRecommended(t *testing.T) {
	// Both are SOFT states the catalog keeps on purpose -- the row stays so the
	// next operator can read why an id disappeared -- and neither is something
	// to pull. A removed entry that came back as a recommendation would undo
	// the operator's decision on the next page load.
	rows := []map[string]any{
		{"modelId": "retired:7b", "active": false},
		{"modelId": "gone:7b", "unavailable": true},
		{"modelId": "fine:7b"},
	}
	got := catalogProfilesFromRows(rows)
	if len(got) != 1 || got[0].ModelId != "fine:7b" {
		t.Fatalf("expected only the live entry, got %+v", got)
	}
}

func TestACatalogRowWithNoActiveKeyIsActive(t *testing.T) {
	// `active` defaults true on the concept, and a row written before the flag
	// existed has no key. Reading a missing key as false would empty the
	// catalog rather than fail, which is the shape of outage nobody diagnoses:
	// every machine reports "nothing recommended" and every machine is fine.
	got := catalogProfilesFromRows([]map[string]any{{"modelId": "fine:7b"}})
	if len(got) != 1 {
		t.Fatalf("a row with no `active` key must be active, got %+v", got)
	}
}

func TestBlockedSummaryNamesOneReasonRatherThanFour(t *testing.T) {
	// A person with an empty set has one thing to fix first. Four sentences
	// saying the machine is too small for four different models is one fact
	// repeated, and a wall of text is read as noise rather than as an answer.
	got := blockedSummary([]map[string]any{
		{"modelId": "a", "reason": "Needs the Ollama runtime, which this machine has not reported."},
		{"modelId": "b", "reason": "Needs a 64 GB machine; this one is class 24."},
	})
	if !strings.Contains(got, "Ollama") {
		t.Fatalf("the summary must be the first (most fixable) reason, got %q", got)
	}
	if strings.Contains(got, "64 GB") {
		t.Fatalf("the summary must not concatenate every reason, got %q", got)
	}
}

func TestBlockedSummaryOfNothingSaysTheCatalogIsEmptyForThisClass(t *testing.T) {
	// The other way to have nothing to pull: the machine is fine and the
	// catalog simply recommends nothing at its class. That is not a blocked
	// entry and must not be worded as one.
	got := blockedSummary(nil)
	if !strings.Contains(got, "recommends nothing") {
		t.Fatalf("got %q", got)
	}
}
