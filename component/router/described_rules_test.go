package router

import (
	"strings"
	"testing"
)

// Described rules, their simulation, and the free-form classifier
// (epic memql#5137, D7-D9).
//
// Every test here is over PURE FUNCTIONS -- validation, matching, the rules
// table -- because that is where the decisions are. A model call is not exercised
// and does not need to be: what can go wrong is the compiler producing something
// the loader refuses, a simulation that disagrees with the runtime about
// matching, or a classifier that reaches a model when the table should have
// answered. None of those need a provider.

// ===========================================================================
// D7 -- compiling
// ===========================================================================

func TestActivationRefusesAShippedName(t *testing.T) {
	// A custom rule taking a shipped name is either silently replaced at the
	// next boot or silently replaces the shipped one, and which of those
	// happens depends on load order.
	for _, name := range ShippedRuleNames {
		rule := CompiledRule{Name: name, Policy: "localFirst", Conditions: map[string]string{"level": "fast"}}
		if _, err := rule.Validate(); err == nil {
			t.Errorf("a compiled rule named %q must be refused", name)
		}
	}
}

func TestACompiledRuleMayNotMintAPolicy(t *testing.T) {
	// Minting a policy from a sentence would put a PROVIDER CHAIN -- a
	// spending decision -- in a place no review looks.
	rule := CompiledRule{
		Name:       "campaignsStayLocal",
		Policy:     "somethingNobodyShipped",
		Conditions: map[string]string{"touches": "v1:campaigns:recipient"},
	}
	_, err := rule.Validate()
	if err == nil {
		t.Fatal("a rule naming an unshipped policy must be refused")
	}
	if !strings.Contains(err.Error(), "localFirst") {
		t.Errorf("the refusal must name the policies that ARE available; got %q", err)
	}
}

func TestACompiledRuleMayNotInventAConditionKey(t *testing.T) {
	// The loader would refuse the rule, so activating it leaves a rule that
	// never runs and a person who believes it does.
	rule := CompiledRule{
		Name:       "somethingElse",
		Policy:     "localOnly",
		Conditions: map[string]string{"vendor": "anthropic"},
	}
	if _, err := rule.Validate(); err == nil {
		t.Fatal("a condition outside the @when vocabulary must be refused")
	}
}

func TestAnUnconditionalRuleIsWarnedAboutRatherThanRefused(t *testing.T) {
	// It is legal -- the shipped `default` rule is exactly this -- and it is
	// rarely what a sentence about a particular kind of work means. Refusing it
	// would make the compiler unable to express the shipped rules themselves.
	rule := CompiledRule{Name: "everything", Policy: "localOnly", Restatement: "Everything local."}
	warnings, err := rule.Validate()
	if err != nil {
		t.Fatalf("an unconditional rule is legal: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("an unconditional rule must be warned about")
	}
	if warnings[0].Field != "when" {
		t.Errorf("the warning must name the field; got %q", warnings[0].Field)
	}
}

func TestTheCompilerPromptIsTheNameTheLockedRuleMatches(t *testing.T) {
	// If these two disagree the compiler resolves through the ordinary rules --
	// so a rule about how to spend money could be compiled by a paid model,
	// silently, and the only sign would be a decision record nobody was reading.
	if CompilerPromptName != "compileRule" {
		t.Errorf("the compiler prompt is %q; the locked rule matches @when(prompt=\"compileRule\")", CompilerPromptName)
	}
	if CompilerRuleName != "compilerLocalOnly" || CompilerPolicyName != "localOnly" {
		t.Errorf("the locked rule/policy pair moved: %q -> %q", CompilerRuleName, CompilerPolicyName)
	}
}

func TestAnExclusionNamesAFleetModel(t *testing.T) {
	rule := CompiledRule{
		Name: "noBigOne", Policy: "localFirst",
		Conditions: map[string]string{"level": "fast"},
		Excludes:   []string{"qwen3.5:122b"},
	}
	if _, err := rule.Validate(); err == nil {
		t.Fatal("an exclusion must be spelled fleet:<modelId>")
	}
	rule.Excludes = []string{"fleet:qwen3.5:122b"}
	if _, err := rule.Validate(); err != nil {
		t.Fatalf("a fleet-prefixed exclusion is valid: %v", err)
	}
}

// ===========================================================================
// D8 -- simulating
// ===========================================================================

func records() []DecisionRecord {
	return []DecisionRecord{
		{CallId: "c1", Level: "fast", PromptName: "agentReply", Policy: "localFirst", Door: "local"},
		{CallId: "c2", Level: "strong", PromptName: "agentReply", Policy: "federationStrongest", Door: "federation"},
		{CallId: "c3", Level: "fast", PromptName: "docSummary", Policy: "federationStrongest", Door: "federation"},
		{CallId: "c4", Level: "embeddings", PromptName: "", Policy: "embeddingsBinding", Door: "local"},
		{CallId: "c5", Level: "strong", PromptName: "agentReply", Policy: "federationStrongest", Door: "federation",
			Touches: []string{"v1:campaigns:recipient"}},
	}
}

func TestSimulationCountsWhatWouldChangeAndWhatAlreadyAgrees(t *testing.T) {
	rule := CompiledRule{
		Name: "repliesLocal", Policy: "localFirst",
		Conditions: map[string]string{"prompt": "agentReply"},
	}
	r := Simulate(rule, records())
	if r.Considered != 5 {
		t.Errorf("considered = %d, want 5", r.Considered)
	}
	if r.Matched != 3 {
		t.Errorf("matched = %d, want 3 (the agentReply calls)", r.Matched)
	}
	// c1 is already on localFirst; c2 and c5 are not.
	if len(r.Changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(r.Changes))
	}
	if r.AlreadyAgreed != 1 {
		t.Errorf("alreadyAgreed = %d, want 1 -- 'this rule agrees with what you were already doing' is worth saying", r.AlreadyAgreed)
	}
	if r.Changes[0].CallId != "c2" || r.Changes[1].CallId != "c5" {
		t.Errorf("changes must be in a stable order; got %v", r.Changes)
	}
}

