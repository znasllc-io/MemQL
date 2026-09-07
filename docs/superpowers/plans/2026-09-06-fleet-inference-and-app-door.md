# Fleet Inference and the App Door (Engine Half) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A fleet model can serve every kind of turn the engine makes, a signed-in Claude
Code or Codex on a fleet machine is an inference door the router can pick, and the default
chain is local-strongest, then a fleet app, then federation under the cost ceilings, with
work parking only when every door is shut.

**Architecture:** PR 1 lands the wire change (tool calling on the model-call family, a
follow-up control and a structured result on the app-session family, app descriptors and
model attributes on registration) and closes the four fleet gaps against it, so the
cockpit can build on a merged proto. PR 2 adds the `SubscriptionApp` provider type and the
`app` door, rewrites the shipped policy chain and re-homes the park onto the work spine,
and adds the MCP `submit` and `nextTask` tools. The cockpit half (`memql-cockpit`,
`docs/superpowers/plans/2026-09-06-fleet-inference-and-app-door.md` there) lands between
the two.

**Tech Stack:** Go 1.26 (`component/memql`, `component/router`, `component/worker`,
`integrations/agent/worker`, `component/mcp`), protobuf (`component/grpc/worker.proto`),
MemQL DSL (`dsl/providers`, `dsl/policies`, `dsl/worker`, `dsl/work`).

**Spec:** `docs/superpowers/specs/2026-09-06-fleet-inference-and-app-door-design.md`.
Read it first, then the local-models record (`2026-08-26-local-models-on-the-fleet-design.md`)
and the work-spine record's section D (`2026-09-05-work-spine-design.md`), which the park
re-homes onto.

**Closes:** the epic issue and its task issues, filed by Task 0.

---

## Global constraints

- **Verification is `make test`, never `go test ./...`.** Every touched module builds with
  `GOWORK=off go build ./...` inside it; every node tag builds (`for t in "" identity mcp
  agent planner workbench edge bff; do go build -tags "$t" . || exit 1; done`), because
  `integrations/agent/worker` is `//go:build agent` and a plain build never compiles it.
- **Proto first, in its own commit, fields only.** `component/grpc/worker.proto` changes
  are regenerated with `scripts/dev/proto-gen.sh`; new fields take the next free numbers
  (server-to-worker oneof: 20; worker-to-server oneof: 20) and nothing existing is
  renumbered. The cockpit reaches the proto through `.github/memql-pin`, so PR 1 must MERGE
  before the cockpit's PR 1 starts.
- **The label grammar is mirrored by hand in the cockpit** (`internal/worker/models/models.go`,
  pinned by `TestWireContract` there). Every attribute key this plan adds to
  `integrations/agent/worker/model_routing.go` is added in the cockpit plan's Task 1 with
  the same spelling; the engine's `TestModelAttributesRoundTripThroughTheLabelValue` and
  `component/worker/modelcall_test.go`'s `TestModelLabelRoundTrip` pin this side.
- **Park, never a silent paid call, is still the rule for a policy that pins a fleet
  model with no fallback.** The DEFAULT chain changes; `consentedCloudFallback` and
  `req.CloudConsent` stay for the pinned case.
- **The dollar ceiling excludes `subscription` and `local`; the loop caps include every
  call** (`component/memql/ai_guard.go`, `ai_guard_fleet.go`). A new billing class never
  moves those.
- **`app:` names one thing in two places** (spec D10): a test pins the provider reference
  set to `component/worker/apps.go`'s closed id set.
- **A new DSL construct fans out:** `go run ./cmd/memqllint dsl/`, `make sdk-gen` (gate
  `make sdk-gen-check`), `make arch-model` when `make arch-model-check` is stale;
  `test/dslconformance/local_first_policies_test.go` pins the local-first policies and
  changes with them.
- **Stage by explicit path; commit trailers `Co-Authored-By` and `Claude-Session`; no
  emojis; hostnames in docs are `example.com` or `<domain>`.**

---

## The shape of the epic

| Layer | Where | What it holds |
|---|---|---|
| Wire | `component/grpc/worker.proto`, `component/grpc/gen` | Tools on the model call; `message` control and `result` end on app sessions; app descriptors on `Register` |
| Envelopes | `component/worker/modelcall.go`, `component/worker/session.go`, `component/worker/apps.go` | The Go side of the new fields; the app descriptor |
| Fleet | `component/memql/fleet_provider.go`, `ai_providers.go`, `integrations/agent/worker/model_routing.go` | Tool calling, embeddings through the fleet, `fleet:*` ordering, attributes |
| Door | `component/memql/app_provider.go` (new), `dsl/providers/providers.memql`, `component/memql/fleet_catalog_read.go` | `SubscriptionApp`, `app:` references, the `app` door |
| Chain and park | `dsl/policies/policies.memql`, `component/router/router.go`, `component/router/fleet_refusal.go`, `integrations/work` | The three-step default, the ceiling on the federation hop, the work-spine approval |
| MCP | `component/mcp/tool_surface.go`, `server.go` | `submit`, `nextTask` |

