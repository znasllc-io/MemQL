# Open-weight defaults and the curated catalog -- Design

- **Date:** 2026-09-07
- **Status:** approved in the 2026-09-07 brainstorm as epic 3 of the local-first
  inference and routing program (`2026-09-07-local-first-routing-program.md`). The owner
  set the goal: every part of the platform that calls a model has a good-enough
  open-weight default that runs on a current Apple Silicon machine through the fleet,
  offered from a catalog organised by category with the best few entries per category
  rather than every model in the world, and custom routing rules a person can describe
  in their own words. D1-D9 are rulings recorded for the owner to overturn; the cache
  argument in D8 is the owner's, stated in the brainstorm and adopted.
- **Owner areas:** `dsl/models` (new), `dsl/providers`, `dsl/prompts` across
  namespaces, `component/memql` (the embedder binding, the fleet provider's new
  modalities), `component/worker` and `integrations/agent/worker` (the wire kinds),
  `component/grpc` (transcription), `integrations/knowledge`, `embedding`,
  `similarity`, `harnessrecall`, `component/fileprocessor`, `clients/os` (the catalog
  read in Fleet), the cockpit half in `memql-cockpit`.
- **Depends on:** epic 2 (levels, the seam, rules, the authoring compile paths). The
  cockpit half shares the wire change.

---

## 1. Problem

Every concrete provider record in `dsl/providers/providers.memql` is a paid OpenAI or
Anthropic model, and since vendor keys were removed none of them is reachable on a local
cluster. The registry default is gpt-5.4; if it is unavailable the fallback is whichever
available entry Go's map iteration yields first. The embedding model is pinned by name in
five files and the vector column is fixed at its 1,536 dimensions. Twelve of thirty-three
provider records are inert placeholders. No text-to-speech provider is declared. The
fleet wire carries two call kinds, chat and embedding, so vision, transcription, speech
and images have no local door. The cockpit's guided setup pulls `llama3.1:8b` and
`nomic-embed-text`, a floor chosen in August, while the Ollama library now serves models
that carry tools, thinking, structured output and vision in one record at 9B, and
Apple Silicon inference runs on MLX.

The special sauce is not the model. This harness asks narrow questions: structured
output at temperature zero, rules before models, catalog replay before either. Small
local models are good enough for most of the platform because the platform rarely asks
a model an open question. What is missing is the plumbing that lets a level resolve to
whatever the fleet can serve, and a catalog that says what a machine class should pull.

## 2. What the tree already has

- `dsl/providers/providers.memql`: four bases (`anthropic`, `openai`, `fleet`, `app`),
  thirty-three concrete records, two per vendor model (one streaming type, one not),
  cost figures per million on the chat records, `@modality` on the placeholders.
- `component/memql/fleet_provider.go`: `fleet:` names resolve at call time against the
  acting user's catalog; `FleetModel` carries `ContextWindow`, `StructuredOutput`,
  `Embeddings`, `Tools`, `Params`, `Quant`; `Dimensions()` deliberately returns 0.
- `component/worker/modelcall.go`: `ModelCallKindChat`, `ModelCallKindEmbedding`; the
  label grammar `model:<id>` with `ctx=,structured=,tools=,embeddings=,params=,quant=,max=`
  parsed by `integrations/agent/worker/model_routing.go`, every flag false and every
  number zero unless advertised.
- `v1:platform:fleetModel` (`dsl/platform/concepts.memql`): the virtual fleet-wide
  projection; `clients/os/src/apps/fleet/models/` reads it and `inferenceStatus`.
- `node_vectors` at `vector(1536)` (`component/memql/engine_ai.go`), `embedding_cache`
  keyed by text and provider with no expiry, `semantic_ai_cache` inert.
- `integrations/stt`: `openai_whisper.go` and `openai_realtime.go` outside the registry;
  `component/grpc/ai_transcribe_stream.go` the streaming flow; `component/fileprocessor/image.go`
  the one vision call; the TTS client with no record.
- The proving suite (`component/proving`) and its figure discipline; the cluster e2e
  harness (`test/clustere2e/`).
