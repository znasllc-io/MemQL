# The machine scanner, measured capability, and shared machines -- Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A registration carries a hardware inventory; the engine computes a machine
class and recommends a model set with one act; a probe measures what a pulled model
actually does on that machine; routing ranks by measurement and gates on nothing; a
nightly fold proposes a demotion as an approval and never applies one; and a machine can
serve the whole cluster on two consents.

**Architecture:** Everything the engine decides is a PURE function over reported facts
(`machineClass`, `recommendedSet`, `measuredKey`, the fold's thresholds), so the claims
are checkable without a cluster. Everything the engine cannot know -- what a machine has,
what a model does on it -- arrives as a REPORT over the existing worker stream, in the
`ModelPull` shape (start / progress / end, a `targetNodeId` claim, a forward hop, a stale
sweep) because that shape is already proven for a long-running per-machine act. Every
measured number is a `figure`: a `Stat` or an `AbsentReason`, never both and never
neither, all the way to the pixel.

**Tech Stack:** Go 1.26, protobuf/gRPC (`WorkerService.Stream`, `NodeService.Stream`),
MemQL DSL, PostgreSQL + TimescaleDB, React + TypeScript (MemQL OS).

**Spec:** `docs/superpowers/specs/2026-09-07-machine-scanner-and-shared-machines-design.md`
Program index: `docs/superpowers/specs/2026-09-07-local-first-routing-program.md`

## Global Constraints

- **One PR for all six issues** (owner instruction, 2026-09-07, overriding the record's
  section 7 three-PR grouping). Body carries `Closes #5147` ... `Closes #5152` on separate
  lines -- `Closes #a, #b` links only the first.
- **Branch:** `epic/machine-scanner-and-shared-machines`, worktree
  `/home/znas/memql-projects/epic-machine-scanner-and-shared-machines`, based on
  `epic/open-weight-defaults-and-catalog` @ `bd63f10b3` (recorded; the rebase is
  `git rebase --onto origin/main bd63f10b3`, never a plain `git rebase main`).
  Landing order 1 -> 2 -> 3 -> 4; the PR opens only after epics 2 and 3 have merged.
- **Pre-release: no shims.** The `sharedInference` operator label is REPLACED by
  `registration.sharing`, not kept beside it.
- **No emoji anywhere** -- docs, UI copy, commit messages, test names.
- **`make test`, never `go test ./...`** -- the latter misses `component/memql`,
  `component/database` and `component/language`.
- **Stage by explicit path.** Never `git add -A` / `git add .`; three peer sessions share
  this repo's working trees.
- **Commit format:** `Issue #<N>: <description>`, with the session trailer.
- **An absent figure is never a zero**, in Go and in the browser. A machine nobody probed
  renders the absence's own sentence, not `0`.
- **An absent inventory is `""`, never `"unsupported"`.** The first says the cockpit
  predates the field; the second is an accusation.
- **Machine class is COMPUTED and never stored** on the registration row.
- Six machine-class values, closed: `"" | "unsupported" | "16" | "24" | "32" | "64" | "128"`.
- Six runtime names, closed: `ollama | mlx | whispercpp | kokoro | mflux | docker`.
  (The CATALOG's `runtime` enum is a different, overlapping set -- `nemo` and `comfyui`
  are catalog runtimes the cockpit does not install. A catalog profile naming a runtime
  outside the inventory's six is reported as needing a runtime this machine cannot
  report, never silently recommended.)
- **Acts' accessible names are frozen** and shared with epic 5's enumeration test:
  `Pull recommended set`, `Probe this model`, `Share with the cluster` /
  `Stop sharing with the cluster`.

---

## File structure

**Wire**
- `component/grpc/worker.proto` -- `HardwareInventory` / `GpuInfo` / `RuntimeInfo`;
  `Register.hardware`; `Heartbeat.hardware` + `hardware_present`; the probe quartet
  `ModelProbeStart / Progress / End / Cancel` and `ProbeFigure`.
- `component/node/node.proto` -- `ModelProbeForwardRequest / Response / Progress / Cancel`.

**Engine, pure**
- `component/memql/fleet_class.go` (new) -- `MachineClass`, `UsableBytes`, `ClassAtLeast`.
- `component/memql/fleet_recommend.go` (new) -- `RecommendedSet`, `RuntimeGap`.
- `component/memql/fleet_measured.go` (new) -- `MeasuredKey`, `OrderMeasured`.
- `component/router/evidence.go` (new) -- `FoldWindow`, `Proposal`, `ProposeExclusion`,
  `RuleText`, `ProposalHash`.

**Engine, wired**
- `component/worker/hardware.go` (new) -- `Inventory` type, proto <-> row conversion,
  `MaterialChange`.
- `component/worker/probe/suite.go`, `cases.go`, `score.go` (new) -- the versioned suite.
- `component/worker/modelprobe.go` (new) -- `StartModelProbe`, the handle, the stream hook.
- `integrations/agent/worker/model_probe.go` (new) -- the claim, the hop, the runner.
- `integrations/agent/worker/model_probe_runner.go` (new) -- the stale sweep.
- `component/memql/fleet_model_probe.go` (new) -- the `fleetModelProbe` builtin.
- `component/memql/fleet_recommended_pull.go` (new) -- the `fleetPullRecommended` builtin.
- `component/memql/fleet_catalog_read.go` -- shared resolution; `machineClass` on the row.
- `component/memql/fleet_provider.go` -- `FleetModel.Measured`, `FleetMachine.OwnerUserId`
  already present, measured ordering hooked into `orderModels`.
- `integrations/agent/worker/store.go` -- `SharedInference` from the two consents.
- `integrations/agent/worker/model_routing.go` -- `runtime:<name>` labels; the sharing read.
- `component/worker/server.go` -- register + heartbeat inventory; probe stream hook.
- `component/worker/store.go` -- `UpdateLastSeen` carries a pending inventory.

**DSL**
- `dsl/worker/concepts.memql` -- `registration.hardware`, `registration.sharing`.
- `dsl/worker/mutations.memql` -- `setWorkerSharing`.
- `dsl/worker/builtins.memql` -- `fleetPullRecommended`, `fleetModelProbe`.
- `dsl/worker/queries.memql` -- `sharedInferenceRegistrations`.
- `dsl/platform/concepts.memql` -- `modelMeasurement`, `modelEvidence`.
- `dsl/platform/{queries,mutations,shapes}.memql` -- their reads and writes.
- `dsl/work/concepts.memql` -- `approval.kind` gains `routingReview`; `runId` relaxed.
- `dsl/router/automations.memql` (new) -- `routingEvidenceFold`.
- `dsl/rules/rules.memql` -- `preferOwnMachines` (lands at rebase; see Task 5 note).

**OS**
- `clients/os/src/apps/fleet/machines/HardwareGroup.tsx` (new)
- `clients/os/src/apps/fleet/machines/SharingGroup.tsx` (new)
- `clients/os/src/apps/fleet/machines/ModelsGroup.tsx` -- recommended set, measured, probe
- `clients/os/src/apps/fleet/machines/hardware.ts` (new) -- pure readers
- `clients/os/src/apps/fleet/rows.ts` -- `MachineRow.hardware`, `.sharing`, `.machineClass`
- `clients/os/src/apps/fleet/machines/useMachineWrites.ts` -- `setSharing`
- `clients/os/src/apps/fleet/machines/useModelPulls.ts` -- recommended pull, probes
- `clients/os/src/apps/fleet/machines/MachineDetail.tsx` -- mount the two new groups
- `clients/os/src/index.css` -- the groups' styles

**Docs**
- `docs/public/operate/local-models.md`, `workers-runbook.md`,
  `shared-machines.md` (new), `ai-routing.md` (the measured selectors' sentence).

---

## Task 1 (#5147): The hardware inventory

**Files:**
- Modify: `component/grpc/worker.proto`
- Create: `component/worker/hardware.go`, `component/worker/hardware_test.go`
- Modify: `component/worker/server.go`, `component/worker/store.go`,
  `component/worker/worker.go`, `component/worker/registry.go`
- Modify: `dsl/worker/concepts.memql`, `dsl/worker/mutations.memql`,
  `dsl/worker/shapes.memql`
- Modify: `integrations/agent/worker/store.go`
- Create: `clients/os/src/apps/fleet/machines/hardware.ts`,
  `clients/os/src/apps/fleet/machines/HardwareGroup.tsx`
- Modify: `clients/os/src/apps/fleet/rows.ts`,
  `clients/os/src/apps/fleet/machines/MachineDetail.tsx`, `clients/os/src/index.css`
- Test: `component/worker/hardware_test.go`,
  `component/worker/server_registration_test.go`, `component/worker/server_heartbeat_test.go`,
  `clients/os/test/fleet/machineHardware.test.tsx`

**Interfaces produced:**

```go
// component/worker/hardware.go
type Gpu struct { Name string; VramBytes uint64; Backend string } // metal|cuda|rocm|none
type Runtime struct { Name string; Version string }
type Inventory struct {
    Chip string; MemoryBytes uint64; Gpu Gpu; CpuCores int
    OsVersion string; DiskFreeBytes uint64; Runtimes []Runtime
    ReportedAt time.Time
}
func InventoryFromProto(p *memqlv1.HardwareInventory) (Inventory, error)
func (i Inventory) Present() bool
func (i Inventory) Row() map[string]any          // the shape stored on registration.hardware
func InventoryFromRow(v any) Inventory
func MaterialChange(old, new Inventory) bool     // ignores DiskFreeBytes and ReportedAt
const MaxRuntimes = 16
var RuntimeNames = []string{"ollama","mlx","whispercpp","kokoro","mflux","docker"}
var GpuBackends = []string{"metal","cuda","rocm","none"}
```

- [ ] **Step 1: Add the wire messages.** In `component/grpc/worker.proto`, after
      `PermissionStatus`:

```proto
// HardwareInventory is what the machine IS, as its cockpit reports it
// (epic memql#5146, design D1). PRESENCE FACTS ONLY -- no serial numbers, no
// user names, no paths. The engine computes a machine class and a recommended
// model set from this and stores neither: a class is a function of the
// inventory, and a stored copy would be a second answer that ages.
//
// ABSENT IS "THIS COCKPIT PREDATES THE FIELD", NEVER "A MACHINE WITH NO
// MEMORY". Every reader takes it that way, which is why the class of a machine
// with no inventory is the empty string rather than `unsupported` -- the second
// one is an accusation, and it would be levelled at every machine in a fleet
// the moment this field shipped.
message HardwareInventory {
  // The marketing name the OS reports ("Apple M4 Max", "AMD Ryzen 9 7950X").
  string chip = 1;
  // Unified memory on Apple Silicon, system memory elsewhere.
  uint64 memory_bytes = 2;
  GpuInfo gpu = 3;
  uint32 cpu_cores = 4;
  string os_version = 5;
  uint64 disk_free_bytes = 6;
  repeated RuntimeInfo runtimes = 7;
  google.protobuf.Timestamp reported_at = 8;
}

// GpuInfo describes the accelerator the machine can actually reach.
//
// `backend` is what the machine can RUN, not what is installed: a discrete card
// with no working driver reports `none`, because a backend the runtime cannot
// use is a capability this cluster must not count.
message GpuInfo {
  string name = 1;
  // Dedicated VRAM. ZERO on unified-memory machines, where memory_bytes is the
  // pool -- not a card with no memory. `backend` is what tells the two apart.
  uint64 vram_bytes = 2;
  // metal | cuda | rocm | none.
  string backend = 3;
}

// RuntimeInfo is one model runtime the machine has, with its version.
//
// The set the engine understands is closed (ollama, mlx, whispercpp, kokoro,
// mflux, docker) and an unknown name is STORED and never acted on -- the rule
// AppInfo already follows, for the same reason: a newer cockpit must be able to
// report more than this engine can drive without a coordinated release.
message RuntimeInfo {
  string name = 1;
  string version = 2;
}
```

      Then `HardwareInventory hardware = 12;` on `Register` (with a comment saying a
      cockpit that omits it registers exactly as before), and on `Heartbeat`:

```proto
  // hardware re-reports the inventory (epic memql#5146, D1). The cockpit sends
  // it on every tenth beat, so an installed runtime or a pulled model shows
  // within minutes rather than at the next reconnect.
  HardwareInventory hardware = 6;
  // hardware_present distinguishes "I am reporting an inventory" from "I am not
  // reporting one", which a proto3 message field cannot express on its own --
  // the same reason apps_present exists one field above.
  bool hardware_present = 7;
```

- [ ] **Step 2: Regenerate and commit the proto.** `make proto-gen` (or
      `scripts/dev/proto-gen.sh`), then commit -- `proto-gen-check` diffs against the
      COMMITTED tree, so an uncommitted proto edit fails the check even when the working
      tree is consistent.

- [ ] **Step 3: Write the failing inventory tests.** `component/worker/hardware_test.go`:

```go
func TestInventoryFromProtoRejectsAnUnknownBackend(t *testing.T)
    // backend "vulkan" -> error naming the four; nothing partially applied.
func TestInventoryFromProtoKeepsAnUnknownRuntimeName(t *testing.T)
    // "vllm" survives into Runtimes: the engine stores what it cannot drive.
func TestInventoryFromProtoCapsTheRuntimeList(t *testing.T)
    // 17 entries -> error; a misbehaving cockpit cannot grow the row.
func TestAbsentInventoryIsNotAnEmptyOne(t *testing.T)
    // InventoryFromProto(nil) -> Present() == false, and Row() returns nil so the
    // mutation writes no key at all rather than an object of zeroes.
func TestMaterialChangeIgnoresFreeDiskAndTheTimestamp(t *testing.T)
    // Same machine, 4 GB less free and a later reportedAt -> false.
func TestMaterialChangeNoticesANewRuntime(t *testing.T)
    // ollama -> ollama+kokoro -> true. This is the case the whole predicate is for:
    // installing a runtime changes what the machine should be recommended.
func TestMaterialChangeNoticesAMemoryChange(t *testing.T)
    // A VM resized, or a card swapped -> true.
```

- [ ] **Step 4: Run them and watch them fail.**
      `go test ./component/worker/ -run TestInventory -v` -- expect build failure,
      `undefined: InventoryFromProto`.

- [ ] **Step 5: Implement `component/worker/hardware.go`.** Validate the backend against
      the closed four and the list length against `MaxRuntimes`; keep unknown runtime
      NAMES; `Row()` returns `nil` for an absent inventory so the caller writes no key.
      `MaterialChange` compares chip, memoryBytes, gpu (all three fields), cpuCores,
      osVersion and the runtime SET (name+version, order-insensitive) -- and deliberately
      not `DiskFreeBytes` or `ReportedAt`.

- [ ] **Step 6: Run them and watch them pass.** `go test ./component/worker/ -run TestInventory -v`

- [ ] **Step 7: Declare the concept field.** In `dsl/worker/concepts.memql`, on
      `concept registration`, after `platformInfo`:

```
  hardware       object  @description("What the machine IS, as its cockpit reported it (epic memql#5146, D1): {chip, memoryBytes, gpu:{name, vramBytes, backend}, cpuCores, osVersion, diskFreeBytes, runtimes:[{name, version}], reportedAt}. Presence facts only -- no serial numbers, no user names, no paths. Sent on Register and refreshed on every tenth heartbeat, so an installed runtime shows within minutes. ABSENT MEANS THIS COCKPIT PREDATES THE FIELD, never a machine with no memory: the engine's machineClass reads an absent inventory as the empty string rather than `unsupported`, because the second one is an accusation and it would be levelled at every machine in a fleet on the day this shipped. The MACHINE CLASS and the RECOMMENDED SET are computed from this and stored nowhere -- a class is a function of the inventory, and a stored copy is a second answer that ages while the first one cannot.")
```

- [ ] **Step 8: Write the registration through.** Add `hardware` to
      `createWorkerRegistration` and `refreshWorkerRegistration` in
      `dsl/worker/mutations.memql`. **`refreshWorkerRegistration` is a read-merge and the
      prohibition there is the ABSENCE of a line** -- `displayName` and `operatorLabels`
      are absent on purpose. `hardware` IS written by register, so it goes in; add a
      comment at the field saying which side of that line it is on and why.
      Add `hardware` to the registration shapes in `dsl/worker/shapes.memql` that the
      Fleet page reads (`registrationFull` and whichever shape `allWorkersWithStatus`
      projects) -- a concept field is not a readable field until a shape projects it.

- [ ] **Step 9: Accept it on the wire.** In `component/worker/server.go`:
      - Register: parse `Register.hardware`, store on the `Worker` registry entry, pass to
        the store's create/refresh. A validation error REFUSES the registration with a
        `RegisterError` naming the field -- an inventory is a fact the cluster acts on, and
        a malformed one silently dropped is a machine that classes as unsupported forever
        with nothing to read.
      - Heartbeat: when `hardware_present`, parse and set on the registry entry. Persist
        immediately when `MaterialChange` is true (the app-inventory rule, and for the same
        reason: the derived `runtime:` labels live on the row as well as in the registry).
        Otherwise let it ride the throttled `UpdateLastSeen` write.
      - Extend `store.UpdateLastSeen` with the pending inventory so a non-material refresh
        costs no extra write. **This is the write-storm guard:** `diskFreeBytes` changes on
        every beat, so a persist-on-any-change rule would be one DB write per machine per
        beat forever.

- [ ] **Step 10: Test the wire.** In `component/worker/server_registration_test.go` and
      `server_heartbeat_test.go`:

```go
func TestRegisterStoresTheHardwareInventory(t *testing.T)
func TestRegisterRefusesAMalformedInventory(t *testing.T)   // RegisterError, no row written
func TestRegisterWithNoInventoryStillRegisters(t *testing.T) // the older-cockpit case
func TestHeartbeatWithoutHardwarePresentLeavesTheInventoryAlone(t *testing.T)
func TestHeartbeatPersistsAMaterialInventoryChangeInsideTheThrottleWindow(t *testing.T)
func TestHeartbeatDoesNotPersistAFreeDiskChangeInsideTheThrottleWindow(t *testing.T)
```

      The last two are the pair: one asserts the reason the immediate write exists, the
      other asserts the reason it is conditional. Run
      `go test ./component/worker/ -run 'TestRegister|TestHeartbeat' -v`.

- [ ] **Step 11: Read it into the Candidate.** In `integrations/agent/worker/store.go`,
      project `hardware` onto `Candidate` (`Hardware workerservice.Inventory`) in both
      `WorkersForOwner` and `SharedInferenceWorkers`. Assert it in
      `list_workers_test.go`.

- [ ] **Step 12: Commit the engine half.**

```bash
git add component/grpc/worker.proto component/grpc/gen component/worker/hardware.go \
  component/worker/hardware_test.go component/worker/server.go component/worker/store.go \
  component/worker/worker.go component/worker/registry.go \
  component/worker/server_registration_test.go component/worker/server_heartbeat_test.go \
  dsl/worker/concepts.memql dsl/worker/mutations.memql dsl/worker/shapes.memql \
  integrations/agent/worker/store.go integrations/agent/worker/list_workers_test.go
git commit -m "Issue #5147: a registration carries what the machine is"
```

- [ ] **Step 13: The OS readers.** `clients/os/src/apps/fleet/machines/hardware.ts` --
      pure, no JSX:

```ts
export interface MachineGpu { name: string; vramBytes: number; backend: string }
export interface MachineRuntime { name: string; version: string }
export interface MachineHardware {
  present: boolean; chip: string; memoryBytes: number; gpu: MachineGpu;
  cpuCores: number; osVersion: string; diskFreeBytes: number;
  runtimes: MachineRuntime[]; reportedAt: string;
}
export function hardwareFrom(row: Record<string, unknown> | null | undefined): MachineHardware;
export function formatBytes(n: number): string;   // "64 GB", "1.4 TB"; 0 -> "" not "0 B"
export function acceleratorSentence(hw: MachineHardware): string;
  // metal  -> "<gpu.name>, sharing <memory> of unified memory"
  // cuda   -> "<gpu.name>, <vram> of dedicated memory"
  // rocm   -> the same with the ROCm word
  // none   -> "No accelerator the runtimes can reach"  (NOT "no GPU": the machine may
  //           have a card whose driver does not work, and that is a different repair)
```

- [ ] **Step 14: Test the readers** in `clients/os/test/fleet/machineHardware.test.tsx`:

```ts
it("reads an absent hardware object as not present rather than as zeroes")
it("formats a unified-memory machine without claiming dedicated VRAM")
it("says no accelerator the runtimes can reach, never no GPU")
it("renders a machine with no inventory as not reported yet, with no class")
it("lists a runtime the engine does not drive, because the machine really has it")
```

      Run them from inside the OS tree: `cd clients/os && npx vitest run test/fleet/machineHardware.test.tsx`.
      **Running vitest from the repo root runs the OS tests without the OS setup** -- the
      tell is `clients/os/` on every FAIL path.

- [ ] **Step 15: Build `HardwareGroup.tsx`.** Facts, in the reading order a person asks
      them in: the class first as a single word with the usable-memory figure that
      produced it, then chip / memory / accelerator / cores / OS / free disk, then the
      runtimes as chips. Two empty states, and they say different things:
      - no inventory: "This machine's cockpit has not reported its hardware yet. An older
        cockpit does not send one; the machine is working normally."
      - inventory present, class `unsupported`: the floor's own sentence -- "Local models
        need Apple Silicon with 16 GB, or a discrete GPU with 8 GB. This machine reports
        <figure>." No act.
      Wire it into `MachineDetail.tsx` ABOVE `ModelsGroup` (what a machine IS precedes
      what it can run) and add `machine.hardware` to `rows.ts`.

- [ ] **Step 16: Style, typecheck, test.** Add `.os-fleet-hardware` styles to
      `clients/os/src/index.css`. Run `make os-typecheck` and `make os-build` -- **only
      `os-build` parses the stylesheet**, so a broken CSS rule passes the typecheck.

- [ ] **Step 17: Commit the OS half.**

```bash
git add clients/os/src/apps/fleet/machines/hardware.ts \
  clients/os/src/apps/fleet/machines/HardwareGroup.tsx \
  clients/os/src/apps/fleet/machines/MachineDetail.tsx \
  clients/os/src/apps/fleet/rows.ts clients/os/src/index.css \
  clients/os/test/fleet/machineHardware.test.tsx
git commit -m "Issue #5147: the Hardware group, and the two ways a machine says nothing"
```

---

## Task 2 (#5148): The machine class and the recommended set

**Files:**
- Create: `component/memql/fleet_class.go`, `fleet_class_test.go`,
  `fleet_recommend.go`, `fleet_recommend_test.go`, `fleet_recommended_pull.go`,
  `fleet_recommended_pull_test.go`
- Modify: `component/memql/fleet_catalog_read.go`, `component/memql/engine_types.go`,
  `dsl/worker/builtins.memql`, `component/node/routing.go`
- Modify: `clients/os/src/apps/fleet/machines/ModelsGroup.tsx`,
  `useModelPulls.ts`, `models.ts`, `rows.ts`
- Test: as above plus `clients/os/test/fleet/machineModels.test.tsx`

**Interfaces consumed:** `workerservice.Inventory` (Task 1).
**Interfaces produced:**

```go
// component/memql/fleet_class.go
const (
    ClassUnknown     = ""            // no inventory: the cockpit predates the field
    ClassUnsupported = "unsupported" // an inventory, under the floor
)
func MachineClasses() []uint64                       // {16, 24, 32, 64, 128}
func UsableBytes(inv workerservice.Inventory) uint64
func MachineClass(inv workerservice.Inventory) string
func ClassAtLeast(machine, need string) bool         // false whenever machine is Unknown

// component/memql/fleet_recommend.go
type CatalogProfile struct {
    ModelId, Category, Runtime, MinMachineClass string
    RecommendedFor, OfferedOn, Flags []string
    Params int64; ContextWindow int; SizeBytes int64; Notes string
}
type Recommendation struct {
    Profile CatalogProfile
    Level   string // fast | strong | reasoning | embeddings
    Blocked string // "" when pullable; otherwise the SENTENCE saying why
}
func RecommendedSet(class string, inv workerservice.Inventory, os string,
                    catalog []CatalogProfile) []Recommendation
```

- [ ] **Step 1: Write the failing class tests** in `component/memql/fleet_class_test.go`.
      The five real cards from memql-cf's report on #5147 are the point of the file:

```go
func TestClassAbsorbsReservedGpuMemory(t *testing.T) {
    // nvidia-smi reports TOTAL MINUS THE DRIVER'S RESERVATION, so a card's real
    // figure sits just under its nominal size. Flooring to whole gigabytes
    // misclassifies almost every discrete GPU a whole step low, always in the
    // same direction, and the two that break are the two most common cards in a
    // fleet. Rounding to nearest moves a figure by at most half a gigabyte and
    // every threshold is a multiple of eight, so it absorbs the slop and
    // nothing else. Figures measured by `memql worker hardware` on real
    // hardware; the cockpit asserts the same five in
    // internal/worker/hardware/class.go's TestClassAbsorbsReservedGPUMemory.
    cases := []struct{ name string; mib uint64; want string }{
        {"RTX 4090 / 5090 Laptop", 24564, "24"},
        {"RTX 4080", 16376, "16"},
        {"A6000", 49140, "32"},
        {"H100", 81559, "64"},
        {"a card that genuinely has 15 GB stays under the floor", 15360, "unsupported"},
    }
}
func TestClassOfAnAbsentInventoryIsUnknownNotUnsupported(t *testing.T)
func TestUsableBytesIsThreeQuartersOfUnifiedMemoryOnMetal(t *testing.T)
    // 64 GiB M4 Max -> 48 GiB usable -> class 32. The remaining quarter is the
    // OS and everything else the person is running.
func TestUsableBytesIsVramOnADiscreteGpu(t *testing.T)
    // 128 GB of system RAM behind an 8 GB card is an 8 GB machine for this
    // purpose: the weights have to fit in the card.
func TestABackendOfNoneFallsBackToSystemMemory(t *testing.T)
    // A CPU-only machine is classed on its RAM at the same 75 percent, so a
    // 64 GB workstation with no accelerator is not `unsupported` -- it is slow,
    // which is a different answer and one the probe will measure.
func TestClassAtLeastIsFalseForAnUnknownClass(t *testing.T)
    // The fail-closed direction. A machine that has not reported cannot clear a
    // floor by silence: `>=` over an unparsed class would admit every one.
```

- [ ] **Step 2: Run and watch them fail.**
      `go test ./component/memql/ -run TestClass -v`

- [ ] **Step 3: Implement `fleet_class.go`.** Round to nearest, do not floor:

```go
func usableGigabytes(b uint64) uint64 {
    const gb = uint64(1) << 30
    return (b + gb/2) / gb
}
```

- [ ] **Step 4: Run and watch them pass.**

- [ ] **Step 5: Write the failing recommendation tests** in `fleet_recommend_test.go`:

```go
func TestRecommendedSetIsOnePerLevelStrongestFirst(t *testing.T)
func TestRecommendedSetSkipsAProfileAboveTheClass(t *testing.T)
func TestAMissingRuntimeIsBlockedWithItsOwnSentence(t *testing.T)
    // Blocked names the runtime and does not silently drop the row: "needs the
    // Kokoro runtime" is what the machine page shows, and a row that vanished
    // answers "why is there no voice model" with nothing.
func TestARuntimeTheInventoryCannotReportIsBlocked(t *testing.T)
    // The catalog's enum has nemo and comfyui; the inventory's closed six do
    // not. A profile naming one is BLOCKED with a sentence, never recommended:
    // recommending a runtime no cockpit reports produces a pull that fails on
    // somebody's laptop for a reason the page never showed.
func TestAProfileNotOfferedOnThisOsIsBlocked(t *testing.T)
func TestAnAbsentInventoryRecommendsNothingAndBlocksNothing(t *testing.T)
    // Empty slice. There is nothing to say about a machine that has not
    // reported, and a page listing every profile as blocked would read as a
    // machine that failed rather than one that has not spoken.
func TestRecommendedSetIsStableAcrossReplicas(t *testing.T)
    // Ties broken by modelId. Two replicas answering different sets for one
    // machine is a page that changes when it is reloaded.
```

- [ ] **Step 6: Run, fail, implement `fleet_recommend.go`, run, pass.** One entry per
      level from `recommendedFor`, ordered by `params` descending then `modelId`; the
      `Blocked` sentence is built once and reused by the page and the act.

- [ ] **Step 7: Put the class on the machine read.** In `fleet_catalog_read.go` (and
      wherever `allWorkersWithStatus` rows reach the OS), add a computed `machineClass`
      and `usableBytes` to the projected row. Assert it is NOT a stored field:

```go
func TestMachineClassIsComputedAndNeverStored(t *testing.T)
    // Grep the mutations: no insert or update writes `machineClass`. A stored
    // class is a second answer that ages while the inventory it came from does
    // not.
```

- [ ] **Step 8: The one act.** `component/memql/fleet_recommended_pull.go` serves the
      `fleetPullRecommended` builtin: resolve the machine under the caller (the existing
      `modelPullMachineFor` refuses a machine that is not theirs), compute the set, and
      run the EXISTING pull path once per model IN ORDER, re-advertising once at the end.
      Refuse with a typed refusal when the class is `unsupported` or `""`.

```go
func TestPullRecommendedRunsInOrderAndReadvertisesOnce(t *testing.T)
func TestPullRecommendedRefusesAMachineThatIsNotTheCallers(t *testing.T)
func TestPullRecommendedRefusesBelowTheFloor(t *testing.T)
func TestPullRecommendedSkipsABlockedProfileAndSaysSo(t *testing.T)
func TestPullRecommendedForwardsToTheReplicaHoldingTheStream(t *testing.T)  // the hop
```

- [ ] **Step 9: Declare the builtin** in `dsl/worker/builtins.memql`
      (`@executor("integration.agentworker.pullRecommended")` is wrong here -- this one is
      an ENGINE builtin like `fleetModelPull`, so it registers through
      `engine_types.go`'s `BuiltinExecutorFleetPullRecommended`), add the routing rule in
      `component/node/routing.go` beside `fleetModelPull`'s, and run
      `make sdk-gen` -- **one new DSL construct reds five generated artifacts.**

- [ ] **Step 10: Commit the engine half.**

```bash
git add component/memql/fleet_class.go component/memql/fleet_class_test.go \
  component/memql/fleet_recommend.go component/memql/fleet_recommend_test.go \
  component/memql/fleet_recommended_pull.go component/memql/fleet_recommended_pull_test.go \
  component/memql/fleet_catalog_read.go component/memql/engine_types.go \
  component/node/routing.go dsl/worker/builtins.memql sdk
git commit -m "Issue #5148: the class a machine is, and the set it should pull"
```

- [ ] **Step 11: The Models group.** Grow `ModelsGroup.tsx`: a "Recommended for this
      machine" block above the present models, one row per level, each with the model id,
      the level, the size, and either the pull state or the `Blocked` sentence. One act,
      `Pull recommended set`, owner only, absent below the floor and absent when the set
      is empty. Tests in `clients/os/test/fleet/machineModels.test.tsx`:

```ts
it("offers Pull recommended set to the owner and to nobody else")
it("shows the floor sentence and no act on an unsupported machine")
it("shows neither a set nor a floor sentence when the cockpit has not reported")
it("says which runtime a blocked profile needs, rather than hiding the row")
```

- [ ] **Step 12: Typecheck, build, test, commit.**

```bash
git add clients/os/src/apps/fleet/machines/ModelsGroup.tsx \
  clients/os/src/apps/fleet/machines/useModelPulls.ts \
  clients/os/src/apps/fleet/machines/models.ts clients/os/src/apps/fleet/rows.ts \
  clients/os/src/index.css clients/os/test/fleet/machineModels.test.tsx
git commit -m "Issue #5148: the recommended set on the machine page, and one act to take it"
```

---

## Task 3 (#5149): The probe

**Files:**
- Modify: `component/grpc/worker.proto`, `component/node/node.proto`
- Create: `component/worker/probe/suite.go`, `cases.go`, `score.go`, `suite_test.go`,
  `score_test.go`
- Create: `component/worker/modelprobe.go`, `modelprobe_test.go`
- Create: `integrations/agent/worker/model_probe.go`, `model_probe_runner.go`,
  `model_probe_hop_test.go`, `model_probe_runner_test.go`
- Create: `component/memql/fleet_model_probe.go`, `fleet_model_probe_test.go`,
  `component/memql/fleet_measured.go`, `fleet_measured_test.go`
- Modify: `dsl/platform/concepts.memql`, `queries.memql`, `mutations.memql`,
  `shapes.memql`; `dsl/worker/builtins.memql`; `component/auth/maintenance_actor.go`
- Modify: `component/memql/fleet_provider.go` (measured ordering)
- Modify: `clients/os/src/apps/fleet/machines/ModelsGroup.tsx`, `models.ts`, `rows.ts`

**Interfaces produced:**

```go
// component/worker/probe/suite.go
const SuiteVersion = "1"
type Case struct { Id, Kind string; Prompt string; Schema string; Tools string; PromptTokens int }
func Suite(version string) ([]Case, error)   // error on an unknown version
func Cases() []Case                          // the pinned SuiteVersion's

// component/worker/probe/score.go
type CaseResult struct { Id string; Ok bool; ErrorMessage string; LatencyMs, TokensOut, TtftMs int }
type Figures struct {
    StructuredValidity  figure.Figure
    ToolCallCorrectness figure.Figure
    ThroughputTps       figure.Figure
    TtftMs              figure.Figure
}
func Score(results []CaseResult) Figures     // every arm a Stat or an AbsentReason

// component/memql/fleet_measured.go
type Measured struct { StructuredValidity, ThroughputTps float64; Has bool; SuiteVersion string }
func MeasuredKey(m Measured) (validity float64, tps float64, measured bool)
// The two accessors are shaped to epic 2's hooks EXACTLY, so the rebase is a
// one-line body swap and not a redesign:
func ThroughputOf(m FleetModel) (float64, bool)  // fills orderModelsFastest's `throughputOf`
func ValidityOf(m FleetModel) (float64, bool)    // the NEW first key in orderModels
```

**`(float64, bool)`, never a zero and never a nil.** Zero tokens per second and
"nobody measured this" are different answers; the sort reads the BOOL FIRST
(`if okA != okB { return okA }`) so a measured model outranks an unmeasured one
before any number is compared. Returning `0, true` for an unmeasured model sorts
it as the slowest MEASURED one -- precisely the confusion the bool exists to
prevent. A number computed from parameters and CALLED throughput would read on
the decision record exactly like a measurement, which is why epic 2 left the
hook empty rather than filling it with a proxy.

- [ ] **Step 1: The wire.** In `worker.proto`, the probe quartet, in the `ModelPull`
      shape, with the same reasoning about what it protects written down:

```proto
// ModelProbeStart asks the machine to MEASURE a model it already has
// (epic memql#5146, design D3).
//
// The pull's shape, and it is the pull's shape for the pull's reasons: a probe
// runs a suite of generations against a local runtime and takes minutes, which
// is past workerHost.exec's 600s cap and past its buffer. Start, progress, end,
// correlated by request id.
//
// WHAT IT MEASURES IS THIS CLUSTER'S OWN QUESTION. Structured validity is
// checked against five schemas drawn from the platform's own prompts, not
// against a general benchmark: a model's parameter count says nothing about
// whether its structured output validates against the schemas this cluster
// actually sends, and that is the fact the router needs.
//
// THE SUITE IS PINNED BY VERSION and the machine may refuse one it does not
// know. Two figures scored by different suites are not comparable, so the
// version is part of the measurement's key rather than metadata beside it.
message ModelProbeStart {
  string request_id = 1;
  string registration_id = 2;
  string model = 3;
  // The suite the engine wants run. A cockpit that does not know this version
  // ENDS with ok=false and `unknown_suite_version`, rather than running its own
  // and reporting figures under a name that means something else.
  string suite_version = 4;
}

message ModelProbeProgress {
  string request_id = 1;
  string model = 2;
  // The case just finished, and how many there are. A probe's progress is
  // COUNTABLE where a pull's is not, so this reports the whole and not just the
  // current step.
  string case_id = 3;
  uint32 completed_cases = 4;
  uint32 total_cases = 5;
  bool case_ok = 6;
  // The runtime's own failure text for this case, when it failed.
  string case_error = 7;
}

// ModelProbeEnd is the terminal answer, carrying the figures.
//
// A FIGURE IS A STAT OR A REASON, NEVER BOTH AND NEVER NEITHER. That is the
// proving suite's discipline (component/proving/figure) carried onto the wire:
// a probe whose runtime crashed reports an ABSENT figure naming the crash, and
// nothing is ranked on it. Reporting 0 tok/s for a probe that never ran is a
// lie that reads exactly like a slow machine.
message ModelProbeEnd {
  string request_id = 1;
  string model = 2;
  bool ok = 3;
  string error = 4;
  string suite_version = 5;
  ProbeFigure structured_validity = 6;
  ProbeFigure tool_call_correctness = 7;
  ProbeFigure throughput_tps = 8;
  ProbeFigure ttft_ms = 9;
}

// ProbeFigure is one measured number or the reason there is not one.
//
// `n` is the sample size and it is on the MEASURED side only: a median with no
// count behind it is a number a reader cannot weigh. `spread_low` and
// `spread_high` are the interquartile bounds -- medians and spread, never a
// best case.
message ProbeFigure {
  bool measured = 1;
  double median = 2;
  double spread_low = 3;
  double spread_high = 4;
  uint32 n = 5;
  // One of: unmeasured | refused | failed. Empty when measured is true.
  string absent_reason = 6;
  // The machine's own sentence for the absence. Never invented by the engine.
  string absent_detail = 7;
}

message ModelProbeCancel { string request_id = 1; string reason = 2; }
```

      Add them to both oneofs (`model_probe_progress = 22`, `model_probe_end = 23` on the
      client message; `model_probe_start = 22`, `model_probe_cancel = 23` on the server
      message). In `node.proto`, the four `ModelProbeForward*` messages mirroring
      `ModelPullForward*`, at tags 106/107 on each oneof.

- [ ] **Step 2: `make proto-gen`, commit.**

- [ ] **Step 3: Write the failing suite tests** in `component/worker/probe/suite_test.go`:

```go
func TestSuiteIsPinnedAndAnUnknownVersionIsAnError(t *testing.T)
func TestTheFiveStructuredCasesAreTheseSchemas(t *testing.T)
    // triage, intake, symptom, factory decision, healing patches -- the platform's
    // own prompts, named. A suite of invented schemas measures a model against a
    // benchmark nobody runs.
func TestThreeToolCasesAndTwoThroughputPrompts(t *testing.T)
    // 8K and 32K. A model whose window is under 32K reports the 32K case as
    // ABSENT with `refused`, never as a failure: it was asked something it
    // never claimed to do.
func TestEveryCaseIdIsUniqueAndStable(t *testing.T)
    // The ids are the join key between a progress message and a case, and they
    // are compared across releases in a measurement's history.
```

- [ ] **Step 4: Write the failing score tests** in `score_test.go`:

```go
func TestScoreOfNoResultsIsAbsentNotZero(t *testing.T)
func TestScoreOfAllFailedStructuredCasesIsAMeasuredZero(t *testing.T)
    // THE PAIR THAT MATTERS. Nothing ran -> absent. Five ran and all five failed
    // -> a measured 0.0 validity, which is a real and useful answer. Collapsing
    // the two is exactly the defect the figure type exists to prevent.
func TestARefusedCaseIsExcludedFromTheDenominator(t *testing.T)
    // A 32K case refused by a 8K model must not drag validity down: it is not a
    // failure, it is a question the model never claimed to answer.
func TestThroughputCarriesSpreadAndNeverAMean(t *testing.T)
```

- [ ] **Step 5: Run, fail, implement `suite.go` / `cases.go` / `score.go`, run, pass.**
      `component/worker/probe` is stdlib + `component/proving/figure` only. **Check the
      module boundary before importing:** if `component/proving` is not already reachable
      from `component/worker`'s module, do NOT add the edge -- copy the four-field figure
      shape into `probe` as a local type and assert the two agree with a parity test, the
      way `component/worker/online_client_parity_test.go` already does for `OnlineWindow`.
      Run `go build ./...` AND `make test` -- **`go build ./...` does not see
      module-boundaries**.

- [ ] **Step 6: The stream hook and the handle.** `component/worker/modelprobe.go`:
      `StartModelProbe(ctx, ModelProbeRequest) (*ModelProbeHandle, error)`, the
      `openModelProbe` per-stream hook in `server.go`, mirroring `openModelPull`.

- [ ] **Step 7: The claim, the hop and the sweep.**
      `integrations/agent/worker/model_probe.go` -- `ForwardModelProbe` with the mandatory
      `ForwardedAuthority` assertion (absent REFUSES: like a pull there is no
      fall-through), the `targetNodeId` claim, and the receiver's ownership re-check
      against the verified authority rather than the envelope's owner field.
      `model_probe_runner.go` -- the two-minute stale sweep, registered in
      `component/auth/maintenance_actor.go` as `workerModelProbeStaleSweep` with its own
      paragraph saying why it needs the principal (the same silence as
      `workerModelPullStaleSweep`: a sweep that finds nothing is indistinguishable from a
      cluster with nothing stuck).

```go
func TestProbeForwardsToTheReplicaHoldingTheStream(t *testing.T)  // in-process hop test
func TestProbeForwardRefusesWithNoForwardedAuthority(t *testing.T)
func TestProbeStaleSweepClosesAProbeWhoseReplicaIsGone(t *testing.T)
func TestProbeStaleSweepLeavesALiveProbeAlone(t *testing.T)
```

      **The hop test is IN-PROCESS**, wiring the real router to the real handler --
      a `clustere2e` lane is skipped on every developer machine, and a gate skipped by
      default cannot be what stands between a feature and the bug it prevents.

- [ ] **Step 8: The measurement row.** `dsl/platform/concepts.memql`:

```
/// One probe's figures for one model on one machine, under one suite version.
///
/// KEYED BY (machineId, modelId, suiteVersion) AND THE THIRD IS NOT METADATA. Two figures
/// scored by different suites are not comparable, so a new suite version produces a new row
/// rather than overwriting a figure that meant something else. The row id is a digest of the
/// three, so a re-probe under the same suite is a new VERSION of one logical row.
///
/// EVERY FIGURE IS A STAT OR AN ABSENT REASON, never both and never neither -- the discipline
/// component/proving/figure enforces in Go, carried here because the alternative is a page
/// that renders 0 tok/s for a machine nobody has probed. `unmeasured` and `0` are different
/// answers and this row keeps them distinguishable all the way to the pixel.
///
/// TIER: the composite owner, clusterOwner. A measurement is a fact about somebody's machine
/// -- it says what their hardware does -- so the owner tier is right, and a plain one would
/// hide every machine's figures from the operator who has to explain the routing.
@rowAuthz(owner="ownerUserId", clusterOwner)
@displayCard(primary="modelId", secondary="machineId", tertiary="measuredAt", status="suiteVersion")
concept modelMeasurement {
  ownerUserId  string!  @serverSet  @description(...)
  machineId    string!  @description("v1:worker:registration.id the suite ran on.")
  modelId      string!  @description("The runtime's own id, byte-identical to the `model:<id>` label and to modelProfile.modelId, so a measurement joins to both by string equality.")
  suiteVersion string!  @description(...)
  measuredAt   datetime!  @description(...)
  structuredValidity   object  @description("Figure. Fraction of the five structured cases whose output validated. A MEASURED ZERO is a real answer -- five ran and five failed -- and is not the same as the absent figure a crashed runtime produces.")
  toolCallCorrectness  object  @description("Figure over the three tool definitions.")
  throughputTps        object  @description("Figure, tokens per second.")
  ttftMs               object  @description("Figure, time to first token in milliseconds.")
  probeError   string  @description("The machine's own sentence when the probe itself failed. Empty on a clean run. Present WITH figures when some cases ran and some did not.")
}
```

      Plus `recordModelMeasurement` (`@serverOnly`), `measurementsForMachine` and
      `measurementsForModel` queries, and the shapes. Register the write under the
      MAINTENANCE actor -- the probe's report arrives on the agent and the row is the
      owner's.

- [ ] **Step 9: The act.** `fleetModelProbe` builtin -- owner only, on one model, plus the
      automatic probe after a pull the engine started. **The automatic one must not fail a
      pull:** a probe that could not run leaves the pull successful and the measurement
      absent, because the person asked for a model and got one.

- [ ] **Step 10: Measured ordering.** `component/memql/fleet_measured.go` +
      `FleetModel.Measured`. Order: structured validity descending, then throughput
      descending, then the EXISTING keys (preference, params, context, id). Unmeasured
      sorts after every measured model and the decision record says `unmeasured`.

```go
func TestMeasuredBeatsUnmeasured(t *testing.T)
func TestAMeasuredZeroStillBeatsUnmeasured(t *testing.T)
    // A model measured at 0.0 validity is a model somebody looked at. It sorts
    // below every other measured model and above one nobody has probed, because
    // the alternative rewards never being measured.
func TestEligibilityIsUnchangedByAFailedProbe(t *testing.T)
    // D4, and the reason is in the name: a probe that refuses a working model on
    // a bad run is worse than no probe. The advertised flags decide.
func TestOrderingIsStableAcrossReplicas(t *testing.T)
```

      **NOTE for the rebase:** epic 2 re-points `orderModels` as `fleet:strongest`'s
      implementation and states there is a hook for measured capability that returns
      nothing today. Write `MeasuredKey` as a pure function here and plug it into that
      hook when rebasing -- do NOT add a second ranking beside it.

- [ ] **Step 11: The measured figures on the page.** `ModelsGroup.tsx` shows validity,
      throughput and TTFT per model with `FigureValue` (from
      `clients/os/src/cluster/figure` on this base; epic 5 promotes it to `kit/measure` and
      the rebase is a one-line import change). An absent figure renders the em dash with
      its sentence on hover -- never `0`, never a blank cell. One act, `Probe this model`.

- [ ] **Step 12: Commit.**

```bash
git commit -m "Issue #5149: the probe, and figures that cannot pretend to be zero"
```

---

## Task 4 (#5150): The evidence fold and routingReview

**Files:**
- Create: `component/router/evidence.go`, `evidence_test.go`
- Create: `dsl/router/automations.memql`
- Modify: `dsl/platform/concepts.memql` (`modelEvidence`), queries, mutations, shapes
- Modify: `dsl/work/concepts.memql` (`approval.kind`, `runId`)
- Modify: `component/auth/maintenance_actor.go`
- Modify: `integrations/work` (the approval kind and its decision path)

**Interfaces produced:**

```go
// component/router/evidence.go
type Window struct { ModelId, Level, Week string
    Calls, StructuredFailures, Repairs, Retries, Parks int }
func (w Window) FailureRate() float64
const MinimumCalls = 20
const FailureThreshold = 0.3

// Form mirrors component/routingrules.Form FIELD FOR FIELD, and it is a mirror
// rather than an import because that package is epic 2's and is not on this
// base. The rebase deletes this type and imports the real one; the parity is
// asserted by TestEvidenceFormMatchesTheRuleGrammar until then.
type Form struct {
    Name, Description string
    When map[string]string      // only the keys the author SET; see below
    Policy, Level string
    Precedence int
    OnUnavailable string
    Excludes []string
}

// Renderer is INJECTED, and that is the whole design of this file: there is
// exactly one renderer of the rule grammar in the tree
// (routingrules.GenerateRule) and this package must not become a second. Two
// renderers of one grammar drift, and with a hash over the output the drift is
// a silent approval mismatch -- a person approves text A and text B is armed.
type Renderer func(Form) (string, error)

type Proposal struct { ModelId, Level, Week, RuleName, RuleSource, Hash, Direction string }
func ProposeExclusion(w Window, render Renderer) (Proposal, bool, error)
func ProposePromotion(w Window, measuredValidity float64, render Renderer) (Proposal, bool, error)
func ProposalHash(ruleSource string) string   // sha256 over the RENDERED SOURCE
```

**`When` carries only the keys actually set.** The closed key set is `level`,
`modality`, `prompt`, `role`, `actorRole`, `tag`, `touches`; a key PRESENT with
an empty value is a condition matching only an empty value, and a key ABSENT is
no condition at all. Emitting every key with `""` for the blanks produces a rule
that silently never fires.

**The hash is over the rendered SOURCE, not over the Form**, so it covers what
will actually be armed. `GenerateRule` is stable across map iteration (asserted
by its own test) precisely so a re-render of an unchanged rule is byte-identical.

- [ ] **Step 1: Write the failing threshold tests.**

```go
func TestNoProposalUnderTwentyCalls(t *testing.T)
    // Nineteen calls all failing is not evidence. The row still SAYS the count,
    // so an operator reading it can see why nothing was proposed.
func TestNoProposalAtExactlyTheThreshold(t *testing.T)
    // 0.3 is not "exceeds 0.3". A boundary decided by nothing is a boundary two
    // replicas can disagree about.
func TestProposalAboveTheThreshold(t *testing.T)
func TestOneProposalPerWeekPerModelAndLevel(t *testing.T)
func TestADeclinedProposalIsNotReProposedFromTheSameWeeksEvidence(t *testing.T)
func TestTheHashIsOverTheExactRuleText(t *testing.T)
    // An approval is a decision about one specific rule. A hash over anything
    // else lets an approved decision carry to a rule the person did not read.
func TestRuleTextIsDeterministic(t *testing.T)
    // Same inputs, byte-identical text, on every replica and every run. The hash
    // is the guarantee and a nondeterministic renderer voids it.
func TestPromotionIsTheSymmetricCase(t *testing.T)
```

- [ ] **Step 2: Run, fail, implement `evidence.go`, run, pass.** `RuleText` renders
      exactly the `@when(level=...) @exclude("fleet:<modelId>") @policy("localFirst")`
      shape epic 2's `routingRuleActivate` consumes. **Do not write a second renderer of
      that grammar** -- two renderers drift, and the hash makes the drift a silent
      approval mismatch. On this base the activation surface is not present; render the
      text, hash it, park the approval, and wire the activation call at the rebase.

- [ ] **Step 3: The evidence row and the approval kind.** `v1:platform:modelEvidence`
      (`@rowAuthz(clusterOwner)`, one row per model/level/week, carrying the counts AND the
      call count so a below-floor week says so). `dsl/work/concepts.memql`:
      add `routingReview` to `approval.kind`, and relax `runId` from `string!` to `string`
      with the reason written at the field:

```
  runId  string  @description("v1:work:run.id parked on this approval. REQUIRED IN EFFECT for every kind but one, and the exception is why the bang is gone: `routingReview` (epic memql#5146, D5) is raised by a nightly FOLD rather than by a run, so there is no run to park and a synthetic one would be a run that never ran. component/work's approval builder refuses an empty runId for every other kind, so the invariant is enforced where it can name the kind rather than by a schema that cannot.")
```

      Add `TestOnlyRoutingReviewMayOmitTheRun` in `component/work`.

- [ ] **Step 4: The automation.** `dsl/router/automations.memql`:

```
@trigger(schedule="0 30 3 * * *", partition="*")
@enabled
@description("Fold the week's decision records and step receipts into per-model evidence, and propose a demotion where the evidence carries one.")
automation routingEvidenceFold { ... }
```

      Register it in `maintenanceAutomations` with its own paragraph: the decision records
      span every owner, the fold cannot know whose model was failing before it looks, and
      under the default reader actor it reads ZERO ROWS AND NO ERROR -- a fold that
      proposes nothing is indistinguishable from a fleet with nothing wrong.

- [ ] **Step 5: Test the fold end to end** (db-gated, in `component/memql` or
      `integrations/work`): twenty failing calls produce one approval carrying the hash;
      the same fold run twice produces one approval, not two.

- [ ] **Step 6: Commit.**

```bash
git commit -m "Issue #5150: evidence proposes a demotion and never applies one"
```

---

## Task 5 (#5151): Shared machines

**Files:**
- Modify: `dsl/worker/concepts.memql`, `mutations.memql`, `queries.memql`, `shapes.memql`
- Modify: `component/worker/capability_descriptor.go`
- Modify: `integrations/agent/worker/store.go`, `model_routing.go`
- Modify: `component/memql/fleet_catalog_read.go`, `fleet_provider.go`
- Create: `clients/os/src/apps/fleet/machines/SharingGroup.tsx`
- Modify: `useMachineWrites.ts`, `rows.ts`, `MachineDetail.tsx`, `index.css`

- [ ] **Step 1: The two consents, as one predicate, tested first.**

```go
// integrations/agent/worker/sharing_test.go
func TestBothConsentsAreRequired(t *testing.T)
    // owner=cluster + cockpit=owner   -> not shared
    // owner=owner   + cockpit=cluster -> not shared
    // owner=cluster + cockpit=cluster -> shared
func TestAnAbsentCockpitConsentIsNotAConsent(t *testing.T)
    // A cockpit that predates inferenceServe has said nothing, and silence is
    // not agreement to serve other people's work on somebody's laptop.
func TestTheRefusalNamesWHICHConsentIsMissing(t *testing.T)
    // The repairs are in different places -- one is an act on the Fleet page,
    // the other is a line in that machine's policy.yaml -- so a single "not
    // shared" sentence sends half the operators to the wrong machine.
```

- [ ] **Step 2: Declare the fields.** `registration.sharing`
      (`{mode: "owner"|"cluster", sharedAt, sharedBy}`) and
      `capabilityDescriptor.inferenceServe` (validated in
      `component/worker/capability_descriptor.go` against the closed two, absent = `owner`).

- [ ] **Step 3: `setWorkerSharing`.** Owner only; `sharedBy` and `sharedAt` server-stamped.
      Refuse from a non-owner (the row-authz write guard already does; assert it).

- [ ] **Step 4: RETIRE the `sharedInference` operator label.** Pre-release, no shim:
      `Candidate.SharedInference` is derived from the two consents, `SharedInferenceLabel`
      and `parseAdvertisedBool(operator[...])` are DELETED, and the tests that assert the
      label path are rewritten rather than kept beside the new one. Sweep for the name
      across `.go`, `.memql`, `.ts`, `.tsx` and docs -- **deleting a mechanism leaves
      remnants a build cannot see**, including tests that PASS while asserting the deleted
      thing.

- [ ] **Step 5: Shared resolution for a USER.** Today `fleetCatalogForCaller` merges the
      caller's machines with `FleetCatalog(ctx, "")` -- the cluster set for calls with no
      acting user. D6 widens that: a user's resolution reads their own registrations PLUS
      every effectively-shared one, under the maintenance actor.

```go
func TestAnotherUsersSharedMachineIsInMyCatalog(t *testing.T)
func TestAnotherUsersUnsharedMachineIsNotInMyCatalog(t *testing.T)
func TestRevokingSharingTakesEffectOnTheNextResolution(t *testing.T)
func TestACallInFlightWhenSharingIsWithdrawnCompletes(t *testing.T)
```

- [ ] **Step 6: `preferOwnMachines`.** The shipped rule orders a caller's own machines
      first. On this base `dsl/rules/` does not exist (it is epic 2's); implement the
      ORDERING as a pure predicate in `component/memql` with its test, and add the rule
      row at the rebase.

- [ ] **Step 7: The ledger.** `machineOwnerUserId` and the acting user on every decision
      record; the machine page's "served N calls for M people this week".

      **The exact seam, for the rebase:** the field is `MachineOwnerUserId` on
      `airoute.Decision` (`core/airoute/resolution.go`, a leaf package importing only the
      standard library, so it can be named from anywhere with no module edge). It is
      DECLARED AND UNWRITTEN on purpose. The one place to fill it is
      `component/router/router.go`'s
      `func (r *Router) resolvedFrom(winner *chainWinner, mod providerModality, policyName string, report *doorReporter, decision airoute.Decision) Resolved`
      -- the success path, which holds `winner.entry` and can therefore reach a shared
      machine's owner. `observer.go`'s `buildRecord` and `router.go`'s
      `buildRouterCallArgs` already carry the field through to `v1:router:call`, so
      setting it in `resolvedFrom` before the `return` is the whole change. On this base
      `core/airoute` does not exist; build the owner-resolution as a pure function here
      with its test and wire the one line at the rebase.

      **Counts and levels, never content** -- assert it:

```go
func TestTheLedgerCarriesNoPromptContent(t *testing.T)
    // Walk the projected shape's field list; fail on any field whose value could
    // be a prompt, a message or an output. A shared machine's owner seeing
    // another person's prompts is the failure this whole feature is one act away
    // from, and a review comment is not what should stand between them.
```

- [ ] **Step 8: The Sharing group.** Both consents rendered SEPARATELY with their own
      repair sentences; the week's count; one act, `Share with the cluster` /
      `Stop sharing with the cluster`; and a line stating plainly that the owner sees
      counts and never content -- the person deciding whether to share needs that on the
      screen where they decide.

- [ ] **Step 9: Commit.**

```bash
git commit -m "Issue #5151: a machine can serve the cluster, on two consents"
```

---

## Task 6 (#5152): Runtimes as capabilities, and the docs

- [ ] **Step 1: `runtime:<name>` labels** derived from the inventory in
      `integrations/agent/worker/model_routing.go`, beside the existing `model:` and
      `app:` derivations. Version in the value (`runtime:ollama=0.5.4`).
      Test: a runtime the engine does not know derives NO label and is not an error.
- [ ] **Step 2: The needs-runtime sentence** on the machine page, from Task 2's
      `Blocked`, with the cockpit's install sentence beside it. The engine never installs
      software on a person's machine and the copy must not imply it does.
- [ ] **Step 3: `docs/public/operate/shared-machines.md`** (new): the two consents, what
      the owner sees and does not, how to revoke, and what happens to a call in flight.
- [ ] **Step 4: `local-models.md`** gains the scanner, the class table, the recommended
      set and the probe; **`workers-runbook.md`** gains the inventory and sharing;
      **`ai-routing.md`** gains the measured-selectors sentence.
- [ ] **Step 5: The docs gates.** A new doc reds two unrelated gates -- docs front-matter
      and the docs index. Run `make test` and fix both. **Never name a real vendor
      hostname** in a spec or doc; the vendor-domain gate scans them.
- [ ] **Step 6: Commit.**

```bash
git commit -m "Issue #5152: runtimes as capabilities, and the docs for a shared machine"
```

---

## Final verification (before the PR)

- [ ] `make test` from the repo root -- the MODULE path form, 200+ packages.
- [ ] `MEMQL_REQUIRE_DB=1 MEMQL_DATABASE_DSN=postgres://...@localhost:15434/... go test -count=1 ./component/memql/...`
      (the throwaway Postgres is SHARED across sessions; a peer's run can red mine, and
      seven `component/memql` db tests fail identically on `origin/main` -- compare in a
      main worktree before reading one as mine).
- [ ] `go test .` (root repo-walking gates) and `make arch-model-check` AFTER committing --
      **a failed `go-tests` step hides the next steps' failures.**
- [ ] `make frontdoor-hosts-check`, `make frontdoor-paths-check` -- no new HTTP routes, so
      both should be clean; run them to prove it rather than assuming.
- [ ] `make os-typecheck` AND `make os-build` -- only the second parses the stylesheet.
- [ ] `cd clients/os && npx vitest run` -- from inside the tree, never the repo root.
- [ ] `make sdk-gen` and confirm no diff.
- [ ] A real-browser pass on the machine page: screenshots are the acceptance for an OS
      surface, and a control that moved leaves the original behind with no test seeing it.
### The rebase, as a checklist

Use the `--onto` form against the recorded stack base, once epics 2 and 3 have landed.
**Never the plain form** -- this branch is STACKED on epic 3, so the plain form replays
ten of somebody else's commits into this PR and conflicts on `dsl/embed.go` and
`embed_inventory_test.go` on the way. (Measured: it does exactly that.)

The branch cannot leave epic 3 behind, either: `component/memql/fleet_recommend.go` reads
`v1:models:modelProfile` rows, so `fleetRecommended` and the whole recommended block are
epic 3's catalog. Dropping their commits leaves a builtin querying a concept that does not
exist, which strict boot refuses.

- [ ] **The ledger read is BROKEN until this lands, and the patch is written.**
      `scratchpad/ledger-fix.patch` plus `sharing_ledger_projection_test.go`. Two
      independent faults, both silent:
      1. `sharing_ledger_read.go` renders `routerCallsInWindow`, which is gated on
         `actor.isClusterOwner==true`. The caller is a machine's OWNER, usually not a
         cluster owner, so the read returns zero rows.
      2. That query's shape, `routerCallEvidence`, does not project `executionSurface` or
         `userId` -- the two fields the fold reads off every row. So even as a cluster
         owner the surface is empty, every row fails the `fleet:` prefix check and is
         skipped.
      Either one alone folds to **"No calls have run on this machine this week."** -- a
      specific claim, made on no evidence, to the one person entitled to a true answer,
      rendered identically to the honest zero.
      The fix is a dedicated `routerCallLedger` shape (`executionSurface`, `userId`,
      `level`) and a `routerCallsForMachineOwner` query filtered on
      `machineOwnerUserId==actor.userId`. **It needs the rebase**: `level` and
      `machineOwnerUserId` are epic 2's fields and `v1:router:call` declares neither on
      this branch, so applying the patch early refuses strict boot naming `level`.
      Three gates ship with it and the negative control is run: pointing the query back at
      `routerCallEvidence` fails `TestTheLedgerQueryUsesTheLedgerShape` by name.
- [ ] `Decision.MachineOwnerUserId` in `resolvedFrom` -- **the fix above depends on it.**
      The field exists on the row and nothing writes it, so the owner-scoped filter matches
      zero rows until this line lands. A declared field with no writer reads as "no value".
- [ ] Plug `ValidityOf` / `ThroughputOf` into epic 2's `orderModels` / `orderModelsFastest`.
- [ ] `preferOwnMachines` into `dsl/rules/rules.memql`.
- [ ] `routingrules.GenerateRule` as the evidence fold's injected `Renderer`.
- [ ] `level` onto the router-call evidence shape.
- [ ] The `kit/measure` import rename (`FigureValue` -> `Measure`) if epic 5 landed first.
- [ ] `TestUndeclaredRowAuthzPopulationOnlyShrinks` is RED on this branch for
      `routerCallsInWindow`, and the new ledger query joins it. Decide the tier on
      `v1:router:call` once epic 2's `routerDecisionsRecent` is visible in the tree:
      `@rowAuthz(owner="machineOwnerUserId", clusterOwner)` fits both reads IF their query
      is already cluster-owner gated. Declaring a tier NARROWS every existing read, so
      check theirs before declaring, and do not take the list's escape hatch without
      filing the issue it asks for.
- [ ] `TestDocsRelativeLinksResolve` is RED for the `shared-machines.md` link to the AI
      routing runbook. The link is correct; the file arrives with epic 2. Re-run after.
- [ ] `clients/os/src/styles/index.css`: epic 3 appends a 104-line block of its own and
      this branch appends one. **Take theirs whole and re-append ours** -- two appended
      blocks interleave on a three-way merge and the result still compiles, which neither
      the OS typecheck nor any test catches. The OS build is the only thing that parses
      the stylesheet.
- [ ] `component/architecture/topology.model.json`: take main's wholesale and regenerate
      with `make arch-model`. A derived file has no side worth keeping.
- [ ] Re-run, in this order: the root repo gates, `make test`, `make sdk-gen` (no diff),
      `make arch-model-check`, the OS typecheck, `make os-build`, the OS vitest suite from
      inside `clients/os`, `make frontdoor-hosts-check`, `make frontdoor-paths-check`.
- [ ] Delete this plan in the epic's merge commit.