---

## Task 0: file the epic and its tasks

**DONE on 2026-09-06:** epic #5096; tasks #5097, #5098, #5099 (PR 1), #5100, #5101, #5102 (PR 2), in the table's order; the cockpit half is znasllc-io/memql-cockpit#382. Do not file them again; verify with `gh issue list --repo znasllc-io/memql --label epic:fleet-inference-app-door`.

As epic 1's Task 0 (`docs/superpowers/plans/2026-09-06-configuration-readiness.md`), label
`epic:fleet-inference-app-door`; the epic issue in `memql` links the cockpit epic issue
filed by the cockpit plan's Task 0 and vice versa. Task issues (`task`, `epic:fleet-inference-app-door`,
`claude`, `engine`):

| Title | PR |
|---|---|
| Wire: tools on the model call, message and result on app sessions, app descriptors and model attributes on Register | 1 of 2 |
| Fleet: the structured fallthrough test and fix, embeddings through the fleet, tool calling on the fleet provider | 1 of 2 |
| Fleet: params and quant attributes, `fleet:*` strongest-first ordering, `modelPreference` | 1 of 2 |
| Door: the SubscriptionApp provider type, `app:` references, the `app` inference door | 2 of 2 |
| Chain: the three-step default policies, the ceiling on the federation hop, the park on the work spine | 2 of 2 |
| MCP: `submit` and `nextTask` for app-session bearers; the engine side of `message` and `result` | 2 of 2 |

---

# PR 1: the wire and the fleet

## Task 1: the proto

**Files:**
- Modify: `component/grpc/worker.proto`
- Regenerate: `component/grpc/gen/*` via `scripts/dev/proto-gen.sh`
- Modify: `component/worker/modelcall.go` (envelope types), `component/worker/session.go`
  (control action), `component/worker/apps.go` (descriptor), `component/worker/registry.go`
  (registration carries descriptors)
- Create: `component/worker/wire_readiness_test.go` (round trips)

**Interfaces (the proto additions, verbatim):**

```proto
// ModelCallStart gains tools; ModelCallMessage gains the tool fields.
message ModelCallTool { string name = 1; string description = 2; string parameters_json = 3; }
message ModelCallToolCall { string id = 1; string name = 2; string arguments_json = 3; }
// on ModelCallStart:            repeated ModelCallTool tools = 12;
// on ModelCallMessage:          string tool_call_id = 3; string name = 4; repeated ModelCallToolCall tool_calls = 5;
// on ModelCallDelta:            repeated ModelCallToolCall tool_calls = 3;   // incremental arguments allowed
// on ModelCallEnd:              repeated ModelCallToolCall tool_calls = 8;
// on AppSessionStart:           string response_schema_json = 13;
// on AppSessionControl:         string prompt = 5;   // action = "message"
// on AppSessionEnd:             string result_json = 7;
message AppDescriptor { string id = 1; string harness = 2; bool structured_result = 3; bool follow_ups = 4; }
// on Register (beside apps):    repeated AppDescriptor app_descriptors = <next free>;
```

Go: `ModelCallRequest.Tools []ToolDefinition`, `ModelCallMessage{ToolCallID, Name, ToolCalls}`,
`ModelCallOutcome.ToolCalls []ToolCall`, `RunSpec.ResponseSchema string`,
`ActionMessage = "message"` beside `ActionCancel`, `RunResult.Result json.RawMessage`,
`AppDescriptor{ID, Harness, StructuredResult, FollowUps}` with `Harness` in
`{"codex-app-server", "codex-mcp", "claude-headless"}`.

- [ ] **Step 1: Round-trip tests first** (`wire_readiness_test.go`): a `ModelCallStart`
  with two tools survives marshal/unmarshal with `parameters_json` intact; a message with
  a tool call; an end with two tool calls; a control `message` with a prompt; a
  `Register` with two descriptors; and a negative control that an unknown harness word
  is refused by `AppDescriptor.Validate()`.
- [ ] **Step 2: Edit the proto, regenerate, add the Go types, run**

Run: `scripts/dev/proto-gen.sh && go test -count=1 ./component/worker/ -v -run 'Wire|ModelLabel' && for t in "" agent; do go build -tags "$t" . || exit 1; done`

