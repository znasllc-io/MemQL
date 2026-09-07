# Open-weight defaults and the curated catalog -- Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every part of the platform that calls a model a good-enough open-weight default that runs on a current Apple Silicon machine through the fleet, chosen from a catalog organised by category with the best few entries per category, with no paid default anywhere, a local door for every modality, the embedder as a cluster binding, and custom routing rules a person can describe in their own words.

**Architecture:** The catalog is seeded DSL data (`dsl/models`), not a list in Go, so adding a model is a data change rather than a release. Paid defaults are deleted rather than deprecated -- pre-release, no shims -- and a build gate keeps them gone. The fleet wire gains four call kinds and four label flags rather than a second transport, because the stream, the credential and the ledger already exist. The embedder becomes a cluster binding whose vector width belongs to the provider, with one `node_vectors_<dims>` table per width and a re-embed goal that flips the binding only on a matched count. A described rule is compiled once, locally, into a rule the person confirms after a simulation over real decision records.

**Tech Stack:** Go 1.26.1, MemQL DSL, PostgreSQL 16 + TimescaleDB + pgvector, gRPC (protobuf), React + TypeScript + Vite (MemQL OS).

**Spec:** `docs/superpowers/specs/2026-09-07-open-weight-defaults-and-catalog-design.md`
**Program index:** `docs/superpowers/specs/2026-09-07-local-first-routing-program.md`

---

## Global Constraints

