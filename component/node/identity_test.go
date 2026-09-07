package node

import (
	"os"
	"testing"
)

func TestNewIdentity_Defaults(t *testing.T) {
	// Ensure env vars are clean
	os.Unsetenv("MEMQL_NODE_TYPE")
	os.Unsetenv("MEMQL_NODE_ID")
	os.Unsetenv("MEMQL_NODE_ADDRESS")
	os.Unsetenv("MEMQL_PARENT_ADDRESS")
	os.Unsetenv("MEMQL_NODE_LABELS")

	id := NewIdentity("1.0.0")

	// Tagged binaries force their compiled type; standalone respects env var
	expected := CompiledNodeType()
	if id.Type != expected {
		t.Errorf("expected %s, got %s", expected, id.Type)
	}
	if id.ID == "" {
		t.Error("expected generated UUID, got empty")
	}
	if id.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", id.Version)
	}
	if id.HasParent() {
		t.Error("expected HasParent() to be false")
	}
}

func TestNewIdentity_EnvVars(t *testing.T) {
	t.Setenv("MEMQL_NODE_TYPE", "bff")
	t.Setenv("MEMQL_NODE_ID", "test-node-123")
	t.Setenv("MEMQL_NODE_ADDRESS", "localhost:50052")
	t.Setenv("MEMQL_PARENT_ADDRESS", "parent:50052")
	t.Setenv("MEMQL_NODE_LABELS", "region=us-central1,env=staging")

	id := NewIdentity("2.0.0")

	// Compiled node type always takes precedence. Default builds compile as BFF.
	compiled := CompiledNodeType()
	if id.Type != compiled {
		t.Errorf("expected %s (compiled type), got %s", compiled, id.Type)
	}

	if id.ID != "test-node-123" {
		t.Errorf("expected test-node-123, got %s", id.ID)
	}
	if id.Address != "localhost:50052" {
		t.Errorf("expected localhost:50052, got %s", id.Address)
	}
	if id.ParentAddress != "parent:50052" {
		t.Errorf("expected parent:50052, got %s", id.ParentAddress)
	}
	if !id.HasParent() {
		t.Error("expected HasParent() to be true")
	}
	if id.Labels["region"] != "us-central1" {
		t.Errorf("expected label region=us-central1, got %s", id.Labels["region"])
	}
	if id.Labels["env"] != "staging" {
		t.Errorf("expected label env=staging, got %s", id.Labels["env"])
	}
}

func TestNewIdentity_HostnameFallback(t *testing.T) {
	// When MEMQL_NODE_ID is unset, the node id falls back to the
	// container/host hostname. In k8s/k3d the manifests stamp
	// MEMQL_NODE_ID from `fieldRef: metadata.name`, so each pod replica
	// gets a unique id; this hostname fallback is the same per-replica
	// guarantee for any runtime that leaves the env unset -- the parity
	// fix for memql#1212/#1217 (cross-node fan-out reproduction).
	os.Unsetenv("MEMQL_NODE_ID")

	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("os.Hostname() unavailable on this platform; fallback is to a random short id")
	}

	id := NewIdentity("1.0.0")

	if id.ID != host {
		t.Errorf("expected node id to fall back to hostname %q, got %q", host, id.ID)
	}
}

// The WIRING half of the precedence rule: that NewIdentity reads both the
// compiled type AND the tagged flag and defers to resolveNodeType, which is
// where the decision itself is tested (node_type_precedence_test.go covers
// every combination on any build). Wiring is what memql#5115 got wrong -- the
// decision was never written down as one place to get wrong.
//
// Stated against resolveNodeType rather than against a literal because the
// right answer DEPENDS on the build tag this test is compiled under, and the
// point is that all seven give an answer rather than that one of them does.
func TestNewIdentity_BuildTagBeatsTheEnvironment(t *testing.T) {
	t.Setenv("MEMQL_NODE_TYPE", "identity")

	id := NewIdentity("1.0.0")

	want, _ := resolveNodeType(CompiledNodeType(), CompiledNodeTypeIsTagged(), NodeTypeIdentity)
	if id.Type != want {
		t.Errorf("MEMQL_NODE_TYPE=identity on a compiled=%q tagged=%v build resolved to %q, want %q",
			CompiledNodeType(), CompiledNodeTypeIsTagged(), id.Type, want)
	}

	// The #430 invariant, in the form that survives the precedence fix: on an
	// UNTAGGED build an explicit non-mesh MEMQL_NODE_TYPE is honoured verbatim
	// and must not fall back to the compiled bff default. Falling back would
	// pass the `Type == NodeTypeBFF` gate in app/cluster.go, start the worker
	// dialer, and dial every peer tokenless -- "node auth: token extraction
	// failed" every 30s. A tagged build does not reach this: it is what its
	// tag says, and a tag that disagrees with the manifest is a warned
	// misconfiguration rather than a type to negotiate.
	if !CompiledNodeTypeIsTagged() && ValidNodeTypes[id.Type] {
		t.Errorf("untagged build asked for non-mesh type %q resolved to mesh type %q (it would re-enable peer dialing)",
			"identity", id.Type)
	}
}

func TestCompiledNodeType(t *testing.T) {
	compiled := CompiledNodeType()
	if compiled == "" {
		t.Fatal("CompiledNodeType() is empty -- a compiled_<tag>.go left compiledNodeType unset")
	}
	// NOT `ValidNodeTypes[compiled]`, which this asserted until memql#5115.
	// ValidNodeTypes is the MESH-DIALABLE subset, and identity and edge are
	// real node roles deliberately outside it; asserting membership here is
	// what a missing compiled_<tag>.go looked like from the inside -- the two
	// roles with no file compiled as bff, so the assertion passed by being
	// asked the wrong question.
	if !CompiledNodeTypeIsTagged() && compiled != NodeTypeBFF {
		t.Errorf("an untagged build must compile as %q, got %q", NodeTypeBFF, compiled)
	}
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]string
	}{
		{"", map[string]string{}},
		{"key=val", map[string]string{"key": "val"}},
		{"a=1,b=2,c=3", map[string]string{"a": "1", "b": "2", "c": "3"}},
		{" a = 1 , b = 2 ", map[string]string{"a": "1", "b": "2"}},
		{"noequals", map[string]string{}},
	}

	for _, tt := range tests {
		result := parseLabels(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseLabels(%q): got %d entries, want %d", tt.input, len(result), len(tt.expected))
			continue
		}
		for k, v := range tt.expected {
			if result[k] != v {
				t.Errorf("parseLabels(%q)[%s] = %q, want %q", tt.input, k, result[k], v)
			}
		}
	}
}
