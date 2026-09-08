package parser

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseRuleDecl_FullSurface locks every field a rule can carry, in one
// declaration, so a field added to RuleDecl without a parser arm shows up here
// rather than as a value that is silently always zero.
func TestParseRuleDecl_FullSurface(t *testing.T) {
	source := `@description("Operators reason at the reasoning level")
@when(prompt="agentReply", role="operator")
@level("reasoning")
@policy("federationStrongest")
@precedence(60)
@onUnavailable("park")
@exclude("fleet:qwen3.5:7b")
@exclude("federation:streamClaudeHaiku")
@locked
rule operatorReasoning { }`

	got, err := ParseRuleDecl(source)
	if err != nil {
		t.Fatalf("ParseRuleDecl: %v", err)
	}

	if got.Name != "operatorReasoning" {
		t.Errorf("Name = %q, want operatorReasoning", got.Name)
	}
	if got.Description != "Operators reason at the reasoning level" {
		t.Errorf("Description = %q", got.Description)
	}
	if got.Policy != "federationStrongest" {
		t.Errorf("Policy = %q, want federationStrongest", got.Policy)
	}
	if got.Level != "reasoning" {
		t.Errorf("Level = %q, want reasoning", got.Level)
	}
	if got.Precedence != 60 {
		t.Errorf("Precedence = %d, want 60", got.Precedence)
	}
	if got.OnUnavailable != "park" {
		t.Errorf("OnUnavailable = %q, want park", got.OnUnavailable)
	}
	if !got.Locked {
		t.Error("Locked = false, want true")
	}

	// @exclude is repeatable and accumulates in DECLARATION order. Order is
	// not load-bearing for exclusion the way it is for a chain, but an
	// accumulation that dropped entries would look identical to a rule that
	// excluded fewer models.
	wantExcludes := []string{"fleet:qwen3.5:7b", "federation:streamClaudeHaiku"}
	if !reflect.DeepEqual(got.Excludes, wantExcludes) {
		t.Errorf("Excludes = %v, want %v", got.Excludes, wantExcludes)
	}

	if got.When.Prompt != "agentReply" {
		t.Errorf("When.Prompt = %q, want agentReply", got.When.Prompt)
	}
	if got.When.Role != "operator" {
		t.Errorf("When.Role = %q, want operator", got.When.Role)
	}

	// Present records which keys were WRITTEN, which is a different question
	// from which are non-empty. An absent key is no condition at all; a key
	// written empty is a condition matching only the empty value. Collapsing
	// the two would make a rule with an empty value match every call.
	if !got.When.Has("prompt") || !got.When.Has("role") {
		t.Errorf("When.Present = %v, want prompt and role written", got.When.Present)
	}
	for _, absent := range []string{"level", "modality", "actorRole", "tag", "touches"} {
		if got.When.Has(absent) {
			t.Errorf("When.Present marks %q as written, but the declaration does not state it", absent)
		}
	}
}

// TestParseRuleDecl_EmptyWhenMatchesEverything pins the floor.
//
// @when() with no arguments is LEGAL and states no condition, which is what
// makes the shipped `default` rule match every call. That is not a convenience:
// it is what makes "a call that matches no rule" impossible by construction
// rather than by care, so nothing downstream has to handle the case.
func TestParseRuleDecl_EmptyWhenMatchesEverything(t *testing.T) {
	source := `@when()
@policy("localFirst")
@precedence(0)
@onUnavailable("degrade")
@locked
rule default { }`

	got, err := ParseRuleDecl(source)
	if err != nil {
		t.Fatalf("ParseRuleDecl: %v", err)
	}
	if len(got.When.Present) != 0 {
		t.Errorf("When.Present = %v, want empty -- @when() states no condition", got.When.Present)
	}
}

// TestParseRuleDecl_NoWhenAtAllIsAlsoTheFloor: omitting @when entirely is the
// same statement as writing @when(). Both leave the condition set empty, and a
// rule cannot be forced to state a condition it does not have.
func TestParseRuleDecl_NoWhenAtAllIsAlsoTheFloor(t *testing.T) {
	got, err := ParseRuleDecl("@policy(\"localFirst\")\nrule bare { }")
	if err != nil {
		t.Fatalf("ParseRuleDecl: %v", err)
	}
	if len(got.When.Present) != 0 {
		t.Errorf("When.Present = %v, want empty", got.When.Present)
	}
}

// TestParseRuleDecl_RefusesUnknownWhenKey is the closed-key gate.
//
// The refusal must LIST the seven, because the mistake it catches is a person
// reaching for the thing levels exist to remove: @when(model="gpt-5"). Told
// only "unknown key", they would look for the right spelling of `model` and
// not find that there is none.
func TestParseRuleDecl_RefusesUnknownWhenKey(t *testing.T) {
	source := `@when(model="gpt-5")
@policy("localFirst")
rule namesAModel { }`

	_, err := ParseRuleDecl(source)
	if err == nil {
		t.Fatal("ParseRuleDecl accepted @when(model=...); the key set is closed")
	}
	for _, key := range RuleWhenKeys() {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("the refusal does not list the closed key %q: %v", key, err)
		}
	}
}

