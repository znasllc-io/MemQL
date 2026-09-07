package memql

import (
	"strings"
	"testing"
)

func ruleAt(name string, locked bool, precedence int) *RuleConfig {
	return &RuleConfig{
		Name:          name,
		Policy:        "localFirst",
		Precedence:    precedence,
		Locked:        locked,
		OnUnavailable: OnUnavailableDegrade,
		SourceFile:    "fixture/" + name + ".memql",
		When:          RuleWhen{Present: map[string]bool{}},
	}
}

func finalized(t *testing.T, rules ...*RuleConfig) *RuleRegistry {
	t.Helper()
	r := NewRuleRegistry()
	for _, c := range rules {
		if err := r.Register(c); err != nil {
			t.Fatalf("register %s: %v", c.Name, err)
		}
	}
	if err := r.Finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return r
}

func orderOf(r *RuleRegistry) []string {
	out := make([]string, 0)
	for _, c := range r.Ordered() {
		out = append(out, c.Name)
	}
	return out
}

// TestOrderedPutsLockedFirstThenPrecedence is what @locked buys the shipped
// rules: an owner may give a rule any precedence and the shipped ones still go
// first.
func TestOrderedPutsLockedFirstThenPrecedence(t *testing.T) {
	r := finalized(t,
		ruleAt("mine", false, 900),
		ruleAt("shippedLow", true, 10),
		ruleAt("shippedHigh", true, 100),
		ruleAt(DefaultRuleName, true, 0),
	)
	got := orderOf(r)
	want := []string{"shippedHigh", "shippedLow", "mine", DefaultRuleName}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// TestTheDefaultRuleAlwaysSortsLastSoAuthoredRulesAreReachable is the one that
// matters, and it was written because the first version of Finalize failed it
// silently.
//
// `default` is locked and states no conditions, so under a plain locked-first
// partition it matched every call before any authored rule was consulted --
// making the whole authored tier unreachable while every test that used only
// locked rules passed. Design D7's "a runtime-authored rule may add and may
// take precedence" would have been false of every rule anybody wrote, and
// nothing would have said so: rules would simply never fire.
func TestTheDefaultRuleAlwaysSortsLastSoAuthoredRulesAreReachable(t *testing.T) {
	r := finalized(t,
		ruleAt(DefaultRuleName, true, 0),
		ruleAt("authored", false, 1),
	)
	got := orderOf(r)
	if len(got) != 2 || got[0] != "authored" || got[1] != DefaultRuleName {
		t.Fatalf("order = %v, want the authored rule before the floor -- a locked catch-all "+
			"evaluated first makes every unlocked rule dead code", got)
	}
}

// TestTheFloorIsLastEvenAtTheHighestPrecedence pins that the exemption is by
// IDENTITY and not by its precedence value, so an operator who edits the
// shipped file cannot promote the floor by giving it a big number.
func TestTheFloorIsLastEvenAtTheHighestPrecedence(t *testing.T) {
	r := finalized(t,
		ruleAt(DefaultRuleName, true, 10000),
		ruleAt("authored", false, 1),
	)
	if got := orderOf(r); got[0] != "authored" {
		t.Fatalf("order = %v, want the floor last regardless of its precedence", got)
	}
}

// TestATiedPrecedenceIsALoadErrorNamingBothFiles. A tie is resolved by
// nothing, so the rule that wins would differ between replicas -- which is a
// routing decision nobody wrote and nobody can reproduce.
func TestATiedPrecedenceIsALoadErrorNamingBothFiles(t *testing.T) {
	r := NewRuleRegistry()
	for _, c := range []*RuleConfig{
		ruleAt(DefaultRuleName, true, 0),
		ruleAt("alpha", false, 50),
		ruleAt("beta", false, 50),
	} {
		if err := r.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	err := r.Finalize()
	if err == nil {
		t.Fatal("a precedence tie was accepted")
	}
	for _, want := range []string{"alpha", "beta", "fixture/alpha.memql", "fixture/beta.memql"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal %q does not name %q; a tie message that names one side sends "+
				"an author to change the wrong file", err, want)
		}
	}
}

// TestTheFloorTiesWithNothing: the floor is alone at the end, so a rule
// sharing its precedence is not a tie with it.
func TestTheFloorTiesWithNothing(t *testing.T) {
	r := NewRuleRegistry()
	for _, c := range []*RuleConfig{
		ruleAt(DefaultRuleName, true, 0),
		ruleAt("authored", false, 0),
	} {
		if err := r.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	if err := r.Finalize(); err != nil {
		t.Fatalf("the floor tied with an authored rule at the same precedence: %v", err)
	}
}

// TestADuplicateNameIsRefusedNamingBothFiles. Overwriting was the pre-existing
// policy-registry behaviour and it is what let a mounted bundle silently
// replace a shipped construct: last writer wins, no message, and a cluster
// routing by a rule nobody can find.
func TestADuplicateNameIsRefusedNamingBothFiles(t *testing.T) {
	r := NewRuleRegistry()
	first := ruleAt("clash", false, 10)
	first.SourceFile = "core/rules.memql"
	second := ruleAt("clash", false, 20)
	second.SourceFile = "bundle/rules.memql"
	if err := r.Register(first); err != nil {
		t.Fatalf("register: %v", err)
	}
	err := r.Register(second)
	if err == nil {
		t.Fatal("a duplicate rule name was accepted; last-wins is what this refusal replaces")
	}
	for _, want := range []string{"core/rules.memql", "bundle/rules.memql"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal %q does not name %q", err, want)
		}
	}
}

// TestFinalizeRefusesARuleSetWithNoFloor. Every other reader assumes a call
// always matches something; without the floor that assumption is false and the
// failure is a call that resolves through no policy at all.
func TestFinalizeRefusesARuleSetWithNoFloor(t *testing.T) {
	r := NewRuleRegistry()
	if err := r.Register(ruleAt("only", false, 5)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := r.Finalize(); err == nil {
		t.Fatal("a rule set with no `default` was accepted")
	}
}

// TestOrderedIsNilBeforeFinalize. An unfinalized registry has no order, and
// answering with an arbitrary one would route calls by map iteration.
func TestOrderedIsNilBeforeFinalize(t *testing.T) {
	r := NewRuleRegistry()
	if err := r.Register(ruleAt(DefaultRuleName, true, 0)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if got := r.Ordered(); got != nil {
		t.Fatalf("Ordered() = %v before Finalize; an unfinalized registry has no order", got)
	}
}

// TestDisabledRulesAreLoadedAndNotEvaluated. @disabled is a reversible on/off
// switch, not a deletion: the rule stays in the corpus, stays conformance-
// checked, and simply does not run.
func TestDisabledRulesAreLoadedAndNotEvaluated(t *testing.T) {
	off := ruleAt("off", false, 5)
	off.Disabled = true
	r := finalized(t, ruleAt(DefaultRuleName, true, 0), off)
	if r.Count() != 2 {
		t.Fatalf("Count() = %d, want both rules registered", r.Count())
	}
	for _, c := range r.Ordered() {
		if c.Name == "off" {
			t.Fatal("a disabled rule is in the evaluation order")
		}
	}
}
