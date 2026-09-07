# OpenAI workload identity federation, the official SDK, and no more API keys -- Design

- **Date:** 2026-09-06
- **Status:** approved in the 2026-09-06 brainstorm as epic 2 of the inference and setup
  program (`2026-09-06-inference-and-setup-program.md`). The forks below (D1-D6) were put to
  the owner or follow from what the owner decided; each says which.
- **Owner areas:** `component/memql` (provider construction, federation, the exchange
  observer, `provider-auth check`), `integrations/openai` and `integrations/stt` (the
  bearer for transcription), `deploy/k8s` (the projected token and the ConfigMap seam),
  `dsl/providers`, `dsl/common/builtins.memql`, `component/envregistry`, `scripts/install`,
  `clients/os` (Settings, AI providers), `docs/public/operate`.

---

## 1. Problem

Three things, and the owner wants all three closed by one epic:

1. **OpenAI has no federated door.** The engine reaches Anthropic by workload identity
   federation (epic memql#4333) and OpenAI by a static key. The 2026-08-22 record said
   OpenAI offered no federation; that was wrong when written. OpenAI's workload identity
   federation has been generally available since 2025-05-26, with a Kubernetes guide and an
   Azure guide that accepts an AKS projected token directly, the same shape as ours.
2. **The community SDK has no credential seam.** The engine pins
   `github.com/sashabaranov/go-openai` v1.42.0, whose bearer is a string fixed at
   construction; the official `github.com/openai/openai-go` (v3.56.0 at the time of writing)
   carries federation natively and is the SDK OpenAI maintains against its API.
3. **A typed key is a long-lived credential at rest.** The owner's decision (program
   decision 1): manual vendor API keys are removed everywhere, the local cluster included.
   Federation and the fleet are the only doors.

## 2. What the tree already has

### 2.1 The Anthropic federation, the template

`component/memql/ai_anthropic_federation.go` decides the credential (all four ids
federate, none means the key, one to three refuses boot); `ai_federation_observer.go`
watches the exchange on the guarded transport by path (`/v1/oauth/token`) and records the
last exchange for `provider-auth check`; `provider_auth_check.go` and the `memql
provider-auth check` subcommand force one exchange and list models from inside a pod;
`provider_auth_status_read.go` reports `authSource=federation`; `provider_config_write.go`
writes the federation ids as global variables (`providerFederationSet`).
`deploy/k8s/base/anthropic-federation.yaml` ships the `memql-engine` ServiceAccount and the
empty `memql-anthropic-federation` ConfigMap; every engine Deployment carries the projected
`anthropic-identity` volume (audience `https://api.anthropic.com`, 3600 s) mounted at
`/var/run/secrets/anthropic.com`; the cloud overlays patch the ids; render tests pin the
shape and that the committed overlays carry placeholders only.
`scripts/install/verify-provider-key.sh --federation-deploy` runs the check from outside.

### 2.2 The OpenAI SDK usage

Exactly two non-test files import the community SDK: `component/memql/ai_providers.go`
(client construction through `openai.DefaultConfig` and `NewClientWithConfig`, seven
`CreateChatCompletion` sites, three `CreateChatCompletionStream` sites, ten request
literals, the tool and message types, the JSON-schema response format, the vision content
parts, two finish-reason constants, the usage fields) and
`integrations/stt/openai_whisper.go` (`CreateTranscription`). Two OpenAI calls are NOT SDK
calls and NOT behind the LLM guard today: embeddings (`component/memql/openai_embedding.go`,
a hand-rolled `POST /v1/embeddings` on a bare `http.Client`) and speech (`ai_providers.go`'s
text-to-speech provider, a hand-rolled `POST /v1/audio/speech` on `http.DefaultClient`).
The Realtime transcription WebSocket in `integrations/openai/asr.go` (its own Go module)
sets `Authorization: Bearer <key>` itself at dial time from a static field that
`app/integrations_stt.go`'s `openAIKeyFromEnv` fills; that one field is the whole seam.
An `OpenAI-Project` header is added by two hand-written client wrappers. Nothing in the
tree type-asserts the community SDK's error type.

### 2.3 The key surfaces

