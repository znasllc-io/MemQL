package conceptfields

import (
	"strings"
	"testing"
)

// The instrument, proved to move.
//
// Every case here plants ONE narrowing against a fixture and asserts it is
// reported, and every one has a control that must stay silent. Without the
// controls a rule that reported EVERY difference would pass all four positive
// cases while making the snapshot unusable -- which is the failure mode that
// matters most here, because a gate that fires on ordinary work gets a blanket
// waiver written for it within a week.

func before() Snapshot {
	return Snapshot{Concepts: []Entry{{
		Concept:  "v1:cluster:cluster",
		File:     "cluster/concepts.memql",
		Fields:   []string{"name", "provider", "status"},
		Required: []string{"name"},
		Enums:    map[string][]string{"provider": {"azure", "docker-local"}},
	}}}
}

func after(mutate func(*Entry)) Snapshot {
	s := before()
	mutate(&s.Concepts[0])
	return s
}

func onlyNarrowing(t *testing.T, got []Narrowing) Narrowing {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("want exactly 1 narrowing, got %d: %v", len(got), got)
	}
	return got[0]
}

func TestAFieldRemovalIsReported(t *testing.T) {
	got := Narrowings(before(), after(func(e *Entry) {
		e.Fields = []string{"name", "provider"}
	}))
	n := onlyNarrowing(t, got)
	if n.Kind != KindField || n.Field != "status" {
		t.Fatalf("want the status field reported, got %+v", n)
	}
	if !strings.Contains(n.String(), "no longer declares") {
		t.Errorf("the message must say what happened: %s", n.String())
	}
}

func TestAnEnumValueRemovalIsReported(t *testing.T) {
	// A stored row carrying the dropped value stops validating, and the
	// message JSON Schema produces points at the enum rather than at the row's
	// history -- so nothing about the failure says which release did it.
	got := Narrowings(before(), after(func(e *Entry) {
		e.Enums = map[string][]string{"provider": {"azure"}}
	}))
	n := onlyNarrowing(t, got)
	if n.Kind != KindEnumValue || n.Field != "provider" || n.Value != "docker-local" {
		t.Fatalf("want the dropped enum value reported, got %+v", n)
	}
}

func TestAFieldBecomingRequiredIsReported(t *testing.T) {
	// The read-merge fails the OTHER way round: rows written before the
	// annotation do not carry the field at all.
	got := Narrowings(before(), after(func(e *Entry) {
		e.Required = []string{"name", "provider"}
	}))
	n := onlyNarrowing(t, got)
	if n.Kind != KindRequired || n.Field != "provider" {
		t.Fatalf("want the newly-required field reported, got %+v", n)
	}
}

func TestAConceptDisappearingIsReported(t *testing.T) {
	got := Narrowings(before(), Snapshot{})
	n := onlyNarrowing(t, got)
	if n.Kind != KindConcept || n.Concept != "v1:cluster:cluster" {
		t.Fatalf("want the concept reported, got %+v", n)
	}
	if n.Field != "" {
		t.Errorf("a concept departure names no field, got %q", n.Field)
	}
}

func TestARemovedFieldDoesNotAlsoReportItsEnumValues(t *testing.T) {
	// One line, not three. A field removal already says everything, and burying
	// it under one line per enum value is how the line that matters gets
	// skimmed past.
	got := Narrowings(before(), after(func(e *Entry) {
		e.Fields = []string{"name", "status"}
		delete(e.Enums, "provider")
	}))
	n := onlyNarrowing(t, got)
	if n.Kind != KindField || n.Field != "provider" {
		t.Fatalf("want one field-removal line, got %+v", n)
	}
}

// ---- the controls ----

func TestANewConceptIsNotANarrowing(t *testing.T) {
	// A concept absent from the snapshot has no stored rows, so its required
	// fields and its enums are free. Reporting them would fire this gate on
	// every PR that adds a concept.
	next := before()
	next.Concepts = append(next.Concepts, Entry{
		Concept:  "v1:cluster:brandNew",
		Fields:   []string{"a"},
		Required: []string{"a"},
		Enums:    map[string][]string{"a": {"one"}},
	})
	if got := Narrowings(before(), next); len(got) != 0 {
		t.Fatalf("a new concept was reported as a narrowing: %v", got)
	}
}

