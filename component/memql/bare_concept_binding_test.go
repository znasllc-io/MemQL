package memql

import (
	"log/slog"
	"os"
	"testing"

	memoryNodes "github.com/znasllc-io/memql/component/database/memory-nodes"
)

// TestAmbiguousBareConceptQueriesBindConcept guards memql#759 end to end: a
// query declared `query <bareName> <queryName>` whose bare name is AMBIGUOUS
// across namespaces must bind through the file's own `use` import.
//
// It used to assert this through plansForSpace / allPlans, where the ambiguity
// was `plan` (v1:planner:plan and v1:harness:plan both ended ":plan"). Both
// concepts are now retired -- harness in epic A1, planner in memql#5053 -- so
// the pair that DEMONSTRATES the rule moved, exactly as the sibling test below
// already had to move once. `invocation` is the live one: v1:worker:invocation
// and v1:observability:invocation share the trailing segment.
//
// A regression leaves BoundConcept empty and every call fails the engine with
// `concept "" not found in registry`.
func TestAmbiguousBareConceptQueriesBindConcept(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if _, err := LoadUnifiedConcepts(logger); err != nil {
		t.Fatalf("LoadUnifiedConcepts: %v", err)
	}

	registry := newFunctionRegistry()
	if _, _, err := LoadUnifiedFunctions(logger, registry, memoryNodes.DefaultRegistry()); err != nil {
		t.Fatalf("LoadUnifiedFunctions: %v", err)
	}

	for _, tc := range []struct{ name, want string }{
		{"invocationsForRun", "v1:worker:invocation"},
		{"invocationsForUser", "v1:worker:invocation"},
		{"codeMetricsInWindow", "v1:observability:codeMetric"},
	} {
		fn, err := registry.Get(tc.name)
		if err != nil {
			t.Errorf("%s: not registered: %v", tc.name, err)
			continue
		}
		if fn.BoundConcept != tc.want {
			t.Errorf("%s: BoundConcept=%q, want %q", tc.name, fn.BoundConcept, tc.want)
		}
	}
}

// TestResolveBareConceptName_NamespaceHintDisambiguates is the unit-level
// guard for the disambiguation rule: a trailing segment shared across two
// namespaces resolves with a namespace hint and stays ambiguous without one.
func TestResolveBareConceptName_NamespaceHintDisambiguates(t *testing.T) {
	if _, err := LoadUnifiedConcepts(nil); err != nil {
		t.Fatalf("LoadUnifiedConcepts: %v", err)
	}
	resolver := NewConceptResolver(memoryNodes.DefaultRegistry())

	// `invocation` is the live ambiguity: v1:worker:invocation and
	// v1:observability:invocation share the trailing segment. It was `plan`
	// until the work spine's epic A1 retired v1:harness:plan; the rule is
	// unchanged, only the pair that demonstrates it.
	if _, err := resolver.resolveBareConceptName("invocation"); err == nil {
		t.Fatal("resolveBareConceptName(\"invocation\") with no hint: expected ambiguity error, got nil")
	}
	if id, err := resolver.resolveBareConceptNameWithNamespace("invocation", "worker"); err != nil || id != "v1:worker:invocation" {
		t.Fatalf("resolveBareConceptNameWithNamespace(\"invocation\", \"worker\") = (%q, %v), want (v1:worker:invocation, nil)", id, err)
	}
	if id, err := resolver.resolveBareConceptNameWithNamespace("invocation", "observability"); err != nil || id != "v1:observability:invocation" {
		t.Fatalf("resolveBareConceptNameWithNamespace(\"invocation\", \"observability\") = (%q, %v), want (v1:observability:invocation, nil)", id, err)
	}
	// An unhelpful hint (matches neither) stays ambiguous.
	if _, err := resolver.resolveBareConceptNameWithNamespace("invocation", "nope"); err == nil {
		t.Fatal("resolveBareConceptNameWithNamespace(\"invocation\", \"nope\"): expected ambiguity error, got nil")
	}
}
