# OpenAI Federation and Key Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The engine reaches OpenAI by workload identity federation through the official
SDK, the way it reaches Anthropic, and no manually entered vendor API key exists anywhere:
not in the OS, not in the engine, not in the install graph, not in the docs, not on a
local cluster.

**Architecture:** PR 1 swaps the community `sashabaranov/go-openai` for
`github.com/openai/openai-go/v3` at every call site with recorded-request tests proving the
wire is unchanged, and puts the two hand-rolled OpenAI calls (embeddings, speech) on the SDK
behind the guarded transport. PR 2 adds the OpenAI federation switch (three ids or none;
partial refuses boot), an engine-owned RFC 8693 exchanger feeding the SDK through a
middleware and the Realtime WebSocket through a bearer source, the second projected token
and ConfigMap on every engine Deployment, a vendor-aware `provider-auth check` and
observer, and then deletes every key surface, with a repo-wide gate that keeps them gone.

**Tech Stack:** Go 1.26 (`component/memql`, `integrations/openai`, `integrations/stt`),
`openai-go/v3`, kustomize render tests, MemQL DSL, React (`clients/os` Settings), the
capability-script contract for `scripts/install`.

**Spec:** `docs/superpowers/specs/2026-09-06-openai-federation-and-key-removal-design.md`.
Read it first; sections 3 and 4 are the argument for every task below. The Anthropic
federation is the template: `docs/superpowers/specs/2026-08-22-anthropic-workload-identity-federation-design.md`
and `docs/public/operate/auth/anthropic-federation.md`.

**Closes:** the epic issue and its task issues, filed by Task 0.

---

## Global constraints

- **Verification is `make test`, never `go test ./...`.** `component/memql` and
  `integrations/openai` are their own modules; build each with `GOWORK=off go build ./...`
  inside the module before pushing (the `module-boundaries` lane runs that way).
- **The official module path is `github.com/openai/openai-go/v3`**; the `/v3` suffix is part
  of the import path. Pin an exact version in `component/memql/go.mod` and every module that
  carried the community SDK indirectly (root `go.mod` line 44 and `integrations/stt`), and
  `go mod tidy` each; the `go.work` list does not change.
- **Behaviour-preserving migration:** every migrated call site has a recorded request test
  (an `httptest` server capturing the request body) written BEFORE the change and green
  after it. The chat guard fingerprints `/chat/completions` only; embeddings and speech
  must pass through unguarded, which is the documented intent.
- **Three ids or none:** `MEMQL_AI_OPENAI_IDENTITY_PROVIDER_ID`,
  `MEMQL_AI_OPENAI_SERVICE_ACCOUNT_ID`, `MEMQL_AI_OPENAI_IDENTITY_TOKEN_FILE`. All set
  federates; none leaves the providers unavailable (not an error); one or two refuse boot
  naming both halves. The Anthropic switch loses its key arm the same way.
- **The exchange is engine-owned** (`component/memql/ai_openai_federation.go`), rides the
  guarded client so the observer sees it, re-exchanges at expiry minus 120 s (advisory) and
  minus 30 s (mandatory), and never logs or stores the subject token or the bearer.
- **Env names are `MEMQL_`-prefixed and registered** (`scripts/secrets/manifest.yaml` then
  `make env-registry-sync`, `make env-registry-check`); all federation names are
  `optional: true` or `TestNoAIVariableIsInTheSealFloor` reds.
- **Kustomize render tests pin the shape** for every engine Deployment in every overlay;
  committed overlays carry placeholders only (`REPLACE-WITH-...`).
- **Stage by explicit path; commit trailers `Co-Authored-By` and `Claude-Session`; no
  emojis; hostnames in docs are `example.com` or `<domain>`.** The vendor-domain gate
  (`TestNoVendorDomainLiterals`) scans docs too.
- **A new `.memql` construct or builtin change fans out:** `go run ./cmd/memqllint dsl/`,
  `make sdk-gen` (gate `make sdk-gen-check`), `make arch-model` if `make arch-model-check`
  is stale.

---

## The shape of the epic