- From epic 2: `@level` on every prompt, the router seam, rules and policies, the
  authoring compile paths for `rule` and `policy`, decision records, the semantic cache
  primitive with per-namespace enablement.
- The cockpit's `memql worker setup --inference` with its default pair, the hardware
  floor verdict, `ModelPullStart / Progress / End`, and the `models.allow` policy.

## 3. Decisions

### D1 -- The catalog is seeded DSL data, organised by category

`dsl/models/concepts.memql` declares `modelProfile`: `id` (the runtime's own id, an
Ollama tag or `hf.co/<owner>/<repo>:<quant>`), `category` (closed: `text`, `reasoning`,
`omni`, `vision`, `audioIn`, `audioOut`, `imageGen`, `videoGen`, `embeddings`), `runtime`
(closed: `ollama`, `mlx`, `whispercpp`, `nemo`, `kokoro`, `mflux`, `comfyui`), `family`,
`params`, `quant`, `sizeBytes`, `memoryNeedBytes`, `contextWindow`, `flags`
(`structured`, `tools`, `thinking`, `vision`, `audioIn`, `audioOut`, `imageGen`,
`streaming`), `dimensions` for an embedder, `license`, `source` (a URL), `recommendedFor`
(levels), `minMachineClass` (`16`, `24`, `32`, `64`, `128`, in gigabytes of unified
memory or VRAM), `offeredOn` (`macos`, `linux`), `notes`, and `measured` (empty until
epic 4). `@rowAuthz(public, requiresIdentity)`; every write `@serverOnly` except an
owner-or-developer `modelProfileAdd` and `modelProfileRemove` for entries an operator
adds by id, marked `curated: false`. Streaming is a flag, never a category, and a
vendor model is one record, not two. Chosen over a list in Go (a model becomes a release)
and over an unbounded mirror of the Ollama library (the owner asked for the best few).

### D2 -- The curated set, by category, as of 2026-09-07

Seeded in `dsl/models/seeds.memql`, verified against the Ollama library pages and vendor
documentation on 2026-09-07. The implementing session re-verifies each id and size
before seeding, since tags move.

| Category | Entries | Notes |
|---|---|---|
| text | `qwen3.5:9b`, `gemma4:12b`, `qwen3.8:27b` | tools, thinking, structured, vision, 256K context; 9B is the 16 GB workhorse, 27B the 32 GB strong pick |
| reasoning | `gpt-oss:20b`, `qwen3.8:27b`, `gemma4:26b` | gpt-oss Apache 2.0, structured and tools, fits 16 GB at the edge; gemma4 26B is a 4B-active mixture |
| omni | `hf.co/Qwen/Qwen3-Omni-30B-A3B-Instruct` | runtime `mlx`, 32 GB, speech out; not in the Ollama library |
| vision | the text entries | built in; no extra model |
| audioIn | `gemma4:e4b`, `whisper-large-v3-turbo`, `parakeet-tdt-0.6b-v3`, `canary-qwen-2.5b` | gemma4 through Ollama's OpenAI-compatible endpoint; the others through `whispercpp` and `nemo` |
| audioOut | `kokoro-82m`, `fish-speech-1.5`, `dia-1.6b` | runtime `kokoro`; none served by Ollama |
| imageGen | `x/flux2-klein:4b`, `x/z-image-turbo`, `qwen-image-2.0` | the first two are Ollama's experimental macOS image generation; the third through `mflux` |
| videoGen | `wan2.2:5b`, `ltx-2.3-distilled`, `hunyuanvideo-1.5` | `offeredOn: linux`; listed so the catalog is honest, never a default |
| embeddings | `qwen3-embedding:0.6b`, `qwen3-embedding:4b`, `nomic-embed-text`, `bge-m3` | dimensions recorded per entry |

The recommended set per machine class, from `recommendedFor` and `minMachineClass`:
16 GB pulls `qwen3.5:9b` and `qwen3-embedding:0.6b`; 32 GB adds `qwen3.8:27b`; 64 GB
adds `qwen3.5:35b` and `gemma4:26b` and moves embeddings to the 4B. The cockpit's
`--inference` default pair becomes `qwen3.5:9b` and `qwen3-embedding:0.6b` (cockpit
half).

