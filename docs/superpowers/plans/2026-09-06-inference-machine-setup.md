# Inference Machine Setup (Engine Half) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The engine can ask a fleet machine to pull a model and watch it happen, the
Fleet app shows each machine's models with size and quantization and offers the pull to
the machine's owner, and "Add machine" can mark a machine as one that will run local
models, which puts `--inference` on the one-line install command.

**Architecture:** PR 1 lands the `ModelPull*` message family on `WorkerService.Stream`
with the cross-replica forward, the `fleetModelPull` builtin (owner of the machine only),
and `params`/`quant` on `v1:platform:fleetModel` with a stated reduction. PR 2 adds the
Fleet app surfaces: the checkbox and the flag on the install line, the Models group on the
machine detail with the Pull act and live progress, and the Readiness signpost's link.

**Tech Stack:** Go 1.26 (`component/grpc`, `component/worker`, `integrations/agent/worker`),
MemQL DSL (`dsl/worker/builtins.memql`, `dsl/platform/concepts.memql`), React
(`clients/os/src/apps/fleet`).

**Spec:** `docs/superpowers/specs/2026-09-06-inference-machine-setup-design.md`. Read D3 to
D5 and section 4.2 first. The cockpit half is `memql-cockpit`'s
`docs/superpowers/plans/2026-09-06-inference-machine-setup.md`; order: cockpit PR 1,
engine PR 1, cockpit PR 2, engine PR 2.

**Closes:** the epic issue and its task issues, filed by Task 0.

---

## Global constraints

- **Verification is `make test`**; every node tag builds; `integrations/agent/worker` is
  `//go:build agent`.
- **Proto first, fields only, next free oneof numbers**, regenerated with
  `scripts/dev/proto-gen.sh`; the cockpit reaches it through its pin after PR 1 merges.
- **A cross-node hop is tested in process** (`integrations/agent/worker/forward_hop_test.go`
  is the model; a live-cluster gate is skipped everywhere and gates nothing).
- **The install line is ONE physical line** (`clients/os/src/apps/fleet/addMachine/install.ts`):
  the `--inference` flag joins it; three surfaces print it and one function builds it.
- **`refreshWorkerRegistration` keeps `operatorLabels` and `displayName` absent** from its
  overwrite list; nothing this plan adds there.
- **`fleetModel` attributes are a UNION today; `params` is a MAX and `quant` a SET**, and
  the concept doc says so.
- **A new DSL construct fans out** (memqllint, sdk-gen, arch-model); OS tests run from
  `clients/os`; acceptance for OS surfaces is screenshots in both modes.
- **Stage by explicit path; commit trailers; no emojis; `example.com` hostnames.**

---

## Task 0: file the epic and its tasks

**DONE on 2026-09-06:** epic #5103; tasks #5104 (PR 1), #5105 (PR 2); the cockpit half is znasllc-io/memql-cockpit#387. Do not file them again; verify with `gh issue list --repo znasllc-io/memql --label epic:inference-machine-setup`.

Label `epic:inference-machine-setup`; epic issue (`epic`, label, `feature`, `engine`,
`claude`) linking the cockpit epic. Task issues:

| Title | PR | Labels |
|---|---|---|
| `ModelPull*` on the worker stream, the forward hop, `fleetModelPull`, `fleetModel.params` and `.quant` | 1 of 2 | task, engine, claude |
| Fleet app: the local-models checkbox and `--inference`, the Models group with Pull and progress, the Readiness link, screenshots, docs | 2 of 2 | task, area/portal, claude |

---

# PR 1: the wire and the builtin

## Task 1: `ModelPull*`, the hop, the builtin, the attributes

**Files:**
- Modify: `component/grpc/worker.proto` (server-to-worker `ModelPullStart{registration_id,
  model, request_id}` at the next free oneof number; worker-to-server
  `ModelPullProgress{request_id, model, completed_bytes, total_bytes, status}` and
  `ModelPullEnd{request_id, model, ok, error}`), regenerate
