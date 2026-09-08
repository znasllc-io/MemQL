package router

import (
	"fmt"
	"sort"
	"strings"
)

// A described rule: a sentence, compiled once, into a rule a person confirms
// (epic memql#5137, D7).
//
// ===========================================================================
// COMPILED ONCE, NOT EVALUATED PER REQUEST
// ===========================================================================
// A person types "nothing that touches campaign recipients leaves the fleet".
// The alternative to compiling that is asking a model, per request, whether
// this request touches campaign recipients -- a model in front of every model
// call, which is the cost the whole program exists to avoid. So the sentence is
// compiled to a `rule` once, pinned, and from then on the router matches
// structurally at no cost.
//
// The owner's cache argument, adopted: the sentence does not change at request
// time, so its hash is the key and the compiled rule is the value.
//
// ===========================================================================
// THE COMPILED FORM IS THE CONTRACT, THE SENTENCE IS THE RECORD
// ===========================================================================
// What runs is the rule. The sentence is kept as the rule's description so a
// later reader can see what was meant -- but if the two disagree, the rule is
// what the cluster does, and the simulation is how a person finds that out
// BEFORE confirming rather than from a bill afterwards.

// WhenKeys is the closed `@when` vocabulary (epic memql#5127).
//
// RESTATED HERE RATHER THAN IMPORTED, and the duplication is deliberate: this
// list is handed TO THE MODEL as the vocabulary it may use. A compiler told a
// list that drifted from the loader's would emit rules that refuse to load, and
// the person would see a confident restatement of a rule that never activates.
// The parity gate is that both are asserted against the same literals.
var WhenKeys = []string{"level", "modality", "prompt", "role", "actorRole", "tag", "touches"}

// ShippedPolicies are the policy names a compiled rule may name.
//
// A compiled rule may NAME a shipped policy and may not REDEFINE one. Letting a
// sentence mint a new policy would put a provider chain in a place no review
// ever looks, which is the shape of the problem this epic removed from Go.
var ShippedPolicies = []string{"localFirst", "localOnly", "federationStrongest", "embeddingsBinding"}

// ShippedRuleNames are the rule names a compiled rule may NOT take.
//
// Shipped rules are locked and re-seeded on every boot, so a custom rule taking
// one of their names is either silently replaced at the next restart or
// silently replaces the shipped one -- and which of those happens depends on
// load order, which is not a thing anybody should have to know.
var ShippedRuleNames = []string{"default", "localFirst", "operatorReasoning", "compilerLocalOnly", "embeddingsBound"}

// OnUnavailableValues is the closed set for what happens when a rule's policy
// resolves to nothing.
var OnUnavailableValues = []string{"degrade", "park"}

// CompiledRule is what a sentence becomes.
type CompiledRule struct {
	// Name is the rule's identifier, lowerCamelCase.
	Name string
	// Conditions are the `@when` keys. EMPTY IS LEGAL and means "every call" --
	// that is how the shipped `default` rule is the floor -- but a compiled
	// rule with no conditions is almost certainly not what a person meant, and
	// Validate says so rather than refusing: it is their cluster.
	Conditions map[string]string
	// Policy the rule routes to. Required.
	Policy string
	// Level overrides the call's, when the sentence said one.
	Level string
	// Precedence decides between rules that both match; higher wins.
	Precedence int
	// OnUnavailable is degrade or park. Empty means degrade.
	OnUnavailable string
	// Excludes are `fleet:<modelId>` entries the rule rules out.
	Excludes []string
	// Restatement is the compiler's one-sentence account of what it produced,
	// shown beside the person's own words. NOT the sentence back -- a compiler
	// that echoes the input proves nothing about what it understood.
	Restatement string
	// Sentence is what the person typed, kept as the rule's description.
	Sentence string
}

// Warning is a thing a person should see before confirming, which is not a
// reason to refuse.
type Warning struct {
	Field string
	Text  string
}

// Validate checks a compiled rule against the closed vocabularies.
//
// IT SEPARATES REFUSALS FROM WARNINGS, and the split is the design. A rule
// naming a policy that does not exist cannot be activated -- the loader would
// refuse it and the person would be told nothing useful. A rule that matches
// every call CAN be activated and is probably a mistake, so it is surfaced and
// left to them: it is their cluster, and a compiler that refused anything
// surprising would be unable to express the shipped rules themselves.
func (r CompiledRule) Validate() ([]Warning, error) {
	if strings.TrimSpace(r.Name) == "" {
		return nil, fmt.Errorf("compiled rule has no name")
	}
	for _, shipped := range ShippedRuleNames {
		if strings.EqualFold(strings.TrimSpace(r.Name), shipped) {
			return nil, fmt.Errorf(
				"a custom rule may not be called %q: that is a shipped rule, re-seeded on every boot, "+
					"so one of the two would silently replace the other depending on load order", shipped)
		}
	}
	if strings.TrimSpace(r.Policy) == "" {
		return nil, fmt.Errorf("rule %q names no policy; @policy is required", r.Name)
	}
	if !containsString(ShippedPolicies, strings.TrimSpace(r.Policy)) {
		return nil, fmt.Errorf(
			"rule %q names policy %q, which is not one of the shipped policies (%s). A compiled rule may "+
				"NAME a policy and may not mint one -- a provider chain minted from a sentence would be a "+
				"spending decision in a place no review looks",
			r.Name, r.Policy, strings.Join(ShippedPolicies, ", "))
	}
	for key := range r.Conditions {
		if !containsString(WhenKeys, key) {
			return nil, fmt.Errorf(
				"rule %q uses condition %q, which is not in the @when vocabulary (%s). The loader would "+
					"refuse this rule, so activating it would leave a rule that never runs and a person who "+
					"believes it does",
				r.Name, key, strings.Join(WhenKeys, ", "))
		}
	}
	if u := strings.TrimSpace(r.OnUnavailable); u != "" && !containsString(OnUnavailableValues, u) {
		return nil, fmt.Errorf("rule %q sets onUnavailable=%q; the values are %s", r.Name, u, strings.Join(OnUnavailableValues, " or "))
	}
	for _, ex := range r.Excludes {
		if !strings.HasPrefix(ex, "fleet:") {
			return nil, fmt.Errorf("rule %q excludes %q; an exclusion names a fleet model as fleet:<modelId>", r.Name, ex)
		}
	}

	var warnings []Warning
	if len(r.Conditions) == 0 {
		warnings = append(warnings, Warning{
			Field: "when",
			Text:  "This rule matches every call. That is legal and is how the shipped default works, but it is rarely what a sentence about a particular kind of work means.",
		})
	}
	if strings.TrimSpace(r.Restatement) == "" {
		warnings = append(warnings, Warning{
			Field: "restatement",
			Text:  "The compiler produced no restatement, so there is nothing to check the rule against except the rule itself.",
		})
	}
	sort.SliceStable(warnings, func(i, j int) bool { return warnings[i].Field < warnings[j].Field })
	return warnings, nil
}

// CompilerPromptName is the prompt that does the compiling.
//
// NAMED AS A CONSTANT because the shipped locked rule `compilerLocalOnly`
// matches on it (`@when(prompt="compileRule")`). If the two ever disagree the
// compiler resolves through the ordinary rules instead -- which means a rule
// about how to spend money could be compiled by a paid model, silently, and the
// only sign would be a line in a decision record nobody was looking at.
const CompilerPromptName = "compileRule"

// CompilerRuleName is the locked rule that keeps the compiler local.
const CompilerRuleName = "compilerLocalOnly"

// CompilerPolicyName is the policy that rule routes to.
const CompilerPolicyName = "localOnly"
