package memql

// THE NEGATIVE CONTROL for park-not-fallback on the STRUCTURED path (epic
// memql#5096, task memql#5098, design D6).
//
// The local-models epic made "an unavailable fleet parks the work, it does not
// quietly run on a paid API" structural for the chat path: the router's chain
// contains only names the policy wrote, so there is no branch that could pick
// a provider nobody mentioned. `InvokeAIStructured` does not go through the
// router. It resolves the prompt's @defaultProvider through
// `ChatStructuredProviderByName`, and when that answers nil it falls back to
// `StructuredChatProvider()`, which SCANS THE WHOLE REGISTRY for anything
// structured-capable -- a cloud provider the policy never named.
//
// `ChatStructuredProviderByName` answered nil for every fleet model, because
// its `isNonStreamingType` gate knew "openai" and "anthropic" and not "Fleet".
// So a structured prompt with a fleet default made a silent cloud call, which
// is the one thing this epic's predecessor exists to forbid.
//
// The control is an httptest server standing in for the money: it is wired
// behind a registered, available, structured-capable cloud provider, and the
// assertion is that ZERO requests reach it. It is not a mock that records an
// intention -- a request that arrives at that handler is a request that would
// have arrived at a vendor.
//
// WHY A COUNTER AND NOT t.Fatalf IN THE HANDLER: FailNow may only be called
// from the goroutine running the test, and an httptest handler is not it. A
// t.Fatalf there ends the handler's goroutine and the test reports a timeout
// or a nil answer instead of the fact that a paid provider was called.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"text/template"

	"github.com/znasllc-io/memql/core/common"
)

// cloudStructuredStub is a structured-capable provider that makes a REAL HTTP
// request when called, so "was a paid provider used" is observed at a socket
// rather than asserted about a flag this file also sets.
type cloudStructuredStub struct{ url string }

func (c cloudStructuredStub) Call(ctx context.Context, prompt string) (any, error) {
	return c.CallChatStructured(ctx, []common.ChatMessage{{Role: "user", Content: prompt}}, common.StructuredSchema{})
}

func (c cloudStructuredStub) CallChatStructured(ctx context.Context, _ []common.ChatMessage, _ common.StructuredSchema) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	return `{"answered":"by the cloud"}`, nil
}

var (
	_ AIProvider                    = cloudStructuredStub{}
	_ common.ChatStructuredProvider = cloudStructuredStub{}
)

// engineWithFleetDefaultPrompt builds the smallest engine that can run
// InvokeAIStructured: a prompt whose @defaultProvider is a fleet model, a
// registry holding the fleet seam, and the cloud provider that must not be
// reached.
func engineWithFleetDefaultPrompt(t *testing.T, fleet FleetInference, cloudURL string) *MemQLEngine {
	t.Helper()

	providers := newProviderRegistry("")
	providers.SetFleetInference(fleet)
	providers.RegisterForTest("chat54Mini", "openai", "gpt-5.4-mini", cloudStructuredStub{url: cloudURL})
	providers.SetDefaultForTest("chat54Mini")

	tmpl, err := template.New("localConductorTurn").Parse("decide: {{.utterance}}")
	if err != nil {
		t.Fatalf("compile the fixture template: %v", err)
	}
	prompts := newPromptRegistry()
	prompts.set(&PromptTemplate{
		Name:            "localConductorTurn",
		TemplateSource:  "decide: {{.utterance}}",
		DefaultProvider: FleetReferencePrefix + "llama3.1:8b",
		tmpl:            tmpl,
	})

	e := &MemQLEngine{providers: providers, prompts: prompts, modelSeam: &modelSeam{}}
	e.aiRuntime = newAIRuntime(nil, prompts, providers, aiCacheConfig{})
	e.aiRuntime.seam = e.modelSeam
	return e
}

