package memql

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/znasllc-io/memql/core/common"
)

// Recorded request shapes for every OpenAI call site (epic memql#5088, Task 1).
//
// WHAT THESE ARE FOR, because a reader who meets them after the migration will
// see tests that assert a fixture against itself and wonder what they buy.
//
// The engine's OpenAI calls moved from the community SDK
// (github.com/sashabaranov/go-openai) to the official one
// (github.com/openai/openai-go/v3). That migration is required to be
// BEHAVIOUR-PRESERVING: same endpoint, same JSON, same headers. "Behaviour
// preserving" is a claim about bytes on a wire, and the only way to check it
// is to write the bytes down BEFORE the change and diff them after. These
// fixtures were recorded against the community SDK and are NOT regenerated as
// part of the migration; a diff here is the migration changing the request,
// which is the one outcome the migration is not allowed to have.
//
// So: if a fixture fails after an SDK change, the fix is the code, not the
// fixture. Regenerate with -update-openai-wire ONLY when the request is
// deliberately changing and you have said so in the commit message.
//
// The exception the design names explicitly: the two hand-rolled calls,
// embeddings and speech, were NOT SDK calls before this epic and were not
// behind the guarded transport. Their fixtures pin the shape the hand-rolled
// code produced, so the move onto the SDK is held to the same standard as the
// rest.
var updateOpenAIWireFixtures = flag.Bool("update-openai-wire", false,
	"rewrite the recorded OpenAI request fixtures from this run instead of asserting against them")

// recordedRequest is one captured call: enough to assert the wire shape and
// nothing that varies run to run.
type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

// openAIRecorder is the fake OpenAI the providers talk to. It answers each
// endpoint plausibly enough for the provider to finish its call, and records
// what it was asked.
type openAIRecorder struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (r *openAIRecorder) record(req recordedRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
}

// Last returns the most recent recorded request, failing the test when the
// provider made none -- a silent zero here would let a call site that never
// fired assert an empty body against an empty fixture and pass.
func (r *openAIRecorder) Last(t *testing.T) recordedRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		t.Fatal("no request reached the recording server: the provider made no call")
	}
	return r.requests[len(r.requests)-1]
}