- **One PR for all eight issues** (#5138-#5145), on branch `epic/open-weight-defaults-and-catalog`, worktree `/home/znas/memql-projects/epic-open-weight-defaults-and-catalog`. The owner overrode the record's three-PR grouping in section 7 on 2026-09-07. The PR body carries one `Closes #n` line per issue -- `Closes #a, #b` links only the first.
- **This epic lands THIRD.** The agreed sequence is epic 1 (#5118, session memql-21) then epic 2 (#5127, session memql-2a) then this one. Build against origin/main; rebase onto main after epic 2 merges.
- **Levels are a closed set of exactly four:** `fast`, `strong`, `reasoning`, `embeddings`. `@level("<one>")` is REQUIRED on every prompt after epic 2. Every prompt this epic adds carries one.
- **Policy entry grammar (closed, epic 2's):** a bare provider name; `fleet:strongest` / `fleet:fastest` / `fleet:<modelId>`; `app:*` / `app:<id>`; `federation:cheapest` / `federation:strongest` / `federation:<providerName>`; `policy:<name>`; and, added by epic 2 for this epic, `embedder:active`. **`fleet:*` is retired** and refuses load.
- **Shipped policies after epic 2 are exactly three:** `localFirst`, `localOnly`, `federationStrongest`. This epic adds a fourth, `embeddingsBinding`, authored whole in this branch. `balancedChat`, `strongReasoning`, `cheapestCapable`, `fastCoding`, `backgroundExecution`, `backgroundEscalation` are DELETED by epic 2 -- name none of them.
- **`@precedence` must be unique across rules of the same locked-ness; a tie is a LOAD ERROR naming both files.** Epic 2's six locked rules use 0, 40, 50, 60, 100, 110. This epic takes **115** (`embeddingsBound`) and **120** (`compilerLocalOnly`). Both are mechanism rather than preference, which is why both sit above every shipped preference rule.
- **Every model call goes through the router.** Epic 2 adds a Go AST gate that fails the build on a call to `providers.Entry`, `ChatProvider`, `StructuredChatProvider`, `SuggestChatProvider`, `ChatStreamProvider`, `VisionProvider` or `EmbeddingProvider` outside `component/router` and the registry itself. Build the four new modality doors on the seam from the start.
- **Do not build around `ExplicitProvider` for embeddings.** memql-2a is adding `embedder:active` to the entry grammar with a typed refusal arm naming memql#5137 as the thing that installs the resolver. This epic owns the resolver. Passing the embedder as an explicit pin would make every embedding decision record read "explicit pin" forever, which is the story D6 exists to replace.
- **Pre-release discipline:** no backwards-compat shims, no deprecation windows, no fallback paths. Delete what is replaced (root CLAUDE.md, Branch Workflow 3).
- **Test command is `make test`,** never `go test ./...` -- the bare pattern misses `component/memql`, `component/database` and `component/language`. Db-gated trees need `MEMQL_REQUIRE_DB=1` and a real Postgres (`MEMQL_DATABASE_DSN=postgres://memql:memql_dev@localhost:15434/memql`); an open 5432 from k3d is not a database, and a skip is not a pass.
- **Stage files by explicit path** (`git add <file>`); never `git add -A` or `git add .` -- three sessions share this repo's checkout tree.
- **No emojis** anywhere: docs, code, CLI output, commit messages, UI copy. Use `[ ]`/`[x]`, "SUCCESS:", "ERROR:", "WARNING:", "INFO:".
- **Never claim MemQL is a database.** `TestNoDatabaseProductClaims` fails the build on it.
- **Wire strings, fixed with the cockpit session (memql-cockpit-ad) on 2026-09-07 and already implemented there:** kinds are exactly `"vision"`, `"transcribe"`, `"speak"`, `"image"`; label flags are exactly `vision`, `audioin`, `audioout`, `imagegen`, all lowercase, value `1`, and **false is ABSENT** (the key is omitted, never `=0`).
- **Proto field numbers are fixed and already implemented against.** Do not renumber: `ModelCallMessage.images = 6`; `ModelCallStart.audio = 13`, `.speech = 14`, `.image = 15`; `ModelCallDelta.segments = 6`, `.audio = 7`; `ModelCallEnd.segments = 9`, `.audio = 10`, `.images = 11`.
- **The engine does NOT validate `ModelCallAudio.media_type` against a closed set.** The container list belongs where the runtime knowledge is, which is the cockpit; it refuses an unrecognised media type by name rather than passing it through, because a runtime that decodes bytes as something they are not returns a confident transcript of noise.
- **Do not bump `CapabilityDescriptorSchemaVersion`.** Every existing cockpit registration fails validation on `unsupported schemaVersion`.
- **OS surfaces are judged at real size, in both light and dark, under the `frontend-design` process.** jsdom sees neither WebGL nor CSS custom properties; screenshots are the acceptance.

---

## File Structure

### New files

| Path | Responsibility |
|---|---|
| `dsl/models/concepts.memql` | The `modelProfile` concept: what a machine class should pull, by category. |
| `dsl/models/seeds.memql` | The curated set of D2, one `seed modelProfile` per entry. |
| `dsl/models/shapes.memql` | `modelProfileFull`, `modelProfileCard`. |
| `dsl/models/queries.memql` | `modelProfiles` (by category / runtime / machine class), `modelProfileById`. |
| `dsl/models/mutations.memql` | `modelProfileAdd`, `modelProfileRemove` (developer floor, `curated: false`). |
| `dsl/router/prompts.memql` | `compileRule` (level strong), `classifyRequest` (level fast). |
| `dsl/router/prompts/compileRule.tmpl`, `classifyRequest.tmpl` | The two templates. |
| `dsl/router/builtins.memql` | `describedRuleCompile`, `describedRuleSimulate`, `describedRuleActivate`. |
| `component/memql/embedder_binding.go` | The `v1:platform:embedderBinding` reader/activator, the per-width table DDL, and the `embedder:active` resolver. |
| `component/memql/fleet_modalities.go` | The fleet provider's vision / STT / TTS / image implementations. |
| `component/router/compile.go` | Sentence to structured rule, under the locked local-only rule. |
| `component/router/simulate.go` | Replay a compiled rule over the last 200 decision records. |
| `component/router/freeform.go` | The rules-first free-form classifier and its cache namespace. |
| `component/proving/scenarios/zeropaid/` | The zero-paid-calls scenario and its negative control. |
| `docs/public/operate/local-models.md` | The catalog, the recommended sets, the runtimes. |
| `clients/os/src/apps/fleet/models/catalog.ts` | Pure join of `modelProfiles` x `fleetModels` plus the "cannot serve, because" reasons. |
| `clients/os/src/apps/fleet/models/CatalogSection.tsx` | The catalog read in the Fleet models surface. |

### Modified files

| Path | Change |
|---|---|
| `dsl/providers/providers.memql` | One record per vendor model, `streaming` as a capability flag, twelve placeholders deleted, `@default` removed, `dimensions` on embedders. |
| `dsl/**/prompts.memql` (20 pins) | Every federated `@defaultProvider` removed, replaced by `@level`. |
| `dsl/rules/rules.memql` (epic 2's file) | Two shipped locked rules appended: `embeddingsBound` (115), `compilerLocalOnly` (120). |
| `dsl/policies/policies.memql` (epic 2's file) | One policy appended, authored whole: `embeddingsBinding`. |
| `dsl/platform/concepts.memql` | `embedderBinding` concept; `fleetModel` gains the four modality flags. |
| `component/memql/ai_providers.go` | `finalizeDefault`, `envDefaultProvider`, `VarDefaultStreamProvider`, `VarDefaultChatProvider` and the placeholder provider path deleted. |
| `component/memql/engine_ai.go` | The pinned `embedding3Small` default and the per-width vector table selection. |
| `component/memql/fleet_provider.go` | `Dimensions()` off the model profile; the four modality entry points. |
| `component/memql/ai_semantic_cache_registry.go` | `router.freeform.<ruleSetHash>` as the first compiled-in namespace. |
| `component/worker/modelcall.go` | Four kind constants; `IsValidModelCallKind`. |
| `component/grpc/worker.proto` | Five new messages, nine new fields. |
| `component/grpc/ai_transcribe_stream.go` | The five-second window mode on the fleet door. |
| `integrations/agent/worker/model_routing.go` | Four `ModelAttributes` fields, four parser cases, four `String()` emissions. |
| `integrations/agent/worker/fleet_inference.go` | Dispatch for the four new kinds. |
| `integrations/{embedding,knowledge,similarity,harnessrecall}` | The five pinned literals replaced by the binding through the router. |
| `component/fileprocessor/image.go` | Image description through the router at the vision modality. |
| `docs/public/operate/ai-routing.md` | Described rules. |

---

## Task 1: The `modelProfile` concept, the curated seeds, reads and operator writes (#5138)

**Files:**
- Create: `dsl/models/concepts.memql`, `seeds.memql`, `shapes.memql`, `queries.memql`, `mutations.memql`
- Create: `dsl/models/seeds_test.go`

**Interfaces:**
- Produces: concept id `v1:models:modelProfile`; query `modelProfiles(category, runtime, minMachineClass)`; query `modelProfileById(modelId)`; mutations `modelProfileAdd`, `modelProfileRemove`. Task 3 consumes `modelProfiles`; task 5 consumes `modelProfileById(...).dimensions`; task 4 consumes the `flags` object.

- [ ] **Step 1: Re-verify every catalog id and size against the Ollama library.** Section 9 of the record requires this and section 5 says a stale id is a failed pull with the runtime's own error. For each entry in D2's table, confirm the tag resolves and record its size. Do not seed an id you did not check today.

- [ ] **Step 2: Write `dsl/models/concepts.memql`.** Closed sets exactly as D1 spells them.

```memql
/// A curated model the catalog recommends, by category and machine class. The catalog says what a
/// machine SHOULD pull; it gates nothing. Eligibility is what a machine ADVERTISES on its
/// registration label, and a profile with no machine behind it is a recommendation, never a door.
///
/// TIER: public, requiresIdentity. Any signed-in person reads it -- the Fleet models surface has to
/// say what this fleet could run before anybody has pulled anything, and a catalog narrowed by owner
/// would show a new operator an empty page with no way to tell that from "nothing is recommended".
@rowAuthz(public, requiresIdentity)
@displayCard(primary="modelId", secondary="category", tertiary="runtime", status="curated")
concept modelProfile {
  modelId          string!  @description("The runtime's own id -- an Ollama tag (qwen3.5:9b) or hf.co/<owner>/<repo>:<quant>. Also the row id: there is one row per model and the model is what it is about. Byte-identical to what a machine advertises as `model:<id>`, so a catalog hit is a string equality and never a fuzzy match.")
  category         enum("text", "reasoning", "omni", "vision", "audioIn", "audioOut", "imageGen", "videoGen", "embeddings")!  @description("What the model is FOR. Streaming is deliberately absent: it is a flag on every category, and a category for it would put one model in two rows.")
  runtime          enum("ollama", "mlx", "whispercpp", "nemo", "kokoro", "mflux", "comfyui")!  @description("Which runtime serves it. A machine lacking the runtime cannot pull the model, which is one of the three reasons the Fleet page gives for a profile this fleet cannot serve.")
  family           string   @description("The model family (qwen3.5, gemma4, gpt-oss), for grouping on a page.")
  params           int      @description("Parameter count expanded to a number (9B -> 9000000000). Zero means not stated.")
  quant            string   @description("The quantization the recommended tag carries (Q4_K_M, F16). Operator-facing; nothing selects on it.")
  sizeBytes        int      @description("Download size of the recommended tag, as the library reported it on the day of seeding.")
  memoryNeedBytes  int      @description("Unified memory or VRAM the model needs at this quantization. Larger than sizeBytes -- weights plus the KV cache at the stated context window.")
  contextWindow    int      @description("Largest context window the model supports, in tokens.")
  flags            object   @description("Capabilities: {structured, tools, thinking, vision, audioIn, audioOut, imageGen, streaming}, each a bool. These are what the model CAN do; what a given machine SERVES is on its registration label, and the two are compared rather than conflated.")
  dimensions       int      @description("Vector width, for an embeddings entry. Zero for everything else. This is what makes the embedder binding's width a fact about the provider rather than a constant in the schema.")
  license          string   @description("The weights' licence (apache-2.0, gemma, qwen). Recorded because a person choosing what to run on their own hardware is entitled to know.")
  source           string   @description("URL of the library or vendor page the entry was verified against.")
  recommendedFor   []string @description("Levels this model is a good answer for: fast | strong | reasoning | embeddings. Empty means listed and recommended for nothing, which is how videoGen is honest.")
  minMachineClass  enum("16", "24", "32", "64", "128")!  @description("Gigabytes of unified memory or VRAM below which this model does not run usefully. A machine's recommended set is every profile whose class it meets.")
  offeredOn        []string @description("macos | linux. A videoGen entry is linux-only and says so, rather than being absent on a Mac and unexplained.")
  notes            string   @description("One sentence for an operator: what this entry is the best pick FOR.")
  measured         object   @description("Empty until epic 4. Measured throughput and quality with provenance; the catalog ranks nothing on it this release.")
  curated          bool     @default("true")  @description("False for an entry an operator added by id. A curated entry is a release-time act re-seeded on every boot; an operator entry is theirs and is not.")
  unavailable      bool     @default("false") @description("Set when a pull failed because the runtime no longer serves the id. Curation is a release-time act, so the row stays and says so rather than vanishing.")
}
```

- [ ] **Step 3: Confirm the concept loads.**

Run: `go test -count=1 ./component/memql/... -run TestDSLLoads`
Expected: PASS. If the loader reports an unresolved kind, suspect a reserved field name -- a reserved concept-field name drops the WHOLE concept and the error blames imports.

- [ ] **Step 4: Write the failing fixture test FIRST,** `dsl/models/seeds_test.go`. It is this task's acceptance and must fail before the seeds exist.

```go
// TestEveryLevelHasAnEntryPerMachineClass is the catalog's honesty gate: a
// level with no entry at a machine class is a person told "your machine can
// run this" by a page with nothing to name. It reads the LOADED corpus rather
// than the file, so a seed that parses and does not load fails here.
func TestEveryLevelHasAnEntryPerMachineClass(t *testing.T) {
	profiles := loadSeededProfiles(t)
	for _, level := range []string{"fast", "strong", "reasoning", "embeddings"} {
		for _, class := range []string{"16", "24", "32", "64", "128"} {
			if !anyProfileFor(profiles, level, class) {
				t.Errorf("no catalog entry recommended for level %q at machine class %s GB", level, class)
			}
		}
	}
}

// TestSeedsAreValidAgainstTheClosedSets fails on a category, runtime or machine
// class outside D1's closed sets, and on an embeddings entry with no dimensions
// -- the one field the embedder binding cannot work without.
func TestSeedsAreValidAgainstTheClosedSets(t *testing.T) { /* per-field assertions */ }

// TestStreamingIsAFlagAndNoCategory pins D1's ruling so a later entry cannot
// reintroduce the two-rows-per-model shape the vendor records are losing.
func TestStreamingIsAFlagAndNoCategory(t *testing.T) { /* assert no category == "streaming" */ }
```

- [ ] **Step 5: Run it and confirm it fails for the right reason.**

Run: `go test -count=1 ./dsl/models/... -run TestEveryLevelHasAnEntryPerMachineClass -v`
Expected: FAIL naming every (level, class) pair, because no seeds exist. A BUILD error is not the right reason -- fix the harness first, or the negative control is measuring the compiler.

- [ ] **Step 6: Write `dsl/models/seeds.memql`** with the set verified in step 1, following `dsl/rbac/seeds.memql`'s form. Cover D2's nine categories. `minMachineClass` and `recommendedFor` together must satisfy step 4's matrix: 16 GB gets `qwen3.5:9b` (fast, strong), `qwen3-embedding:0.6b` (embeddings) and a reasoning entry (`gpt-oss:20b`, which D2 says fits at the edge).

- [ ] **Step 7: Run the fixture test to green.**

Run: `go test -count=1 ./dsl/models/... -v`
Expected: PASS.

- [ ] **Step 8: Write `shapes.memql` and `queries.memql`.**

```memql
use models.concepts.{ modelProfile }
use models.shapes.{ modelProfileFull }

/// The catalog, optionally narrowed. Every argument is optional and absent means "do not narrow" --
/// the Fleet page asks for the whole catalog and groups it client-side, and a page that had to ask
/// nine times would show nine loading states for one answer.
@unbounded("the curated catalog -- a small bounded release-time set consumed whole")
@cache(300)
query modelProfile modelProfiles {
  args {
    category         enum("text", "reasoning", "omni", "vision", "audioIn", "audioOut", "imageGen", "videoGen", "embeddings")
    runtime          enum("ollama", "mlx", "whispercpp", "nemo", "kokoro", "mflux", "comfyui")
    minMachineClass  enum("16", "24", "32", "64", "128")
  }
  filter  when(args.category) { category==args.category } && when(args.runtime) { runtime==args.runtime } && when(args.minMachineClass) { minMachineClass==args.minMachineClass }
  shape   modelProfileFull
}
```

Note: `@unbounded` excludes sort and paginate -- do not add either.

- [ ] **Step 9: Write `mutations.memql`** -- `modelProfileAdd` and `modelProfileRemove`, both `@requiresRank("developer")` (owner at 400 outranks developer at 300, so the floor admits both), both stamping `curated: false`. `modelProfileRemove` REFUSES a `curated: true` row: a curated entry is re-seeded on the next boot, so removing one is a lie that undoes itself at the next restart.

- [ ] **Step 10: Add the refusal test.**

```go
// TestModelProfileRemoveRefusesACuratedEntry -- removing a seeded row is undone
// by the next boot's SeedMaterializer, so the mutation refuses rather than
// appearing to work until the pod restarts.
func TestModelProfileRemoveRefusesACuratedEntry(t *testing.T) { /* ... */ }
```

- [ ] **Step 11: Run the whole engine tree.**

Run: `make test 2>&1 | tail -30`
Expected: PASS. A new DSL construct fans out to five generated artifacts -- if memqllint, the SDK generators or the arch model red, regenerate rather than hand-edit.

- [ ] **Step 12: Commit.**

```bash
git add dsl/models
git commit -m "Issue #5138: dsl/models, the curated catalog as seeded data"
```

---

## Task 2: No paid default anywhere, and the gate that keeps it so (#5139)

**Files:**
- Modify: `dsl/providers/providers.memql`; the 20 `@defaultProvider` pins across `dsl/*/prompts.memql`
- Modify: `component/memql/ai_providers.go:483-506` (`finalizeDefault`), `:29,35,36`, `component/memql/engine_ai.go:495,544,569`, `integrations/agent/replier.go:313-316`
- Modify: `component/envregistry/manifest.yaml`, `component/envregistry/legacyalias.go:76,151`, `scripts/secrets/manifest.yaml`
- Create: `paid_default_gate_test.go` at the repo root, beside `vendor_api_key_gate_test.go`

**Interfaces:**
- Produces: a registry with no default. Tasks 5 and 7 rely on `finalizeDefault` being gone, so an unresolved level is a refusal rather than a silent map-order pick.

- [ ] **Step 1: Write `TestNoPaidDefault` FIRST, with its three fixtures.** Model it on `vendor_api_key_gate_test.go`, which walks `git ls-files` -- an untracked file is invisible to it, so the fixtures must be table-driven strings, not files on disk.

```go
// TestNoPaidDefault is the gate that keeps paid inference out of the default
// path. It fails on three things, each individually easy to add back and none
// of which would have failed anything else:
//
//  1. a prompt pinned to a federated provider with @defaultProvider,
//  2. the provider registry declaring a @default,
//  3. a federated provider record named by string literal in Go outside
//     dsl/providers and the registry loader.
//
// The third reads as the most pedantic and is the one that matters: a literal
// "chat54Mini" in an integration is a paid default wearing an integration's
// name, and it routes around every rule the router applies.
func TestNoPaidDefault(t *testing.T) { /* ... */ }

func TestNoPaidDefaultFailsOnAFederatedPin(t *testing.T)    { /* fixture 1 */ }
func TestNoPaidDefaultFailsOnARegistryDefault(t *testing.T) { /* fixture 2 */ }
func TestNoPaidDefaultFailsOnAGoStringLiteral(t *testing.T) { /* fixture 3 */ }
```

- [ ] **Step 2: Run it and confirm it fails on the real tree.**

Run: `go test -count=1 . -run TestNoPaidDefault -v`
Expected: FAIL, naming the 20 pins and the `embedding3Small` literals. That failure list IS the work list for steps 3-6.

- [ ] **Step 3: Delete the twelve placeholder provider records** from `dsl/providers/providers.memql` -- the `@modality` entries for computerUse, image (2), moderation, research (2), search, video (2) and the two stt records the fleet door replaces. Delete `newOpenAIPlaceholderProvider` and the modality constants left with no record. Keep `ModalityText`, `ModalityTTS`, `ModalitySTT`, `ModalityEmbedding`.

- [ ] **Step 4: Collapse the eight vendor chat models to one record each.** Each keeps its cost figures and gains `streaming: true` in its capability flags; the `stream*` twin goes. Add `dimensions` to the embedding records (1536 for `embedding3Small`, 3072 for `embedding3Large`) -- task 5 reads that instead of a constant.

- [ ] **Step 5: Remove the registry `@default` and all 20 federated `@defaultProvider` pins,** replacing each with the prompt's `@level`: `chat54Mini` and `chat54Nano` become `fast`, `streamClaudeSonnet` becomes `strong`, `streamClaudeOpus` becomes `reasoning`.

- [ ] **Step 6: Delete `finalizeDefault` and the five env overrides.** `ai_providers.go:483-506` goes wholesale with its call at `:957`, plus `envDefaultProvider`, `VarDefaultStreamProvider`, `VarDefaultChatProvider`, the `defaultPinned` field and its branches. In `replier.go:313-316` delete both `os.Getenv` reads. Remove the five names from both manifests and both legacy aliases.

- [ ] **Step 7: Run the gate to green, plus the env registry gate.**

Run: `go test -count=1 . -run 'TestNoPaidDefault|TestNoVendorApiKeyEntryPoint' -v && go test -count=1 ./component/envregistry/...`
Expected: PASS on both. `TestNoVendorApiKeyEntryPoint` must be unaffected -- if it moved, step 3 deleted more than placeholders.

- [ ] **Step 8: Run the whole tree.** This task touches the provider registry, which everything reads.

Run: `make test 2>&1 | tail -40`

- [ ] **Step 9: Commit.** `git commit -m "Issue #5139: no paid default anywhere, and a gate that keeps it so"`

---

## Task 3: The catalog read in the Fleet models surface (#5140)

**Files:**
- Create: `clients/os/src/apps/fleet/models/catalog.ts`, `catalog.test.ts`, `catalog.fixture.json`, `CatalogSection.tsx`
- Modify: `clients/os/src/apps/fleet/models/ModelsSection.tsx`, `useInference.ts`

**Interfaces:**
- Consumes: `modelProfiles` from task 1; the existing `fleetModels` live collection.
- Produces: `joinCatalog(profiles, fleetModels, machines): CatalogRow[]` where `CatalogRow = { profile, served: boolean, servedBy: string[], blocked: BlockedReason | null }` and `BlockedReason = { kind: "no-machine-of-class" | "runtime-missing" | "not-offered-on-platform"; detail: string }`.

- [ ] **Step 1: Read `clients/os/DESIGN.md`'s twelve interface rules and run the frontend-design process** before writing any component. The program record requires every OS surface in it to be judged at real size in both modes under `frontend-design`.

- [ ] **Step 2: Write `catalog.test.ts` FIRST.** The join is the whole feature and it is pure, so it is tested on fixtures with no DOM.

```ts
describe("joinCatalog", () => {
  it("marks a profile served when a fleet model matches its id exactly", () => {});
  it("does not fuzzy-match: qwen3.5:9b and qwen3.5:9b-q4 are different models", () => {});
  it("blocks with no-machine-of-class when every machine is below minMachineClass", () => {});
  it("blocks with runtime-missing when no machine reports the profile's runtime", () => {});
  it("blocks with not-offered-on-platform for a linux-only entry on an all-macos fleet", () => {});
  it("reports a fleet model with no catalog hit rather than hiding it", () => {});
});
```

- [ ] **Step 3: Run it and confirm it fails.**

Run: `cd clients/os && npx vitest run src/apps/fleet/models/catalog.test.ts`
Expected: FAIL, "joinCatalog is not defined". Run it from inside `clients/os` in the same command -- vitest from the repo root runs the OS tests without the OS setup, and every failure path then says `clients/os/`, which reads as a real failure and is not one.

- [ ] **Step 4: Implement `catalog.ts`.** Exact id equality, never a prefix or fuzzy match -- the record makes the model id byte-identical from cockpit to policy for exactly this reason.

- [ ] **Step 5: Run to green.** Same command; expected PASS.

- [ ] **Step 6: Build `CatalogSection.tsx`.** Two required modes: a fleet model WITH a catalog hit (category, runtime, flags, what it is good for) and one WITHOUT (listed, honestly, as an entry the catalog does not know). A profile this fleet cannot serve says which of the three reasons in a sentence, not an icon. Follow `ModelsSection.tsx`'s existing plate/row idiom rather than inventing a second one. `position: fixed` breaks inside an OS desk plate -- the transformed plate is the containing block.

- [ ] **Step 7: Typecheck and build.** A broken `index.css` passes typecheck and fails only the real build.

Run: `cd clients/os && npm run typecheck && cd .. && make os-build`

- [ ] **Step 8: Screenshot both modes at real size in a real browser.** jsdom sees neither WebGL nor CSS custom properties, so a green test is not evidence about pixels. Capture catalog-hit and no-hit, light and dark. Attach to the PR.

- [ ] **Step 9: Commit.** `git commit -m "Issue #5140: the catalog read in the Fleet models surface"`

---

## Task 4: Four fleet wire kinds and their flags (#5141)

**Files:**
- Modify: `component/grpc/worker.proto`, `component/worker/modelcall.go`, `integrations/agent/worker/model_routing.go`, `fleet_inference.go`, `component/memql/fleet_provider.go`, `component/grpc/ai_transcribe_stream.go`, `component/fileprocessor/image.go`, `dsl/platform/concepts.memql`
- Create: `component/memql/fleet_modalities.go`, `integrations/agent/worker/forward_transcribe_hop_test.go`

**Interfaces:**
- Produces: `worker.ModelCallKindVision|Transcribe|Speak|Image`; `ModelAttributes.{Vision,AudioIn,AudioOut,ImageGen} bool`.

- [ ] **Step 1: Write the label parser test FIRST.** The direction of the default is the whole point.

```go
// TestNewModalityFlagsDefaultFalse -- an unadvertised modality must cost
// eligibility, never grant it. A machine that says nothing about vision is a
// machine that cannot see, and routing a vision turn to it fails on somebody
// else's laptop with an error naming nothing.
func TestNewModalityFlagsDefaultFalse(t *testing.T) {
	a := ParseModelAttributes("ctx=131072,structured=1,tools=1")
	if a.Vision || a.AudioIn || a.AudioOut || a.ImageGen {
		t.Fatalf("unadvertised modality read as true: %+v", a)
	}
}

// TestModalityFlagsParseOrderIndependently pins the cockpit contract: it
// appends its four after `tools` and requires byte-identical labels for an
// unchanged inventory, so the engine must never depend on position.
func TestModalityFlagsParseOrderIndependently(t *testing.T) { /* ... */ }
```

- [ ] **Step 2: Run and confirm it fails to compile** (`a.Vision` undefined). That is the right failure here.

- [ ] **Step 3: Add the four fields, four parser cases and four `String()` emissions,** after `tools`, in the order vision, audioin, audioout, imagegen -- this exact order is what the cockpit implemented against. Reuse `parseAdvertisedBool` unchanged.

- [ ] **Step 4: Run to green.** `go test -count=1 ./integrations/agent/worker/... -run TestModality -v`

- [ ] **Step 5: Add the four kind constants and extend `IsValidModelCallKind`** in `component/worker/modelcall.go:32-89`. Strings exactly `"vision"`, `"transcribe"`, `"speak"`, `"image"`.

- [ ] **Step 6: Write the proto.** Five new messages, at the fixed field numbers.

```proto
message ModelCallImage {
  bytes data = 1;
  string media_type = 2;
}
message ModelCallAudio {
  bytes data = 1;
  string media_type = 2;
  int32 sample_rate_hz = 3;
}
message ModelCallTranscriptSegment {
  double start_seconds = 1;
  double end_seconds = 2;
  string text = 3;
}
message ModelCallSpeech {
  string voice = 1;
  string format = 2;
  double speed = 3;
  bool speed_set = 4;
}
message ModelCallImageRequest {
  int32 width = 1;
  int32 height = 2;
  int32 count = 3;
  string format = 4;
}
```

Then `ModelCallMessage.images = 6`; `ModelCallStart.audio = 13`, `.speech = 14`, `.image = 15`; `ModelCallDelta.segments = 6`, `.audio = 7`; `ModelCallEnd.segments = 9`, `.audio = 10`, `.images = 11`. Document on End that a worker which streamed leaves the field empty -- the rule `content` already follows, so bytes on Delta and bytes on End stay one code path rather than two. A speak call's text and an image call's prompt ride `messages` as an ordinary role="user" turn; the knob messages carry only knobs.

- [ ] **Step 7: Regenerate and build.**

Run: `scripts/dev/proto-gen.sh && go build ./component/grpc/...`

- [ ] **Step 8: Write one round-trip test per kind,** then the fleet provider's four entry points in `component/memql/fleet_modalities.go`, each gated on the advertised flag. `Dimensions()` stays at 0 here -- task 5 owns it.

- [ ] **Step 9: Write the in-process hop test for a forwarded transcription window.** Model it on `integrations/agent/worker/forward_hop_test.go`: wire the real router to the real handler in one process. A `clustere2e` lane is skipped on every CI lane and every developer machine, and a gate skipped by default cannot be what stands between a feature and the bug it prevents.

- [ ] **Step 10: Implement the five-second window mode** in `ai_transcribe_stream.go`, on the fleet door only. One delta per window (content = the window's text, segments = its timings), the full transcript on End with End.segments authoritative.

- [ ] **Step 11: Route image description through the router** at the vision modality in `component/fileprocessor/image.go`. Build it on the seam -- epic 2's AST gate fails the build on the direct `VisionProvider` call there today.

- [ ] **Step 12: Add the four modality flags to `fleetModel`** in `dsl/platform/concepts.memql:348`, as a union across machines exactly like `tools` and `structuredOutput`.

- [ ] **Step 13: Report the modalities in BOTH readings, which are no longer one reading.** After epic 1, `inferenceStatus` (per-caller live presence, `component/memql/fleet_catalog_read.go`) and the `ai` readiness lanes (cluster-wide configuration, `component/memql/readiness/inference.go`) are two independent answers to two different questions, and the acceptance names both. On the lanes, add each modality as another `SlotReport` on the existing `local` / `app` / `federation` lanes -- **not** a fourth lane and not a field beside `slots` -- with `Optional: true`, because `evaluateLane`'s rule is that an optional slot is reported and never decides completeness, and a local door that serves no images is still a door. If you add a fold fixture under `component/memql/readiness/testdata/fold/`, bump the minimum-file count in BOTH `component/memql/readiness/fold_test.go` and `clients/os/test/system/readinessFold.test.ts`.

  Constraint from epic 1: keep `attrContext = "ctx"` and `attrStructured = "structured"` as plain string literals in a const block. `component/memql/readiness/worker_contract_parity_test.go` reads `model_routing.go` by regexp (a real import is a module cycle -- `component/worker/go.mod` requires `component/memql`) and fails the build if either stops being a literal. Moving them behind a helper reds a lane in another epic's tree.

- [ ] **Step 14: Run the tree, then the tagged trees.** Tagged tests are invisible to `make test`, and most of this task lives behind the agent tag.

Run: `make test 2>&1 | tail -40 && go test -tags agent -count=1 ./integrations/agent/...`

- [ ] **Step 15: Commit.** `git commit -m "Issue #5141: four fleet wire kinds and their flags"`

---

## Task 5: The embedder binding (#5142)

**Files:**
- Create: `component/memql/embedder_binding.go`, `embedder_binding_test.go`
- Create: `component/database/memory-nodes/migrations/<ts>_embedder_binding.up.sql` / `.down.sql`
- Modify: `component/memql/engine_ai.go:78-87`, `ai_semantic_cache.go`, `fleet_provider.go:776`, `dsl/platform/concepts.memql`, `dsl/rules/rules.memql`, `dsl/policies/policies.memql`
- Modify: `integrations/embedding/embedding.go:117,160,376`, `integrations/knowledge/capabilities.go:230`, `integrations/similarity/capabilities.go:54`, `integrations/harnessrecall/recall.go:70`
- Create: the `reembedLibrary` deterministic work template under `dsl/work/`

**Interfaces:**
- Consumes: `modelProfileById(modelId).dimensions` from task 1; epic 2's `embedder:active` entry spelling and its typed refusal arm.
- Produces: `EmbedderBinding{ProviderRef string; Dimensions int; ActivatedAt time.Time; ReembedRunId string}`; `ActiveBinding(ctx) (EmbedderBinding, error)`; `VectorTableFor(dims int) string` returning `node_vectors_<dims>`; the resolver registered against epic 2's `embedder:active` arm.

- [ ] **Step 1: Write the binding tests FIRST.** The record's section 6 names five properties; the third protects the search space.

```go
func TestActivationCreatesTheWidthTable(t *testing.T)           { /* node_vectors_768 exists after activating a 768-wide binding */ }
func TestActiveFlipsOnlyOnAMatchedCount(t *testing.T)           { /* a partial re-embed leaves `active` on the old binding */ }
func TestReadsFollowActiveMidSwitch(t *testing.T)               { /* a query during a re-embed reads the OLD table */ }
func TestCacheKeyIncludesTheBinding(t *testing.T)               { /* two bindings, same text, two cache rows */ }
func TestTwoEmbeddersOfTheSameWidthAreTwoBindings(t *testing.T) { /* keyed by binding id, not width alone */ }
```

- [ ] **Step 2: Run and confirm they fail.**

Run: `MEMQL_REQUIRE_DB=1 MEMQL_DATABASE_DSN=postgres://memql:memql_dev@localhost:15434/memql go test -count=1 ./component/memql/... -run TestActiveFlips -v`
Expected: FAIL. A SKIP is not a failure -- if they skip, the DSN is wrong and the run measured nothing.

- [ ] **Step 3: Declare `embedderBinding`** on `dsl/platform/concepts.memql` at the literal id `active`, `@rowAuthz(clusterOwner)`, every mutation `@serverOnly`. A literal id makes a re-write a new VERSION of one logical row rather than a duplicate.

- [ ] **Step 4: Implement `embedder_binding.go`** -- read `active`, create `node_vectors_<dims>` on activation with the HNSW index the existing `20260325000002_vector_tables.up.sql` builds, and `VectorTableFor`. An untyped vector column was rejected in D6 because an index needs fixed dimensions.

- [ ] **Step 5: Register the `embedder:active` resolver** against the arm epic 2 ships. The decision record must then read `rule=embeddingsBound, policy=embeddingsBinding, door=local|federation` -- that story is the reason this is not an explicit pin.

- [ ] **Step 6: Implement `reembedLibrary`** as a deterministic work-spine template: re-embed every row into the new table under the new binding, flip `active` when the count matches. Interrupted, the old table still serves and resume continues on the same run id -- the automation journal already gives you that.

- [ ] **Step 7: Replace the five pinned literals.** `engine_ai.go:87`, `embedding.go` (3 sites), `knowledge/capabilities.go:230`, `similarity/capabilities.go:54`, `harnessrecall/recall.go:70`. Each resolves the binding through the router at level `embeddings`.

- [ ] **Step 8: Author the policy and the locked rule,** in epic 2's exact syntax. The rule body MUST be empty.

```memql
/// The embedder is a cluster BINDING, not a provider name, and this is the rule that makes an
/// embedding call resolve to whatever the binding says. Precedence 115 sits above every shipped
/// preference rule because this is mechanism rather than preference: a lower-precedence generic rule
/// winning here would resolve an embedding call to a chat model, which fails as a dimension mismatch
/// three layers away. The way to change the embedder is to switch the BINDING, not to outrank this.
@when(level="embeddings")
@policy("embeddingsBinding")
@precedence(115)
@onUnavailable("park")
@locked
rule embeddingsBound { }
```

Append `embeddingsBinding` to `dsl/policies/policies.memql` whole, with a header sentence in the shape of the one exempting `localOnly` from "a policy nothing can name is a decoration" -- its single entry is `embedder:active`, and a later reader must be able to see why it is not a decoration.

- [ ] **Step 9: Key `embedding_cache` by text AND binding, with an expiry.** The migration is additive; old rows keyed by provider are not readable under the new key, which is correct -- a vector from another embedder is not a cache hit, it is a wrong answer.

- [ ] **Step 10: Point `fleetProvider.Dimensions()` at the model profile** rather than returning 0.

- [ ] **Step 11: Run to green, then the db-gated trees the way CI does.**

Run: `MEMQL_REQUIRE_DB=1 MEMQL_DATABASE_DSN=postgres://memql:memql_dev@localhost:15434/memql go test -count=1 ./component/memql/... 2>&1 | tail -30`
A 10.0s failure is pgdriver's ReadTimeout under load from a peer session, not your bug -- the round number is the tell.

- [ ] **Step 12: Confirm no literal survives.** `grep -rn "embedding3Small" --include=*.go . | grep -v dsl/providers` must be empty. That is the task's own acceptance.

- [ ] **Step 13: Commit.** `git commit -m "Issue #5142: the embedder as a cluster binding"`

---

## Task 6: Described rules -- compile, confirm, activate (#5143)

**Files:**
- Create: `dsl/router/prompts.memql`, `dsl/router/prompts/compileRule.tmpl`, `dsl/router/builtins.memql`, `component/router/compile.go`, `compile_test.go`
- Modify: `dsl/rules/rules.memql`

**Interfaces:**
- Consumes: epic 2's `rule` construct and its authoring compile path; the `localOnly` policy.
- Produces: `CompileDescribedRule(ctx, sentence string) (CompiledRule, error)` where `CompiledRule = { Conditions, Policy, Precedence, OnUnavailable, Excludes, Restatement }`; builtins `describedRuleCompile`, `describedRuleActivate`.

- [ ] **Step 1: Write the fixture-sentence tests FIRST,** and the assertion that gives the feature its integrity.

```go
// TestCompilerNeverResolvesToAFederationDoor is the point of the locked rule:
// compiling a rule about how to spend money must not spend money to decide.
// Asserted on the DECISION RECORD rather than on the resolver's return, so it
// covers every path that reaches a provider, not only the one the test calls.
func TestCompilerNeverResolvesToAFederationDoor(t *testing.T) { /* ... */ }

func TestFixtureSentencesCompileToExpectedRules(t *testing.T) {
	// "when generating images, always use the fleet"
	// "nothing that touches campaign recipients leaves the fleet"
}

// TestActivationRefusesAShippedName -- a custom rule may not carry @locked or
// redefine localFirst / localOnly / federationStrongest.
func TestActivationRefusesAShippedName(t *testing.T) { /* ... */ }
```

- [ ] **Step 2: Run and confirm they fail** for the right reason.

- [ ] **Step 3: Write `compileRule` in `dsl/router/prompts.memql`** with `@level("strong")` -- required on every prompt after epic 2. The template takes the sentence, the closed seven-key `@when` vocabulary (`level`, `modality`, `prompt`, `role`, `actorRole`, `tag`, `touches`), the three shipped policy names, the catalog's nine categories and the cluster's concept ids, and answers a structured rule plus a one-sentence restatement.

- [ ] **Step 4: Author `compilerLocalOnly`** in `dsl/rules/rules.memql`.

```memql
/// Compiling a rule about how to spend money must never spend money to decide. Precedence 120 is the
/// highest shipped value for that reason: any rule that outranked this one could send the compiler
/// itself to a paid door, which is the exact outcome the described-rule feature exists to let a
/// person prevent. park rather than degrade -- a compile with no local door waits, because a
/// silently federated compile is worse than a slow one.
@when(prompt="compileRule")
@policy("localOnly")
@precedence(120)
@onUnavailable("park")
@locked
rule compilerLocalOnly { }
```

- [ ] **Step 5: Implement `component/router/compile.go`** on the structured-output path (`ChatStructuredProvider.CallChatStructured`) through the router seam.

- [ ] **Step 6: Implement `describedRuleCompile` and `describedRuleActivate`,** both floored at developer. Activation goes through epic 2's `rule` compile path with the sentence kept as the rule's `description` -- the compiled form is the contract, the sentence is the record of what was meant.

- [ ] **Step 7: Run to green, then `make test`, then `go run ./cmd/memqllint`** -- it catches a precedence tie immediately, and a tie is a load error naming both files.

- [ ] **Step 8: Commit.** `git commit -m "Issue #5143: described rules, compiled once and locally"`

---

## Task 7: Simulation and the free-form classifier (#5144)

**Files:**
- Create: `component/router/simulate.go`, `simulate_test.go`, `freeform.go`, `freeform_test.go`, `decisions.fixture.json`
- Modify: `component/memql/ai_semantic_cache_registry.go:64`, `dsl/router/prompts.memql`, `dsl/router/builtins.memql`, `clients/os/src/ask/`

**Interfaces:**
- Consumes: task 6's `CompiledRule`; `v1:router:call` rows.
- Produces: `Simulate(ctx, rule CompiledRule, records []CallRecord) SimulationResult`; `ClassifyFreeForm(ctx, req Request) (Classification, bool)` where the bool is "the rules table answered".

- [ ] **Step 1: Write the classifier tests FIRST.** The counting assertions ARE the feature: a model in front of every model call is exactly the cost the principle exists to avoid.

```go
func TestRulesTableAnswersFirstAndCallsNoModel(t *testing.T) { /* zero provider calls */ }
func TestOneClassifierCallOnAMiss(t *testing.T)              { /* exactly one */ }
func TestZeroClassifierCallsOnACacheHit(t *testing.T)        { /* exactly zero */ }
func TestARuleEditInvalidatesEveryVerdict(t *testing.T)      { /* the namespace carries ruleSetHash */ }
```

- [ ] **Step 2: Run and confirm they fail.**

- [ ] **Step 3: Implement the rules table** in `freeform.go` -- image and audio verbs, an attached file's kind, a chosen tool. Order inside the table is load-bearing; write it as a table, not a chain of ifs.

- [ ] **Step 4: Add `classifyRequest` at `@level("fast")`** and register `router.freeform.<ruleSetHash>` as the first compiled-in semantic-cache namespace in `defaultSemanticNamespaces()`, which returns an empty map today. Assert its threshold and TTL by value -- the record's acceptance names them.

- [ ] **Step 5: Implement `simulate.go`** over the last 200 `v1:router:call` rows, reporting what would have changed. This is what makes a described rule trustworthy for somebody who cannot read DSL.

- [ ] **Step 6: Carry the classification on the Ask request** in `clients/os/src/ask/`.

- [ ] **Step 7: Run to green, then `make test`, then `cd clients/os && npx vitest run`.**

- [ ] **Step 8: Commit.** `git commit -m "Issue #5144: simulation over decision records, and the free-form classifier"`

---

## Task 8: The zero-paid-calls scenario and the docs (#5145)

**Files:**
- Create: `component/proving/scenarios/zeropaid/scenario.go`, `control.go`
- Create: `docs/public/operate/local-models.md`
- Modify: `docs/public/operate/ai-routing.md`

- [ ] **Step 1: Write the negative control FIRST and make it fail.** Every zero-claim in this suite is paired with a control that must produce a non-zero, and a control reading zero FAILS the suite. Here the control is the same scenario with the machine absent, which must PARK.

- [ ] **Step 2: Write the scenario:** a fresh cluster, one 32 GB machine, every call site served, zero federation calls, asserted by decision records rather than by a counter that never rises on any path.

- [ ] **Step 3: Run both.** The control must read non-zero; the scenario must read zero federation calls.

Run: `go test -count=1 ./component/proving/...`

- [ ] **Step 4: Write `docs/public/operate/local-models.md`** -- the catalog, the recommended set per machine class, the runtimes. Every published numeric claim carries a `<!-- proving: metric=... value=... -->` marker or the claims gate fails the build.

- [ ] **Step 5: Update `docs/public/operate/ai-routing.md`** for described rules.

- [ ] **Step 6: Run the docs gates.** A new doc reds two unrelated gates -- docs front-matter and the area graph.

Run: `go test -count=1 . 2>&1 | tail -20 && make arch-model-check`

- [ ] **Step 7: Commit.** `git commit -m "Issue #5145: the zero-paid-calls scenario and the docs"`

---

## Landing

- [ ] Delete this plan file in the epic's merge -- the record is kept and cited, the plan is not.
- [ ] Wait for epic 1 then epic 2 to merge. Ping memql-2a before enqueuing; whoever lands second goes BEHIND and eats a ~25-minute CI restart on `gh pr update-branch`.
- [ ] Rebase onto main. Reconcile `dsl/rules/rules.memql` and `dsl/policies/policies.memql` as take-both, then re-run `go run ./cmd/memqllint` for a precedence tie.
- [ ] Open ONE PR with eight `Closes #n` lines, one per issue, plus the screenshots from task 3.
- [ ] `scripts/dev/merge-as-owner.sh --pr=<n> --check`, then merge. The bypass merges at once -- no queue, no 20-minute wait.
- [ ] Close epic #5137, remove the worktree and delete the branch.
