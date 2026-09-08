package router

import (
	"errors"
	"strings"
	"testing"
)

// fakeRender stands in for component/routingrules.GenerateRule, which is epic
// memql#5127's and not on this base. It renders DETERMINISTICALLY -- keys
// sorted -- because the real one does, and because the hash is only safe if a
// re-render of an unchanged rule is byte-identical.
func fakeRender(f Form) (string, error) {
	var b strings.Builder
	b.WriteString("@when(")
	keys := make([]string, 0, len(f.When))
	for k := range f.When {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k + "=\"" + f.When[k] + "\"")
	}
	b.WriteString(")\n")
	for _, ex := range f.Excludes {
		b.WriteString("@exclude(\"" + ex + "\")\n")
	}
	b.WriteString("@policy(\"" + f.Policy + "\")\n")
	b.WriteString("rule " + f.Name + " { }\n")
	return b.String(), nil
}

func window(calls, failures int) Window {
	return Window{
		ModelId:            "qwen3.5:9b",
		Level:              "strong",
		Week:               "2026-W36",
		Calls:              calls,
		StructuredFailures: failures,
	}
}

func TestNoProposalUnderTheCallFloor(t *testing.T) {
	// Nineteen calls all failing is not evidence; it is a bad afternoon. The
	// ROW still carries the count -- that is the concept's job -- so an operator
	// asking why nothing was proposed reads the answer rather than inferring it.
	_, ok, err := ProposeExclusion(window(MinimumCalls-1, MinimumCalls-1), fakeRender)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("%d calls is under the %d-call floor and must propose nothing", MinimumCalls-1, MinimumCalls)
	}
}

func TestNoProposalAtExactlyTheThreshold(t *testing.T) {
	// 0.3 is not "exceeds 0.3". A boundary decided by nothing is a boundary two
	// readers can disagree about, and this one decides whether a person is asked
	// to demote a model.
	_, ok, err := ProposeExclusion(window(100, 30), fakeRender)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a failure rate exactly at the threshold must not propose; the rule is EXCEEDS")
	}
	_, ok, err = ProposeExclusion(window(100, 31), fakeRender)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("a failure rate above the threshold must propose")
	}
}

func TestAWindowWithNoLevelProposesNothing(t *testing.T) {
	// THE CASE THIS BRANCH ACTUALLY HITS, and the reason it is a refusal rather
	// than a fallback. A call that did not say which level it was serving cannot
	// support a demotion AT a level; demoting at every level instead would be a
	// far heavier act than the evidence supports, and it would be taken on the
	// strength of a missing field.
	w := window(100, 90)
	w.Level = ""
	_, ok, err := ProposeExclusion(w, fakeRender)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a window with no level must propose nothing")
	}
}

func TestAWindowWithAnUnknownLevelProposesNothing(t *testing.T) {
	// A level outside the closed four would render a rule the grammar refuses
	// to load -- so the proposal would be approved and then fail to arm, which
	// is the worst of both.
	w := window(100, 90)
	w.Level = "colossal"
	_, ok, _ := ProposeExclusion(w, fakeRender)
	if ok {
		t.Fatal("a level outside the closed set must propose nothing")
	}
}

func TestTheProposalCarriesTheNumbers(t *testing.T) {
	// "This model is failing" is not a decidable claim. "17 of 41 structured
	// calls failed last week" is, and the person being asked to demote a model
	// is entitled to the second one.
	p, ok, err := ProposeExclusion(window(41, 17), fakeRender)
	if err != nil || !ok {
		t.Fatalf("expected a proposal: ok=%v err=%v", ok, err)
	}
	for _, want := range []string{"17", "41", "qwen3.5:9b", "strong", "2026-W36"} {
		if !strings.Contains(p.Reason, want) {
			t.Fatalf("the reason must carry %q, got %q", want, p.Reason)
		}
	}
}

func TestTheHashIsOverTheRenderedSourceAndNotTheForm(t *testing.T) {
	// An approval is a decision about one specific rule TEXT. Hashing the inputs
	// would let a renderer change move the text under an approval that still
	// verified, which is the one failure the hash exists to prevent and the one
	// nobody would notice.
	p, _, err := ProposeExclusion(window(100, 90), fakeRender)
	if err != nil {
		t.Fatal(err)
	}
	if p.Hash != ProposalHash(p.RuleSource) {
		t.Fatal("the hash must be over the rendered source")
	}
	if p.Hash == ProposalHash(p.RuleSource+" ") {
		t.Fatal("the hash must change when the source does, to a single space")
	}
}

func TestTheProposalIsDeterministic(t *testing.T) {
	// Same evidence, byte-identical rule, identical hash -- on every run. A
	// nondeterministic renderer voids the approval's whole guarantee, and this
	// is the assertion that would catch one arriving through map iteration.
	first, _, err := ProposeExclusion(window(100, 90), fakeRender)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		again, _, err := ProposeExclusion(window(100, 90), fakeRender)
		if err != nil {
			t.Fatal(err)
		}
		if again.RuleSource != first.RuleSource || again.Hash != first.Hash {
			t.Fatalf("run %d differed:\n%q\n%q", i, again.RuleSource, first.RuleSource)
		}
	}
}