| Layer | Where | What it holds |
|---|---|---|
| SDK migration (PR 1) | `component/memql/ai_providers.go`, `openai_embedding.go`, the TTS provider, `integrations/stt/openai_whisper.go` | Every OpenAI call on `openai-go/v3` |
| Federation (PR 2) | `component/memql/ai_openai_federation.go`, `ai_federation_observer.go`, `provider_auth_check.go`, `provider_auth_status_read.go`, `provider_config_write.go`, `provider_verify.go` | The switch, the exchanger, the middleware, the bearer source, vendor-aware check and observer |
| Transcription bearer (PR 2) | `integrations/openai/openai.go`, `asr.go`; `app/integrations_stt.go` | `Bearer func(ctx) (string, error)` replacing `APIKey` |
| Manifests (PR 2) | `deploy/k8s/base/openai-federation.yaml`, every engine Deployment, `deploy/k8s/overlays/{cloud,cloud-entry}`, `deploy/k8s/overlays/render_openai_federation_test.go` | The second projected token, ConfigMap, patches, render tests |
| Removal (PR 2) | `dsl/providers/providers.memql`, `dsl/common/builtins.memql`, `component/envregistry`, `dsl/install/actions.memql`, `scripts/install/graph/*.json`, `scripts/install/verify-provider-key.sh`, `clients/os/src/apps/settings/*`, docs | Every key surface deleted; the gate that keeps them deleted |

---

## Task 0: file the epic and its tasks

**DONE on 2026-09-06:** epic #5088; tasks #5089, #5090, #5091 (PR 1), #5092, #5093, #5094, #5095 (PR 2), in the table's order. Do not file them again; verify with `gh issue list --repo znasllc-io/memql --label epic:openai-federation`.

Follow epic 1's Task 0 shape exactly (`docs/superpowers/plans/2026-09-06-configuration-readiness.md`,
Task 0): label `epic:openai-federation`, one epic issue (`epic`, `epic:openai-federation`,
`feature`, `engine`, `claude`), and these task issues (`task`, `epic:openai-federation`,
`claude`, plus `engine`, or `area/portal` for the OS row):

| Title | PR |
|---|---|
| SDK: recorded-request tests for every OpenAI call site | 1 of 2 |
| SDK: migrate chat, streaming, tools, structured, vision, usage to openai-go/v3 | 1 of 2 |
| SDK: embeddings, speech and transcription on openai-go/v3, behind the guard | 1 of 2 |
| Federation: the OpenAI credential switch, the exchanger, the middleware, the observer | 2 of 2 |
| Federation: manifests, env registry, provider-auth check, the runbook | 2 of 2 |
| Removal: keys gone from engine, DSL, install graph, scripts and docs, with the gate | 2 of 2 |
| Removal: the OS Settings providers section without a key field; provider gates widened | 2 of 2 |

---

# PR 1: the SDK migration

## Task 1: recorded-request tests before the change

**Files:**
- Create: `component/memql/openai_wire_test.go`
- Create: `component/memql/testdata/openai/*.request.json` (one per call site class)
- Create: `integrations/stt/openai_whisper_wire_test.go`

**Interfaces:**
- Produces: `newRecordingOpenAIServer(t) (*httptest.Server, *recorded)` where
  `recorded.Last() (path string, body map[string]any, headers http.Header)`; a fixture
  per class: `chat`, `chat-stream`, `chat-tools`, `chat-structured`, `chat-vision`,
  `embeddings`, `speech`, `transcription`.

- [ ] **Step 1: Write the recorder and one test per class against the CURRENT code**

Each test builds the provider through the existing constructor with `baseURL` pointing at
the recording server (`cfg.Auth["baseURL"]`, honoured by `newOpenAIProvider` today), makes
one call, and asserts the recorded path and body EQUAL the fixture file. On the first run
the fixture is written from the recording (`-update` flag pattern: `if *update { write }`);
commit the fixtures. Assert on: path (`/v1/chat/completions`, `/v1/embeddings`,
`/v1/audio/speech`, `/v1/audio/transcriptions`), the `Authorization` header shape
(`Bearer ` prefix present), the `OpenAI-Project` header when a project id is configured,
and the full body with volatile fields (none expected) normalised.

