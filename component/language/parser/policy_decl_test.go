package parser

import (
	"reflect"
	"strings"
	"testing"
)

// TestParsePolicyDecl_GoldenPath locks the canonical AI-router
// policy shape: @description + @primary + @fallback + an empty
// `policy NAME { }` declaration.
func TestParsePolicyDecl_GoldenPath(t *testing.T) {
	source := `@description("Local strongest, then an app, then the cheapest federated model.")
@primary("fleet:strongest")
@fallback("app:*")
policy localFirst { }`

	got, err := ParsePolicyDecl(source)
	if err != nil {
		t.Fatalf("ParsePolicyDecl: %v", err)
	}
	if got.Name != "localFirst" {
		t.Errorf("Name = %q, want localFirst", got.Name)
	}
	if got.Description != "Local strongest, then an app, then the cheapest federated model." {
		t.Errorf("Description = %q", got.Description)
	}
	if got.Primary != "fleet:strongest" {
		t.Errorf("Primary = %q, want fleet:strongest", got.Primary)
	}
	if !reflect.DeepEqual(got.Fallbacks, []string{"app:*"}) {
		t.Errorf("Fallbacks = %v, want [app:*]", got.Fallbacks)
	}
}

// TestParsePolicyDecl_RefusesTheThreeInertAnnotations pins the epic memql#5127
// removal.
//
// @maxLatencyMs, @maxTimeToFirstTokenMs and @preferredRole parsed, stored and
// steered NOTHING -- no selection path read any of them. An author who wrote
// one was telling the router something it would not act on, and silence there
// is worse than a refusal: the policy loads, the annotation shows in the
// catalog, and the behaviour is whatever it would have been anyway.
//
// The refusal comes from the Policy receiver no longer carrying them, so the
// message is the ordinary unknown-annotation one and names the annotation.
func TestParsePolicyDecl_RefusesTheThreeInertAnnotations(t *testing.T) {
	for _, tc := range []struct {
		annotation string
		name       string
	}{
		{`@maxLatencyMs(60000)`, "maxLatencyMs"},
		{`@maxTimeToFirstTokenMs(500)`, "maxTimeToFirstTokenMs"},
		{`@preferredRole("operator")`, "preferredRole"},
	} {
		source := "@primary(\"fleet:strongest\")\n" + tc.annotation + "\npolicy someName { }"
		_, err := ParsePolicyDecl(source)
		if err == nil {
			t.Fatalf("ParsePolicyDecl accepted %s; it is removed from the grammar", tc.annotation)
		}
		if !strings.Contains(err.Error(), tc.name) {
			t.Errorf("the refusal of %s does not name it: %v", tc.annotation, err)
		}
	}
}

// TestParsePolicyDecl_MultipleFallbacks locks the repeatable-annotation
// behaviour: multiple @fallback annotations accumulate in declaration order.
// Order is the whole content of a policy -- it is three kinds of cost in
// increasing order -- so an accumulation that reordered would invert the
// meaning while every name stayed right.
func TestParsePolicyDecl_MultipleFallbacks(t *testing.T) {
	source := `@primary("fleet:strongest")
@fallback("app:*")
@fallback("federation:cheapest")
@fallback("federation:strongest")
policy multiChain { }`

	got, err := ParsePolicyDecl(source)
	if err != nil {
		t.Fatalf("ParsePolicyDecl: %v", err)
	}
	want := []string{"app:*", "federation:cheapest", "federation:strongest"}
	if !reflect.DeepEqual(got.Fallbacks, want) {
		t.Errorf("Fallbacks = %v, want %v", got.Fallbacks, want)
	}
}

// TestParsePolicyDecl_MinimalShape locks a policy with only
// @primary (no fallbacks). The
// converter on the memql side enforces @primary being present;
// the parser itself is permissive.
func TestParsePolicyDecl_MinimalShape(t *testing.T) {
	source := `@primary("onlyProvider")
policy minimal { }`

	got, err := ParsePolicyDecl(source)
	if err != nil {
		t.Fatalf("ParsePolicyDecl: %v", err)
	}
	if got.Primary != "onlyProvider" {
		t.Errorf("Primary = %q, want onlyProvider", got.Primary)
	}
	if len(got.Fallbacks) != 0 {
		t.Errorf("Fallbacks = %v, want empty", got.Fallbacks)
	}
}

// TestParsePolicyDecl_RejectsUnknownAttribute locks the memql#2395
// HOLE-1 fix: an unknown @-annotation is REJECTED against the canonical
// annotations.ByReceiver registry (the previous drain-and-skip tolerance
// silently swallowed typos like @primry; a future tuning knob ships by
// adding it to the registry first, which is the single source of truth).
func TestParsePolicyDecl_RejectsUnknownAttribute(t *testing.T) {
	source := `@primary("p")
@futureKnob("ignored")
policy tolerant { }`

	_, err := ParsePolicyDecl(source)
	if err == nil {
		t.Fatal("ParsePolicyDecl accepted an unknown annotation @futureKnob (registry gate missing)")
	}
	if !strings.Contains(err.Error(), "unknown annotation @futureKnob") {
		t.Errorf("error should name the unknown annotation, got: %v", err)
	}
}

// TestParsePolicyDecl_RejectsMissingName errors when the policy
// keyword isn't followed by an identifier.
func TestParsePolicyDecl_RejectsMissingName(t *testing.T) {
	source := `@primary("p")
policy { }`

	_, err := ParsePolicyDecl(source)
	if err == nil {
		t.Fatal("expected error for missing policy name, got nil")
	}
	if !strings.Contains(err.Error(), "policy") {
		t.Errorf("error should mention policy, got %v", err)
	}
}