func TestTheWhenBlockCarriesOnlyTheKeysThatAreSet(t *testing.T) {
	// A key PRESENT with an empty value is a condition matching only an empty
	// value; a key ABSENT is no condition at all. Emitting every key with "" for
	// the blanks produces a rule that silently never fires -- which here would
	// be a demotion a person approved and that did nothing.
	form := exclusionForm(window(100, 90))
	if len(form.When) != 1 {
		t.Fatalf("the when block must carry only `level`, got %#v", form.When)
	}
	if form.When["level"] != "strong" {
		t.Fatalf("got %#v", form.When)
	}
	for _, key := range []string{"modality", "prompt", "role", "actorRole", "tag", "touches"} {
		if _, present := form.When[key]; present {
			t.Fatalf("an unset condition key must be ABSENT, not empty: %q", key)
		}
	}
}

func TestARendererIsRequired(t *testing.T) {
	// There is exactly one renderer of the rule grammar in the tree, and this
	// package must not become a second. A missing one is an error rather than a
	// local fallback.
	_, _, err := ProposeExclusion(window(100, 90), nil)
	if err == nil {
		t.Fatal("a proposal with no renderer must be an error, never a locally-rendered rule")
	}
}

func TestARenderFailureIsAnErrorAndNotASilentSkip(t *testing.T) {
	// A rule that could not be rendered is a proposal that could not be made,
	// and it must be loud: the alternative is a fold that quietly stops
	// proposing and looks exactly like a fleet where every model behaves.
	boom := func(Form) (string, error) { return "", errors.New("grammar refused") }
	_, ok, err := ProposeExclusion(window(100, 90), boom)
	if ok {
		t.Fatal("a failed render must not produce a proposal")
	}
	if err == nil {
		t.Fatal("a failed render must be reported")
	}
}

func TestPromotionReadsAMeasurementAndNotAWeekOfCalls(t *testing.T) {
	// The asymmetry is forced rather than chosen: an excluded model serves no
	// calls, so there is no service evidence to fold, and the probe is the only
	// thing that can speak for it.
	w := window(0, 0)
	if _, ok, _ := ProposePromotion(w, 0.5, true, fakeRender); ok {
		t.Fatal("a measurement under the bar must not propose a promotion")
	}
	if _, ok, _ := ProposePromotion(w, 0.95, false, fakeRender); ok {
		t.Fatal("no measurement at all must not propose a promotion")
	}
	p, ok, err := ProposePromotion(w, 0.95, true, fakeRender)
	if err != nil || !ok {
		t.Fatalf("a measurement at or above the bar must propose: ok=%v err=%v", ok, err)
	}
	if p.Direction != DirectionPromotion {
		t.Fatalf("direction %q", p.Direction)
	}
	if len(p.RuleSource) == 0 || strings.Contains(p.RuleSource, "@exclude") {
		t.Fatalf("a promotion is the same rule with nothing excluded, got %q", p.RuleSource)
	}
}

func TestARuleNameIsDerivedSoTwoFoldsAgree(t *testing.T) {
	// Derived rather than random, so a second fold of the same evidence produces
	// the same name and the authoring pipeline treats it as the same rule rather
	// than as a second one beside it.
	a := RuleName(DirectionDemotion, "qwen3.5:9b", "strong")
	b := RuleName(DirectionDemotion, "qwen3.5:9b", "strong")
	if a != b {
		t.Fatalf("%q vs %q", a, b)
	}
	if strings.ContainsAny(a, ":/.") {
		t.Fatalf("a rule name must be an identifier, got %q", a)
	}
	if a == RuleName(DirectionDemotion, "qwen3.5:9b", "fast") {
		t.Fatal("two levels must produce two rule names")
	}
}

func TestSortWindowsIsStable(t *testing.T) {
	ws := []Window{
		{ModelId: "b", Level: "reasoning"},
		{ModelId: "a", Level: "strong"},
		{ModelId: "a", Level: "fast"},
	}
	SortWindows(ws)
	if ws[0].ModelId != "a" || ws[0].Level != "fast" {
		t.Fatalf("got %+v", ws)
	}
	if ws[1].ModelId != "a" || ws[1].Level != "strong" {
		t.Fatalf("got %+v", ws)
	}
}

func TestFailureRateOfAnEmptyWindowIsZeroAndTheFloorIsWhatMakesThatSafe(t *testing.T) {
	// A rate over no calls is not a rate. Returning zero is only safe because
	// MinimumCalls gates every caller -- so the assertion is the pairing, not
	// the number.
	if r := (Window{}).FailureRate(); r != 0 {
		t.Fatalf("rate %v", r)
	}
	if _, ok, _ := ProposeExclusion(Window{ModelId: "m", Level: "fast", Week: "2026-W36"}, fakeRender); ok {
		t.Fatal("an empty window must be stopped by the floor, not by the rate")
	}
}