func TestStructuredCallWithAFleetDefaultMakesNoCloudCall(t *testing.T) {
	var cloudHits atomic.Int64
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cloudHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer cloud.Close()

	// A fleet with the model but NO online machine: available as a name,
	// unusable as a provider. This is the state a closed laptop produces,
	// and the state that used to hand the turn to the cloud.
	offline := onlineModel("llama3.1:8b", true)
	offline.Machines[0].Online = false
	e := engineWithFleetDefaultPrompt(t, &stubFleet{models: []FleetModel{offline}}, cloud.URL)

	_, err := e.InvokeAIStructured(
		userCtx("alice"),
		"localConductorTurn",
		map[string]any{"utterance": "book a table"},
		"conductorDecision",
		json.RawMessage(`{"type":"object"}`),
		true,
	)

	if got := cloudHits.Load(); got != 0 {
		t.Fatalf("a paid provider was called %d time(s) for a prompt whose default is a fleet model. "+
			"park-not-fallback is what stops a closed laptop from silently starting to bill "+
			"(epic memql#4676 design D2); the structured path must refuse instead.", got)
	}
	if err == nil {
		t.Fatal("want a refusal when the fleet default cannot serve the call, got a successful answer")
	}
	if !errors.Is(err, ErrFleetUnavailable) {
		t.Fatalf("want a typed fleet refusal, got %v", err)
	}
}

// The same property with the fleet WORKING: the answer comes from the machine,
// and the cloud is still not touched. Without this the test above passes for a
// cluster where the fleet path is broken in some entirely different way.
func TestAWorkingFleetAnswersTheStructuredCallItself(t *testing.T) {
	var cloudHits atomic.Int64
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cloudHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer cloud.Close()

	fleet := &stubFleet{models: []FleetModel{onlineModel("llama3.1:8b", true)}, answer: `{"decision":"respond"}`}
	e := engineWithFleetDefaultPrompt(t, fleet, cloud.URL)

	got, err := e.InvokeAIStructured(
		userCtx("alice"),
		"localConductorTurn",
		map[string]any{"utterance": "book a table"},
		"conductorDecision",
		json.RawMessage(`{"type":"object"}`),
		true,
	)
	if err != nil {
		t.Fatalf("a structured call to an online fleet model must succeed: %v", err)
	}
	if got != `{"decision":"respond"}` {
		t.Fatalf("answer = %q, want the machine's", got)
	}
	if n := cloudHits.Load(); n != 0 {
		t.Fatalf("the cloud was called %d time(s) even though the fleet answered", n)
	}
	if fleet.lastReq.Schema == nil {
		t.Error("the schema must reach the runtime rather than being appended to the prompt: " +
			"a machine reaches this path only because it advertised structured output")
	}
	if fleet.lastReq.ActingUserId != "alice" {
		t.Errorf("acting user = %q, want alice -- a fleet entry resolved against the SYSTEM "+
			"catalog reports a live laptop as unavailable, which with any fallback is a silent "+
			"cloud call for a user whose machine was awake", fleet.lastReq.ActingUserId)
	}
}

// The gate the fallthrough ran through, asserted directly so a future edit to
// isNonStreamingType is caught by a test that names the reason rather than by
// the end-to-end one alone.
func TestTheSynchronousProviderGateAdmitsTheLocalTypes(t *testing.T) {
	for _, tt := range []struct {
		providerType string
		want         bool
		why          string
	}{
		{"openai", true, "unchanged"},
		{"anthropic", true, "unchanged"},
		{FleetProviderType, true, "a fleet call is one request and one response; it has no streaming-only endpoint to be wrong about"},
		{AppProviderType, true, "an app answers a prompt in one turn"},
		{"OpenAIStream", false, "a streaming build may target an endpoint that refuses synchronous completions"},
	} {
		if got := isNonStreamingType(tt.providerType); got != tt.want {
			t.Errorf("isNonStreamingType(%q) = %v, want %v -- %s", tt.providerType, got, tt.want, tt.why)
		}
	}
}