Two dozen places enter, store, verify, read or document a static vendor key: the OS Settings
key entry (`ProvidersSection.tsx`, `providerFacts.ts`: `providerKeySet`, `saveKey`), the
engine builtin `providerKeySet` and the `vendor_api_key` global-secret kind for the AI
vendors, the `apiKey env(...)` entries of the two base providers in
`dsl/providers/providers.memql`, four env names and four legacy aliases in the env registry,
`component/config`'s `SiOpenaiApiKey` and its policy-exposable flag, the install graph's
`providerKey` node and eight `dependsOn` references, the `--key-file` branch of
`verify-provider-key.sh`, the ten placeholder OpenAI provider types (`openaistt`,
`openaiwhisper`, `openairealtime`, `openaiaudio`, `openaiimage`, ...) whose constructor
validates nothing but `auth.apiKey`, `inferenceStatus`'s `apiKey` door, the test that pins
the optional auth names to Anthropic only, the keyless-boot test's fifteen-name blank list,
and the docs (`env-vars.md`, `minimum-requirements.md`, which still says an OpenAI key is
required, `azure-entry-install.md`, `local-models.md`, `audio-streaming.md`,
`anthropic-federation.md`). `providerVerify` and the keyless-boot tests stay: they verify a
federated credential as well as they did a key. Nothing under `deploy/` and nothing in the
k3d scripts names a vendor key; keys arrive only through the hand-seeded `memql-secrets`.

### 2.4 OpenAI's federation, verified 2026-09-06

The exchange is RFC 8693 token exchange: a JSON `POST` to OpenAI's auth host at
`/oauth/token` with `grant_type`, `subject_token_type`, `subject_token`,
`identity_provider_id` and `service_account_id`, answering a `Bearer` access token that
expires after at most one hour, never outlives the subject token, and comes with no refresh
token. The principal is a Platform service account in a project; Admin API endpoints are
excluded; limits are 50 identity providers per organization and 50 mappings per provider.
The Kubernetes guide projects a token with audience `https://api.openai.com/v1` and matches
the mapping on `sub` = `system:serviceaccount:<namespace>:<name>`; AKS uses discovery;
self-hosted clusters must upload a JWKS. Whether a federated token is accepted on the
Realtime WebSocket is not stated in OpenAI's documentation and is verified by the runbook.

## 3. Decisions

### D1 -- Mirror the Anthropic shape exactly: one more projected token, one more ConfigMap