For streaming, the recorder answers two SSE `data:` frames and `[DONE]`; the test asserts
the request body carries `"stream": true` and the provider yields two deltas.

- [ ] **Step 2: Run them; all green against the community SDK**

Run: `go test -count=1 ./component/memql/ -run 'OpenAIWire' -v && go test -count=1 ./integrations/stt/ -run 'WhisperWire' -v`

- [ ] **Step 3: Commit**

```bash
git add component/memql/openai_wire_test.go component/memql/testdata/openai integrations/stt/openai_whisper_wire_test.go
git commit -m "openai: record every call site's wire shape before the SDK moves"
```

## Task 2: chat, streaming, tools, structured output, vision, usage on openai-go/v3

**Files:**
- Modify: `component/memql/go.mod` (add `github.com/openai/openai-go/v3 vX.Y.Z`, keep the
  community module until Task 3 removes it)
- Modify: `component/memql/ai_providers.go` (the OpenAI constructors and the
  `openAIProvider` / `openAIStreamProvider` methods)

**Interfaces:**
- Produces: `newOpenAIClient(cfg ProviderConfig, httpClient *http.Client) (openai.Client, error)`
  building `openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(base),
  option.WithHTTPClient(httpClient), option.WithProject(projectId))`; the provider structs
  hold `client openai.Client` (a value). PR 2 replaces the `WithAPIKey` line with the
  federation middleware; nothing else changes then.
- Consumes: Task 1's fixtures.

- [ ] **Step 1: The mapping, applied per call site**