### D3 -- No paid default anywhere, and a gate that keeps it so

Every `@defaultProvider` that names a federated provider is removed; the prompt's
`@level` (epic 2) is its routing. The registry's `@default` provider, the
`MEMQL_DEFAULT_PROVIDER`, `MEMQL_DEFAULT_CHAT_PROVIDER`, `MEMQL_DEFAULT_STREAM_PROVIDER`,
`MEMQL_OPERATOR_AGENT_PROVIDER` and `MEMQL_DEFAULT_AGENT_PROVIDER` overrides, and the
`finalizeDefault` map-order fallback are deleted, pre-release, no shims: the shipped
`default` rule is the default. `TestNoPaidDefault` fails the build when a prompt's pin
names a federated provider, when the registry declares a `@default`, or when a Go source
file names a federated provider record by string literal outside `dsl/providers` and the
registry loader. The ELEVEN placeholder records are deleted; the eight vendor chat models
become one record each with `streaming: true` in their capability flags; the TTS client
gains a record when the cockpit's speech door exists, not before.

### D4 -- The fleet wire gains four kinds, and the labels four flags

`ModelCallKindVision` (chat messages with image parts), `ModelCallKindTranscribe`
(audio bytes in, text with timestamps out), `ModelCallKindSpeak` (text in, audio bytes
out) and `ModelCallKindImage` (prompt in, image bytes out) join `chat` and `embedding` on
`WorkerService.Stream`. The label grammar gains `vision=`, `audioin=`, `audioout=` and
`imagegen=`, false unless advertised, parsed in the one place the grammar lives. The
fleet provider implements the engine's vision, STT and TTS interfaces for machines that
advertise the flags, and `inferenceStatus` and the `ai` readiness lanes report the
modalities a door serves. The cockpit serves vision and transcription through Ollama's
OpenAI-compatible endpoint, speech through a Kokoro runtime and images through Ollama's
image generation where the platform offers it (cockpit half). Chosen over a second
transport per modality: the stream, the credential and the ledger are already there.

### D5 -- Transcription streams as windows

The streaming transcription flow keeps its `Start / Chunk / End` envelope and, on the
fleet door, transcribes accumulated audio in five-second windows, emitting one delta per
window and the full transcript at `End`. Chosen over building a realtime protocol on
the worker stream: hold-to-talk is the one caller, and a window is what a local
transcriber can serve.

### D6 -- The embedder is a cluster binding; the vector width belongs to the provider

`v1:platform:embedderBinding` at the literal id `active` carries `providerRef` (a
provider name or `fleet:<modelId>`), `dimensions`, `activatedAt` and `reembedRunId`. The
provider record declares `dimensions`; the fleet provider reads it off the model
profile. Vectors live in one table per width, `node_vectors_<dims>`, created by the
engine when a binding with a new width is activated. Switching the binding is an
owner-or-developer act that opens a system-origin work goal, `reembedLibrary`, which
re-embeds every row into the new table under the new binding and flips `active` when
the count matches; reads follow `active`, so a switch mid-way serves the old space
until the new one is complete. `embedding_cache` is keyed by text and binding and gains
an expiry. The five pinned literals go; every embedding site resolves `embedder:active`
through the router at level `embeddings`, and the shipped locked rule `embeddingsBound`
maps that level to the `embeddingsBinding` policy whose single entry is the selector.
Chosen over an untyped vector column (an index needs fixed dimensions) and over
re-embedding in place (a half-written column is a corrupt search space).

### D7 -- Described rules compile once, locally, into a rule the person confirms

A person types a sentence in the OS ("when generating images, always use the fleet",
"nothing that touches campaign recipients leaves the fleet"). The `compileRule` prompt,
level `strong`, takes the sentence, the closed `@when` vocabulary, the shipped policy
names, the catalog's categories and the cluster's concept ids, and answers a structured
rule (conditions, policy, precedence, on-unavailable, excludes) plus a one-sentence
restatement. The shipped locked rule `compilerLocalOnly` maps `prompt="compileRule"` to
the `localOnly` policy with `park`, so compiling a rule can never spend money to decide
how to spend money. The compiled form is shown beside the sentence and a simulation
(D8), the person confirms, and the authoring pipeline activates it through epic 2's
`rule` compile path with the sentence kept as the rule's `description`. Chosen over
evaluating the sentence per request. Compiling is deterministic in effect because it is
done once and pinned; a recompile is an explicit act offered when the vocabulary grows
or the sentence changes.