- [ ] **Step 3: Commit (fields only; nothing reads them yet)**

```bash
git add component/grpc/worker.proto component/grpc/gen component/worker/modelcall.go component/worker/session.go component/worker/apps.go component/worker/registry.go component/worker/wire_readiness_test.go
git commit -m "worker proto: tools on the model call, a follow-up and a result on app sessions, app descriptors"
```

## Task 2: the four gaps

**Files:**
- Create: `component/memql/fleet_structured_fallthrough_test.go` (the negative control first)
- Modify: `component/memql/ai_providers.go` (`isNonStreamingType` knows `fleet` and
  `subscriptionapp`; `EmbeddingProvider` resolves through `EntryForUser`)
- Modify: `component/memql/fleet_provider.go` (`CallChatWithTools`, `CallChatStreamWithTools`)
- Modify: `integrations/agent/worker/fleet_inference.go` (tools on the envelope; tool
  calls back), `model_routing.go` (`ModelAttributes.Tools bool`, key `tools`)
- Modify: `dsl/policies/policies.memql` (wire `localPlanner`, `localConductor`,
  `localSuggest`, `localEmbeddings` to their purposes or delete them; the conformance test
  `test/dslconformance/local_first_policies_test.go` follows)

- [ ] **Step 1: The negative control**

```go
// A structured call whose default provider is a fleet model, on a cluster with a
// registered cloud structured provider that MUST NOT be hit.
func TestStructuredCallWithFleetDefaultMakesNoCloudCall(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("a cloud provider was called at %s -- park-not-fallback violated", r.URL.Path)
	}))
	defer cloud.Close()
	e := newTestEngineWithProviders(t, cloudChatStructuredAt(cloud.URL), fleetProviderWithNoMachines())
	_, err := e.InvokeAIStructured(context.Background(), "aPromptWhoseDefaultIsFleet", map[string]any{}, schema)
	var fu *FleetUnavailable
	if !errors.As(err, &fu) {
		t.Fatalf("want a typed fleet refusal, got %v", err)
	}
}
```

Build the two fixtures from the harness `fleet_provider_test.go` already uses. Run it:
it FAILS today if the fallthrough is real (the cloud server is hit) and PASSES if it is
not; either answer is recorded in the report before the fix.

- [ ] **Step 2: The fixes, each with its test** — `isNonStreamingType` (`fleet`);
  `EmbeddingProvider` through `EntryForUser` (`TestEmbeddingProviderResolvesAFleetName`);
  `fleetProvider.CallChatWithTools` mapping `common.ToolDefinition` to `ModelCallTool` and
  `ModelCallOutcome.ToolCalls` back to `common.ToolCall`, with `Tools` in `Satisfies` so a
  runtime advertising `tools=0` is skipped for a tool turn (`TestToolTurnSkipsAMachineWithoutTools`);
  the policies wired (the planner prompt's `@defaultProvider`, the suggest domain, the
  embeddings default) and the conformance test updated.
- [ ] **Step 3: Run and commit**

Run: `go test -count=1 ./component/memql/ -run 'Fleet|Embedding|Structured' -v && go test -tags agent -count=1 ./integrations/agent/worker/ -run 'Model|Tool' -v && go run ./cmd/memqllint dsl/ && make test 2>&1 | tail -5`

```bash
git add component/memql/fleet_structured_fallthrough_test.go component/memql/ai_providers.go component/memql/fleet_provider.go \
  integrations/agent/worker/fleet_inference.go integrations/agent/worker/model_routing.go dsl/policies/policies.memql test/dslconformance/local_first_policies_test.go
git commit -m "fleet: tool calling, embeddings and structured calls all reach a fleet model, and nothing falls through to the cloud"
```

## Task 3: params, quant, strongest-first, `modelPreference`

**Files:**
- Modify: `integrations/agent/worker/model_routing.go` (`ModelAttributes.Params int64`,
  `.Quant string`; keys `params`, `quant`; parse and `String()`; `orderModels`)
- Modify: `component/memql/fleet_provider.go` (`FleetModel.Params`, `.Quant`;
  `fleetEntry` resolves `fleet:*` to the strongest eligible model)
- Modify: `component/memql/fleet_catalog_read.go` (`fleetModels` rows carry both; the
  reduction is MAX of `params`, the SET of `quant`)
- Modify: `dsl/worker/concepts.memql` (`routingPolicy.modelPreference []string`),
  `dsl/platform/concepts.memql` (`fleetModel.params`, `.quant`)
