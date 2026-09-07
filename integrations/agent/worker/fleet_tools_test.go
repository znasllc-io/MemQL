//go:build agent

package worker

// Tool calling over the fleet, on the agent side (epic memql#5096, task
// memql#5098, design D11 / D6).
//
// Three properties, each with its own reason to be pinned here rather than in
// component/memql: the label grammar is mirrored by hand in the cockpit and
// drifting it silently un-routes every machine; the schema is rendered ONCE
// per call so two candidates are offered identical bytes; and a machine that
// never advertised tool support is SKIPPED for a tool turn rather than handed
// one it will answer in prose.

import (
	"encoding/json"
	"testing"

	memqlengine "github.com/znasllc-io/memql/component/memql"
	workerservice "github.com/znasllc-io/memql/component/worker"
	"github.com/znasllc-io/memql/core/common"
)

func TestToolSupportRoundTripsThroughTheLabelValue(t *testing.T) {
	want := ModelAttributes{
		ContextWindow:    131072,
		StructuredOutput: true,
		Embeddings:       false,
		Tools:            true,
		MaxConcurrent:    2,
	}
	got := ParseModelAttributes(want.String())
	if got != want {
		t.Fatalf("round trip lost something.\n want %+v\n  got %+v\n via %q", want, got, want.String())
	}
	// The cockpit mirrors this grammar by hand, so the exact spelling is
	// part of the contract rather than an implementation detail.
	if want.String() != "ctx=131072,structured=1,tools=1,max=2" {
		t.Errorf("label value = %q; the cockpit parses this string literally", want.String())
	}
}

// FAIL-CLOSED, like every other capability: a machine that says nothing about
// tools is not selected for a tool turn. A model that silently answers prose
// to a tool-calling turn produces an agent that appears to have stopped using
// its tools, three layers from anything that names the cause.
func TestAMachineThatDoesNotAdvertiseToolsIsSkippedForAToolTurn(t *testing.T) {
	silent := ParseModelAttributes("ctx=8192,structured=1")
	if ok, why := silent.Satisfies(ModelNeeds{Tools: true}); ok {
		t.Fatal("a machine that never advertised tool calling must not be selected for a tool turn")
	} else if why != "model does not advertise tool calling" {
		t.Errorf("reason = %q, want it to name the missing capability -- the refusal report lists "+
			"every machine considered and why each was ruled out", why)
	}
	// And it stays eligible for the turns it CAN serve. A capability gate
	// that took a machine out of the fleet entirely would cost more than it
	// protects.
	if ok, why := silent.Satisfies(ModelNeeds{StructuredOutput: true}); !ok {
		t.Errorf("the same machine must still serve a structured turn: %s", why)
	}

	capable := ParseModelAttributes("ctx=8192,structured=1,tools=1")
	if ok, why := capable.Satisfies(ModelNeeds{Tools: true}); !ok {
		t.Errorf("an advertising machine must be eligible: %s", why)
	}
}

func TestNarrowToModelReportsWhyAToolTurnRuledAMachineOut(t *testing.T) {
	plan := RoutePlan{
		Policy: DefaultPolicy(),
		Candidates: []Candidate{{
			RegistrationId: "laptop",
			Labels:         map[string]string{workerservice.ModelLabel("llama3.1:8b"): "ctx=131072,structured=1"},
		}},
		Total: 1,
	}
	narrowed := narrowToModel(plan, "llama3.1:8b", ModelNeeds{Tools: true})
	if len(narrowed.Candidates) != 0 {
		t.Fatalf("want no candidate for a tool turn, got %d", len(narrowed.Candidates))
	}
	if narrowed.Rejected["laptop"] != "model does not advertise tool calling" {
		t.Errorf("rejection = %q, want the capability named", narrowed.Rejected["laptop"])
	}
}

// The schema is rendered ONCE per call, in buildStart, so every candidate is
// offered byte-identical bytes. A per-hop re-encode would give two machines
// two different schemas for one call, and the difference would only ever show
// up as one machine's answers being subtly worse.
func TestTheToolSchemaIsRenderedOncePerCall(t *testing.T) {
	f := &FleetInference{}
	start := f.buildStart(memqlengineFleetRequestWithTools())
	if len(start.GetTools()) != 2 {
		t.Fatalf("tools on the wire = %d, want 2", len(start.GetTools()))
	}
	if got := start.GetTools()[0].GetParametersJson(); got != `{"type":"object","properties":{"q":{"type":"string"}}}` {
		t.Errorf("a raw schema must travel verbatim; got %s", got)
	}
	// A map schema is marshalled, and a nil one becomes the empty object
	// rather than being dropped: a tool with no parameters is still callable,
	// where a tool absent from the list is invisible and the model reports
	// the capability as unavailable.
	if got := start.GetTools()[1].GetParametersJson(); got != "{}" {
		t.Errorf("nil schema -> %q, want {}", got)
	}
}

func TestToolSchemaJSONHandlesEveryShapeAToolCanDeclare(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema any
		want   string
	}{
		{"nil", nil, "{}"},
		{"raw", json.RawMessage(`{"type":"object"}`), `{"type":"object"}`},
		{"string", `{"type":"object"}`, `{"type":"object"}`},
		{"map", map[string]any{"type": "object"}, `{"type":"object"}`},
		{"unmarshallable", make(chan int), "{}"},
	} {
		if got := toolSchemaJSON(tt.schema); got != tt.want {
			t.Errorf("%s: toolSchemaJSON = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func memqlengineFleetRequestWithTools() memqlengine.FleetCallRequest {
	return memqlengine.FleetCallRequest{
		ModelId: "llama3.1:8b",
		Kind:    "chat",
		Messages: []common.ChatMessage{
			{Role: "user", Content: "find the invoice"},
			{Role: "assistant", ToolCalls: []common.ToolCall{{ID: "call_a", Name: "searchLibrary", Arguments: `{"q":"x"}`}}},
			{Role: "tool", ToolCallId: "call_a", Name: "searchLibrary", Content: `{"rows":1}`},
		},
		Tools: []common.ToolDefinition{
			{Name: "searchLibrary", Description: "Search", InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)},
			{Name: "listFolders", Description: "List"},
		},
	}
}

// A tool RESULT message must carry its call id to the runtime: both runtimes
// require the id to round-trip, and a result with none cannot be matched to
// its request.
func TestAToolResultCarriesItsCallIdToTheRuntime(t *testing.T) {
	f := &FleetInference{}
	start := f.buildStart(memqlengineFleetRequestWithTools())
	msgs := start.GetMessages()
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	if msgs[2].GetToolCallId() != "call_a" || msgs[2].GetName() != "searchLibrary" {
		t.Errorf("tool result lost its identity: %+v", msgs[2])
	}
	if len(msgs[1].GetToolCalls()) != 1 || msgs[1].GetToolCalls()[0].GetId() != "call_a" {
		t.Errorf("the assistant's own calls must be replayed so the model sees what it asked for: %+v", msgs[1])
	}
}
