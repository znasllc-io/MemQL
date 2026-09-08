package memql

import (
	"strings"
	"testing"
)

// TestParsePolicyMemQL_GoldenPath locks the AI-router policy
// surface: @description + @primary + @fallback + an empty
// `policy NAME { }` declaration.
//
// The three tuning knobs this used to assert -- @maxLatencyMs,
// @maxTimeToFirstTokenMs, @preferredRole -- are removed with epic memql#5127.
// Note where they went in THIS parser specifically: it is the legacy
// hand-rolled path, whose default branch SKIPS an unrecognised decorator, so
// removing their cases makes it ignore them rather than refuse them. The
// refusal lives on the production path (the langparser's Policy receiver),
// which is what a .memql file in the tree actually goes through.
func TestParsePolicyMemQL_GoldenPath(t *testing.T) {
	src := []byte(`@description("Local strongest, then an app, then the cheapest federated model.")
@primary("fleet:strongest")
@fallback("app:*")
policy localFirst { }`)

	cfg, err := parsePolicyMemQL("test.memql", src)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil *PolicyConfig")
	}
	if cfg.Name != "localFirst" {
		t.Errorf("Name = %q, want localFirst", cfg.Name)
	}
	if cfg.Primary != "fleet:strongest" {
		t.Errorf("Primary = %q, want fleet:strongest", cfg.Primary)
	}
	if len(cfg.Fallbacks) != 1 || cfg.Fallbacks[0] != "app:*" {
		t.Errorf("Fallbacks = %v, want [app:*]", cfg.Fallbacks)
	}
}

// TestParsePolicyMemQL_ProviderChainOrder locks the ordering
// semantics: primary first, then fallbacks in declaration order.
func TestParsePolicyMemQL_ProviderChainOrder(t *testing.T) {
	src := []byte(`@primary("primaryProvider")
@fallback("fallbackA")
@fallback("fallbackB")
policy chain { }`)

	cfg, err := parsePolicyMemQL("test.memql", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	chain := cfg.ProviderChain()
	if len(chain) != 3 {
		t.Fatalf("ProviderChain len = %d, want 3", len(chain))
	}
	want := []string{"primaryProvider", "fallbackA", "fallbackB"}
	for i, w := range want {
		if chain[i] != w {
			t.Errorf("chain[%d] = %q, want %q", i, chain[i], w)
		}
	}
}

// TestParsePolicyMemQL_RequiresPrimary locks the rule: a routing
// policy without @primary is an error.
func TestParsePolicyMemQL_RequiresPrimary(t *testing.T) {
	src := []byte(`@description("missing primary")
@fallback("a")
policy orphan { }`)

	_, err := parsePolicyMemQL("test.memql", src)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "@primary") {
		t.Errorf("error should mention @primary, got %v", err)
	}
}

// TestParsePolicyMemQL_RequiresPolicyDeclaration locks the rule:
// a file without `policy NAME { }` is an error.
func TestParsePolicyMemQL_RequiresPolicyDeclaration(t *testing.T) {
	src := []byte(`@primary("foo")`)

	_, err := parsePolicyMemQL("test.memql", src)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "policy") {
		t.Errorf("error should mention missing policy declaration, got %v", err)
	}
}
