package node

import "testing"

// The precedence between the build tag and MEMQL_NODE_TYPE is decided by a
// PURE function, so every combination is covered by `make test` on an untagged
// build. That matters more here than it usually would: the inputs the bug
// lived in are BUILD-TAG-SELECTED (`compiled_<tag>.go`), so a test that reads
// them through `NewIdentity` only checks the one combination the build it runs
// under happens to compile. memql#5115 sat behind exactly that -- the four
// tagged builds that got it wrong were never run by any lane, and the untagged
// build they were tested on was the one combination where the answer agreed.
func TestResolveNodeType(t *testing.T) {
	tests := []struct {
		name         string
		compiled     NodeType
		tagged       bool
		env          NodeType
		want         NodeType
		wantOverride bool
	}{
		// A TAGGED binary is what its tag says. This is the case memql#5115
		// got backwards: the env won, so a `-tags agent` binary whose
		// manifest said `bff` reported bff and passed the
		// `Type == NodeTypeBFF` gate in app/cluster.go -- starting the worker
		// mesh's WorkerDialer on a binary wired as an agent.
		{"tagged agent, env agrees", NodeTypeAgent, true, NodeTypeAgent, NodeTypeAgent, false},
		{"tagged agent, env disagrees", NodeTypeAgent, true, NodeTypeBFF, NodeTypeAgent, true},
		{"tagged agent, env empty", NodeTypeAgent, true, "", NodeTypeAgent, false},
		{"tagged planner, env bff", NodeTypePlanner, true, NodeTypeBFF, NodeTypePlanner, true},
		{"tagged workbench, env bff", NodeTypeWorkbench, true, NodeTypeBFF, NodeTypeWorkbench, true},
		{"tagged mcp, env bff", NodeTypeMCP, true, NodeTypeBFF, NodeTypeMCP, true},
		{"tagged bff, env agent", NodeTypeBFF, true, NodeTypeAgent, NodeTypeBFF, true},

		// The two node roles that are deliberately NOT mesh types get their
		// answer from the tag as well, which is what they were missing: with
		// no `compiled_<tag>.go` they compiled as the untagged default and
		// depended entirely on the manifest setting MEMQL_NODE_TYPE.
		{"tagged identity, env agrees", NodeTypeIdentity, true, NodeTypeIdentity, NodeTypeIdentity, false},
		{"tagged identity, env empty", NodeTypeIdentity, true, "", NodeTypeIdentity, false},
		{"tagged edge, env empty", NodeTypeEdge, true, "", NodeTypeEdge, false},
		{"tagged edge, env bff", NodeTypeEdge, true, NodeTypeBFF, NodeTypeEdge, true},

		// An UNTAGGED build has no opinion of its own, so MEMQL_NODE_TYPE
		// selects -- mesh type or not. This is the half of the docstring that
		// was true before and has to stay true: `go build .` plus an env var
		// is how a single binary plays another role.
		{"untagged, env agent", NodeTypeBFF, false, NodeTypeAgent, NodeTypeAgent, false},
		{"untagged, env identity", NodeTypeBFF, false, NodeTypeIdentity, NodeTypeIdentity, false},
		{"untagged, env edge", NodeTypeBFF, false, NodeTypeEdge, NodeTypeEdge, false},
		{"untagged, env unknown honored verbatim", NodeTypeBFF, false, NodeType("sidecar"), NodeType("sidecar"), false},
		{"untagged, env empty defaults to bff", NodeTypeBFF, false, "", NodeTypeBFF, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, override := resolveNodeType(tt.compiled, tt.tagged, tt.env)
			if got != tt.want {
				t.Errorf("resolveNodeType(%q, %v, %q) = %q, want %q", tt.compiled, tt.tagged, tt.env, got, tt.want)
			}
			if override != tt.wantOverride {
				t.Errorf("resolveNodeType(%q, %v, %q) override = %v, want %v", tt.compiled, tt.tagged, tt.env, override, tt.wantOverride)
			}
		})
	}
}

// A tagged binary never yields to the environment, whatever the environment
// says. Stated as a property over every node type rather than as one more row
// above, because the failure mode was one specific pair (compiled agent, env
// bff) being reachable at all -- not that pair being wrong in isolation.
func TestTaggedNodeTypeNeverYieldsToTheEnvironment(t *testing.T) {
	roles := []NodeType{
		NodeTypeAgent, NodeTypePlanner, NodeTypeBFF,
		NodeTypeWorkbench, NodeTypeMCP, NodeTypeIdentity, NodeTypeEdge,
	}
	for _, compiled := range roles {
		for _, env := range append(append([]NodeType{}, roles...), "", "sidecar") {
			got, _ := resolveNodeType(compiled, true, env)
			if got != compiled {
				t.Errorf("tagged %q with MEMQL_NODE_TYPE=%q resolved to %q; the tag must win", compiled, env, got)
			}
		}
	}
}