// TestParseRuleDecl_RefusesRepeatedWhenKey pins the duplicate refusal.
//
// @when(level="fast", level="strong") collapses last-wins in a map, so the
// value a reader scanning left to right sees is not the value the engine uses
// -- the standing reason a repeated argument name is refused everywhere in this
// DSL. The refusal comes from the generic parseAttribute (memql#2968) rather
// than from the rule parser, and it has to: by the time a per-annotation
// validator sees Attribute.Args, the two spellings are indistinguishable.
func TestParseRuleDecl_RefusesRepeatedWhenKey(t *testing.T) {
	source := `@when(level="fast", level="strong")
@policy("localFirst")
rule twoLevels { }`

	_, err := ParseRuleDecl(source)
	if err == nil {
		t.Fatal("ParseRuleDecl accepted a repeated @when key; only the last value would take effect")
	}
	if !strings.Contains(err.Error(), "level") {
		t.Errorf("the refusal does not name the repeated key: %v", err)
	}
}

// TestParseRuleDecl_RefusesPositionalWhen catches a plausible guess at the
// syntax. @when("reasoning") would otherwise leave the condition set EMPTY,
// producing a rule that matches every call at whatever precedence its author
// gave it -- the worst possible outcome for a typo, because it is silent and
// it wins.
func TestParseRuleDecl_RefusesPositionalWhen(t *testing.T) {
	_, err := ParseRuleDecl("@when(\"reasoning\")\n@policy(\"localFirst\")\nrule positional { }")
	if err == nil {
		t.Fatal("ParseRuleDecl accepted @when with a bare value; it takes keyword arguments")
	}
}

// TestParseRuleDecl_RequiresPolicy: a rule that names no policy would match,
// win over every rule below it, and resolve nothing -- which reads on the
// decision record as "no provider could serve this" rather than as the
// authoring mistake it is.
func TestParseRuleDecl_RequiresPolicy(t *testing.T) {
	_, err := ParseRuleDecl("@when(level=\"fast\")\nrule noPolicy { }")
	if err == nil {
		t.Fatal("ParseRuleDecl accepted a rule with no @policy")
	}
	if !strings.Contains(err.Error(), "@policy") {
		t.Errorf("the refusal does not name @policy: %v", err)
	}
}

// TestParseRuleDecl_RefusesASelectorInPolicy: @policy takes the bare NAME of a
// policy. A selector there is a category error -- the selectors belong inside
// that policy's own chain -- and it would resolve as a policy literally named
// "fleet".
func TestParseRuleDecl_RefusesASelectorInPolicy(t *testing.T) {
	_, err := ParseRuleDecl("@policy(\"fleet:strongest\")\nrule selectorPolicy { }")
	if err == nil {
		t.Fatal("ParseRuleDecl accepted a selector in @policy")
	}
	if !strings.Contains(err.Error(), "@policy") {
		t.Errorf("the refusal does not name @policy: %v", err)
	}
}

// TestParseRuleDecl_RefusesUnknownLevel covers both places a level appears: the
// override and the @when match. A rule matching level="smart" would never fire
// and would say nothing about why.
func TestParseRuleDecl_RefusesUnknownLevel(t *testing.T) {
	for _, source := range []string{
		"@level(\"smart\")\n@policy(\"localFirst\")\nrule badOverride { }",
		"@when(level=\"smart\")\n@policy(\"localFirst\")\nrule badMatch { }",
	} {
		err := parseRuleErr(t, source)
		if err == nil {
			t.Fatalf("ParseRuleDecl accepted an unknown level in:\n%s", source)
		}
		// The message names all four, because it is what the author reads.
		for _, level := range []string{"fast", "strong", "reasoning", "embeddings"} {
			if !strings.Contains(err.Error(), level) {
				t.Errorf("the refusal does not name %q: %v", level, err)
			}
		}
	}
}

