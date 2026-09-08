package conceptfields

import (
	"fmt"
	"sort"
	"strings"
)

// The three shapes a concept can narrow in, plus the one that takes a whole
// concept away. Every one of them makes some stored row fail validation on its
// next write, at the same line, with a message that points at the schema rather
// than at the row's history -- which is why they are one ledger and not four.
const (
	// KindField -- a field disappeared from a concept. The stored key is now
	// `additionalProperties '<field>' not allowed`.
	KindField = "field"
	// KindEnumValue -- an enum lost a value. A row carrying it stops
	// validating, and the message names the enum.
	KindEnumValue = "enumValue"
	// KindRequired -- a field became required, or arrived required on a
	// concept that already had rows. Existing rows lack it, so the read-merge
	// fails the other way round.
	KindRequired = "required"
	// KindConcept -- the concept itself is gone from the tree.
	//
	// NOT a bricking hazard: nothing writes a concept the engine no longer
	// registers, so its rows are inert rather than broken. It is in the ledger
	// because of the OTHER way a concept leaves the snapshot -- an id that
	// DRIFTED. A `namespace.pin` added to a directory, or a change in how ids
	// are assembled, silently re-keys every concept under it, and a diff that
	// only compared concepts present on both sides would read that as nothing
	// at all while the snapshot quietly stopped describing the tree. Requiring
	// a line per departure is what makes such a drift stop the build.
	KindConcept = "concept"
)

// Narrowing is one thing the tree takes away that the snapshot still records.
type Narrowing struct {
	Concept string
	// Field is empty for KindConcept.
	Field string
	Kind  string
	// Value is set for KindEnumValue only.
	Value string
}

// Key is the ledger identity of one narrowing. A Retirement covers a Narrowing
// when their keys are equal.
func (n Narrowing) Key() string {
	return strings.Join([]string{n.Concept, n.Kind, n.Field, n.Value}, "\x1f")
}

// String renders a narrowing the way the generator reports it.
func (n Narrowing) String() string {
	switch n.Kind {
	case KindConcept:
		return fmt.Sprintf("%s is no longer declared anywhere in the tree", n.Concept)
	case KindEnumValue:
		return fmt.Sprintf("%s.%s no longer accepts the enum value %q", n.Concept, n.Field, n.Value)
	case KindRequired:
		return fmt.Sprintf("%s.%s is now @required, and rows written before it was do not carry it", n.Concept, n.Field)
	default:
		return fmt.Sprintf("%s no longer declares %q", n.Concept, n.Field)
	}
}

// Retirement is one recorded narrowing: what was taken away, and what was done
// about it.
//
// EXACTLY ONE OF Migration AND Waiver IS SET, and the pair is the whole point.
// A retirement that genuinely needs no migration -- nothing ever wrote the
// field, or the concept itself is gone -- must still be RECORDED, because
// without a waiver the gate's only remedy is a no-op migration, and a no-op in
// the migrations directory is a lie told to whoever reads it next.
//
// APPEND-ONLY. Nothing here enforces that, and saying so is deliberate: the
// check would need history, and every Go lane in CI checks out at depth 1 --
// which is the same constraint that made this file a snapshot rather than a
// `git merge-base` diff. What DOES stand behind it is VerifyLedger's inverse
// direction: a migration that strips a key must have a ledger entry, so
// deleting an entry whose migration still exists fails the build. Deleting
// both is a diff, and a diff is what review is for.
type Retirement struct {
	Concept string `json:"concept"`
	Field   string `json:"field,omitempty"`
	Kind    string `json:"kind"`
	Value   string `json:"value,omitempty"`
	// Migration is the base name of the up-migration that repairs the stored
	// rows, e.g. `20260908010000_cluster_status_retired.up.sql`.
	Migration string `json:"migration,omitempty"`
	// Waiver is the reason no migration is needed. Free text, and it is read
	// by people rather than by code -- the gate checks that ONE was written,
	// never that it is true.
	Waiver string `json:"waiver,omitempty"`
	// Note carries the provenance: which issue or epic took the field away.
	Note string `json:"note,omitempty"`
}

// Key matches Narrowing.Key.
func (r Retirement) Key() string {
	return strings.Join([]string{r.Concept, r.Kind, r.Field, r.Value}, "\x1f")
}

// Narrowings reports everything `after` takes away that `before` recorded.
//
// A concept absent from `before` contributes nothing: it is NEW, and a new
// concept has no stored rows to brick, so its required fields and its enums
// are free. That asymmetry is the reason this is a diff against a snapshot
// rather than a rule about the tree.
func Narrowings(before, after Snapshot) []Narrowing {
	priorEntries := index(before)
	currentEntries := index(after)

	var out []Narrowing
	for id, prior := range priorEntries {
		current, stillDeclared := currentEntries[id]
		if !stillDeclared {
			out = append(out, Narrowing{Concept: id, Kind: KindConcept})
			continue
		}

		currentFields := set(current.Fields)
		for _, f := range prior.Fields {
			if !currentFields[f] {
				out = append(out, Narrowing{Concept: id, Field: f, Kind: KindField})
			}
		}

		priorRequired := set(prior.Required)
		for _, f := range current.Required {
			if !priorRequired[f] {
				out = append(out, Narrowing{Concept: id, Field: f, Kind: KindRequired})
			}
		}

		for field, priorValues := range prior.Enums {
			if !currentFields[field] {
				// The field is gone entirely and is already reported as
				// KindField. Reporting each of its enum values as well would
				// bury the one line that matters under a dozen that follow
				// from it.
				continue
			}
			currentValues := set(current.Enums[field])
			for _, v := range priorValues {
				if !currentValues[v] {
					out = append(out, Narrowing{Concept: id, Field: field, Kind: KindEnumValue, Value: v})
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// Reconcile builds the snapshot that should be committed, and reports every
// narrowing the ledger does not yet cover.
//
// The committed ledger is carried forward VERBATIM and the current tree
// supplies the concepts, so a regeneration is idempotent: running it twice
// produces the same bytes. Unrecorded narrowings are returned rather than
// written, which is what makes the generator REFUSE rather than quietly
// absorb the removal it exists to notice.
func Reconcile(committed, current Snapshot) (Snapshot, []Narrowing) {
	covered := map[string]bool{}
	for _, r := range committed.Retired {
		covered[r.Key()] = true
	}

	var unrecorded []Narrowing
	for _, n := range Narrowings(committed, current) {
		if !covered[n.Key()] {
			unrecorded = append(unrecorded, n)
		}
	}

	next := Snapshot{Concepts: current.Concepts, Retired: append([]Retirement(nil), committed.Retired...)}
	sort.SliceStable(next.Retired, func(i, j int) bool { return next.Retired[i].Key() < next.Retired[j].Key() })
	return next, unrecorded
}

func index(s Snapshot) map[string]Entry {
	out := make(map[string]Entry, len(s.Concepts))
	for _, e := range s.Concepts {
		out[e.Concept] = e
	}
	return out
}

func set(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, v := range in {
		out[v] = true
	}
	return out
}