### D8 -- The cache is the hash of the sentence; invalidation is an event

The owner's argument, adopted: the sentence never changes at request time, so its hash
is the cache key and the compiled rule is the cached value. What this record adds is
that the cached value must be the selector, never the model, so it cannot go stale when
a machine sleeps, and that invalidation is an event (the sentence edited, the rule
vocabulary grown, the catalog's categories changed) rather than a clock, with a
recompile offered, never applied silently. The simulation replays the compiled rule
over the last two hundred decision records and shows what would have changed, which is
what makes a described rule trustworthy for somebody who cannot read DSL.

### D9 -- Free-form entries are classified rules-first, then locally, with a cached verdict

The Ask surface and an agent turn are the two places nobody declared what the request
is for. A small rules table (image and audio verbs, an attached file's kind, a chosen
tool) classifies first; on a miss the `classifyRequest` prompt at level `fast` answers a
category and a level, and the verdict is cached in the semantic cache under the
namespace `router.freeform.<ruleSetHash>`, so a rule edit invalidates every verdict.
For the other twenty-eight sites the prompt construct already says what the call is,
and no classifier runs. Chosen over classifying every request: a model in front of
every model call is the cost the principle exists to avoid.

## 4. The change

- `dsl/models/concepts.memql`, `seeds.memql`, `queries.memql`, `mutations.memql`,
  `builtins.memql`: the profile, the curated seeds, `modelProfiles` (by category,
  machine class, runtime), the operator add and remove.
- `dsl/providers/providers.memql`: one record per vendor model, placeholders deleted,
  `@default` removed, `dimensions` on embedders. `dsl/**/prompts.memql`: federated
  `@defaultProvider` pins removed.
- `component/memql/ai_providers.go`: the default resolution and env overrides deleted;
  `fleet_provider.go`: vision, transcribe, speak and image; `embedder_binding.go` (new);
  the per-width tables; `reembedLibrary` as a deterministic work template.
- `component/worker/modelcall.go`, `component/grpc/worker.proto`,
  `integrations/agent/worker/model_routing.go` and `fleet_inference.go`: the kinds and
  flags. `component/grpc/ai_transcribe_stream.go`: the window mode.
- `integrations/embedding`, `knowledge`, `similarity`, `harnessrecall`,
  `component/memql/ai_semantic_cache.go`: the binding through the router.
- `dsl/router/prompts.memql` (new): `compileRule`, `classifyRequest`;
  `dsl/rules/rules.memql`: `compilerLocalOnly`, `embeddingsBound`; the free-form rules
  table in `component/router/freeform.go`; the simulation in
  `component/router/simulate.go` over decision records; the `describedRuleCompile`,
  `describedRuleSimulate` and `describedRuleActivate` builtins, owner or developer.
- `clients/os/src/apps/fleet/models/`: the catalog read (categories, runtime, what a
  machine lacks). The full screens are epic 5.
- `test/clustere2e/` and `component/proving`: the zero-paid-calls scenario.
- Docs: `docs/public/operate/local-models.md` (the catalog, the recommended sets, the
  runtimes), `docs/public/operate/ai-routing.md` (described rules), the TimescaleDB
  compliance pack untouched.

## 5. Failure modes

- A catalog id that no longer exists in the Ollama library: the pull fails with the
  runtime's own error, the profile is marked `unavailable` on the row by the pull's end,
  and the Fleet page says so. Curation is a release-time act.
- A machine advertising `vision=1` for a model that cannot see: the call fails on the
  machine and the decision record names it; epic 4's probe is what makes the flag
  trustworthy.
- A re-embed goal interrupted: the old table serves until the run completes on resume;
  the binding never flips on a partial count.
- Two embedders of the same width: two bindings are still two tables, keyed by binding
  id, not by width alone.
- The compiler produces a rule the person did not mean: the simulation shows it before
  activation, and the compiled form is the contract, not the sentence.
- The free-form classifier misfiles a request: the level is at worst one step off, the
  decision record says which rule and verdict decided, and the person can add a
  described rule that overrides it.

## 6. Testing

- The seeds: every profile's category, runtime, flags and dimensions valid against the
  closed sets; a fixture test that every `recommendedFor` level has at least one entry
  per machine class from 16 GB up.
- `TestNoPaidDefault`, and the existing `TestNoVendorApiKeyEntryPoint` unaffected.
- The wire kinds: round-trip tests for each kind, the label parser's four new flags
  default false, the in-process hop test for a forwarded transcription window.
- The binding: activation creates the table, the re-embed goal fills it, `active` flips
  on a matched count and not before, reads follow `active`, the cache key includes the
  binding.
- The compiler: fixture sentences to expected structured rules, with the locked
  local-only rule asserted by name; the simulation over a fixture of decision records;
  activation refuses a shipped name.
- The free-form path: the rules table first, one classifier call on a miss, zero on a
  cache hit, invalidation on a rule edit.
- The scenario: a fresh cluster with one 32 GB machine serving every site with zero
  federation calls, asserted by decision records; its negative control is the same
  scenario with the machine absent, which must park.
- Screenshots of the catalog read in both modes.

## 7. Delivery

Three PRs, plus the cockpit half's epic.

- **PR 1, the catalog and no paid default.** Task 1: the `modelProfile` concept, seeds,
  reads and operator writes. Task 2: paid pins and env defaults removed, one record per
  vendor model, the placeholders deleted, `TestNoPaidDefault`. Task 3: the catalog read
  in the Fleet models surface.
- **PR 2, doors and the embedder.** Task 4: the four wire kinds, the flags, the fleet
  provider's modalities, image description and transcription through the router. Task 5:
  the embedder binding, per-width tables, the re-embed goal, every embedding site on the
  binding.
- **PR 3, described rules.** Task 6: `compileRule`, the locked rule, the structured
  activation through the pipeline, the sentence kept. Task 7: the simulation and the
  free-form classifier with its cache namespace. Task 8: the zero-paid-calls scenario
  and docs.

The picking-up session writes the plan from this record with `superpowers:writing-plans`
and deletes it in the epic's merge. The cockpit half (the default pair, vision and
transcription over the OpenAI-compatible endpoint, the speech and image runtimes'
advertisement) is `memql-cockpit`'s epic for this program and shares the proto change.