- Modify: `integrations/agent/worker/model_routing_test.go`, `component/worker/modelcall_test.go`

**Interfaces:**
- `func orderModels(models []FleetModel, preference []string) []FleetModel`: preference
  order first for the ids it names, then parameters descending, then context window
  descending, then model id; a model with `Params == 0` sorts after every model with a
  value. `IsFleetWildcard(name) bool` for `fleet:*`.

- [ ] **Step 1: Tests first** — round trip of the two new keys; `orderModels` on a
  fixture table (unknown size last; preference wins; ties by context then id);
  `fleetEntry("fleet:*")` picks the first of `orderModels` among eligible models and
  reports `no_local_model_available` naming the considered set when none is eligible.
- [ ] **Step 2: Implement, regenerate, run** — `make sdk-gen && make sdk-gen-check` (two
  concept fields), `go run ./cmd/memqllint dsl/`, the two package suites, `make test`.
- [ ] **Step 3: Commit, push, open PR 1**

```bash
git add integrations/agent/worker/model_routing.go integrations/agent/worker/model_routing_test.go component/worker/modelcall_test.go \
  component/memql/fleet_provider.go component/memql/fleet_catalog_read.go dsl/worker/concepts.memql dsl/platform/concepts.memql sdk/go/client sdk/ts/src/client
git commit -m "fleet: models advertise size and quantization; fleet:* picks the strongest; a policy may prefer"
git push -u origin epic/fleet-inference-app-door
```

PR 1 closes the three PR 1 task issues. After it merges, the cockpit plan's PR 1 bumps
its pin to this merge's sha.

---

# PR 2: the door, the chain, the park, the tools (branch from `main` after the cockpit's PR 2 merges)

## Task 4: the SubscriptionApp provider type and the `app` door

**Files:**
- Create: `component/memql/app_provider.go`, `app_provider_test.go`
- Modify: `dsl/providers/providers.memql` (`@base @type("SubscriptionApp") provider app { }`)
- Modify: `component/memql/ai_providers.go` (`newAIProvider` refuses static children of
  the type; `EntryForUser` resolves `app:` beside `fleet:`; `isNonStreamingType`)
- Modify: `component/memql/fleet_catalog_read.go` (`InferenceDoorApp = "app"`,
  `appEligible` from the runnable apps on machines this replica holds)
- Modify: `component/worker/apps.go` (a `ProviderReferences()` returning `app:<id>` for
  the closed set) and the test that pins it
- Modify: `component/router/types.go` (billing `subscription` already exists; the
  `ExecutionSurface` is `app:<id>@<registrationId>`)

**Interfaces:**
- `AppReferencePrefix = "app:"`, `IsAppReference(name) bool`, `appEntry(ctx, actingUserId,
  appId) *ProviderConfigEntry` whose `Client` is an `appProvider` implementing
  `common.ChatAIProvider` and `common.ChatStructuredProvider` and NOT the tool-calling
  surfaces; `appProvider.Call*` builds a `RunSpec{App, Prompt, ResponseSchema, Kind: run}`
  and runs it through `SessionRunner` (component/worker/runner.go), taking `RunResult.Result`
  as the structured answer and the transcript's final text otherwise; billing
  `subscription`; the loop guard admits it through `GuardLocalModelCall`'s sibling with
  cost zero.

- [ ] **Step 1: Tests first** — `IsAppReference`; `appEntry` unavailable when no machine
  runs the app (typed refusal naming the machines considered, `no_app_available`);
  `TestAppProviderReferencesMatchTheClosedSet`; a structured call round-trips through a
  fake `SessionRunner` returning `Result`; `inferenceStatus` with only an app runnable
  answers `doorsOpen == ["app"]`.
- [ ] **Step 2: Implement, regenerate, run, commit**

```bash
git add component/memql/app_provider.go component/memql/app_provider_test.go dsl/providers/providers.memql component/memql/ai_providers.go component/memql/fleet_catalog_read.go component/worker/apps.go component/worker/apps_test.go component/router/types.go sdk/go/client sdk/ts/src/client
git commit -m "providers: a signed-in Claude Code or Codex on a fleet machine is an inference door"
```

## Task 5: the three-step default, the ceiling on the federation hop, the park on the work spine

**Files:**
- Modify: `dsl/policies/policies.memql` (every shipped policy: `@primary("fleet:*")`,
  `@fallback("app:*")`, then the vendor entries; the "missing fallback is the feature"
  comment rewritten to say why the DEFAULT now falls through and where park still holds)
