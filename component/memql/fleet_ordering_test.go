package memql

// Strongest first, and what "strongest" means (epic memql#5096, task
// memql#5099, design D5).
//
// These are fold-style tests over a fixture table rather than assertions
// against a running fleet, because the property is a total order and the
// interesting cases are the ties and the absences -- neither of which a live
// catalog produces on demand.

import (
	"errors"
	"testing"

	"github.com/znasllc-io/memql/core/common"
)

func sized(id string, params int64, ctxWindow int) FleetModel {
	m := onlineModel(id, true)
	m.Params = params
	m.ContextWindow = ctxWindow
	return m
}

func ids(models []FleetModel) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ModelId)
	}
	return out
}

func sameOrder(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestOrderModelsRanksByParametersThenContextThenId(t *testing.T) {
	models := []FleetModel{
		sized("qwen2.5:7b", 7_000_000_000, 32768),
		sized("llama3.3:70b", 70_000_000_000, 8192),
		sized("llama3.1:8b", 8_000_000_000, 131072),
		sized("mistral:7b", 7_000_000_000, 32768),
	}
	sameOrder(t, ids(orderModels(models, nil)),
		"llama3.3:70b", // most parameters, despite the smallest window
		"llama3.1:8b",
		// 7B tie: same window, so the id breaks it.
		"mistral:7b",
		"qwen2.5:7b",
	)
}

// A model that does not say how big it is must not WIN by silence. Sorting an
// unknown size first would make a cockpit that predates the attribute the
// fleet's strongest model on every machine it runs on.
func TestAModelThatDoesNotSayItsSizeSortsLast(t *testing.T) {
	unknown := sized("mystery:latest", 0, 999999)
	known := sized("llama3.1:8b", 8_000_000_000, 8192)

	sameOrder(t, ids(orderModels([]FleetModel{unknown, known}, nil)), "llama3.1:8b", "mystery:latest")
	// ...even with an enormous context window, which is the tempting
	// tie-break to reach for and would be the wrong one: a window is not a
	// size, and a runtime that reports one and not the other has said
	// nothing about how capable the model is.
	sameOrder(t, ids(orderModels([]FleetModel{known, unknown}, nil)), "llama3.1:8b", "mystery:latest")
}

func TestAnExplicitPreferenceOutranksSize(t *testing.T) {
	models := []FleetModel{
		sized("llama3.3:70b", 70_000_000_000, 8192),
		sized("qwen2.5:7b", 7_000_000_000, 32768),
		sized("llama3.1:8b", 8_000_000_000, 131072),
	}
	// The owner said: try the 7B first. It is their hardware and their
	// judgement about it, and the size ordering exists for the users who
	// have not made one.
	sameOrder(t, ids(orderModels(models, []string{"qwen2.5:7b"})),
		"qwen2.5:7b", "llama3.3:70b", "llama3.1:8b")

	// A preference ORDERS, it does not filter: the models it does not name
	// stay eligible, ranked among themselves by the default.
	sameOrder(t, ids(orderModels(models, []string{"llama3.1:8b", "qwen2.5:7b"})),
		"llama3.1:8b", "qwen2.5:7b", "llama3.3:70b")

	// A preference naming a model the fleet does not run is inert rather
	// than an error: a laptop that has not pulled it yet must not break
	// routing for the models it has.
	sameOrder(t, ids(orderModels(models, []string{"not-installed:8b"})),
		"llama3.3:70b", "llama3.1:8b", "qwen2.5:7b")
}

func TestOrderModelsIsStableAcrossReplicas(t *testing.T) {
	// Two models identical on every signal. Every replica must order them
	// the same way or `fleet:*` resolves differently per node, which is the
	// property the routing strategies already depend on.
	a := sized("a:1b", 0, 0)
	b := sized("b:1b", 0, 0)
	for range 20 {
		sameOrder(t, ids(orderModels([]FleetModel{a, b}, nil)), "a:1b", "b:1b")
		sameOrder(t, ids(orderModels([]FleetModel{b, a}, nil)), "a:1b", "b:1b")
	}
}

func TestFleetWildcardIsRecognisedAndOrdinaryNamesAreNot(t *testing.T) {
	if !IsFleetWildcard(FleetWildcard) {
		t.Error("fleet:* must be recognised as the wildcard")
	}
	for _, name := range []string{"fleet:llama3.1:8b", "fleet:", "*", "chat54Mini", ""} {
		if IsFleetWildcard(name) {
			t.Errorf("%q must not be read as the wildcard", name)
		}
	}
}

// `fleet:*` is AVAILABLE when any model is online, because the concrete model
// depends on what the call needs and the chain walk is asking a different
// question: is there any local model at all.
func TestTheWildcardEntryIsAvailableWhenAnyModelIsOnline(t *testing.T) {
	r := newProviderRegistry("")
	r.SetFleetInference(&stubFleet{models: []FleetModel{sized("llama3.1:8b", 8_000_000_000, 8192)}})

	entry, ok := r.EntryForUser(userCtx("alice"), "alice", FleetWildcard)
	if !ok || entry == nil {
		t.Fatal("the wildcard must resolve to an entry")
	}
	if !entry.Available {
		t.Fatalf("want available, got unavailable: %v", entry.Err())
	}

	offline := sized("llama3.1:8b", 8_000_000_000, 8192)
	offline.Machines[0].Online = false
	r2 := newProviderRegistry("")
	r2.SetFleetInference(&stubFleet{models: []FleetModel{offline}})
	entry2, _ := r2.EntryForUser(userCtx("alice"), "alice", FleetWildcard)
	if entry2.Available {
		t.Error("a fleet with nothing online must make the wildcard unavailable")
	}
}

// The concrete model is chosen PER CALL, from what the call needs. A fleet
// running one structured model and one embeddings model serves both kinds of
// turn through the same `fleet:*` reference.
func TestTheWildcardPicksTheStrongestModelThatFitsTheCall(t *testing.T) {
	chatBig := sized("llama3.3:70b", 70_000_000_000, 8192)
	chatBig.Embeddings = false
	embed := sized("nomic-embed-text", 137_000_000, 8192)
	embed.Embeddings = true
	embed.StructuredOutput = false

	fleet := &stubFleet{models: []FleetModel{chatBig, embed}, answer: "ok"}
	r := newProviderRegistry("")
	r.SetFleetInference(fleet)
	entry, _ := r.EntryForUser(userCtx("alice"), "alice", FleetWildcard)

	chat := entry.Client.(common.ChatAIProvider)
	if _, err := chat.CallChat(userCtx("alice"), []common.ChatMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("chat through the wildcard: %v", err)
	}
	if fleet.lastReq.ModelId != "llama3.3:70b" {
		t.Errorf("chat ran on %q, want the strongest eligible model", fleet.lastReq.ModelId)
	}

	embedder := entry.Client.(EmbeddingAIProvider)
	if _, err := embedder.EmbedBatch(userCtx("alice"), []string{"x"}); err == nil || fleet.lastReq.ModelId != "nomic-embed-text" {
		// The stub returns no vectors, so EmbedBatch errors on the count --
		// what matters here is WHICH model the call was routed to.
		if fleet.lastReq.ModelId != "nomic-embed-text" {
			t.Errorf("embedding ran on %q, want the model that advertises embeddings", fleet.lastReq.ModelId)
		}
	}
}

// The owner's preference reaches the wildcard resolver through the seam, so a
// policy edit changes routing with no restart.
func TestTheWildcardHonoursTheOwnersModelPreference(t *testing.T) {
	fleet := &stubFleet{
		models: []FleetModel{
			sized("llama3.3:70b", 70_000_000_000, 8192),
			sized("qwen2.5:7b", 7_000_000_000, 32768),
		},
		answer:     "ok",
		preference: []string{"qwen2.5:7b"},
	}
	r := newProviderRegistry("")
	r.SetFleetInference(fleet)
	entry, _ := r.EntryForUser(userCtx("alice"), "alice", FleetWildcard)

	if _, err := entry.Client.(common.ChatAIProvider).CallChat(
		userCtx("alice"), []common.ChatMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if fleet.lastReq.ModelId != "qwen2.5:7b" {
		t.Errorf("ran on %q, want the owner's preferred model", fleet.lastReq.ModelId)
	}
}

// A failed policy read must not decide the model. Degrading to the default
// ordering gives the caller the strongest eligible model; refusing would park
// a run over a row nobody asked about.
func TestAFailedPreferenceReadFallsBackToTheDefaultOrdering(t *testing.T) {
	fleet := &stubFleet{
		models: []FleetModel{
			sized("qwen2.5:7b", 7_000_000_000, 32768),
			sized("llama3.3:70b", 70_000_000_000, 8192),
		},
		answer:        "ok",
		preferenceErr: errors.New("the policy read failed"),
	}
	r := newProviderRegistry("")
	r.SetFleetInference(fleet)
	entry, _ := r.EntryForUser(userCtx("alice"), "alice", FleetWildcard)

	if _, err := entry.Client.(common.ChatAIProvider).CallChat(
		userCtx("alice"), []common.ChatMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if fleet.lastReq.ModelId != "llama3.3:70b" {
		t.Errorf("ran on %q, want the default ordering's answer", fleet.lastReq.ModelId)
	}
}

// A wildcard whose fleet can serve nothing this call needs is the TYPED
// refusal, naming every MODEL considered -- not every machine. `fleet:*` asked
// about models, and a machine-shaped report would name one laptop once per
// model it hosts.
func TestAWildcardMissNamesTheModelsConsidered(t *testing.T) {
	proseOnly := sized("tinyllama:1b", 1_000_000_000, 2048)
	proseOnly.StructuredOutput = false
	fleet := &stubFleet{models: []FleetModel{proseOnly}}
	r := newProviderRegistry("")
	r.SetFleetInference(fleet)
	entry, _ := r.EntryForUser(userCtx("alice"), "alice", FleetWildcard)

	_, err := entry.Client.(common.ChatStructuredProvider).CallChatStructured(
		userCtx("alice"),
		[]common.ChatMessage{{Role: "user", Content: "decide"}},
		common.StructuredSchema{Name: "decision", Schema: []byte(`{"type":"object"}`)},
	)
	if err == nil {
		t.Fatal("a structured turn on a fleet with no structured model must refuse")
	}
	var refusal *FleetUnavailable
	if !errors.As(err, &refusal) {
		t.Fatalf("want the typed refusal, got %T: %v", err, err)
	}
	if refusal.Considered["tinyllama:1b"] != "does not advertise structured output" {
		t.Errorf("considered = %v, want the model named with its miss", refusal.Considered)
	}
	if !errors.Is(err, ErrFleetUnavailable) {
		t.Error("the refusal must read as unavailable")
	}
}

func TestEligibilityAsksTheSameQuestionsSatisfiesDoes(t *testing.T) {
	m := onlineModel("llama3.1:8b", true)
	m.Embeddings = true
	m.Tools = true
	m.ContextWindow = 8192

	for _, tt := range []struct {
		name  string
		needs FleetNeeds
		ok    bool
		why   string
	}{
		{"nothing", FleetNeeds{}, true, ""},
		{"structured", FleetNeeds{StructuredOutput: true}, true, ""},
		{"embeddings", FleetNeeds{Embeddings: true}, true, ""},
		{"tools", FleetNeeds{Tools: true}, true, ""},
		{"window under the floor", FleetNeeds{MinContextWindow: 131072}, false, "context window 8192 is under the floor 131072"},
	} {
		ok, why := m.eligibleFor(tt.needs)
		if ok != tt.ok || (!ok && why != tt.why) {
			t.Errorf("%s: ok=%v why=%q, want ok=%v why=%q", tt.name, ok, why, tt.ok, tt.why)
		}
	}

	offline := m
	offline.Machines = []FleetMachine{{RegistrationId: "laptop", Online: false}}
	if ok, why := offline.eligibleFor(FleetNeeds{}); ok || why != "no machine offering it is online" {
		t.Errorf("offline: ok=%v why=%q", ok, why)
	}
}
