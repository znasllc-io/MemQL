# The machine scanner, measured capability, and shared machines -- Design

- **Date:** 2026-09-07
- **Status:** approved in the 2026-09-07 brainstorm as epic 4 of the local-first
  inference and routing program (`2026-09-07-local-first-routing-program.md`). The owner
  asked for a scanner that reads a machine's resources through the cockpit and compiles
  which models it should run. This record adds what makes that more than a hint:
  measurement after a pull, routing that ranks by what was measured, demotion proposed
  from evidence and never applied silently, and a machine that can serve a team.
  D1-D8 are rulings recorded for the owner to overturn.
- **Owner areas:** `dsl/worker` (registration, sharing, measurements), `component/worker`
  and `integrations/agent/worker` (register and heartbeat, the probe orchestration),
  `component/memql` (machine class, recommendation, measured ordering, the evidence
  fold), `component/router` (measured selectors), `dsl/platform` (measurements,
  evidence), `clients/os` (the machine page's groups), `docs/public/operate`, the
  cockpit half in `memql-cockpit`.
- **Depends on:** epic 3 (the catalog, the wire kinds); epic 2 (rules, `@exclude`,
  decision records). The cockpit half shares the proto change.

---

## 1. Problem

A registration carries the operating system, the architecture and the hostname. The
cluster is never told a machine has 64 GB and an M4 Max; it is told the machine offers a
model with a parameter count. The router orders local models by parameter count and
context window, which are proxies: a model's parameter count says nothing about whether
its structured output validates against this cluster's schemas, and a 4-bit
quantization of one model is not the 8-bit of another. Whether a model can serve a level
is asserted by a label the cockpit wrote from `ollama show`, never checked.

A fleet machine serves only its owner's calls, by the resolution of `fleet:` names
against the acting user. A business with one Mac Studio in the office has no way to
make it serve the team, so "local by default" is per person, not per company, and the
cost principle applies to an individual rather than to the business whose logic this
platform is meant to hand back to them.

## 2. What the tree already has

- `v1:worker:registration` (`dsl/worker/concepts.memql`): `platformInfo` (os, arch,
  hostname), `capabilityDescriptor`, `labels` (overwritten on every register),
  `operatorLabels` (the owner's, never overwritten), `apps`, `appDescriptors`,
  `concurrency`, `connectedNodeId`, `lastSeenAt`. `@rowAuthz(owner=..., clusterOwner)`.
- The label grammar and its parser (epic 3 adds four flags); `FleetModel` and
  `orderModels` (epic 2's `fleet:strongest` and `fleet:fastest` selectors read
  `Params`, `ContextWindow` and, when present, measured figures).
- `ModelPullStart / Progress / End` and `v1:worker:modelPull`, the persisted, broadcast
  progress row; the `targetNodeId` claim and the `ModelPullForward*` hop
  (inference-machine-setup record, section 9).
- The cockpit's floor verdict (Apple Silicon with 16 GB or a discrete GPU with 8 GB)
  evaluated on the machine, and `memql worker setup --inference`.
- `component/proving/figure`: a figure is a `Stat` or an `AbsentReason`, never both,
  with provenance; `Render` refuses an incomplete one.
- `v1:router:call` with epic 2's decision fields; healing receipts on `v1:work:step`;
  `v1:work:approval` with its artifact hash.
- `maintenanceAutomations` and the cron leader for nightly sweeps; the fleet router's
  strategies on `v1:worker:routingPolicy`.
- The Fleet app: machines, the per-machine Models group with the owner's Pull act, the
  fleet-wide models view, routing.

## 3. Decisions

### D1 -- Registration carries a hardware inventory, presence facts only

`registration.hardware`: `chip` (the marketing name the OS reports), `memoryBytes`
(unified or system), `gpu` (`{name, vramBytes, backend}` where `backend` is `metal`,
`cuda`, `rocm` or `none`), `cpuCores`, `osVersion`, `diskFreeBytes`, `runtimes`
(`[{name, version}]` for `ollama`, `mlx`, `whispercpp`, `kokoro`, `mflux`, `docker`),
`reportedAt`. Reported on register and refreshed on every tenth heartbeat, so a pulled
model or an installed runtime shows within minutes. No serial numbers, no user names, no
paths. Chosen over deriving class from the models advertised (a machine with no model
yet is exactly the one the scanner is for).

### D2 -- The machine class is computed on the engine and the recommendation is one act

`machineClass(hardware)` is a pure function in `component/memql/fleet_class.go`: the
class is the largest of `16`, `24`, `32`, `64`, `128` not exceeding usable memory, where
usable is 75 percent of unified memory on Metal and VRAM on a discrete GPU, `unsupported`
below the floor. The recommended set is the catalog's profiles whose `minMachineClass`
is at or below the class and whose `runtime` the machine has, one per level, strongest
first, computed by `recommendedSet(class, hardware, catalog)` and shown on the machine
page as one group with one act, "Pull recommended set", owner only, which runs the
existing pull path once per model in order and re-advertises once at the end. A machine
below the floor shows the floor's sentence and no act. Chosen over recommending on the
cockpit: the catalog lives in the graph, and one function on the engine is what every
surface and the wizard's fleet door read.

### D3 -- After a pull, a probe measures, with the proving suite's honesty

`ModelProbeStart / Progress / End` on `WorkerService.Stream`, the `ModelPull` shape: the
engine names the model and a probe suite version, the cockpit runs the suite against the
local runtime under the appsession supervisor, streams per-case progress, and reports
figures. The suite, pinned by version in `component/worker/probe`: structured-output
validity over five schemas drawn from the platform's own prompts (triage, intake,
symptom, factory decision, healing patches), tool-call correctness over three tool
definitions, and throughput plus time to first token at an 8K and a 32K prompt. Figures
land on `v1:platform:modelMeasurement` rows keyed by `(machineId, modelId, suiteVersion)`,
each a `Stat` or an `AbsentReason` with a date, written under the maintenance actor,
owner-tier readable through the machine. A probe runs automatically after a pull the
engine started, and on demand from the machine page. Chosen over trusting the label: a
validity rate on this cluster's schemas is the fact the router needs.

### D4 -- Measured figures rank first and gate nothing, this release

`fleet:strongest` orders by measured structured validity descending, then throughput,
then parameters and context; a model with no measurement sorts after every measured one
and the decision record says `unmeasured`. `fleet:fastest` orders by measured throughput.
Eligibility is unchanged: the advertised flags decide, so a model that fails the
structured probe is still eligible for a structured call and is reported as failing on
the machine page, the decision record and the Fleet models view. Making a failed probe a
hard eligibility gate is a later decision, after a release of measurements, because a
probe that refuses a working model on a bad run is worse than no probe.

### D5 -- Evidence proposes a demotion and never applies one

The nightly `routingEvidenceFold` on the cron leader folds the week's decision records
and step receipts into `v1:platform:modelEvidence` rows per `(modelId, level, week)`:
calls, structured failures, repair rate, retry rate, park count. When a model's
structured failure rate at a level exceeds 0.3 over at least twenty calls, the fold opens
a `v1:work:approval` of kind `routingReview` proposing a generated custom rule that
excludes the model at that level (`@when(level=...) @exclude("fleet:<modelId>")
@policy("localFirst")`), hashed over the exact rule text. Approving activates it through
the authoring pipeline; declining records the decision so the same week's evidence does
not re-propose. Promotion is the symmetric case for an excluded model whose newer
measurement passes. Chosen over automatic demotion: the never-a-silent-edit rule, and a
week of bad luck on one schema is not a reason to route a whole level elsewhere.

### D6 -- A machine can serve the cluster, with two consents

`registration.sharing`: `mode` (`owner`, `cluster`), `sharedAt`, `sharedBy`, set by
the machine's owner from the machine page through `setWorkerSharing`. The cockpit reports
its own policy verdict on register as `capabilityDescriptor.inferenceServe` (`owner` or
`cluster`, from `policy.yaml` `inference.serve`), and a machine serves another user only
when both say `cluster`, the `allowed` and `signedIn` shape the app door already uses.
Fleet resolution for a user reads their own registrations plus every registration whose
effective sharing is `cluster`, under the maintenance actor, and the shipped rule
`preferOwnMachines` orders a caller's own machines first. Every decision record on a
shared machine carries `machineOwnerUserId` and the acting user, the machine page shows
"served N calls for M people this week", and the owner can revoke sharing with one act.
A shared machine's owner never sees another person's prompts: the ledger carries counts
and levels, not content. Chosen over a group model: one flag, two consents, and the
existing ledger are what a business needs first.

### D7 -- Runtimes are advertised as capabilities, and the catalog says what a machine lacks

The cockpit advertises `runtime:<name>` labels (it already does for Ollama) and the
hardware inventory carries versions; the catalog's `runtime` field lets the machine page
say "needs the Kokoro runtime" beside an audio-out profile and offer the cockpit's
install sentence. The speech and image runtimes' installation is the cockpit's (its
epic), consented and idempotent as `setup --inference` is; the engine never installs
software on a person's machine.

### D8 -- A label fingerprint change costs one reconnect, as before

The new labels and the inventory change every machine's registration once on rollout,
the cost the inference-machine-setup record accepted for `params` and `quant`. The
cockpit's version gate refuses a probe request from an engine older than the suite it
knows, and an engine reads an absent inventory as "this cockpit predates the field",
never as a machine with no memory.

## 4. The change

- `dsl/worker/concepts.memql`: `hardware`, `sharing`, `capabilityDescriptor.inferenceServe`;
  `dsl/worker/mutations.memql`: `setWorkerSharing`; `dsl/platform/concepts.memql`:
  `modelMeasurement`, `modelEvidence`; queries for the machine page and the fold.
- `component/grpc/worker.proto`: the inventory on `Register` and heartbeat, the probe
  message pair, the forward hop; `component/worker/probe` (new): the suite.
- `component/memql/fleet_class.go` (new), `fleet_recommend.go` (new), the measured
  ordering in `fleet_provider.go`, shared resolution in `fleet_catalog_read.go`.
- `integrations/agent/worker`: register and heartbeat handling, the probe handle with
  its `targetNodeId` claim and stale sweep (the pull's shape), the evidence fold
  automation in `dsl/router/automations.memql`.
- `component/router`: `preferOwnMachines`, measured selectors, the `routingReview`
  proposal builder.
- `clients/os/src/apps/fleet/machines/`: Hardware, Models with the recommended set and
  measured figures, Sharing groups; the fleet-wide models view's measured column. The
  full redesign is epic 5; this epic ships the groups on the existing page.
- Docs: `docs/public/operate/local-models.md`, `workers-runbook.md`, a new
  `shared-machines.md`.

## 5. Failure modes

- A probe that crashes the runtime: the appsession supervisor's timeout ends it, the
  measurement is an `AbsentReason` naming the crash, nothing is ranked on it.
- An inventory that lies (a VM reporting host memory): the class is wrong, the pull may
  fail, and the machine page shows the reported figures so a person can see why.
- A shared machine that goes offline mid-call: the same refusal-before-start rule as
  today; nothing re-picks after a generation may have run.
- Sharing turned off while a call is in flight: the call completes, the next resolution
  excludes the machine.
- The evidence fold with fewer than twenty calls: no proposal, and the row says the
  count.

## 6. Testing

- `machineClass` and `recommendedSet` on fixtures for every class, the floor, a missing
  runtime, an absent inventory.
- The probe: suite fixtures with known-good and known-bad outputs; figures as `Stat` or
  `AbsentReason`; the hop test for a machine on a sibling replica; the stale sweep.
- Measured ordering: measured before unmeasured, ties, the `unmeasured` reason on the
  record.
- The fold: thresholds, the twenty-call floor, one proposal per week per model and
  level, the hash on the approval, decline recorded.
- Sharing: both consents required; resolution includes shared machines for another user
  and excludes them when either consent is missing; `preferOwnMachines`; the ledger
  fields; revoke.
- The Fleet page groups, both modes, with and without an inventory.

## 7. Delivery

Three PRs, plus the cockpit half's epic.

- **PR 1, the scanner.** Task 1: the inventory on register and heartbeat, the concept
  fields, the Hardware group. Task 2: machine class, the recommended set, the one-act
  pull.
- **PR 2, measurement.** Task 3: the probe pair, the suite, measurements with
  provenance, the measured selectors. Task 4: the evidence fold and `routingReview`
  proposals.
- **PR 3, sharing.** Task 5: the sharing field, both consents, shared resolution,
  `preferOwnMachines`, the ledger and the Sharing group. Task 6: runtimes as
  capabilities, the catalog's "needs runtime" sentence, and docs.

The picking-up session writes the plan from this record with `superpowers:writing-plans`
and deletes it in the epic's merge. The cockpit half (inventory reporting, the probe
executor, `inference.serve`, the speech and image runtimes) is `memql-cockpit`'s epic for
this program.

## 8. Out of scope

- Probe results as a hard eligibility gate.
- A group or per-user sharing list; a shared machine's owner seeing prompt content.
- Installing any runtime from the engine.
- Metering that charges anyone anything.

## 9. Facts to re-verify before starting

The `registration` field list and the `capabilityDescriptor` schema version; the
`ModelPull` message pair and the claim field; the proving figure API; the selectors'
signatures from epic 2; the cockpit's `policy.yaml` schema for `models.allow` and
`apps.allow`. All were read on 2026-09-07 at commit 907e385fb.