// TestParseRuleDecl_RefusesUnknownOnUnavailable pins the two-value set, and
// that the message names BOTH -- an author who guessed "fail" needs to know
// the alternative is "park", not merely that "fail" is wrong.
func TestParseRuleDecl_RefusesUnknownOnUnavailable(t *testing.T) {
	err := parseRuleErr(t, "@policy(\"localFirst\")\n@onUnavailable(\"fail\")\nrule badExhaustion { }")
	if err == nil {
		t.Fatal("ParseRuleDecl accepted @onUnavailable(\"fail\")")
	}
	for _, want := range OnUnavailableValues() {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestParseRuleDecl_RefusesNonIntegerPrecedence. Precedence is the evaluation
// order, not a budget: rounding a fractional value or accepting a word would
// pick an order the author did not write.
func TestParseRuleDecl_RefusesNonIntegerPrecedence(t *testing.T) {
	for _, source := range []string{
		"@policy(\"localFirst\")\n@precedence(\"high\")\nrule wordy { }",
		"@policy(\"localFirst\")\n@precedence(1.5)\nrule fractional { }",
	} {
		if err := parseRuleErr(t, source); err == nil {
			t.Errorf("ParseRuleDecl accepted a non-integer @precedence in:\n%s", source)
		}
	}
}

// TestParseRuleDecl_RefusesNonEmptyBody. A rule is a record whose whole content
// is its annotations. A body walked over and ignored is content somebody wrote
// expecting it to mean something.
func TestParseRuleDecl_RefusesNonEmptyBody(t *testing.T) {
	err := parseRuleErr(t, "@policy(\"localFirst\")\nrule withBody { level \"fast\" }")
	if err == nil {
		t.Fatal("ParseRuleDecl accepted a non-empty body")
	}
	if !strings.Contains(err.Error(), "non-empty body") {
		t.Errorf("the refusal does not say what is wrong: %v", err)
	}
}

// TestParseRuleDecl_RefusesInvalidExclude holds @exclude to the same closed
// entry grammar @primary and @fallback are held to. An exclusion the resolver
// cannot match removes nothing and reports nothing -- a demotion that silently
// did not happen.
func TestParseRuleDecl_RefusesInvalidExclude(t *testing.T) {
	for _, entry := range []string{"cloud:cheapest", "fleet:*", "fleet:"} {
		source := "@policy(\"localFirst\")\n@exclude(\"" + entry + "\")\nrule badExclude { }"
		if err := parseRuleErr(t, source); err == nil {
			t.Errorf("ParseRuleDecl accepted @exclude(%q)", entry)
		}
	}
}

// TestParseRuleDecl_RefusesUnknownAnnotation: the Rule receiver's annotation
// set is the canonical registry, so a typo fails at load rather than reading as
// a rule that governs less than its author thought.
func TestParseRuleDecl_RefusesUnknownAnnotation(t *testing.T) {
	err := parseRuleErr(t, "@policy(\"localFirst\")\n@priority(60)\nrule typo { }")
	if err == nil {
		t.Fatal("ParseRuleDecl accepted @priority; the annotation is @precedence")
	}
	if !strings.Contains(err.Error(), "priority") {
		t.Errorf("the refusal does not name the unknown annotation: %v", err)
	}
}

// TestParseRuleDecl_AttachesDocComment pins the /// path.
//
// Without the RuleDecl arm in attachDocComment the block is parsed, dropped,
// and the rule reads as having no description at all -- a silence that looks
// exactly like an author who wrote none.
func TestParseRuleDecl_AttachesDocComment(t *testing.T) {
	source := `/// Reasoning parks rather than degrading.
@when(level="reasoning")
@policy("federationStrongest")
@onUnavailable("park")
rule reasoningParks { }`

	got, err := ParseRuleDecl(source)
	if err != nil {
		t.Fatalf("ParseRuleDecl: %v", err)
	}
	if !strings.Contains(got.DocComment, "Reasoning parks rather than degrading.") {
		t.Errorf("DocComment = %q, want the /// block", got.DocComment)
	}
}

// TestParseRuleDecl_DefaultsAreAbsences pins what an unstated annotation means.
// An empty OnUnavailable reads as degrade at the consuming end; the parser
// stores the absence rather than substituting a value, so "the author said
// degrade" and "the author said nothing" stay distinguishable in the AST.
func TestParseRuleDecl_DefaultsAreAbsences(t *testing.T) {
	got, err := ParseRuleDecl("@policy(\"localFirst\")\nrule minimal { }")
	if err != nil {
		t.Fatalf("ParseRuleDecl: %v", err)
	}
	if got.OnUnavailable != "" {
		t.Errorf("OnUnavailable = %q, want empty for an unstated annotation", got.OnUnavailable)
	}
	if got.Level != "" {
		t.Errorf("Level = %q, want empty -- an unstated @level does not override the call's", got.Level)
	}
	if got.Precedence != 0 {
		t.Errorf("Precedence = %d, want 0", got.Precedence)
	}
	if got.Locked {
		t.Error("Locked = true without @locked")
	}
	if len(got.Excludes) != 0 {
		t.Errorf("Excludes = %v, want none", got.Excludes)
	}
}

// parseRuleErr runs ParseRuleDecl and returns only the error, failing the test
// if a rule came back alongside one.
func parseRuleErr(t *testing.T, source string) error {
	t.Helper()
	decl, err := ParseRuleDecl(source)
	if err != nil && decl != nil {
		t.Fatalf("ParseRuleDecl returned both a rule (%q) and an error (%v)", decl.Name, err)
	}
	return err
}
