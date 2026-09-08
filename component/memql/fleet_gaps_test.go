package memql

// The other three gaps the local-models epic left open (epic memql#5096,
// task memql#5098, design D6): embeddings that never reached the fleet, and a
// fleet model that could not serve a turn with tools.
//
// Each is asserted at the seam that was broken rather than end to end, because
// each broke in a DIFFERENT way and one end-to-end test would report all three
// as "the fleet was not used".

import (
	"context"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/core/common"
)

func embeddingModel(id string) FleetModel {
	m := onlineModel(id, false)
	m.Embeddings = true
	return m
}

// EmbeddingProvider used to read r.byName directly -- the one map a dynamic
// name is deliberately absent from. So `fleet:nomic-embed-text` answered "not
// found", and the seeded local embeddings policy had no consumer that could
// ever have worked.
func TestEmbeddingProviderResolvesAFleetName(t *testing.T) {
	r := newProviderRegistry("")
	r.SetFleetInference(&stubFleet{models: []FleetModel{embeddingModel("nomic-embed-text")}})

	provider, err := r.EmbeddingProvider(userCtx("alice"), FleetReferencePrefix+"nomic-embed-text")
	if err != nil {
		t.Fatalf("a fleet embedding model must resolve: %v", err)
	}
	if provider == nil {
		t.Fatal("resolved with no error and no provider")
	}
	// Zero is the honest dimensionality for a local model: every runtime in
	// the v1 set reports the vector length only by producing one.
	if got := provider.Dimensions(); got != 0 {
		t.Errorf("Dimensions() = %d, want 0 (unknown) for a local model", got)
	}
}

// The acting user decides WHOSE machines are eligible, and the embedding path
// must carry it exactly as the chat path does. Resolved context-free, a user's
// own awake laptop is invisible and the call quietly uses whatever else is
// registered.
func TestAFleetEmbeddingResolvesAgainstTheCallersMachines(t *testing.T) {
	fleet := &stubFleet{models: []FleetModel{embeddingModel("nomic-embed-text")}}
	r := newProviderRegistry("")
	r.SetFleetInference(fleet)

	if _, err := r.EmbeddingProvider(userCtx("alice"), FleetReferencePrefix+"nomic-embed-text"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if fleet.lastActor != "alice" {
		t.Errorf("catalog was read for %q, want alice", fleet.lastActor)
	}

	// And with no actor it is SYSTEM work -- the shared-inference set, not
	// "any machine". An unresolved caller must narrow the fleet, never widen
	// it.
	if _, err := r.EmbeddingProvider(context.Background(), FleetReferencePrefix+"nomic-embed-text"); err != nil {
		t.Fatalf("resolve as system work: %v", err)
	}
	if fleet.lastActor != "" {
		t.Errorf("catalog was read for %q, want the shared set", fleet.lastActor)
	}
}

// An unreachable fleet embedding model reports UNAVAILABLE rather than "not
// found". They read identically to a caller and have opposite fixes: one is a
// closed laptop, the other a typo.
func TestAnOfflineFleetEmbeddingModelIsUnavailableNotMissing(t *testing.T) {
	offline := embeddingModel("nomic-embed-text")
	offline.Machines[0].Online = false
	r := newProviderRegistry("")
	r.SetFleetInference(&stubFleet{models: []FleetModel{offline}})

	_, err := r.EmbeddingProvider(userCtx("alice"), FleetReferencePrefix+"nomic-embed-text")
	if err == nil {
		t.Fatal("an offline embedding model must not resolve to a usable provider")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("error = %q, want it to say unavailable rather than not found", err)
	}
}

// The tool-calling surface, end to end through the provider: schemas go OUT to
// the runtime and calls come BACK, rather than the schema being described in
// the prompt and the answer parsed out of prose.
func TestAFleetModelCanServeAToolCallingTurn(t *testing.T) {
	fleet := &stubFleet{
		models: []FleetModel{onlineModel("llama3.1:8b", true)},
		answer: "",
		toolCalls: []common.ToolCall{
			{ID: "call_a", Name: "searchLibrary", Arguments: `{"q":"invoice"}`},
		},
	}
	r := newProviderRegistry("")
	r.SetFleetInference(fleet)

	entry, ok := r.EntryForUser(userCtx("alice"), "alice", FleetReferencePrefix+"llama3.1:8b")
	if !ok || !entry.Available {
		t.Fatal("the model must resolve as available")
	}
	tp, ok := entry.Client.(common.ToolCallingChatAIProvider)
	if !ok {
		t.Fatal("a fleet provider must implement the tool-calling surface; without it the router " +
			"skips every fleet model for any turn that offers tools, which is every agent turn")
	}

	res, err := tp.CallChatWithTools(
		userCtx("alice"),
		[]common.ChatMessage{{Role: "user", Content: "find the invoice"}},
		[]common.ToolDefinition{{
			Name:        "searchLibrary",
			Description: "Search the Library",
			InputSchema: map[string]any{"type": "object"},
		}},
	)
	if err != nil {
		t.Fatalf("tool turn: %v", err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "searchLibrary" {
		t.Fatalf("tool calls = %+v, want the one the runtime made", res.ToolCalls)
	}
	if len(fleet.lastReq.Tools) != 1 {
		t.Fatalf("the tool definitions must reach the runtime; got %d", len(fleet.lastReq.Tools))
	}
	// The presence of tools is what makes the call REQUIRE a tool-capable
	// model, exactly as a schema's presence requires a structured one. A
	// caller cannot offer tools and forget to ask for a model that can use
	// them.
	if !fleet.lastReq.Needs().Tools {
		t.Error("a request carrying tools must derive a Tools need")
	}
}

// A tool turn on a fleet with no tool-capable machine is UNAVAILABLE, not a
// prose answer. A runtime handed tools it cannot honour answers text, and the
// failure then surfaces three layers away as an agent that stopped using its
// tools for no reason anyone can see.
func TestAToolTurnNeedsAToolCapableModel(t *testing.T) {
	needs := FleetCallRequest{
		Kind:  FleetKindChat,
		Tools: []common.ToolDefinition{{Name: "searchLibrary"}},
	}.Needs()
	if !needs.Tools {
		t.Fatal("Needs() must report a tool requirement")
	}

	plain := FleetCallRequest{Kind: FleetKindChat}.Needs()
	if plain.Tools {
		t.Error("an ordinary chat turn must not require tool support -- doing so would take every " +
			"model that never advertised it out of the fleet for work it can do")
	}
}

// The catalog's Tools flag is the UNION across the machines behind a model, so
// one tool-capable machine makes the model tool-capable. Taking the
// intersection would hide a capability the fleet demonstrably has whenever one
// machine under-reports.
func TestTheCatalogReportsToolSupportAsACapability(t *testing.T) {
	m := onlineModel("llama3.1:8b", true)
	m.Tools = true
	r := newProviderRegistry("")
	r.SetFleetInference(&stubFleet{models: []FleetModel{m}})

	models, err := r.FleetCatalog(userCtx("alice"), "alice")
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if len(models) != 1 || !models[0].Tools {
		t.Fatalf("catalog = %+v, want the model to report tool support", models)
	}
}

// The engine-side local-refusal guard is GONE (epic memql#5127, design D2).
// It existed because InvokeAIStructured's registry-wide scan would otherwise
// fall through a shut local door to a paid vendor, silently; there is no scan
// left to fall through. The property it protected -- an unavailable fleet model
// yields the TYPED refusal naming every machine considered, so a run parks and
// resumes when one wakes -- is the router's now, and component/router's
// fleet_park_test.go covers it against the real chain walk rather than against
// one helper.