func (r *openAIRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// newRecordingOpenAIServer starts a fake OpenAI that records every request and
// answers the four endpoints the engine calls.
func newRecordingOpenAIServer(t *testing.T) (*httptest.Server, *openAIRecorder) {
	t.Helper()
	rec := &openAIRecorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw := readAllForTest(t, req)
		entry := recordedRequest{
			Method: req.Method,
			Path:   req.URL.Path,
			Query:  req.URL.RawQuery,
			Header: req.Header.Clone(),
			Raw:    raw,
		}
		// A multipart body (transcription) is not JSON; leave Body nil and let
		// the test assert on Raw and the content type instead of pretending
		// the parse succeeded.
		if json.Valid(raw) {
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err == nil {
				entry.Body = body
			}
		}
		rec.record(entry)

		switch {
		case strings.HasSuffix(req.URL.Path, "/chat/completions"):
			streaming := false
			if entry.Body != nil {
				streaming, _ = entry.Body["stream"].(bool)
			}
			if streaming {
				writeSSEChatChunks(w)
				return
			}
			writeJSON(w, chatCompletionResponse())
		case strings.HasSuffix(req.URL.Path, "/embeddings"):
			writeJSON(w, embeddingsResponse())
		case strings.HasSuffix(req.URL.Path, "/audio/speech"):
			w.Header().Set("Content-Type", "audio/pcm")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("pcm-bytes"))
		case strings.HasSuffix(req.URL.Path, "/audio/transcriptions"):
			writeJSON(w, map[string]any{"text": "a recorded transcript"})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"unexpected path in the wire recorder"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func readAllForTest(t *testing.T, req *http.Request) []byte {
	t.Helper()
	if req.Body == nil {
		return nil
	}
	defer func() { _ = req.Body.Close() }()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := req.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

// chatCompletionResponse is one non-streaming answer carrying every field the
// engine reads back: content, a tool call, a finish reason, usage and a model.
func chatCompletionResponse() map[string]any {
	return map[string]any{
		"id":      "chatcmpl-recorded",
		"object":  "chat.completion",
		"created": 1757000000,
		"model":   "gpt-5.4-mini",
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "a recorded answer",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     11,
			"completion_tokens": 7,
			"total_tokens":      18,
		},
	}
}

func embeddingsResponse() map[string]any {
	return map[string]any{
		"object": "list",
		"model":  "text-embedding-3-small",
		"data": []any{
			map[string]any{
				"object":    "embedding",
				"index":     0,
				"embedding": []any{0.1, 0.2, 0.3},
			},
		},
		"usage": map[string]any{"prompt_tokens": 3, "total_tokens": 3},
	}
}

// writeSSEChatChunks answers two content deltas and [DONE], which is the
// smallest stream that proves the provider yields more than one chunk and
// terminates on the sentinel rather than on connection close.
func writeSSEChatChunks(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	frames := []string{
		`{"id":"chatcmpl-recorded","object":"chat.completion.chunk","created":1757000000,"model":"gpt-5.4-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"first "},"finish_reason":null}]}`,
		`{"id":"chatcmpl-recorded","object":"chat.completion.chunk","created":1757000000,"model":"gpt-5.4-mini","choices":[{"index":0,"delta":{"content":"second"},"finish_reason":null}]}`,
	}
	for _, frame := range frames {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// --- fixture comparison ----------------------------------------------------

const openAIWireFixtureDir = "testdata/openai"

// assertRecordedShape compares one recorded request against its fixture.
//
// It asserts the PATH and the BODY, and deliberately not the whole header set:
// a Go HTTP client adds User-Agent, Accept-Encoding and its own framing, and an
// SDK is entitled to change those. What the engine promises about headers is
// asserted separately and by name (the Authorization shape, OpenAI-Project),
// because those are ours and a change to either is a behaviour change.
func assertRecordedShape(t *testing.T, class string, got recordedRequest) {
	t.Helper()
	path := filepath.Join(openAIWireFixtureDir, class+".request.json")

	recorded := map[string]any{
		"method": got.Method,
		"path":   got.Path,
		"body":   got.Body,
	}
	if got.Query != "" {
		recorded["query"] = got.Query
	}

	encoded, err := json.MarshalIndent(recorded, "", "  ")
	if err != nil {
		t.Fatalf("marshal recorded request: %v", err)
	}
	encoded = append(encoded, '\n')

	if *updateOpenAIWireFixtures {
		if err := os.MkdirAll(openAIWireFixtureDir, 0o755); err != nil {
			t.Fatalf("create fixture dir: %v", err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
		t.Logf("wrote fixture %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v (run with -update-openai-wire to record it)", path, err)
	}

	// Compare as decoded values rather than as bytes: Go's map iteration is
	// unordered and json.MarshalIndent sorts keys, but a nested value that
	// arrived as a different NUMBER TYPE (int vs float) would compare equal as
	// text and unequal here, which is the direction we want to be strict in.
	var wantValue, gotValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("fixture %s is not valid JSON: %v", path, err)
	}
	if err := json.Unmarshal(encoded, &gotValue); err != nil {
		t.Fatalf("recorded request is not valid JSON: %v", err)
	}
	dropFalseStreamKey(wantValue)
	dropFalseStreamKey(gotValue)
	if !jsonEqual(wantValue, gotValue) {
		t.Errorf("recorded request for %q does not match %s\n--- want ---\n%s\n--- got ---\n%s",
			class, path, string(want), string(encoded))
	}
}

// dropFalseStreamKey removes `"stream": false` from a recorded body.
//
// THIS IS THE ONE NORMALIZATION, AND IT IS NARROW ON PURPOSE. Measured across
// all nine fixtures, `stream` is the ONLY key whose value differs between the
// community SDK and openai-go/v3: the community SDK serialised the request
// struct's zero-valued Stream field as `false`, while v3 has no Stream field at
// all -- the streaming and non-streaming calls are different methods, and the
// SDK sets `stream: true` itself in NewStreaming. `stream: false` and an
// absent `stream` are the same request to OpenAI, whose default is false.
//
// It only ever drops the key when the value is FALSE. A `stream: true` is left
// in place and compared, so the one direction that would matter -- a streaming
// call that stopped asking to stream, or a non-streaming call that started --
// still fails the fixture. TestOpenAIWireStreamKeyPresence asserts both halves
// directly rather than leaving them to this helper's absence.
func dropFalseStreamKey(value any) {
	obj, ok := value.(map[string]any)
	if !ok {
		return
	}
	body, ok := obj["body"].(map[string]any)
	if !ok {
		return
	}
	if streaming, present := body["stream"].(bool); present && !streaming {
		delete(body, "stream")
	}
}

func jsonEqual(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ja) == string(jb)
}

// --- the providers under test ---------------------------------------------

// wireTestChatProvider builds the non-streaming OpenAI chat provider pointed at
// the recording server.
//
// Construction goes through the REAL constructor, not a hand-built struct: the
// point of a wire test is that the thing the engine builds at boot produces
// this request, and a struct literal here would keep passing after the
// constructor stopped attaching the project header or the guarded transport.
func wireTestChatProvider(t *testing.T, baseURL, model string, params map[string]any) common.ChatAIProvider {
	t.Helper()
	provider, err := newOpenAIProvider(wireTestConfig(baseURL, model, params))
	if err != nil {
		t.Fatalf("newOpenAIProvider: %v", err)
	}
	chat, ok := provider.(common.ChatAIProvider)
	if !ok {
		t.Fatalf("openai provider does not implement ChatAIProvider")
	}
	return chat
}

func wireTestStreamProvider(t *testing.T, baseURL, model string, params map[string]any) AIProvider {
	t.Helper()
	provider, err := newOpenAIStreamProvider(wireTestConfig(baseURL, model, params))
	if err != nil {
		t.Fatalf("newOpenAIStreamProvider: %v", err)
	}
	return provider
}

// The two models the wire tests use, and why there are two.
//
// wireTestShippedModel is what dsl/providers/providers.memql actually
// configures. wireTestSamplingModel is used ONLY by the test that records a
// request carrying temperature and top_p, because the COMMUNITY SDK refuses to
// send those parameters for any model whose name begins "gpt-5":
// reasoning_validator.go returns ErrReasoningModelLimitationsOther before any
// HTTP request is made (it gates o1/o3/o4 and gpt-5 alike).
//
// THIS IS A BEHAVIOUR DIFFERENCE THE MIGRATION DELIBERATELY INTRODUCES, and it
// is recorded here rather than discovered later. The official SDK has no such
// client-side gate: it sends what it is given and lets OpenAI decide. That is
// the correct division -- the vendor's accepted parameter set is the vendor's
// to publish, and a client-side copy of it is a second source of truth that
// goes stale silently and refuses calls the API would have served.
//
// Nothing shipped is affected today: no provider entry in
// dsl/providers/providers.memql sets temperature or topP, so no configured
// call reaches the gate. What changes is that a product DSL bundle or a
// per-request param CAN now set them on a gpt-5 model and have them reach
// OpenAI, instead of failing inside the client with an error that names a
// limitation the model may no longer have.
const (
	wireTestShippedModel  = "gpt-5.4-mini"
	wireTestSamplingModel = "gpt-4o-mini"
)

// wireTestConfig is the one place the wire tests say how an OpenAI provider is
// credentialled. It is a single function on purpose: the credential changes in
// this same epic (a static key becomes workload identity federation), and when
// it does, exactly this function changes and every fixture stays byte-identical
// -- which is the proof that the credential switch did not move the wire.
func wireTestConfig(baseURL, model string, params map[string]any) ProviderConfig {
	if params == nil {
		params = map[string]any{}
	}
	if model == "" {
		model = wireTestShippedModel
	}
	return ProviderConfig{
		Name:  "wireTestOpenAI",
		Type:  "OpenAI",
		Model: model,
		Auth: map[string]string{
			"apiKey":  "sk-wire-test",
			"baseURL": baseURL + "/v1",
		},
		Params: params,
	}
}

// --- the tests -------------------------------------------------------------

func TestOpenAIWireChat(t *testing.T) {
	srv, rec := newRecordingOpenAIServer(t)
	provider := wireTestChatProvider(t, srv.URL, wireTestSamplingModel, map[string]any{
		"temperature":         0.2,
		"topP":                0.9,
		"maxCompletionTokens": 256,
	})

	got, err := provider.CallChat(context.Background(), []common.ChatMessage{
		{Role: "system", Content: "you are terse"},
		{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("CallChat: %v", err)
	}
	if got != "a recorded answer" {
		t.Errorf("CallChat returned %q, want %q", got, "a recorded answer")
	}

	req := rec.Last(t)
	assertRecordedShape(t, "chat", req)
	assertBearerHeader(t, req)
}

func TestOpenAIWireChatOmitsUnsetSamplingParams(t *testing.T) {
	// The `if _, ok := params[...]` guards in the provider exist because some
	// models REJECT temperature and top_p outright. An SDK that serialises a
	// zero value instead of omitting the key would turn every call to such a
	// model into a 400, and would do it only in production -- so the omission
	// is asserted as its own fixture rather than folded into the chat one.
	srv, rec := newRecordingOpenAIServer(t)
	provider := wireTestChatProvider(t, srv.URL, "", nil)

	if _, err := provider.CallChat(context.Background(), []common.ChatMessage{
		{Role: "user", Content: "hello"},
	}); err != nil {
		t.Fatalf("CallChat: %v", err)
	}

	req := rec.Last(t)
	assertRecordedShape(t, "chat-no-params", req)

	for _, key := range []string{"temperature", "top_p", "max_completion_tokens"} {
		if _, present := req.Body[key]; present {
			t.Errorf("body carries %q although no param was configured; body=%s", key, string(req.Raw))
		}
	}
}

func TestOpenAIWireChatStream(t *testing.T) {
	srv, rec := newRecordingOpenAIServer(t)
	provider := wireTestStreamProvider(t, srv.URL, "", nil)

	streamer, ok := provider.(interface {
		CallStream(context.Context, string) (<-chan StreamChunk, error)
	})
	if !ok {
		t.Fatal("openai stream provider does not implement CallStream")
	}

	chunks, err := streamer.CallStream(context.Background(), "hello")
	if err != nil {
		t.Fatalf("CallStream: %v", err)
	}

	var content []string
	var sawDone bool
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("stream chunk carried an error: %v", chunk.Error)
		}
		if chunk.Content != "" {
			content = append(content, chunk.Content)
		}
		if chunk.Done {
			sawDone = true
		}
	}
	if len(content) != 2 {
		t.Errorf("stream yielded %d content chunks (%q), want 2", len(content), content)
	}
	if !sawDone {
		t.Error("stream never yielded a Done chunk")
	}

	req := rec.Last(t)
	assertRecordedShape(t, "chat-stream", req)
	if streaming, _ := req.Body["stream"].(bool); !streaming {
		t.Errorf("streaming request body does not carry stream=true; body=%s", string(req.Raw))
	}
}

func TestOpenAIWireChatTools(t *testing.T) {
	srv, rec := newRecordingOpenAIServer(t)
	provider, err := newOpenAIProvider(wireTestConfig(srv.URL, "", nil))
	if err != nil {
		t.Fatalf("newOpenAIProvider: %v", err)
	}
	toolCaller, ok := provider.(common.ToolCallingChatAIProvider)
	if !ok {
		t.Fatal("openai provider does not implement ToolCallingChatAIProvider")
	}

	if _, err := toolCaller.CallChatWithTools(context.Background(),
		[]common.ChatMessage{{Role: "user", Content: "what is the weather"}},
		[]common.ToolDefinition{{
			Name:        "getWeather",
			Description: "Look up the weather for a city",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
				"required":             []any{"city"},
				"additionalProperties": false,
			},
		}},
	); err != nil {
		t.Fatalf("CallChatWithTools: %v", err)
	}

	assertRecordedShape(t, "chat-tools", rec.Last(t))
}

func TestOpenAIWireChatToolsDefaultsAnEmptySchema(t *testing.T) {
	// A tool with no InputSchema must still send a valid object schema.
	// Sending `null` here is accepted by neither vendor and the failure is a
	// 400 on the first turn that offers the tool.
	srv, rec := newRecordingOpenAIServer(t)
	provider, err := newOpenAIProvider(wireTestConfig(srv.URL, "", nil))
	if err != nil {
		t.Fatalf("newOpenAIProvider: %v", err)
	}
	toolCaller := provider.(common.ToolCallingChatAIProvider)

	if _, err := toolCaller.CallChatWithTools(context.Background(),
		[]common.ChatMessage{{Role: "user", Content: "go"}},
		[]common.ToolDefinition{{Name: "noArgs", Description: "takes nothing"}},
	); err != nil {
		t.Fatalf("CallChatWithTools: %v", err)
	}

	assertRecordedShape(t, "chat-tools-empty-schema", rec.Last(t))
}

func TestOpenAIWireChatStructured(t *testing.T) {
	srv, rec := newRecordingOpenAIServer(t)
	provider, err := newOpenAIProvider(wireTestConfig(srv.URL, "", nil))
	if err != nil {
		t.Fatalf("newOpenAIProvider: %v", err)
	}
	structured, ok := provider.(common.ChatStructuredProvider)
	if !ok {
		t.Fatal("openai provider does not implement ChatStructuredProvider")
	}

	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`)
	if _, err := structured.CallChatStructured(context.Background(),
		[]common.ChatMessage{{Role: "user", Content: "answer in json"}},
		common.StructuredSchema{
			Name:        "recordedAnswer",
			Description: "the recorded answer",
			Schema:      schema,
			Strict:      true,
		},
	); err != nil {
		t.Fatalf("CallChatStructured: %v", err)
	}

	req := rec.Last(t)
	assertRecordedShape(t, "chat-structured", req)

	// The schema must arrive as a JSON OBJECT, not as a string carrying JSON.
	// This is the spike the plan named: the official SDK types the field as
	// `any` rather than json.RawMessage, and the difference between the two is
	// invisible in Go and total on the wire.
	format, _ := req.Body["response_format"].(map[string]any)
	jsonSchema, _ := format["json_schema"].(map[string]any)
	if _, ok := jsonSchema["schema"].(map[string]any); !ok {
		t.Errorf("response_format.json_schema.schema is not a JSON object; body=%s", string(req.Raw))
	}
}

func TestOpenAIWireVision(t *testing.T) {
	srv, rec := newRecordingOpenAIServer(t)
	provider, err := newOpenAIProvider(wireTestConfig(srv.URL, "", nil))
	if err != nil {
		t.Fatalf("newOpenAIProvider: %v", err)
	}
	vision, ok := provider.(common.VisionAIProvider)
	if !ok {
		t.Fatal("openai provider does not implement VisionAIProvider")
	}

	if _, err := vision.CallVision(context.Background(), "describe this",
		[]common.VisionContent{{MimeType: "image/png", Data: []byte("not-really-a-png")}},
	); err != nil {
		t.Fatalf("CallVision: %v", err)
	}

	assertRecordedShape(t, "chat-vision", rec.Last(t))
}

func TestOpenAIWireEmbeddings(t *testing.T) {
	srv, rec := newRecordingOpenAIServer(t)

	client := NewOpenAIEmbeddingClient("sk-wire-test", "text-embedding-3-small", 1536)
	client.baseURL = srv.URL + "/v1"

	vectors, err := client.EmbedBatch(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vectors) != 1 {
		t.Errorf("EmbedBatch returned %d vectors, want 1 (the recorder answers one)", len(vectors))
	}

	req := rec.Last(t)
	assertRecordedShape(t, "embeddings", req)
	assertBearerHeader(t, req)
}

func TestOpenAIWireSpeech(t *testing.T) {
	srv, rec := newRecordingOpenAIServer(t)

	provider, err := newOpenAITTSProvider(ProviderConfig{
		Name:  "wireTestOpenAITTS",
		Type:  "OpenAITTS",
		Model: "gpt-4o-mini-tts",
		// The endpoint override that recorded this fixture before the migration
		// is gone: speech is an SDK call now, so the base URL is the seam, the
		// same one every other OpenAI call site in this file uses.
		Auth:   map[string]string{"apiKey": "sk-wire-test", "baseURL": srv.URL + "/v1"},
		Params: map[string]any{"voice": "nova", "speed": 1.0, "format": "pcm"},
	})
	if err != nil {
		t.Fatalf("newOpenAITTSProvider: %v", err)
	}
	synth, ok := provider.(TTSAIProvider)
	if !ok {
		t.Fatal("openai TTS provider does not implement TTSAIProvider")
	}

	audio, err := synth.Synthesize(context.Background(), "hello there", "nova")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(audio) == 0 {
		t.Error("Synthesize returned no audio")
	}

	req := rec.Last(t)
	assertRecordedShape(t, "speech", req)
	assertBearerHeader(t, req)
}

// TestOpenAIWireProjectHeader pins the OpenAI-Project header, which is ours
// rather than the SDK's: two hand-written client wrappers add it today and the
// official SDK carries option.WithProject instead. The header must survive the
// swap, and it must stay ABSENT when no project id is configured -- a
// service-account key carries no project and an empty header is a 400.
func TestOpenAIWireProjectHeader(t *testing.T) {
	t.Setenv("MEMQL_AI_OPENAI_PROJECT_ID", "")

	t.Run("absent when unconfigured", func(t *testing.T) {
		srv, rec := newRecordingOpenAIServer(t)
		provider := wireTestChatProvider(t, srv.URL, "", nil)
		if _, err := provider.CallChat(context.Background(),
			[]common.ChatMessage{{Role: "user", Content: "hello"}}); err != nil {
			t.Fatalf("CallChat: %v", err)
		}
		if got := rec.Last(t).Header.Get("OpenAI-Project"); got != "" {
			t.Errorf("OpenAI-Project header is %q with no project configured, want absent", got)
		}
	})

	t.Run("present when configured", func(t *testing.T) {
		srv, rec := newRecordingOpenAIServer(t)
		cfg := wireTestConfig(srv.URL, "", nil)
		cfg.Auth["projectId"] = "proj_recorded"
		provider, err := newOpenAIProvider(cfg)
		if err != nil {
			t.Fatalf("newOpenAIProvider: %v", err)
		}
		if _, err := provider.(common.ChatAIProvider).CallChat(context.Background(),
			[]common.ChatMessage{{Role: "user", Content: "hello"}}); err != nil {
			t.Fatalf("CallChat: %v", err)
		}
		if got := rec.Last(t).Header.Get("OpenAI-Project"); got != "proj_recorded" {
			t.Errorf("OpenAI-Project header is %q, want %q", got, "proj_recorded")
		}
	})
}

// assertBearerHeader checks the SHAPE of the credential header rather than its
// value. The value is a static key today and a federated bearer after the
// cutover; what must never change is that the engine sends exactly one
// Authorization header and that it is a Bearer.
func assertBearerHeader(t *testing.T, req recordedRequest) {
	t.Helper()
	values := req.Header.Values("Authorization")
	if len(values) != 1 {
		t.Fatalf("expected exactly one Authorization header, got %d: %v", len(values), values)
	}
	if !strings.HasPrefix(values[0], "Bearer ") {
		t.Errorf("Authorization header %q is not a Bearer credential", redactBearer(values[0]))
	}
	if strings.TrimSpace(strings.TrimPrefix(values[0], "Bearer ")) == "" {
		t.Error("Authorization header carries an empty Bearer credential")
	}
}

// redactBearer keeps a failing assertion from printing a credential. The tests
// use a fake key, but this helper is also what a federated bearer would flow
// through, and a test that prints one teaches the habit of printing them.
func redactBearer(value string) string {
	if !strings.HasPrefix(value, "Bearer ") {
		return "<non-bearer>"
	}
	return "Bearer <redacted>"
}

// TestOpenAIWireFixturesAreAllExercised fails when a fixture file exists that
// no test asserts against, which is how a call site that quietly stopped being
// tested would otherwise look identical to one that still is.
func TestOpenAIWireFixturesAreAllExercised(t *testing.T) {
	entries, err := os.ReadDir(openAIWireFixtureDir)
	if err != nil {
		t.Fatalf("read %s: %v", openAIWireFixtureDir, err)
	}
	var found []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".request.json") {
			found = append(found, strings.TrimSuffix(name, ".request.json"))
		}
	}
	sort.Strings(found)

	want := []string{
		"chat",
		"chat-no-params",
		"chat-stream",
		"chat-structured",
		"chat-tools",
		"chat-tools-empty-schema",
		"chat-vision",
		"embeddings",
		"speech",
	}
	sort.Strings(want)

	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Errorf("recorded fixture set drifted from the tests that assert it\n got: %v\nwant: %v\n"+
			"Add the test, or delete the fixture -- an unasserted fixture proves nothing.", found, want)
	}
}

// TestOpenAIWireGpt5SamplingParamsReachTheVendor is the AFTER half of the
// decision recorded on wireTestSamplingModel above.
//
// The community SDK refused, client-side and before any request, to send
// temperature or top_p for a model whose name begins "gpt-5". The official SDK
// has no such gate, and this asserts the widening rather than leaving it to be
// discovered: the request reaches OpenAI, which is the party entitled to
// decide what it accepts. Its predecessor,
// TestOpenAIWireCommunitySDKGatesGpt5SamplingParams, was deleted in the same
// commit -- the two are the before and after of one decision, not two
// behaviours the engine supports.
func TestOpenAIWireGpt5SamplingParamsReachTheVendor(t *testing.T) {
	srv, rec := newRecordingOpenAIServer(t)
	provider := wireTestChatProvider(t, srv.URL, wireTestShippedModel, map[string]any{
		"temperature": 0.2,
	})

	if _, err := provider.CallChat(context.Background(), []common.ChatMessage{
		{Role: "user", Content: "hello"},
	}); err != nil {
		t.Fatalf("CallChat was refused rather than sent: %v", err)
	}

	req := rec.Last(t)
	if got, _ := req.Body["temperature"].(float64); got != 0.2 {
		t.Errorf("temperature reached the wire as %v, want 0.2; body=%s", req.Body["temperature"], string(req.Raw))
	}
	if got, _ := req.Body["model"].(string); got != wireTestShippedModel {
		t.Errorf("model is %q, want %q", got, wireTestShippedModel)
	}
}

// TestOpenAIWireStreamKeyPresence asserts both halves of the one key the
// migration changed, directly rather than through dropFalseStreamKey's
// silence: a non-streaming call must not ask to stream, and a streaming one
// must.
func TestOpenAIWireStreamKeyPresence(t *testing.T) {
	t.Run("non-streaming omits it", func(t *testing.T) {
		srv, rec := newRecordingOpenAIServer(t)
		provider := wireTestChatProvider(t, srv.URL, "", nil)
		if _, err := provider.CallChat(context.Background(),
			[]common.ChatMessage{{Role: "user", Content: "hello"}}); err != nil {
			t.Fatalf("CallChat: %v", err)
		}
		body := rec.Last(t).Body
		if value, present := body["stream"]; present && value != false {
			t.Errorf("non-streaming request carries stream=%v", value)
		}
	})

	t.Run("streaming carries stream=true", func(t *testing.T) {
		srv, rec := newRecordingOpenAIServer(t)
		provider := wireTestStreamProvider(t, srv.URL, "", nil)
		streamer := provider.(interface {
			CallStream(context.Context, string) (<-chan StreamChunk, error)
		})
		chunks, err := streamer.CallStream(context.Background(), "hello")
		if err != nil {
			t.Fatalf("CallStream: %v", err)
		}
		for range chunks {
		}
		if streaming, _ := rec.Last(t).Body["stream"].(bool); !streaming {
			t.Errorf("streaming request does not carry stream=true; body=%s", string(rec.Last(t).Raw))
		}
	})
}
