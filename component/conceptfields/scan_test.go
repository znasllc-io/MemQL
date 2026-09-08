package conceptfields

import (
	"os"
	"path/filepath"
	"testing"
)

// The scanner's own coverage.
//
// Everything in narrowing_test.go runs on fixtures, so a scanner that read
// zero concepts out of dsl/ would leave every one of those tests green while
// the corpus gate passed over nothing. These read the real tree.

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("expected the repo root at %s: %v", root, err)
	}
	return root
}

func scanRealTree(t *testing.T) map[string]Entry {
	t.Helper()
	snap, err := Scan(filepath.Join(repoRoot(t), DefaultDSLRoot))
	if err != nil {
		t.Fatalf("scanning dsl/: %v", err)
	}
	return index(snap)
}

func TestTheRealTreeScansIntoConceptsAndFields(t *testing.T) {
	got := scanRealTree(t)
	if len(got) < 200 {
		t.Fatalf("parsed %d concepts out of dsl/, expected well over 200 -- the scanner has stopped matching", len(got))
	}

	cluster, ok := got["v1:cluster:cluster"]
	if !ok {
		t.Fatal("v1:cluster:cluster did not parse")
	}
	if !set(cluster.Fields)["provider"] || !set(cluster.Fields)["seedNodeTypes"] {
		t.Errorf("v1:cluster:cluster is missing declared fields: %v", cluster.Fields)
	}
	if set(cluster.Fields)["status"] {
		t.Error("v1:cluster:cluster reads as declaring `status`, which memql#4772 removed -- " +
			"the scanner is reading the prose note that says so")
	}
}

func TestTheScannerReachesTheShopifyGeneratedTree(t *testing.T) {
	// 65 concepts one per file, under a `namespace.pin`. memql#5199's reader
	// globbed `concepts.memql` and missed every one of them; a pin the scanner
	// ignored would key them all under the wrong namespace and report the whole
	// set as retired the first time anybody looked.
	got := scanRealTree(t)
	if _, ok := got["v1:shopify:product"]; !ok {
		var sample []string
		for id := range got {
			if len(sample) < 5 {
				sample = append(sample, id)
			}
		}
		t.Fatalf("v1:shopify:product is absent -- the pin or the per-file tree is not being read. Sample: %v", sample)
	}
}

func TestNestedObjectMembersAreNotTopLevelFields(t *testing.T) {
	// `payload - 'x'` cannot reach a nested key, so recording one would put a
	// narrowing in the ledger that no migration shape can repair.
	got := scanRealTree(t)
	reg, ok := got["v1:worker:registration"]
	if !ok {
		t.Skip("v1:worker:registration is not in the tree")
	}
	if set(reg.Fields)["vramBytes"] {
		t.Error("a nested object's member leaked into the top-level field set")
	}
}

func TestRequiredAndEnumsAreReadFromTheBuiltSchema(t *testing.T) {
	// The whole reason this scanner builds the schema rather than reading the
	// source: `@required` is the `!` sigil AND the annotation, and enum values
	// live on the TypeRef. `v1:router:call.outcome` is declared with the sigil
	// form (`enum(...)!`), which a line-oriented reader gets wrong in both
	// columns at once.
	got := scanRealTree(t)
	call, ok := got["v1:router:call"]
	if !ok {
		t.Fatal("v1:router:call did not parse")
	}
	if !set(call.Required)["outcome"] {
		t.Errorf("outcome is declared `enum(...)!` and must read as required: %v", call.Required)
	}
	values := set(call.Enums["outcome"])
	for _, want := range []string{"ok", "error", "cancelled", "fallback_used"} {
		if !values[want] {
			t.Errorf("outcome is missing the enum value %q: %v", want, call.Enums["outcome"])
		}
	}
	if len(call.Enums["level"]) != 4 {
		t.Errorf("level should carry the closed four, got %v", call.Enums["level"])
	}
}

func TestAnArrayOfEnumCarriesItsValues(t *testing.T) {
	// `[]enum(...)` emits the enum on `items`. A reader that looked only at the
	// top level would record no values, so dropping one would be invisible --
	// silently, on exactly the shape whose stored values are hardest to survey.
	dir := t.TempDir()
	ns := filepath.Join(dir, "fixture")
	if err := os.MkdirAll(ns, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `concept widget {
  kinds  []enum("a", "b")  @description("array of enum")
}
`
	if err := os.WriteFile(filepath.Join(ns, "concepts.memql"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := Scan(dir)
	if err != nil {
		t.Fatalf("scanning fixture: %v", err)
	}
	entries := index(snap)
	w, ok := entries["v1:fixture:widget"]
	if !ok {
		t.Fatalf("the fixture concept did not parse: %v", entries)
	}
	if len(w.Enums["kinds"]) != 2 {
		t.Fatalf("an array-of-enum must carry its values, got %v", w.Enums)
	}
}

func TestTheSnapshotRendersStably(t *testing.T) {
	// The committed file must be a function of the tree alone. A rendering that
	// followed map iteration order would produce a fresh diff on every run and
	// the drift gate would be unusable.
	snap, err := Scan(filepath.Join(repoRoot(t), DefaultDSLRoot))
	if err != nil {
		t.Fatalf("scanning dsl/: %v", err)
	}
	first, err := Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := Marshal(snap)
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatalf("rendering is not stable across runs (attempt %d)", i)
		}
	}
}