func TestAddingAnOptionalFieldIsNotANarrowing(t *testing.T) {
	got := Narrowings(before(), after(func(e *Entry) {
		e.Fields = append(e.Fields, "region")
	}))
	if len(got) != 0 {
		t.Fatalf("adding a field was reported: %v", got)
	}
}

func TestAddingAnEnumValueIsNotANarrowing(t *testing.T) {
	// Widening an enum leaves every stored value valid. Only the removal
	// direction bricks anything.
	got := Narrowings(before(), after(func(e *Entry) {
		e.Enums = map[string][]string{"provider": {"aws", "azure", "docker-local"}}
	}))
	if len(got) != 0 {
		t.Fatalf("widening an enum was reported: %v", got)
	}
}

func TestDroppingRequiredIsNotANarrowing(t *testing.T) {
	got := Narrowings(before(), after(func(e *Entry) { e.Required = nil }))
	if len(got) != 0 {
		t.Fatalf("relaxing @required was reported: %v", got)
	}
}

func TestAFileMoveIsNotANarrowing(t *testing.T) {
	// Keyed by concept id, never by file. memql#5165's own note records that a
	// per-file comparison could not see a field move between concepts in one
	// file, which is exactly how `active` was missed.
	got := Narrowings(before(), after(func(e *Entry) { e.File = "cluster/moved.memql" }))
	if len(got) != 0 {
		t.Fatalf("a file move was reported: %v", got)
	}
}

// ---- Reconcile ----

func TestReconcileClearsARecordedRetirement(t *testing.T) {
	committed := before()
	committed.Retired = []Retirement{{
		Concept: "v1:cluster:cluster", Field: "status", Kind: KindField,
		Migration: "20260908010000_cluster_status_retired.up.sql",
	}}
	next, unrecorded := Reconcile(committed, after(func(e *Entry) {
		e.Fields = []string{"name", "provider"}
	}))
	if len(unrecorded) != 0 {
		t.Fatalf("a recorded retirement was still reported: %v", unrecorded)
	}
	if len(next.Retired) != 1 {
		t.Fatalf("the ledger must be carried forward, got %d entries", len(next.Retired))
	}
}

func TestReconcileCarriesTheLedgerForwardUnchanged(t *testing.T) {
	// Idempotence is what makes the drift gate usable: a second run must
	// produce the same bytes, or every PR carries a spurious diff.
	committed := before()
	committed.Retired = []Retirement{
		{Concept: "v1:z:z", Field: "b", Kind: KindField, Waiver: "nothing wrote it"},
		{Concept: "v1:a:a", Field: "a", Kind: KindField, Waiver: "nothing wrote it"},
	}
	once, _ := Reconcile(committed, before())
	twice, _ := Reconcile(once, before())
	if len(once.Retired) != 2 || len(twice.Retired) != 2 {
		t.Fatalf("ledger length changed across runs: %d then %d", len(once.Retired), len(twice.Retired))
	}
	if once.Retired[0].Concept != "v1:a:a" || twice.Retired[0].Concept != "v1:a:a" {
		t.Errorf("the ledger must sort stably, got %q then %q",
			once.Retired[0].Concept, twice.Retired[0].Concept)
	}
}

func TestAnEntryForADifferentKindDoesNotCover(t *testing.T) {
	// The ledger key carries the kind, so a field-removal entry cannot silence
	// the same field becoming required. They are different repairs -- a strip
	// and a backfill -- and one entry claiming both would hide the second.
	committed := before()
	committed.Retired = []Retirement{{
		Concept: "v1:cluster:cluster", Field: "provider", Kind: KindField, Waiver: "x",
	}}
	_, unrecorded := Reconcile(committed, after(func(e *Entry) {
		e.Required = []string{"name", "provider"}
	}))
	if len(unrecorded) != 1 || unrecorded[0].Kind != KindRequired {
		t.Fatalf("want the required-narrowing still reported, got %v", unrecorded)
	}
}