## 8. Out of scope

- Hardware inventory, machine classes computed on the engine, the post-pull probe,
  measured ranking, shared machines (epic 4).
- The Settings screens for rules and levels (epic 5).
- Video generation as a default anywhere.
- A realtime transcription protocol.

## 9. Facts to re-verify before starting

Every catalog id and size against the Ollama library; that `Dimensions()` still returns
0; the `node_vectors` migration and its width; that `finalizeDefault` still falls to map
order; the placeholder list in `ai_providers.go`; the `ModelCallStart` proto fields; the
semantic cache's namespace registry. All were read on 2026-09-07 at commit 907e385fb.

---

## 10. Corrections found while implementing (2026-09-07)

Section 9 asked for every catalog id and several tree facts to be re-verified
before starting. They were, and four came back different. Recorded here rather
than silently fixed above, because a record whose numbers are quietly corrected
stops being evidence of what was known when the decision was made.

1. **`parakeet-tdt-1.1b` does not exist.** NVIDIA's current Parakeet is
   `parakeet-tdt-0.6b-v3`. D2's audioIn row is corrected in place. This is the
   exact drift section 9 predicted, found by checking rather than by a failed
   pull three weeks from now.
2. **`gemma4:26b` is a 4B-active mixture**, not the 3.8B-active D2 says. Also
   corrected in place.