| Community SDK | openai-go/v3 |
|---|---|
| `openai.DefaultConfig(key)`, `config.BaseURL`, `config.HTTPClient`, `NewClientWithConfig` | `openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(base), option.WithHTTPClient(httpClient), option.WithProject(id))` |
| `openAIProjectHeaderClient` wrapper | deleted; `option.WithProject` |
| `client.CreateChatCompletion(ctx, req)` | `client.Chat.Completions.New(ctx, params)` |
| `client.CreateChatCompletionStream` + `Recv()`/`io.EOF` | `stream := client.Chat.Completions.NewStreaming(ctx, params)`; `for stream.Next() { chunk := stream.Current() }`; `stream.Err()`; `defer stream.Close()` |
| `openai.ChatCompletionRequest{Model, Messages, Temperature float32, TopP float32, MaxCompletionTokens int, Tools, ToolChoice, ParallelToolCalls, Stream, ResponseFormat}` | `openai.ChatCompletionNewParams{Model, Messages, Temperature: openai.Float(v), TopP: openai.Float(v), MaxCompletionTokens: openai.Int(n), Tools, ToolChoice, ParallelToolCalls: openai.Bool(b), ResponseFormat}`; the `if _, ok := params["temperature"]; ok` guards stay and set `openai.Float` inside them, so an unset value is genuinely omitted |
| `ChatCompletionMessage{Role: user/system/assistant/tool, Content, Name, ToolCallID, ToolCalls}` | `openai.UserMessage(content)`, `openai.SystemMessage(text)`, `openai.AssistantMessage(text)` (with tool calls via the assistant param's `ToolCalls`), `openai.ToolMessage(content, toolCallID)` |
| `openai.Tool{Type: ToolTypeFunction, Function: &FunctionDefinition{Name, Description, Parameters}}` | `openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{Name, Description: openai.String(d), Parameters: shared.FunctionParameters(schemaMap)})` |
| `ResponseFormat: &ChatCompletionResponseFormat{Type: JSONSchema, JSONSchema: &{Name, Description, Schema: json.RawMessage, Strict}}` | `ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{Name, Description: openai.String(d), Schema: schemaAsAny, Strict: openai.Bool(s)}}}` where `schemaAsAny` is the schema unmarshalled into `map[string]any` (spike first: assert the recorded body is byte-equal to the fixture, which is what decides whether a `json.RawMessage` would have worked) |
| `ChatMessagePart{Type: ImageURL, ImageURL: &{URL, Detail}}` | `openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{openai.TextContentPart(text), openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: dataURI, Detail: "auto"})})`; confirm the `Detail` field name in the SDK source before writing |
| `resp.Choices[i].FinishReason == openai.FinishReasonLength` | compare the string `"length"` / `"content_filter"`; the v3 field is a string |
| `resp.Usage.PromptTokens`, `.CompletionTokens` | same names on `resp.Usage` (verify once) |
| `choice.Message.Refusal` | same |

- [ ] **Step 2: Run the wire tests; they must be green with no fixture change**

Run: `go test -count=1 ./component/memql/ -run 'OpenAIWire|TestOpenAI|Provider' -v`
If a fixture differs only in key order, normalise the comparison; if a field is missing or
renamed on the wire, that is the SDK expressing the request differently and the task
stops to record it in the report rather than editing the fixture to match.

- [ ] **Step 3: Full tree and commit**

Run: `(cd component/memql && GOWORK=off go build ./...) && make test 2>&1 | tail -5`

```bash
git add component/memql/go.mod component/memql/go.sum component/memql/ai_providers.go
git commit -m "openai: chat, streaming, tools, structured output and vision on openai-go/v3"
```

## Task 3: embeddings, speech and transcription on the SDK, behind the guard

**Files:**
- Modify: `component/memql/openai_embedding.go` (replace the hand-rolled POST)
- Modify: `component/memql/ai_providers.go` (the TTS provider's `Synthesize`)
- Modify: `integrations/stt/openai_whisper.go` and `integrations/stt/go.mod`
- Modify: `component/memql/go.mod`, root `go.mod`, `integrations/stt/go.mod` (remove the
  community module), then `go mod tidy` in each

**Interfaces:**
- Produces: embeddings through `client.Embeddings.New(ctx, openai.EmbeddingNewParams{Model,
  Input, Dimensions: openai.Int(d)})`; speech through `client.Audio.Speech.New(ctx,
  openai.AudioSpeechNewParams{Model, Input, Voice, ResponseFormat})` reading the returned
  `*http.Response` body as today; transcription through
  `client.Audio.Transcriptions.New(ctx, openai.AudioTranscriptionNewParams{Model, File,
  Language})`, with the file name supplied the way the SDK's multipart helper expects
  (spike: the recorded multipart must carry the same filename and language fields as the
  fixture).
- Both `component/memql` providers take the guarded client (`option.WithHTTPClient(guardedHTTPClient(nil))`).

- [ ] **Step 1: Migrate the three, run the wire tests, confirm the guard sees them**

Add to `openai_wire_test.go`: with the guarded transport installed, an embeddings call is
observed by the transport (a counter on a test hook) and NOT counted as an LLM call
(`isGuardedLLMPath` false); the speed field on speech, if the SDK has none, is carried
through `option.WithJSONSet("speed", v)` per request rather than dropped, and the fixture
proves it.

- [ ] **Step 2: Remove the community module everywhere**

```bash
grep -rn "sashabaranov" --include=go.mod --include=go.sum --include=*.go . | grep -v _test
```
must print nothing after `go mod tidy` in `component/memql`, `integrations/stt` and the
root. Then: `for t in "" identity mcp agent planner workbench edge bff; do go build -tags "$t" . || exit 1; done`.

- [ ] **Step 3: Full tree, then open PR 1**

Run: `make test 2>&1 | tail -5 && make sdk-gen-check && make arch-model-check`

```bash
git add component/memql/openai_embedding.go component/memql/ai_providers.go integrations/stt/openai_whisper.go \
  component/memql/go.mod component/memql/go.sum go.mod go.sum integrations/stt/go.mod integrations/stt/go.sum
git commit -m "openai: embeddings, speech and transcription on the official SDK, behind the guarded transport"
git push -u origin epic/openai-federation
```

PR body: "SDK migration, behaviour-preserving; every call site has a recorded wire test;
the community module is gone. Closes the three PR 1 task issues." Merge with
`scripts/dev/merge-as-owner.sh --pr=<n>` once `ci-required` is green.

---

# PR 2: federation and removal (branch from `main` after PR 1 merges)

## Task 4: the OpenAI credential switch, the exchanger, the middleware, the observer

**Files:**
- Create: `component/memql/ai_openai_federation.go`
- Create: `component/memql/ai_openai_federation_test.go`
- Modify: `component/memql/ai_anthropic_federation.go` (the switch loses its key arm; the
  optional-name set gains the OpenAI names; `preflightIdentityToken` takes the audience)
- Modify: `component/memql/ai_federation_observer.go` (path match for `/oauth/token`,
  vendor on the record, `vendor` label on the counter)
- Modify: `component/metrics/metrics.go` (the counter's label)
- Modify: `dsl/providers/providers.memql` (the two auth blocks)

**Interfaces:**
- Produces:

```go
const (
	envOpenAIIdentityProviderID = "MEMQL_AI_OPENAI_IDENTITY_PROVIDER_ID"
	envOpenAIServiceAccountID   = "MEMQL_AI_OPENAI_SERVICE_ACCOUNT_ID"
	envOpenAIIdentityTokenFile  = "MEMQL_AI_OPENAI_IDENTITY_TOKEN_FILE"
	openaiAudience              = "https://api.openai.com/v1"
	openaiTokenEndpoint         = "https://auth.openai.com/oauth/token"
)

type openaiFederation struct{ IdentityProviderID, ServiceAccountID, TokenFile string }
func openaiFederationFrom(auth map[string]string) openaiFederation

// BearerSource hands out the current bearer, exchanging when needed.
type BearerSource interface{ Bearer(ctx context.Context) (token string, expiresAt time.Time, err error) }

type openaiExchanger struct { /* http client, ids, token file, cache, mu */ }
func newOpenAIExchanger(fed openaiFederation, httpClient *http.Client) *openaiExchanger
func (x *openaiExchanger) Bearer(ctx context.Context) (string, time.Time, error)

// openaiCredential decides how an OpenAI client authenticates: federation, or unavailable.
func openaiCredential(cfg ProviderConfig, httpClient *http.Client) ([]option.RequestOption, BearerSource, credentialPath, error)
func openaiBearerMiddleware(src BearerSource) option.Middleware
```

  `credentialPath` gains `credentialPathUnavailable`; `newOpenAIClient` (Task 2) calls
  `openaiCredential` and passes `option.WithMiddleware(openaiBearerMiddleware(src))` plus
  `option.WithHeaderDel("Authorization")` so no ambient key survives.
- The exchange body (JSON): `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`,
  `subject_token_type=urn:ietf:params:oauth:token-type:jwt`, `subject_token` (the file,
  re-read on every exchange, trimmed), `identity_provider_id`, `service_account_id`.
  Response: `access_token`, `expires_in`. Refresh thresholds 120 s advisory, 30 s mandatory.

- [ ] **Step 1: Write the failing tests**

`ai_openai_federation_test.go`: `TestOpenAICredentialFederatesWhenAllThreeAreSet`,
`...IsUnavailableWhenNoneIsSet` (no error, `credentialPathUnavailable`),
`...RefusesOneOrTwo` (error names both halves), `TestOpenAIExchangeHappyPath` (an
`httptest` server at `/oauth/token` asserting the five JSON fields and the content type,
then a chat call carrying `Authorization: Bearer <access_token>` and no static header),
`...ReExchangesBeforeExpiry` (fake clock at expiry minus 20 s forces a new exchange),
`...DenialCountsAndLogsTheBody`, `...NeverLogsTheSubjectToken` (a log sink asserts the
JWT never appears), `TestAnthropicCredentialHasNoKeyArm` (none set means unavailable, not
`WithAPIKey`), `TestOptionalAuthNamesCoverBothVendors` (replaces
`TestOptionalAuthNamesAreAnthropicCredentialOnly`).

- [ ] **Step 2: Implement, run, commit**

The switch is `anthropicCredential`'s shape with the key arm deleted. The exchanger is
~150 lines: read file, POST, parse, cache with `sync.Mutex`, `time.Now` injectable. The
middleware: `func(req *http.Request, next option.MiddlewareNext) (*http.Response, error)`
that calls `src.Bearer(req.Context())`, sets the header, and returns `next(req)`.

Run: `go test -count=1 ./component/memql/ -run 'OpenAI|Anthropic|Federation' -v && go run ./cmd/memqllint dsl/`

```bash
git add component/memql/ai_openai_federation.go component/memql/ai_openai_federation_test.go \
  component/memql/ai_anthropic_federation.go component/memql/ai_anthropic_federation_test.go \
  component/memql/ai_federation_observer.go component/metrics/metrics.go dsl/providers/providers.memql
git commit -m "openai: workload identity federation -- three ids or none, the engine owns the exchange"
```

## Task 5: manifests, env registry, provider-auth check, the transcription bearer, the runbook

**Files:**
- Create: `deploy/k8s/base/openai-federation.yaml` (the `memql-openai-federation` ConfigMap
  with the two ids empty)
- Modify: every engine Deployment (`deploy/k8s/base/{agent,planner,workbench,mcp,edge,identity}.yaml`,
  `deploy/k8s/components/engine-bff/bff.yaml`): a second projected volume `openai-identity`
  (audience `https://api.openai.com/v1`, 3600 s, path `token`), mounted read-only at
  `/var/run/secrets/openai.com`, env `MEMQL_AI_OPENAI_IDENTITY_TOKEN_FILE`, `envFrom` the
  ConfigMap `optional: true`
- Modify: `deploy/k8s/overlays/{cloud,cloud-entry}/kustomization.yaml` (placeholder patches)
- Create: `deploy/k8s/overlays/render_openai_federation_test.go` (copy
  `render_anthropic_federation_test.go`'s three tests for the new volume, audience, mount,
  env, ConfigMap and placeholders)
- Modify: `scripts/secrets/manifest.yaml` (three entries, `component: ai`, `optional: true`,
  the token file with its default) then `make env-registry-sync`
- Modify: `component/memql/provider_auth_check.go`, `subcommand_provider_auth.go`
  (`--provider anthropic|openai`; report `vendor`, ids, subject, audience, exchange,
  expiry, models call), `provider_auth_status_read.go` (`AuthSourceFederation` for either
  vendor), `provider_config_write.go` (`providerFederationSet` takes `vendor` and either
  id set), `provider_verify.go` (the openai-go `Models.List` arm)
- Modify: `integrations/openai/openai.go` (`Bearer func(ctx) (string, error)` replaces
  `APIKey`), `asr.go` (call it at dial), `app/integrations_stt.go` (wire the engine's
  bearer source; the "disabled" log names federation)
- Modify: `scripts/install/verify-provider-key.sh` (federation branch for both vendors)
- Create: `docs/public/operate/auth/openai-federation.md`; modify
  `anthropic-federation.md` (delete the "OpenAI: no federation" paragraph; link the new
  runbook), `docs/public/operate/env-vars.md`

- [ ] **Step 1: Tests first** — the render tests (fail on every Deployment until the
  YAML lands), `TestCheckProviderAuthReportsOpenAIFederation` against the exchange server
  from Task 4, an ASR test that the WebSocket dial carries the bearer the source returns.
- [ ] **Step 2: Implement, regenerate, run** — `make env-registry-sync && make env-registry-check`,
  `go test -count=1 ./deploy/k8s/overlays/ -run Federation -v`, `make sdk-gen && make sdk-gen-check`
  (the builtin's args changed), `make test`.
- [ ] **Step 3: Commit**

```bash
git add deploy/k8s scripts/secrets/manifest.yaml component/envregistry/manifest.yaml \
  component/memql/provider_auth_check.go subcommand_provider_auth.go component/memql/provider_auth_status_read.go \
  component/memql/provider_config_write.go component/memql/provider_verify.go \
  integrations/openai app/integrations_stt.go scripts/install/verify-provider-key.sh \
  docs/public/operate/auth/openai-federation.md docs/public/operate/auth/anthropic-federation.md docs/public/operate/env-vars.md \
  sdk/go/client sdk/ts/src/client
git commit -m "openai federation: the projected token on every engine node, the check, the bearer for transcription, the runbook"
```

## Task 6: keys gone everywhere, with the gate

**Files (delete or edit; the spec's section 2.3 and the research inventory are the list):**
- `dsl/common/builtins.memql`: delete `providerKeySet`; `providerFederationSet` per Task 5
- `component/memql/provider_config_write.go`: delete `evaluateProviderKeySetExpression`,
  `providerKeyNames`; `engine_types.go`: delete `BuiltinExecutorProviderKeySet`;
  `executor_builtin.go`: the map entry; `provider_reload_propagate_test.go:382-397`
- `component/memql/ai_providers.go`: `newOpenAIPlaceholderProvider` validates through
  `openaiCredential` (federated or unavailable), never `auth.apiKey`
- `component/memql/fleet_catalog_read.go`: delete `InferenceDoorApiKey` and the
  `cloudConfigured && !federation` arm; `HasCloudProviderConfigured` doc says "federated";
  `dsl/platform/concepts.memql` `inferenceStatus.cloudConfigured` description
- `component/envregistry/legacyalias.go`: delete the four key alias rows;
  `scripts/secrets/manifest.yaml`: delete `MEMQL_AI_ANTHROPIC_API_KEY`,
  `MEMQL_AI_OPENAI_API_KEY`, `MEMQL_ANTHROPIC_API_KEY`, `MEMQL_OPENAI_API_KEY` and the
  `MEMQL_SI_*` aliases; `component/config/config.go` and `policy_exposable.go`: delete
  `SiOpenaiApiKey` (the bus proto field number is reserved, not reused)
- `dsl/install/actions.memql`: `verifyProviderKey` becomes `verifyProviderFederation`
  (args `provider`, `federationDeploy`, `namespace`); `scripts/install/graph/install.json`
  and `install-main.json`: the node renamed `providerFederation`, description "Skipped when
  no federation ids were supplied: installing spends no AI credit"; `editors/vscode/src/install/*`
  the `providerKey` step follows
- `scripts/install/verify-provider-key.sh`: the `--key-file` branch deleted; renamed
  `verify-provider-federation.sh` with the capability contract kept
  (`scripts/lib/capability_contract_test.go` covers it)
- `scripts/install/seed-bootstrap.sh`: the key names deleted
- Docs: `env-vars.md` (the six-name exception becomes the two vendors' federation sets),
  `minimum-requirements.md` (no key is required; transcription needs OpenAI federation),
  `azure-entry-install.md`, `local-models.md` (two doors plus the fleet), `audio-streaming.md`,
  `llm-cost-control.md`
- `component/memql/keyless_boot_test.go`: the blank list gains the three OpenAI names and
  loses the four key names
- Create: `vendor_api_key_gate_test.go` (root package)

- [ ] **Step 1: Write the gate first**

```go
// TestNoVendorApiKeyEntryPoint: a manually entered vendor key has no way back in.
// Scans git-tracked Go, MemQL, TypeScript, YAML, shell and Markdown files for the
// retired names; the allowlist is this record and the two design records that
// explain the removal.
func TestNoVendorApiKeyEntryPoint(t *testing.T) {
	banned := []string{"MEMQL_AI_ANTHROPIC_API_KEY", "MEMQL_AI_OPENAI_API_KEY", "MEMQL_ANTHROPIC_API_KEY",
		"MEMQL_OPENAI_API_KEY", "providerKeySet", "PROVIDER_KEY_FILE", "provider-key-file"}
	allowed := map[string]bool{
		"docs/superpowers/specs/2026-09-06-openai-federation-and-key-removal-design.md": true,
		"docs/superpowers/specs/2026-08-22-anthropic-workload-identity-federation-design.md": true,
		"docs/superpowers/specs/2026-08-23-zero-key-install-design.md": true,
		"vendor_api_key_gate_test.go": true,
	}
	// walk `git ls-files`, skip allowed, fail naming path:line for every hit
}
```

Run it: it must FAIL on dozens of files before the sweep and pass after. The `git
ls-files` walk is the repo's convention (untracked files are invisible to gates).

- [ ] **Step 2: The sweep, file by file, then every gate**

Run, in order: `go run ./cmd/memqllint dsl/`, `make sdk-gen && make sdk-gen-check`,
`make env-registry-sync && make env-registry-check`, `go test -count=1 -run 'TestNoVendorApiKeyEntryPoint|TestNoAIVariableIsInTheSealFloor|TestDocsFrontMatter|TestDocsRelativeLinks|TestNoVendorDomainLiterals' .`,
`go test -count=1 ./scripts/lib/ ./scripts/install/... -v`, `for t in "" identity mcp agent planner workbench edge bff; do go build -tags "$t" . || exit 1; done`,
`make test`.

- [ ] **Step 3: Commit**

```bash
git add -u   # NOT allowed: stage by explicit path -- list every file from the sweep
git commit -m "providers: no manually entered vendor key anywhere, and a gate that keeps it so"
```

(The `-u` line above is a reminder of what not to do; list the paths.)

## Task 7: the OS providers section without a key, the gates widened, PR 2

**Files:**
- Modify: `clients/os/src/apps/settings/ProvidersSection.tsx`, `providerFacts.ts`
  (delete `saveKey`, the `OpenAiPanel` key box, the "OpenAI publishes no federation" text;
  the OpenAI panel gets the two-id federation form; rule 3 in the header reads
  "federation is the only door"), `clients/os/test/settings/providers.test.tsx`
- Modify: `clients/os/src/apps/registry.tsx` (`providers` section role
  `{ any: ["owner", "developer"] }`), `clients/os/test/settings/settingsContract.test.ts`
  (the pinned role)
- Modify: `component/memql/provider_auth_status_read.go`, `provider_config_write.go`,
  `provider_verify.go`, `provider_reload_propagate.go` (owner-or-developer set: a
  `providersAuthorized(ctx)` helper modelled on email's `configureAuthorized`), and
  `dsl/common/builtins.memql` doc lines that say "owner-only"
- Modify: `clients/os/src/apps/cluster/readiness/ReadinessSection.tsx` (`notReadyNext`
  no longer offers a key; the two federation routes name both vendors)

- [ ] **Step 1: Tests first** — the OS providers suite renders no key field and offers
  both federation forms; a developer session sees the section; the Go gate tests admit a
  developer and refuse a writer.
- [ ] **Step 2: Implement, run** — `cd clients/os && npm run typecheck && npx vitest run test/settings test/cluster`,
  `go test -count=1 ./component/memql/ -run 'Provider' -v`, `make test`.
- [ ] **Step 3: Commit, push, PR 2**

```bash
git add clients/os/src/apps/settings clients/os/src/apps/registry.tsx clients/os/test/settings \
  clients/os/src/apps/cluster/readiness/ReadinessSection.tsx \
  component/memql/provider_auth_status_read.go component/memql/provider_config_write.go \
  component/memql/provider_verify.go component/memql/provider_reload_propagate.go dsl/common/builtins.memql
git commit -m "os: AI providers is federation only, and a developer may federate"
git push -u origin epic/openai-federation-removal
```

PR body closes the four PR 2 task issues and the epic; it names the reversal of the
Anthropic record's D4 and D6 and links the new runbook. Delete this plan in the same PR
(`git rm docs/superpowers/plans/2026-09-06-openai-federation-and-key-removal.md`).

---

## Plan self-review

**Spec coverage.** D1 manifests: Task 5. D2 the switch: Task 4. D3 the engine-owned
exchange and the two consumers: Tasks 4 and 5. D4 the migration in its own PR with
recorded tests: Tasks 1 to 3. D5 removal and the gate: Task 6, the OS half in Task 7. D6
local keyless: Task 6's docs and the keyless-boot list. D7 developer gate: Task 7. Section
6 tests: items 1 to 3 in Task 4, 4 in Tasks 1 to 3, 5 and 6 in Task 5, 7 in Task 6, 8 in
Task 7.

**Placeholders.** The exact `openai-go/v3` version is pinned by the implementer from the
module's current release at execution time; two shapes (JSON-schema `Schema`, the
transcription multipart) are spikes the tasks name explicitly, decided by the recorded
fixture rather than left open.

**Type consistency.** `BearerSource` is produced in Task 4 and consumed by the middleware
(Task 4), the ASR config (Task 5) and `provider-auth check` (Task 5);
`credentialPathUnavailable` is used by Tasks 4, 5 and 6; `openaiCredential` is called by
`newOpenAIClient` from Task 2 and by the placeholder constructor in Task 6.