- Modify: `component/router/router.go` (`resolveChain`: an `app:*` wildcard resolves to
  the first runnable app in the owner's `delegationPolicy.appOrder`, else any; the
  federation hop consults the guard's cost ceiling BEFORE selecting a vendor entry and
  reports `ceiling_reached` as a refusal rather than a park)
- Modify: `component/router/fleet_refusal.go` (`EveryDoorShut` reason; the park writes a
  `v1:work:approval` of kind `inferenceUnavailable` on the run through
  `integrations/work`'s approval builder; `CloudApprovedMetricKey` and
  `FeedbackReasonNoLocalModel` deleted with their tests)
- Modify: `integrations/work` (the approval kind; resume on readiness change: subscribe
  to `graph.node.updated.v1:platform:moduleReadiness` for the `ai` module and retry the
  step)
- Modify: `docs/public/ai/llm-cost-control.md`, `docs/public/operate/local-models.md`

- [ ] **Step 1: Tests first** — router table tests: each door open alone, all shut
  (park), federation shut by the ceiling (refusal, not park), a pinned fleet policy with no
  fallback (park, unchanged); the approval row's fields; the resume on a readiness event.
- [ ] **Step 2: Implement, run, commit**

Run: `go test -count=1 ./component/router/ -v && go test -count=1 ./integrations/work/... -v && go run ./cmd/memqllint dsl/ && make test 2>&1 | tail -5`

```bash
git add dsl/policies/policies.memql test/dslconformance/local_first_policies_test.go component/router integrations/work docs/public/ai/llm-cost-control.md docs/public/operate/local-models.md
git commit -m "routing: local-strongest, then an app, then federation under the ceiling; park only when every door is shut"
```

## Task 6: `submit`, `nextTask`, and the engine side of `message` and `result`

**Files:**
- Modify: `component/mcp/tool_surface.go` (`submit` and `nextTask` in the meta-tool set,
  listed only for an `app_session` bearer), `component/mcp/server.go` (the session id from
  the `node_id` claim, prefix `app-session:`, class checked)
- Modify: `component/worker/runner.go` (`SessionRunner.Message(sessionId, prompt)`;
  `RunResult.Result` from `AppSessionEnd.result_json`; a `submit` received before the end
  becomes the result and ends the turn)
- Modify: `integrations/agent/worker/cockpitapp.go` (the executor passes
  `ResponseSchema` from the task's output contract and reads `Result`)
- Create: `component/mcp/app_session_tools_test.go`, `component/worker/runner_message_test.go`

**Interfaces:**
- MCP `submit` args `{ result: object }`; `nextTask` args `{}` answering `{ taskId,
  prompt, responseSchema }` or `{ idle: true }` for the non-default MCP-only participant
  (a `v1:work:step` waiting on an app door and claimed by this session's owner).
- `AppSessionControl{action: "message", prompt}` sent by `Message`; the cockpit answers
  with chunks and an end per turn.

- [ ] **Step 1: Tests first** — a `submit` with a browser bearer is refused; with an
  app-session bearer the session id is read from `node_id`; `nextTask` hands one step
  and marks it claimed; `Message` on a session with `FollowUps=false` in its descriptor is
  refused before the wire.
- [ ] **Step 2: Implement, run, commit, push, PR 2**

```bash
git add component/mcp/tool_surface.go component/mcp/server.go component/mcp/app_session_tools_test.go component/worker/runner.go component/worker/runner_message_test.go integrations/agent/worker/cockpitapp.go
git commit -m "app sessions: a submit tool, a next-task tool, and a follow-up turn"
git push -u origin epic/fleet-inference-app-door-2
```

PR 2 closes the three PR 2 task issues and the epic; delete this plan in it.

---

## Plan self-review

**Spec coverage.** D1 and D2 are the cockpit's; the engine side of them is Task 6. D3:
Task 4. D4: Task 5. D5: Task 3. D6: Task 2. D7: Task 6 and Task 1's fields. D8: Task 1's
descriptor and Task 4's pin. D9: Task 5. D10: Task 4's test. D11: Tasks 1 and 2. Section 6
tests are named in each task.

**Placeholders.** The proto field numbers marked `<next free>` on `Register` are read from
the file at execution time (the two oneofs' next free numbers, 20, are stated). The
approval kind's exact field set follows the work-spine record's section D, which the
implementer reads.

**Type consistency.** `ToolDefinition` / `ToolCall` are `core/common`'s existing types;
`ModelCallTool` / `ModelCallToolCall` are the wire twins added in Task 1 and mapped in
Task 2; `AppDescriptor.Harness` words are the same three strings the cockpit plan emits;
`RunResult.Result` is produced in Task 6 and consumed by Task 4's `appProvider`.