Chosen over an Entra two-hop (rejected for Anthropic in memql#4333 for the same reasons)
and over reusing the Anthropic token (a projected token is minted for one audience; OpenAI's
is `https://api.openai.com/v1`). Every engine Deployment gains a second projected volume,
`openai-identity`, mounted at `/var/run/secrets/openai.com`, and the base env
`MEMQL_AI_OPENAI_IDENTITY_TOKEN_FILE=/var/run/secrets/openai.com/token`. A
`memql-openai-federation` ConfigMap carries `MEMQL_AI_OPENAI_IDENTITY_PROVIDER_ID` and
`MEMQL_AI_OPENAI_SERVICE_ACCOUNT_ID`, empty in the base, patched in the cloud overlays,
placeholders only in git. The same `memql-engine` ServiceAccount serves both vendors; the
mapping on OpenAI's side matches `sub` = `system:serviceaccount:memql:memql-engine`.

### D2 -- All three or none; there is no key to fall back to

The three OpenAI federation values (provider id, service account id, token file) are read as
a set: all three federate; none leaves the OpenAI providers registered as unavailable, which
is the normal state of a fresh cluster and of every local cluster; one or two refuse boot,
naming both halves. The Anthropic switch loses its key arm the same way: all four federate,
none is unavailable, one to three refuse. "Unavailable" is not an error: a cluster with no
provider is what the readiness model reports as `ai` unconfigured (epic 1).

### D3 -- The engine owns the OpenAI exchange, once, and hands the bearer to every consumer

Chosen over letting the official SDK perform the exchange internally (`option.WithWorkloadIdentity`
with a token-file provider). The SDK's native path would serve the HTTP clients but not the
Realtime WebSocket, which needs a raw bearer, so two exchangers with two caches would exist,
one of them opaque to `provider-auth check`. One exchanger in `component/memql`
(`ai_openai_federation.go`): reads the token file on every exchange, posts the RFC 8693
request through the guarded client so the observer sees it, caches the bearer, re-exchanges
before expiry (advisory at expiry minus 120 s, mandatory at minus 30 s, the Anthropic SDK's
own numbers), and exposes `Bearer(ctx) (string, time.Time, error)`. The official SDK client
takes it through a request middleware that sets `Authorization` per request and deletes any
static header; the Realtime and whisper paths take it as a `BearerSource`. The Anthropic
exchange stays inside the Anthropic SDK, as today.

### D4 -- The official SDK replaces the community one, behaviour-preserving, in its own PR

Every OpenAI call site moves to `github.com/openai/openai-go`: client construction, chat
completions, streaming, tool calling, structured output, embeddings, speech, transcription.
The migration is PR 1 and changes no wire behaviour: the same request shapes, verified by
recording tests against an `httptest` server per call site before and after. Federation
and removal are PR 2, which the migration unblocks. If a capability the engine uses has no
equivalent in the official SDK (the owner's condition: "as long as it doesn't limit us on
functionality"), the migration keeps that one call on a hand-written request against the
same base URL and says so in the plan; nothing is dropped.

### D5 -- Keys leave everywhere, in the same PR that opens the OpenAI door

Executed as program decision 1. The OS Settings, AI providers section shows each vendor's
door (federated, unavailable, or half-configured) and the fleet door, and offers the
federation id form and Apply; the key field, `providerKeySet`, the `vendor_api_key` rows for
the AI vendors, the four env names and their aliases, the install graph's `providerKey` node,
the `--key-file` branch of the verify script and every doc sentence that offers a key are
deleted; the ten placeholder OpenAI provider types validate the federated credential the
way the real constructors do, so a keyless cluster still registers them as unavailable
rather than failing to construct them. The install graph's `providerKey` node becomes
`providerFederation`: it runs the federation check when ids were supplied and is skipped
otherwise, so an install still spends no AI credit. `inferenceStatus` loses the `apiKey`
door; `HasCloudProviderConfigured` means "a federated vendor is available"; the park card's
cloud-approval affordance (`cloudApproved` in `component/router/fleet_refusal.go`) keys on
the same reading. `TestNoAIVariableIsInTheSealFloor` and the keyless-boot tests stay and get
stronger.

### D7 -- The provider writes admit developers, as a set

`providerAuthStatus`, `providerFederationSet`, `providerVerify` and `providersReload` are
owner-only today; the first-run wizard (epic 5) is owner-or-developer, because the owner
said a developer helps an owner through setup. The four builtins move to the
owner-or-developer SET (the Integrations gate, C9 of the integration-config record), never
a rank floor, and the Settings, AI providers section's role follows. A developer still
cannot administer anything a cluster owner alone may (the owner-only gates elsewhere are
untouched); what they can do is put a federation id in front of the engine and apply it.

### D6 -- The local cluster is keyless, and that is a feature with a cost

A local cluster's OIDC issuer is private, so neither vendor can federate with it. After this
epic a developer on k3d reaches cloud models only through a fleet machine's signed-in app
(epic 3) or a local model (epics 3 and 4), and streaming transcription and whisper are off on
a local cluster until a fleet machine can transcribe (a later epic). CI already installs
keyless. The runbooks say so plainly rather than offering a key "for local only".

## 4. The change

### 4.1 Federation (PR 2)

- `dsl/providers/providers.memql`: the `openai` base provider's `auth` block declares
  `identityProviderId env("MEMQL_AI_OPENAI_IDENTITY_PROVIDER_ID")`,
  `serviceAccountId env("MEMQL_AI_OPENAI_SERVICE_ACCOUNT_ID")` and
  `identityTokenFile env("MEMQL_AI_OPENAI_IDENTITY_TOKEN_FILE")`; the `apiKey` entries of
  both base providers are deleted; all federation names are optional at resolution
  (`optionalAuthEnvNames`).
- `component/memql/ai_openai_federation.go`: `openaiFederationFrom(auth)`,
  `openaiCredential(cfg, httpClient) (bearerSource, credentialPath, error)` with the
  three-way switch of D2, `preflightIdentityToken` reused with the OpenAI audience, the
  exchanger with its cache, and the SDK middleware.
- `ai_federation_observer.go`: `isFederationExchange` matches `/v1/oauth/token` and
  `/oauth/token`; the record carries the vendor; the counter gains a `vendor` label.
- `provider_auth_check.go`: covers both vendor types, reports the vendor, the credential
  path, the ids, the token's subject and audience, the exchange result and expiry, and the
  models call; the subcommand takes `--provider` for either.
- `provider_auth_status_read.go` and `provider_config_write.go`: `providerFederationSet`
  takes a `vendor` and the OpenAI pair; `providerKeySet` is deleted.
- Manifests: the second volume and mount on every engine Deployment, the env, the
  ConfigMap, the overlay patches, the render tests extended to both.
- Env registry: three new names; four key names and four aliases removed.
- `scripts/install/verify-provider-key.sh` becomes `verify-provider-federation.sh` in
  spirit: `--provider={anthropic|openai} --federation-deploy=<deploy>` only.
- Docs: `docs/public/operate/auth/openai-federation.md` (runbook, the Anthropic one's
  shape: issuer check, Platform console setup, the two ids, verify, no key to remove), and
  the Anthropic runbook's "OpenAI: no federation mechanism" paragraph deleted.

### 4.2 The SDK migration (PR 1)

The module is `github.com/openai/openai-go/v3` (the `/v3` suffix is part of the path).
`component/memql/ai_providers.go`'s OpenAI constructors, the streaming provider, the
tool-calling and structured-output paths, the vision content parts, the finish-reason and
usage reads; `integrations/stt/openai_whisper.go`; and the two hand-rolled calls, embeddings
and speech, which move onto `client.Embeddings.New` and `client.Audio.Speech.New` and
behind the guarded transport for the first time (the guard fingerprints `/chat/completions`
only, so neither is loop-guarded, which is the documented intent). `option.WithProject`
replaces the two `OpenAI-Project` wrappers. Each call site's request shape is recorded
against `httptest` before the change and asserted after. Two shapes need a spike before
they are transcribed: the JSON-schema response format's `Schema` field is `any` rather than
raw JSON, and the transcription call's file name and language parameters. The community
module is removed from `component/memql/go.mod`, the root `go.mod` and every module that
carried it indirectly, in the same PR.

### 4.3 The bearer for transcription (PR 2)

`integrations/openai.Config.APIKey` becomes `Bearer func(ctx) (string, error)`;
`app/integrations_stt.go` wires the engine's OpenAI bearer source and logs "audio websocket
disabled (no OpenAI federation on this cluster)" when there is none.

## 5. Failure modes

- A half-configured federation refuses boot, naming both halves, for either vendor.
- A denied exchange logs the vendor's own error body, ticks the `denied` counter, and the
  last good bearer keeps working until it expires, exactly as for Anthropic; alert on
  `denied`.
- A cluster with no federated vendor boots, serves everything that needs no model, and the
  readiness model says `ai` is unconfigured; nothing falls back to anything.
- A federated token refused by the Realtime WebSocket is a runbook finding, not a crash:
  the ASR client reports the handshake status and the audio websocket stays disabled.

## 6. Testing

1. `TestOpenAIFederationSwitch`: all three, none, one and two set; the key path absent.
2. The exchange against `httptest`: request body fields, JSON content type, the bearer
   cached, re-exchange at the advisory and mandatory thresholds, denial recorded with the
   body, the observer counter per vendor.
3. The SDK middleware: every request carries `Authorization: Bearer <current>` and never a
   stale one across a re-exchange.
4. Recording tests per migrated call site (chat, stream, tools, structured, embeddings,
   speech, transcription): request shape equal before and after the SDK change.
5. Kustomize render: both projected volumes, both mounts, both envs, both ConfigMaps on
   every engine Deployment in every overlay; placeholders only.
6. `provider-auth check` reports both vendors; the verify script's federation branch for
   each.
7. The removal sweep: a repo-wide grep gate `TestNoVendorApiKeyEntryPoint` that fails on
   `MEMQL_AI_ANTHROPIC_API_KEY`, `MEMQL_AI_OPENAI_API_KEY`, `providerKeySet` or
   `vendor_api_key` for an AI vendor anywhere in Go, DSL, TS, YAML or docs, so a key path
   cannot quietly come back.
8. OS: the Providers section renders the two doors and the fleet, offers the federation
   form, and has no key field; `make os-typecheck`, `make os-test`.

## 7. Delivery

Two PRs. PR 1, the SDK migration, behaviour-preserving, with the recording tests. PR 2,
OpenAI federation, the bearer for transcription, and the removal of keys everywhere, with
the runbook. PR 2 branches from `main` after PR 1 merges.

## 8. Out of scope

- Scripting the Platform console setup through OpenAI's Admin API.
- Local-cluster federation (a private issuer cannot be discovered; uploading a JWKS per
  developer cluster is not reproducible).
- Local transcription on a fleet machine (a later epic; the loss on local clusters is
  recorded in D6).

## 9. Facts to re-verify before starting

OpenAI's exchange endpoint and fields; the official SDK's current version and its
middleware option; whether the Realtime WebSocket accepts a federated bearer; the count of
community-SDK call sites; the 24-surface removal list. All were true on 2026-09-06.