func TestSimulationMatchesEveryPresentConditionAndIgnoresAbsentOnes(t *testing.T) {
	// The simulation must agree with the runtime EXACTLY: matching more
	// liberally promises changes that never happen, matching more strictly
	// hides changes that do.
	rule := CompiledRule{
		Name: "campaignsLocal", Policy: "localOnly",
		Conditions: map[string]string{"prompt": "agentReply", "touches": "v1:campaigns:recipient"},
	}
	r := Simulate(rule, records())
	if r.Matched != 1 {
		t.Fatalf("matched = %d, want 1 -- both conditions must hold", r.Matched)
	}
	if r.Changes[0].CallId != "c5" {
		t.Errorf("matched the wrong call: %v", r.Changes)
	}
}

func TestAnEmptyConditionValueIsAConditionNotAnAbsence(t *testing.T) {
	// This is how a rule says "free-form requests" (prompt="") rather than
	// "any request". Collapsing the two would make every such rule match
	// everything.
	rule := CompiledRule{
		Name: "freeFormLocal", Policy: "localOnly",
		Conditions: map[string]string{"prompt": ""},
	}
	r := Simulate(rule, records())
	if r.Matched != 1 {
		t.Fatalf("matched = %d, want 1 -- only the call with no prompt", r.Matched)
	}
}

func TestNothingToReplayAndMatchedNothingAreDifferentSentences(t *testing.T) {
	// The two ways a described rule goes wrong, and they look identical in a
	// count. Only one of them says anything about the rule.
	rule := CompiledRule{Name: "x", Policy: "localOnly", Conditions: map[string]string{"level": "reasoning"}}

	empty := SimulationSentence(Simulate(rule, nil))
	if !strings.Contains(empty, "No calls to replay") {
		t.Errorf("a cluster with no decisions must say so; got %q", empty)
	}

	noMatch := SimulationSentence(Simulate(rule, records()))
	if !strings.Contains(noMatch, "matches none") {
		t.Errorf("a rule matching nothing must say so; got %q", noMatch)
	}
	if !strings.Contains(noMatch, "narrower than you meant") {
		t.Errorf("matching nothing usually means the compiler was too narrow, and the sentence should say it; got %q", noMatch)
	}
}

func TestAnUnknownConditionKeyMatchesNothing(t *testing.T) {
	// A simulation that ignored an unknown key would show a rule matching
	// everything while the loader was about to refuse it.
	rule := CompiledRule{Name: "x", Policy: "localOnly", Conditions: map[string]string{"vendor": "openai"}}
	if r := Simulate(rule, records()); r.Matched != 0 {
		t.Errorf("matched %d with an unknown condition key; must be 0", r.Matched)
	}
}

