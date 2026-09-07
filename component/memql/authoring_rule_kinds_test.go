package memql

import (
	"strings"
	"testing"
)

// TestSandboxCompilesRuleAndPolicy. Both kinds used to fall through the
// sandbox's default arm and be reported "not compiled by the sandbox" -- a
// graceful skip, which meant a runtime-authored rule reached activation
// without its grammar ever having been checked.
func TestSandboxCompilesRuleAndPolicy(t *testing.T) {
	report := SandboxCompileBundle([]SandboxConstruct{
		{
			Name: "myRule",
			Kind: "rule",
			Source: `@when(prompt="agentReply")
@policy("localFirst")
@precedence(500)
rule myRule { }`,
		},
		{
			Name: "myPolicy",
			Kind: "policy",
			Source: `@primary("fleet:strongest")
@fallback("federation:cheapest")
policy myPolicy { }`,
		},
	})
	if !report.OK {
		t.Fatalf("a valid rule and policy did not compile: %+v", report.Diagnostics)
	}
	for _, d := range report.Diagnostics {
		if d.Skipped {
			t.Fatalf("kind %q was SKIPPED rather than compiled: %s.\n"+
				"A skip here is what let an authored construct reach activation with its grammar "+
				"unchecked.", d.Kind, d.Error)
		}
	}
}

// TestSandboxRefusesAnAuthoredLockedRule is one of the two refusals design D7
// names.
//
// The check is here as well as in the loader because the loader's keys on which
// TREE a file came from, and an authored construct comes from no tree at all --
// so without this arm the annotation would pass Gate 1, reach activation, and
// be refused there instead: later, further from the author, and with a message
// about trees they have no file in.
func TestSandboxRefusesAnAuthoredLockedRule(t *testing.T) {
	report := SandboxCompileBundle([]SandboxConstruct{{
		Name: "sneaky",
		Kind: "rule",
		Source: `@when()
@policy("localFirst")
@precedence(9999)
@locked
rule sneaky { }`,
	}})
	if report.OK {
		t.Fatal("an authored rule carrying @locked compiled; it would then evaluate before every shipped rule")
	}
	joined := ""
	for _, d := range report.Diagnostics {
		joined += d.Error + "\n"
	}
	for _, want := range []string{"@locked", "embedded tree", "@precedence"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the refusal does not mention %q, so it does not tell the author what to do instead:\n%s", want, joined)
		}
	}
}

// TestSandboxRefusesAMalformedRule. The point of routing an authored construct
// through the real parser is that a form the grammar refuses never reaches the
// registry.
func TestSandboxRefusesAMalformedRule(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"no policy", "@when()\n@precedence(5)\nrule noPolicy { }"},
		{"an unknown condition key", "@when(model=\"gpt-5\")\n@policy(\"localFirst\")\n@precedence(5)\nrule badKey { }"},
		{"an unknown level", "@when()\n@level(\"smart\")\n@policy(\"localFirst\")\n@precedence(5)\nrule badLevel { }"},
		{"a non-empty body", "@when()\n@policy(\"localFirst\")\n@precedence(5)\nrule hasBody { step x { } }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := tc.source[strings.LastIndex(tc.source, "rule ")+5:]
			name = strings.TrimSpace(name[:strings.Index(name, " ")])
			report := SandboxCompileBundle([]SandboxConstruct{{Name: name, Kind: "rule", Source: tc.source}})
			if report.OK {
				t.Fatalf("the sandbox accepted a rule with %s", tc.name)
			}
		})
	}
}

// TestEngineCoreHasCoversRuleAndPolicy is the OTHER refusal design D7 names,
// at the layer that enforces it: the authoring pipeline's core-first resolver.
// A kind absent from this switch reports false, which reads as "core does not
// own this name" and lets an authored construct take it.
func TestEngineCoreHasCoversRuleAndPolicy(t *testing.T) {
	e := &MemQLEngine{}
	e.rules = NewRuleRegistry()
	if err := e.rules.Register(&RuleConfig{
		Name: "shippedThing", Policy: "localFirst", SourceFile: "rules/rules.memql",
		When: RuleWhen{Present: map[string]bool{}},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	e.policies = &PolicyRegistry{byName: map[string]*PolicyConfig{
		"shippedPolicy": {Name: "shippedPolicy", Primary: "fleet:strongest"},
	}}

	has := EngineCoreHas(e)
	if !has("rule", "shippedThing") {
		t.Error("core-first does not see a loaded rule, so an authored one could take its name")
	}
	if !has("policy", "shippedPolicy") {
		t.Error("core-first does not see a loaded policy, so an authored one could take its name")
	}
	if has("rule", "somethingElse") || has("policy", "somethingElse") {
		t.Error("core-first claims a name nothing loaded, which would refuse a legitimate authored construct")
	}
}