- Modify: `component/worker/registry.go` and the stream handler (route a pull to the
  replica holding the stream; `WorkerForward*` precedent), `integrations/agent/worker/`
  (`model_pull.go`: `PullModel(ctx, ownerUserId, registrationId, model, onProgress)`,
  refusing when the caller is not the registration's owner)
- Create: `integrations/agent/worker/model_pull_hop_test.go` (the in-process forward hop)
- Modify: `dsl/worker/builtins.memql` (`fleetModelPull { registrationId string!; model
  string! }` with `@sdk`, executor `integration.worker.modelPull`, streaming progress as
  rows on a virtual `v1:worker:modelPullProgress`), `dsl/platform/concepts.memql`
  (`fleetModel.params int`, `.quant []string`), `component/memql/fleet_catalog_read.go`
  (MAX and SET reduction), `component/memql/fleet_provider.go` (`FleetModel.Params`,
  `.Quant`)

- [ ] **Step 1: Tests first** — proto round trip; the hop test (a pull started on the
  replica that does not hold the stream reaches the one that does and its progress comes
  back); `fleetModelPull` refuses a non-owner (`not_your_machine`) and a machine offline
  (`machine_offline`); the reduction over two machines with different `quant` and one
  with `params` absent.
- [ ] **Step 2: Implement, regenerate, run, commit, push, PR 1**

Run: `scripts/dev/proto-gen.sh && go run ./cmd/memqllint dsl/ && make sdk-gen && make sdk-gen-check && go test -tags agent -count=1 ./integrations/agent/worker/ -run 'Pull|Hop' -v && make test 2>&1 | tail -5 && make arch-model-check`

```bash
git add component/grpc/worker.proto component/grpc/gen component/worker integrations/agent/worker dsl/worker/builtins.memql dsl/platform/concepts.memql component/memql/fleet_catalog_read.go component/memql/fleet_provider.go sdk/go/client sdk/ts/src/client
git commit -m "fleet: the engine can ask a machine to pull a model and watch it; models carry size and quantization"
git push -u origin epic/inference-machine-setup
```

---

# PR 2: the Fleet app (branch from `main` after cockpit PR 2 merges)

## Task 2: the checkbox, the flag, the Models group, the Pull act, the link

**Files:**
- Modify: `clients/os/src/apps/fleet/addMachine/AddMachine.tsx` (second checkbox "This
  machine will run local models"), `addMachine/install.ts` (`inference: boolean` appends
  ` --inference`; the one-line test extended), the runbook and any other surface that
  prints the line (the file's header names them)
- Create: `clients/os/src/apps/fleet/machines/ModelsGroup.tsx` (runtime, each model with
  params, quant, context window, capabilities, allowed or blocked from the labels; the
  Pull act for the owner with an `Input` for the model id and live progress from a
  `fleetModelPull` reading), `models.ts` (pure: labels to rows; `formatParams` renders
  `8B`, `70B`)
- Modify: `machines/MachineDetail.tsx` (the group below Apps), `rows.ts` (`MachineRow.models`)
- Modify: `clients/os/src/apps/cluster/readiness/ReadinessSection.tsx` (the fleet route
  names "Fleet, Add machine, with local models" and, where the shell is present, opens
  it)
- Tests: `test/fleet/addMachine.test.tsx` (the line with and without the flag, one
  physical line), `test/fleet/models.test.ts` (labels to rows), `test/fleet/machineDetail.test.tsx`
  (the group; the Pull act absent for a non-owner)
- Docs: `docs/public/operate/local-models.md` (the OS path), the visual-QA note

- [ ] **Step 1: Tests first**, then the components, then screenshots in both modes
  (Add machine with the checkbox, a machine detail with two models, a pull in progress).
- [ ] **Step 2: Run, commit, push, PR 2**

Run: `cd clients/os && npm run typecheck && npx vitest run test/fleet test/cluster && npm run build`

```bash
git add clients/os/src/apps/fleet clients/os/src/apps/cluster/readiness/ReadinessSection.tsx clients/os/test/fleet docs/public/operate/local-models.md docs/internal/ops/
git commit -m "fleet app: a machine can be set up for local models from the one-line install, and pulled to from its page"
git push -u origin epic/inference-machine-setup-2
```

Closes the PR 2 task issue and the epic; delete this plan in it.

---

## Plan self-review

**Spec coverage.** D3: Task 1 (the wire and the hop) and Task 2 (the act). D4: Task 1's
reduction. D5: Task 2. Section 4.2's four items map to Task 1, Task 1, Task 2, Task 2.

**Placeholders.** None: the proto field names are stated; the oneof numbers are the next
free ones read from the file.

**Type consistency.** `fleetModelPull`'s args match `PullModel`'s; `FleetModel.Params`
is the `int64` the cockpit label carries; `install.ts`'s `inference` flag is the string
the installers parse (`--inference`).