// ===========================================================================
// D9 -- classifying a free-form request
// ===========================================================================

func TestRulesTableAnswersFirstAndCallsNoModel(t *testing.T) {
	// The second return is what costs a model call, so a caller counting calls
	// counts misses. These are the cases that must never reach one.
	for _, tc := range []struct {
		name string
		req  FreeFormRequest
		want Modality
		by   string
	}{
		{"an attached image", FreeFormRequest{Text: "what is this", AttachmentTypes: []string{"image/png"}}, ModalityVision, "attachment:image"},
		{"attached audio", FreeFormRequest{Text: "what did they say", AttachmentTypes: []string{"audio/wav"}}, ModalityAudioIn, "attachment:audio"},
		{"a chosen tool", FreeFormRequest{Text: "do the thing", ChosenTool: "workbenchHost"}, ModalityText, "tool:chosen"},
		{"an image verb", FreeFormRequest{Text: "draw me a fox"}, ModalityImageGen, "verb:image"},
		{"a speech verb", FreeFormRequest{Text: "read this aloud please"}, ModalityAudioOut, "verb:speak"},
	} {
		got, ok := ClassifyByRules(tc.req)
		if !ok {
			t.Errorf("%s: the rules table must answer without a model call", tc.name)
			continue
		}
		if got.Modality != tc.want {
			t.Errorf("%s: modality = %q, want %q", tc.name, got.Modality, tc.want)
		}
		if got.DecidedBy != tc.by {
			t.Errorf("%s: decidedBy = %q, want %q -- a misfiling is only debuggable if the record says which rule fired", tc.name, got.DecidedBy, tc.by)
		}
	}
}

func TestAMissIsReportedRatherThanGuessed(t *testing.T) {
	// The miss is what costs one cheap local call, and it must be a MISS rather
	// than a confident default -- a table that always answered would be
	// indistinguishable from one that had stopped working.
	if _, ok := ClassifyByRules(FreeFormRequest{Text: "summarise the last quarter's revenue"}); ok {
		t.Error("an ordinary sentence must miss the table rather than being classified by it")
	}
}

func TestFactsBeatGuesses(t *testing.T) {
	// "draw" appearing in a sentence is a guess about intent, and it is wrong
	// for "what does this diagram draw attention to". An attached image is a
	// fact, so it answers first.
	got, ok := ClassifyByRules(FreeFormRequest{
		Text:            "what does this diagram draw attention to",
		AttachmentTypes: []string{"image/png"},
	})
	if !ok || got.Modality != ModalityVision {
		t.Fatalf("an attached image must outrank a verb in the text; got %+v", got)
	}
}

func TestACacheNamespaceCarriesTheRuleSetHash(t *testing.T) {
	// A verdict is only as good as the rules that failed to answer before it,
	// so a rule change must stop every cached verdict being served -- and
	// putting the hash in the NAMESPACE does that atomically, with no sweep to
	// run and nothing to forget.
	a := FreeFormCacheNamespace("aaaa")
	b := FreeFormCacheNamespace("bbbb")
	if a == b {
		t.Fatal("two rule sets must not share a namespace")
	}
	if !strings.HasPrefix(a, "router.freeform/") {
		t.Errorf("namespace = %q, want the router.freeform prefix", a)
	}
	// An unhashed caller must not collapse every rule set onto one namespace.
	if FreeFormCacheNamespace("") == a {
		t.Error("an empty hash must not share a namespace with a real one")
	}
}

func TestTheRuleSetHashInputChangesWhenTheTableDoes(t *testing.T) {
	// It is built from the rule NAMES in order, so adding, removing or
	// REORDERING a rule changes it -- and reordering matters, because the
	// table's order decides which of two matching rules answers.
	in := RuleSetHashInput()
	for _, name := range []string{"attachment:image", "attachment:audio", "tool:chosen", "verb:image", "verb:speak"} {
		if !strings.Contains(in, name) {
			t.Errorf("the hash input omits rule %q, so a change to it would not invalidate any verdict", name)
		}
	}
	if strings.Index(in, "attachment:image") > strings.Index(in, "verb:image") {
		t.Error("the hash input must preserve table order; facts come before guesses and a reorder must change the hash")
	}
}