3. **Eleven placeholder records, not twelve.** `newAIProvider` routes nine
   modalities through `newOpenAIPlaceholderProvider` (stt, realtime, audio,
   image, video, computerUse, moderation, search, research), and eleven records
   carry one of them. MEASURED from the dispatch switch, not counted by eye.
4. **The pair collapse found a live disagreement, which is the argument for
   doing it.** `chat54Mini` and `stream54Mini` are two records for one model and
   declared different `maxCompletionTokens` -- 16384 and 4096. The survivor
   keeps 16384; 4096 appears on every stream record regardless of model, which
   is the signature of a boilerplate default rather than a measured value. The
   deleted record's own comment had warned about exactly this class of drift
   ("the two providers for one model disagreeing about what that model costs")
   without anyone noticing the pair had already drifted on a different field.

One thing section 9 asked about was confirmed rather than corrected:
`fleetProvider.Dimensions()` still returns 0, and `finalizeDefault` still fell
to map order. Both are addressed as the record specifies.

A fifth fact turned up that section 9 did not ask about: **the provider `params`
grammar accepted only strings and numbers**, so `streaming true` did not parse.
The parser gains a boolean arm (`component/language/parser/provider_decl.go`)
rather than the flag being spelled `"true"` as a string, so a typo is a parse
error instead of a silent false.

---

## 11. What shipped differently from this record

Four things landed other than as written, and each is here because the record
would otherwise promise something the tree does not do.

**One PR, not three.** Section 7 plans three. The owner's instruction was a
single PR for all eight tasks, and that is what shipped (#5194).

**`amortizedCost.providerCalls`, not a federation-call count.** Section 6 asks
for "zero federation calls, asserted by decision records". The proving suite's
own discipline refused it: a `federationCalls` metric's baseline figure reads
`notMeasurableOnReplay`, so nothing would have checked the instrument, and a
zero-claim with no working control is the shape that reads as a result and is
not one. The scenario and its negative control are re-pointed at
`amortizedCost.providerCalls`, which the CI tier does measure. The unmeasured
claim is recorded in `docs/public/operate/local-models.md` as a
`<!-- proving-pending: -->` marker -- the opposite marker, which fails the
build when the claim quietly becomes true.

**`embeddingsBound` REPLACED `embeddingsPark` rather than joining it.** Both
carry `@when(level="embeddings")`, so a new rule above the old one would have
left the old one matching nothing -- a rule that can never fire, in the file
whose own header forbids exactly that for policies.

**Consent falls back to `FederatedByStrength()` when no policy corpus loaded.**
Routing the one-shot human escape through the `federationStrongest` policy is
the reviewable answer and is what runs on a healthy cluster, but it made the
escape depend on the corpus loading -- so on a cluster whose corpus failed,
the person says yes and nothing happens. memql-2a raised it; the chain is now
policy FIRST, direct strongest-federated pick second, and
`TestConsentSurvivesWithNoPolicyCorpus` is the gate. It is not a default:
nothing reaches it without an explicit yes on the call in front of the person.

### Two literals this epic wrote that were wrong when written

Both found by landing on epic memql#5127, which owns the real lists.

`router.ShippedRuleNames` named five entries. One (`localFirst`) is a POLICY
and never was a rule, and four of the six rules the tree ships were missing --
so the gate that stops a compiled rule shadowing a shipped one was, for
`reasoningParks`, watching nothing. It, `ShippedPolicies` and `WhenKeys` are
deleted; `memql.ValidateRuleProposal` is the one validator both front doors
call, reading the live registries through `ShippedNames`.

`routingrules.Validate` declared `HasPolicy` and never called it. A rule could
name a policy nobody registered -- `ValidatePolicyEntry` checks the FORM of a
chain entry, which any identifier passes -- so the rule rendered, loaded, and
then refused EVERY CALL IT MATCHED at request time, on a door report the person
who wrote the rule is not reading. Closed at both doors.

The first attempt to share that validator had `component/router` importing
`component/routingrules`, which is a leaf module importing the root: a
dependency-direction violation `go build ./...` and `make test` both resolve
happily and only the `module-boundaries` lane catches.
`scripts/ci/module-boundaries.sh` now runs that lane's build+vet step locally.
