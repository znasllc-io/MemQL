# Configuration Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The engine knows, per module and cluster-wide, whether the thing behind an app is
configured, partly configured or not configured at all; the OS shows an unconfigured app a
quiet setup surface instead of a wall of refusals, marks Settings while a person is needed,
and points at the one place each module is configured.

**Architecture:** A `modules` block in the env manifest declares lanes over registry entries;
every node evaluates every module at boot and on a change and writes one
`v1:platform:moduleReadiness` row per module as a new version of a deterministic id; a pure
fold in `component/readiness` turns the live rows into one verdict per module, exposed as the
`moduleReadiness` builtin and mirrored in TypeScript over a live feed the shell retains once.
The OS manifest gains `requires` and `wants`, the window frame renders a setup surface and
two dot tones, and each app's Settings section opens with a Set up group.

**Tech Stack:** Go 1.26 (root module and `component/memql`), MemQL DSL, React 18 + Vite +
vitest (`clients/os`), the generated Go and TS SDKs.

**Spec:** `docs/superpowers/specs/2026-09-06-configuration-readiness-design.md`. Read
sections 4 to 8 first; the plan argues from them. Section 3 lists the decisions D1-D8 the
owner made; they are not open.

**Closes:** the epic issue and the task issues Task 0 files. Two PRs: PR 1 (Tasks 1-7,
engine) and PR 2 (Tasks 8-14, OS), PR 2 branched from `main` after PR 1 merges.

---

## Global constraints

- **Verification is `make test`, never `go test ./...`** -- a relative pattern misses
  `component/memql`, `component/database` and `component/language`. Database-gated work runs
  with `MEMQL_REQUIRE_DB=1` and
  `MEMQL_DATABASE_DSN='postgres://memql:memql_dev@localhost:15434/memql?sslmode=disable'`
  (the shared throwaway Postgres; `sslmode=disable` is required). Confirm a db-gated case ran
  with `-v` and `--- PASS`, not `ok`.
- **Generated artifacts, in this order after any DSL change:** `go run ./cmd/memqllint dsl/`,
  `make sdk-gen` (gate `make sdk-gen-check`), `make env-registry-sync` then
  `make env-registry-check` after any manifest edit, `make arch-model` after any Go package
  add (gate `make arch-model-check`).
- **A `@serverOnly` construct executed from Go must run under
  `auth.ContextWithInternalOrigin`** or every call is refused with one WARN and nothing else.
  Only allowlisted packages may stamp it (`call_origin_conformance_test.go`); `component/memql`
  is allowlisted, so the writer lives there and `component/readiness` stays pure.
- **`component/readiness` is a package of the ROOT module, with no `go.mod`** (the precedent
  is `component/campaigns`, `component/logstore`, `component/packages`). A nested module trips
  twelve gates; a new cross-module import reds `module-boundaries`, which runs `GOWORK=off`.
- **A builtin's reply is one id-keyed map:** every `memorynodes.MemoryNode` a builtin returns
  needs a distinct non-empty `ID`, or rows are silently dropped. `sdk/ts`'s `Result.rows()`
  unwraps that map; the OS harnesses must answer builtins with an id-keyed map of nodes, not
  `{ data: [...] }`.
- **Only a shape reaches a client.** A concept field no shape projects is absent from every
  row with no error. Every field of `moduleReadiness` is in `moduleReadinessFull`.
- **Concept and construct names are unique tree-wide** (flat registries, first registration
  wins silently). `moduleReadiness`, `moduleVerdict`, `moduleReadinessAll`,
  `recordModuleReadiness`, `readinessRecompute` are free as of 2026-09-06; re-check with
  `grep -rn "<name>" dsl/` before adding each.
- **Reserved payload names** (`id`, `createdAt`, `createdBy`, `concept`, `partition`,
  `payload`, `schema`, `type`, `provenance`, `row`, `actor`, `args`, `now`, `config`,
  `trace`, `meta`) drop the WHOLE concept silently. None of this plan's fields uses one.
- **OS tests run from `clients/os`:** `cd /home/znas/memql-projects/memql/clients/os && npx
  vitest run test/<dir>` in ONE command, or `make os-test` from the root. A `FAIL` line
  reading `clients/os/test/...` means the wrong config ran.
- **Stage files by explicit path**, never `git add -A`. Commit messages end with the
  `Co-Authored-By` and `Claude-Session` trailers the session was given.
- No emojis. Hostnames in docs and comments are `example.com` or `<domain>`.
- **Copy voice:** an empty or unconfigured surface names the next move, never the failure;
  sentence case; no apology; the module's manifest `description` is rendered verbatim.

---

## Deviations from the spec, decided while planning

Each is a refinement of an approved decision, not a reversal; the spec is amended in Task 7.

1. **`email` is an evaluator, not manifest lanes.** The SMTP lane's variables
   (`SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, plus the from-address pair) are
   NOT env-registry entries, so a lane naming them cannot satisfy "every slot names an entry".
   Declaring only the Graph lane would report an SMTP-configured cluster as unconfigured.
   Instead the module declares `evaluator: integration:email` and the engine asks the email
   integration's own status capability in-process, which already knows both lanes. The
   parity test the spec asked for disappears with the duplicate it was guarding.
2. **`hostedBy` decides `notApplicable`.** A module may name the integrations or node types
   that host it; a node not matching reports `notApplicable`. A module naming neither is
   evaluated by every node.
3. **`optionalSlots` on a lane.** Present slots count toward "touched", not toward
   completeness.
4. **The email configure hook is a hidden builtin.** `integration.email.configure` runs
   `builtin readinessRecompute()` through the `ConfigWriter` it already has; the builtin
   carries no `@sdk` and refuses any origin that is not internal or a cluster owner. No new
   interface crosses the plug-in boundary.
5. **Node liveness is a window plus a health set:** `NodeLiveWindow = 60s` in Go,
   `NODE_LIVE_WINDOW_SECONDS = 60` in TS, pinned by a regexp parity test the way the fleet
   online window is; live health is `healthy`, `connecting`, `degraded`, `draining`.
6. **The Set up group's act uses `useOsIfPresent()`** and renders the words `Settings, under
   AI providers` when the context is absent, when the actor's role cannot reach the target
   section (`providers` is owner-only, the group is owner-or-developer), or on the phone
   layout, which carries no windows. `OsContextValue` gains `layout` for that.
7. **The dot type widens in the kit**, as `DotTone = ProvenanceTone | "needsSetup" |
   "partlySetUp"`; `ProvenanceTone` in `items/provenance.ts` is left alone.
8. **Row id:** `v1:platform:moduleReadiness:<module>--<nodeId>`.

---

## The shape of the epic

| Layer | Where | What it holds |
|---|---|---|
| Declaration | `scripts/secrets/manifest.yaml` `modules:`; `component/envregistry/modules.go` | Modules, lanes, evaluators, hostedBy; strict decode and validation |
| Decision | `component/readiness/` (root module) | The fold and node liveness, pure; JSON fixtures both sides run |
| Evaluation and rows | `component/memql/readiness_eval.go`, `readiness_write.go`, `readiness_read.go` | Per-node evaluation, the `@serverOnly` write under internal origin, the two builtins |
| DSL | `dsl/platform/{concepts,shapes,queries,mutations,builtins}.memql` | `moduleReadiness`, `moduleVerdict`, `moduleReadinessFull`, `moduleReadinessAll`, `recordModuleReadiness`, `moduleReadiness`, `readinessRecompute` |
| Routing | `component/node/routing.go` | Two broadcast rules |
| Hooks | `app/run.go`, `component/memql/provider_reload_propagate.go`, `integrations/email/configure.go` | Boot, reload, configure |
| Shell | `clients/os/src/system/{modules,readinessFold,registry}.ts`, `src/live/readiness.tsx`, `src/chrome/{access,Shell,state,WindowFrame,PhoneShell}.tsx` | Manifest verbs, the feed and fold, the gate |
| Kit | `clients/os/src/kit/{ReadinessStates.tsx,index.tsx}`, `src/styles/index.css` | `SurfaceUnconfigured`, `SetupGroup`, two dot tones |
| Apps | `clients/os/src/apps/*/settings.ts`, `*App.tsx`, `apps/registry.tsx`, `apps/cluster/modules/` | Requirements, Set up groups, the Modules column |

---

## Task 0: file the epic and its tasks

**DONE on 2026-09-06:** epic #5077; tasks #5078 (manifest), #5079 (fold), #5080 (engine
concept, evaluation, rows, builtins, routing, hooks), #5081 (db-gated tests, spec
amendment), #5082, #5083, #5084 (the three OS tasks). Do not file them again; verify with
`gh issue list --repo znasllc-io/memql --label epic:configuration-readiness`. Tasks 1 to 3
of PR 1 are implemented and reviewed on branch `epic/configuration-readiness` (commits
through `38298841d`); a resuming session starts at Task 4 and reads the SDD ledger
described in the program record.

**Files:** none in the tree. GitHub only.

- [ ] **Step 1: Create the epic label and the epic issue**

```bash
gh label create "epic:configuration-readiness" --repo znasllc-io/memql --color 5319e7 --force
gh issue create --repo znasllc-io/memql \
  --title "Epic: configuration readiness -- the platform knows what is set up, and the OS says so" \
  --label epic --label "epic:configuration-readiness" --label feature --label engine --label claude \
  --body-file - <<'BODY'
Per-module readiness verdicts computed on the engine from `modules` lanes declared in the
env manifest, reported by every node as `v1:platform:moduleReadiness` rows and folded on
read; the OS gates unconfigured apps behind a setup surface, marks Settings while a person is
needed, and points at the one place each module is configured.

Design record: docs/superpowers/specs/2026-09-06-configuration-readiness-design.md
Plan: docs/superpowers/plans/2026-09-06-configuration-readiness.md

Ships in TWO PRs. PR 1 (engine) closes the first four tasks; PR 2 (OS) closes the rest and
branches from main after PR 1 merges. All issues `claude`-labeled.
BODY
```

- [ ] **Step 2: Create the task issues, four for PR 1 and three for PR 2**

Run once per row, with `EPIC` set to the number Step 1 printed. Body opens with
`Part of #$EPIC.` and names its PR.

| Title | PR |
|---|---|
| Env manifest: the `modules` block, strict decode, validation, sync gate | 1 of 2 |
| `component/readiness`: the fold, node liveness, shared fixtures | 1 of 2 |
| Engine: `moduleReadiness` concept, per-node evaluation and rows, the two builtins, routing, hooks | 1 of 2 |
| Engine: db-gated writer and fold tests; spec amendment | 1 of 2 |
| OS: manifest verbs, the readiness feed and fold mirror, contract and parity tests | 2 of 2 |
| OS: setup surface, dot tones, Set up group, both renderers, reactivity test | 2 of 2 |
| OS: app requirements, Modules column, screenshots, operator doc | 2 of 2 |

```bash
gh issue create --repo znasllc-io/memql --title "<title>" \
  --label task --label "epic:configuration-readiness" --label claude --label engine \
  --body "Part of #$EPIC. Ships in PR <n> of 2. Plan: docs/superpowers/plans/2026-09-06-configuration-readiness.md"
```

Use `--label area/portal` instead of `engine` for the three OS rows. Record the seven numbers
in the PR bodies as `Closes #a, Closes #b, ...` (one `Closes` per issue; a comma list links
only the first).

---

## Task 1: the `modules` block in the env manifest

**Files:**
- Modify: `scripts/secrets/manifest.yaml` (append at end of file)
- Modify: `component/envregistry/manifest.go:69-76` (`Manifest` struct)
- Create: `component/envregistry/modules.go`
- Create: `component/envregistry/modules_test.go`
- Modify: `component/envregistry/embedded_manifest_sync_test.go:38-62`
- Modify: `main.go` (beside the `envregistry.MissingRequiredError` boot check)
- Regenerate: `component/envregistry/manifest.yaml` via `make env-registry-sync`

**Interfaces:**
- Produces: `envregistry.Module{Name, Core, Description, Evaluator, HostedBy, Lanes}`,
  `envregistry.Lane{Name, ConfigurableFrom, Slots, OptionalSlots}`,
  `envregistry.HostedBy{Integrations, NodeTypes}`, `Manifest.Modules []Module`,
  `Manifest.IsSecret(name) bool`, `Manifest.ValidateModules() error`,
  `DecodeModulesStrict([]byte) ([]Module, error)`, constants `ConfigurableFromOS`,
  `ConfigurableFromDeployment`, `EvaluatorInferenceStatus`, `EvaluatorIntegrationPrefix`.

- [ ] **Step 1: Write the failing tests**

`component/envregistry/modules_test.go`:

```go
package envregistry

import (
	"strings"
	"testing"
)

func TestEmbeddedModulesDecodeStrictlyAndValidate(t *testing.T) {
	mods, err := DecodeModulesStrict(embeddedManifest)
	if err != nil {
		t.Fatalf("embedded modules do not decode strictly: %v", err)
	}
	if len(mods) == 0 {
		t.Fatal("the embedded manifest declares no modules; the block is missing, not empty")
	}
	m, err := LoadManifestFromBytes(embeddedManifest, "embedded")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateModules(); err != nil {
		t.Fatalf("embedded modules do not validate: %v", err)
	}
	core := map[string]bool{}
	for _, mod := range m.Modules {
		if mod.Core {
			core[mod.Name] = true
		}
	}
	for _, want := range []string{"ai", "storage", "email"} {
		if !core[want] {
			t.Errorf("module %q must be core (design record section 4.2)", want)
		}
	}
}

func TestModulesRefuseAnUnknownKey(t *testing.T) {
	doc := []byte("secrets: []\nvariables: []\nmodules:\n  - name: x\n    description: d\n    lane: oops\n")
	if _, err := DecodeModulesStrict(doc); err == nil || !strings.Contains(err.Error(), "lane") {
		t.Fatalf("an unknown key inside a module must refuse decode naming the key, got %v", err)
	}
}

func TestModulesRefuseAnUnknownSlot(t *testing.T) {
	doc := []byte("secrets: []\nvariables:\n  - name: MEMQL_KNOWN\n    scope: node\nmodules:\n" +
		"  - name: x\n    description: d\n    lanes:\n      - name: l\n        configurableFrom: os\n        slots: [MEMQL_KNOWN, MEMQL_TYPO]\n")
	m, err := LoadManifestFromBytes(doc, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	err = m.ValidateModules()
	if err == nil || !strings.Contains(err.Error(), "MEMQL_TYPO") {
		t.Fatalf("a slot naming no entry must fail validation naming the slot, got %v", err)
	}
}

func TestModulesRefuseLanesAndEvaluatorTogether(t *testing.T) {
	doc := []byte("secrets: []\nvariables:\n  - name: MEMQL_KNOWN\n    scope: node\nmodules:\n" +
		"  - name: x\n    description: d\n    evaluator: inferenceStatus\n    lanes:\n      - name: l\n        configurableFrom: os\n        slots: [MEMQL_KNOWN]\n")
	m, _ := LoadManifestFromBytes(doc, "fixture")
	if err := m.ValidateModules(); err == nil {
		t.Fatal("a module with both lanes and an evaluator must fail validation")
	}
	doc = []byte("secrets: []\nvariables: []\nmodules:\n  - name: x\n    description: d\n")
	m, _ = LoadManifestFromBytes(doc, "fixture")
	if err := m.ValidateModules(); err == nil {
		t.Fatal("a module with neither lanes nor an evaluator must fail validation")
	}
}

func TestModulesRefuseAnUnknownEvaluatorOrConfigurableFrom(t *testing.T) {
	doc := []byte("secrets: []\nvariables: []\nmodules:\n  - name: x\n    description: d\n    evaluator: magic\n")
	m, _ := LoadManifestFromBytes(doc, "fixture")
	if err := m.ValidateModules(); err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("unknown evaluator must be refused by name, got %v", err)
	}
	doc = []byte("secrets: []\nvariables:\n  - name: MEMQL_KNOWN\n    scope: node\nmodules:\n" +
		"  - name: x\n    description: d\n    lanes:\n      - name: l\n        configurableFrom: elsewhere\n        slots: [MEMQL_KNOWN]\n")
	m, _ = LoadManifestFromBytes(doc, "fixture")
	if err := m.ValidateModules(); err == nil || !strings.Contains(err.Error(), "elsewhere") {
		t.Fatalf("configurableFrom outside {os, deployment} must be refused by value, got %v", err)
	}
}

func TestIsSecretDistinguishesTheTwoLists(t *testing.T) {
	doc := []byte("secrets:\n  - name: MEMQL_S\n    scope: node\nvariables:\n  - name: MEMQL_V\n    scope: node\n")
	m, _ := LoadManifestFromBytes(doc, "fixture")
	if !m.IsSecret("MEMQL_S") || m.IsSecret("MEMQL_V") || m.IsSecret("MEMQL_NONE") {
		t.Fatal("IsSecret must be true for a secrets entry only")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -count=1 ./component/envregistry/ -run 'TestEmbeddedModules|TestModules|TestIsSecret' -v`
Expected: compile failure on `DecodeModulesStrict`, `ValidateModules`, `IsSecret`.

- [ ] **Step 3: Add the declaration to the authored manifest**

Append to `scripts/secrets/manifest.yaml`:

```yaml

# --- readiness modules (design record 2026-09-06-configuration-readiness, section 4.2) ---
#
# A module is a named set of lanes over the entries above, or a named evaluator. A module
# is configured when any one lane has every slot present, partial when some slot is present
# and no lane is complete, unconfigured when none is. Presence, never a value. `core` marks
# the modules the first-run wizard walks. `hostedBy` names the nodes that report a verdict;
# a node not matching reports notApplicable. `configurableFrom` is what the Set up group
# offers: a button into the OS surface that writes it, or the variable names to set in the
# deployment. Decoded STRICTLY (component/envregistry/modules.go): an unknown key refuses
# boot, and every slot must name an entry above.
modules:
  - name: ai
    core: true
    description: "Inference needs a provider: a machine on your fleet serving a model, or a federated cloud vendor."
    evaluator: inferenceStatus
  - name: storage
    core: true
    description: "Files, materialized outputs, deploy bundles and log archives live in blob storage."
    hostedBy:
      integrations: [storage]
    lanes:
      - name: azure-blob
        configurableFrom: deployment
        slots: [MEMQL_AZURE_BLOB_CONTAINER, MEMQL_AZURE_STORAGE_CONNECTION_STRING]
  - name: email
    core: true
    description: "Sending mail needs a mailbox this cluster can send from."
    evaluator: "integration:email"
  - name: githubApp
    description: "Connecting a source through the GitHub App needs the app registered on the identity node."
    hostedBy:
      nodeTypes: [identity]
    lanes:
      - name: app
        configurableFrom: deployment
        slots:
          - MEMQL_GITHUB_APP_ID
          - MEMQL_GITHUB_APP_SLUG
          - MEMQL_GITHUB_APP_CLIENT_ID
          - MEMQL_GITHUB_APP_CLIENT_SECRET
          - MEMQL_GITHUB_APP_PRIVATE_KEY_B64
          - MEMQL_GITHUB_APP_WEBHOOK_SECRET
  - name: campaigns
    description: "Sending a campaign needs a one-click unsubscribe secret and a reachable unsubscribe address."
    hostedBy:
      nodeTypes: [bff]
    lanes:
      - name: unsubscribe
        configurableFrom: deployment
        slots: [MEMQL_CAMPAIGNS_UNSUBSCRIBE_SECRET, MEMQL_CAMPAIGNS_UNSUBSCRIBE_BASE_URL]
  - name: workbench
    description: "Workbenches need a workbench node this agent can reach."
    hostedBy:
      nodeTypes: [agent]
    lanes:
      - name: remote
        configurableFrom: deployment
        slots: [MEMQL_WORKBENCH_REMOTE, MEMQL_WORKER_PEERS]
  - name: localApps
    description: "Running a task in Claude Code or Codex on your machine needs the agent to mint a session credential."
    hostedBy:
      nodeTypes: [agent]
    lanes:
      - name: default
        configurableFrom: deployment
        slots: [MEMQL_MCP_PUBLIC_URL, MEMQL_NODE_BOOTSTRAP_TOKEN, MEMQL_IDENTITY_VERIFIER_BASE_URL]
```

Every slot above is an existing entry; `grep -n "name: <SLOT>" scripts/secrets/manifest.yaml`
confirms each before moving on.

- [ ] **Step 4: Add the types, the strict decoder and the validator**

In `component/envregistry/manifest.go`, add one field to `Manifest`:

```go
type Manifest struct {
	Secrets   []ManifestEntry `yaml:"secrets"`
	Variables []ManifestEntry `yaml:"variables"`
	// Modules are the readiness modules (design record
	// docs/superpowers/specs/2026-09-06-configuration-readiness-design.md, section 4.2).
	// Decoded leniently here like everything else; DecodeModulesStrict is the gate.
	Modules []Module `yaml:"modules,omitempty"`
	Source  string   `yaml:"-"`
}
```

Create `component/envregistry/modules.go`:

```go
package envregistry

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// A readiness module: a named set of lanes over registry entries, or a named
// evaluator (design record 2026-09-06-configuration-readiness, section 4.2).
type Module struct {
	Name        string   `yaml:"name"`
	Core        bool     `yaml:"core,omitempty"`
	Description string   `yaml:"description"`
	Evaluator   string   `yaml:"evaluator,omitempty"`
	HostedBy    HostedBy `yaml:"hostedBy,omitempty"`
	Lanes       []Lane   `yaml:"lanes,omitempty"`
}

// HostedBy names the nodes that report a verdict for a module. Both lists
// empty means every node evaluates it.
type HostedBy struct {
	Integrations []string `yaml:"integrations,omitempty"`
	NodeTypes    []string `yaml:"nodeTypes,omitempty"`
}

// Lane is one way to configure a module: complete when every slot is present.
type Lane struct {
	Name             string   `yaml:"name"`
	ConfigurableFrom string   `yaml:"configurableFrom"`
	Slots            []string `yaml:"slots"`
	OptionalSlots    []string `yaml:"optionalSlots,omitempty"`
}

const (
	ConfigurableFromOS         = "os"
	ConfigurableFromDeployment = "deployment"
	// EvaluatorInferenceStatus asks the provider registry (any door open).
	EvaluatorInferenceStatus = "inferenceStatus"
	// EvaluatorIntegrationPrefix asks integration.<name>.status in-process.
	EvaluatorIntegrationPrefix = "integration:"
)

// Everywhere reports whether the module carries no hostedBy restriction.
func (h HostedBy) Everywhere() bool {
	return len(h.Integrations) == 0 && len(h.NodeTypes) == 0
}

// strictDocument decodes the whole file with unknown keys REFUSED inside the
// modules block. The entry lists are typed as maps so their own keys are not
// judged here; LoadManifestFromBytes stays lenient for them, which is the
// unshipped C5 of the integration-config record, deliberately not widened by
// this file.
type strictDocument struct {
	Secrets   []map[string]any `yaml:"secrets"`
	Variables []map[string]any `yaml:"variables"`
	Modules   []Module         `yaml:"modules"`
}

// DecodeModulesStrict decodes the modules block refusing unknown keys.
func DecodeModulesStrict(data []byte) ([]Module, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc strictDocument
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("manifest modules: %w", err)
	}
	return doc.Modules, nil
}

// IsSecret reports whether name is declared under `secrets`.
func (m *Manifest) IsSecret(name string) bool {
	for _, e := range m.Secrets {
		if e.Name == name {
			return true
		}
	}
	return false
}

// Module returns the declared module by name.
func (m *Manifest) Module(name string) (Module, bool) {
	for _, mod := range m.Modules {
		if mod.Name == name {
			return mod, true
		}
	}
	return Module{}, false
}

// ValidateModules refuses a declaration the evaluator could not act on: a
// nameless or duplicate module, a module with both lanes and an evaluator or
// neither, an unknown evaluator, a lane with an unknown configurableFrom, and
// a slot naming no registry entry. The last is the one that matters most: a
// typo in a slot would otherwise read as a module nobody can ever configure.
func (m *Manifest) ValidateModules() error {
	seen := map[string]bool{}
	for _, mod := range m.Modules {
		name := strings.TrimSpace(mod.Name)
		if name == "" {
			return fmt.Errorf("manifest modules: a module has no name")
		}
		if seen[name] {
			return fmt.Errorf("manifest modules: module %q is declared twice", name)
		}
		seen[name] = true
		if strings.TrimSpace(mod.Description) == "" {
			return fmt.Errorf("manifest modules: module %q has no description; the setup surface renders it", name)
		}
		hasEval := strings.TrimSpace(mod.Evaluator) != ""
		if hasEval == (len(mod.Lanes) > 0) {
			return fmt.Errorf("manifest modules: module %q must declare lanes or an evaluator, not both and not neither", name)
		}
		if hasEval && mod.Evaluator != EvaluatorInferenceStatus &&
			!strings.HasPrefix(mod.Evaluator, EvaluatorIntegrationPrefix) {
			return fmt.Errorf("manifest modules: module %q names unknown evaluator %q", name, mod.Evaluator)
		}
		lanes := map[string]bool{}
		for _, lane := range mod.Lanes {
			if strings.TrimSpace(lane.Name) == "" {
				return fmt.Errorf("manifest modules: module %q has a lane with no name", name)
			}
			if lanes[lane.Name] {
				return fmt.Errorf("manifest modules: module %q declares lane %q twice", name, lane.Name)
			}
			lanes[lane.Name] = true
			if lane.ConfigurableFrom != ConfigurableFromOS && lane.ConfigurableFrom != ConfigurableFromDeployment {
				return fmt.Errorf("manifest modules: module %q lane %q: configurableFrom %q is not os or deployment", name, lane.Name, lane.ConfigurableFrom)
			}
			if len(lane.Slots) == 0 {
				return fmt.Errorf("manifest modules: module %q lane %q declares no slots", name, lane.Name)
			}
			slots := map[string]bool{}
			for _, slot := range append(append([]string{}, lane.Slots...), lane.OptionalSlots...) {
				if slots[slot] {
					return fmt.Errorf("manifest modules: module %q lane %q lists slot %q twice", name, lane.Name, slot)
				}
				slots[slot] = true
				if _, ok := m.Lookup(slot); !ok {
					return fmt.Errorf("manifest modules: module %q lane %q names slot %q, which is not a registry entry", name, lane.Name, slot)
				}
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: Extend the sync gate and the boot check**

In `component/envregistry/embedded_manifest_sync_test.go`, after the `Variables`
comparison inside `TestEmbeddedManifestInSync`, add:

```go
	if !reflect.DeepEqual(authored.Modules, embedded.Modules) {
		t.Fatalf("embedded manifest modules are out of sync with the authored file: run `make env-registry-sync`\nauthored=%+v\nembedded=%+v", authored.Modules, embedded.Modules)
	}
```

(add `"reflect"` to the imports if absent). In `main.go`, at the site that calls
`envregistry.MissingRequiredError` at boot, add before it:

```go
	if manifest, err := envregistry.LoadManifest(""); err == nil {
		if _, err := envregistry.DecodeModulesStrict(mustEmbeddedManifest()); err != nil {
			serviceLogger.Error("env manifest modules block does not decode strictly", "error", err)
			os.Exit(1)
		}
		if err := manifest.ValidateModules(); err != nil {
			serviceLogger.Error("env manifest modules block is invalid", "error", err)
			os.Exit(1)
		}
	}
```

where `mustEmbeddedManifest` is a one-line exported accessor added to
`component/envregistry/manifest.go`: `func EmbeddedManifestBytes() []byte { return embeddedManifest }`
(use that name in `main.go`). Match the logger variable name the surrounding code uses.

- [ ] **Step 6: Regenerate the snapshot and run the tests**

Run: `make env-registry-sync && make env-registry-check && go test -count=1 ./component/envregistry/ -v -run 'TestEmbeddedModules|TestModules|TestIsSecret|TestEmbeddedManifestInSync'`
Expected: all PASS; `env-registry-check` reports no drift (the block neither creates nor
suppresses findings: `cmd/envscan` reads only `secrets` and `variables`).

- [ ] **Step 7: Commit**

```bash
git add scripts/secrets/manifest.yaml component/envregistry/manifest.yaml \
  component/envregistry/manifest.go component/envregistry/modules.go \
  component/envregistry/modules_test.go component/envregistry/embedded_manifest_sync_test.go main.go
git commit -m "envregistry: the modules block -- lanes over registry entries, decoded strictly"
```

## Task 2: `component/readiness` -- the fold, node liveness, shared fixtures

**Files:**
- Create: `component/readiness/readiness.go`
- Create: `component/readiness/fold_test.go`
- Create: `component/readiness/testdata/fold/*.json` (seven fixtures, listed in Step 3)

**Interfaces:**
- Produces: `readiness.State` and its five constants; `SlotReport`, `LaneReport`,
  `NodeReport`, `NodeLiveness`, `NodeVerdict`, `Verdict`; `NodeLiveWindow`;
  `NodeIsLive(n NodeLiveness, now time.Time) bool`;
  `Fold(reports []NodeReport, nodes []NodeLiveness, now time.Time) []Verdict`.
- The JSON tags below ARE the wire shape of the `lanes` field and of the fixtures; Task 9's
  TypeScript reads the same files.

- [ ] **Step 1: Write the failing fixture-driven test**

`component/readiness/fold_test.go`:

```go
package readiness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type foldFixture struct {
	Name    string         `json:"name"`
	Now     time.Time      `json:"now"`
	Reports []NodeReport   `json:"reports"`
	Nodes   []NodeLiveness `json:"nodes"`
	Expect  []struct {
		Module       string   `json:"module"`
		State        State    `json:"state"`
		Disagreement []string `json:"disagreement"`
	} `json:"expect"`
}

// THE FIXTURES ARE SHARED. clients/os/test/system/readinessFold.test.ts reads
// the same directory, so a case added here is a case the TypeScript mirror
// must also pass; that is the whole parity mechanism.
func TestFoldFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "fold", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 7 {
		t.Fatalf("expected at least 7 fold fixtures, found %d -- the parity set is incomplete", len(paths))
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fx foldFixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		t.Run(fx.Name, func(t *testing.T) {
			got := Fold(fx.Reports, fx.Nodes, fx.Now)
			if len(got) != len(fx.Expect) {
				t.Fatalf("got %d verdicts, want %d: %+v", len(got), len(fx.Expect), got)
			}
			for i, want := range fx.Expect {
				if got[i].Module != want.Module || got[i].State != want.State {
					t.Errorf("verdict %d: got %s=%s, want %s=%s", i, got[i].Module, got[i].State, want.Module, want.State)
				}
				if !reflect.DeepEqual(got[i].Disagreement, want.Disagreement) {
					t.Errorf("verdict %d disagreement: got %v, want %v", i, got[i].Disagreement, want.Disagreement)
				}
			}
		})
	}
}

func TestNodeIsLiveNeedsBothHealthAndRecency(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-NodeLiveWindow / 2)
	stale := now.Add(-NodeLiveWindow - time.Second)
	cases := []struct {
		n    NodeLiveness
		want bool
	}{
		{NodeLiveness{NodeId: "a", Health: "healthy", LastSeen: fresh}, true},
		{NodeLiveness{NodeId: "a", Health: "draining", LastSeen: fresh}, true},
		{NodeLiveness{NodeId: "a", Health: "stopped", LastSeen: fresh}, false},
		{NodeLiveness{NodeId: "a", Health: "offline", LastSeen: fresh}, false},
		{NodeLiveness{NodeId: "a", Health: "healthy", LastSeen: stale}, false},
		{NodeLiveness{NodeId: "a", Health: "healthy"}, false},
	}
	for _, c := range cases {
		if got := NodeIsLive(c.n, now); got != c.want {
			t.Errorf("%+v: got %v want %v", c.n, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -count=1 ./component/readiness/ -v`
Expected: compile failure (package does not exist).

- [ ] **Step 3: Write the seven fixtures**

All under `component/readiness/testdata/fold/`. Every fixture uses
`"now": "2026-09-06T12:00:00Z"`, fresh nodes at `"lastSeen": "2026-09-06T11:59:40Z"`, and
reports at `"reportedAt": "2026-09-06T11:58:00Z"`. Report objects carry `module`, `nodeId`,
`nodeType`, `state`, `core`, `lanes` (may be `[]`), `reportedAt`. Node objects carry
`nodeId`, `health`, `lastSeen`.

| File | Reports | Nodes | Expect |
|---|---|---|---|
| `01-agree-configured.json` | storage: bff-a configured, bff-b configured | both healthy fresh | storage configured, disagreement [] |
| `02-all-unconfigured.json` | storage: bff-a unconfigured, bff-b unconfigured | both healthy fresh | storage unconfigured, [] |
| `03-disagreement-is-partial.json` | storage: bff-a configured, bff-b unconfigured, agent-a partial | all healthy fresh | storage partial, `["bff-b=unconfigured", "agent-a=partial", "bff-a=configured"]` |
| `04-dead-node-dropped.json` | storage: bff-a configured, bff-z unconfigured | bff-a healthy fresh; bff-z healthy with lastSeen `2026-09-06T11:00:00Z` | storage configured, [] |
| `05-not-applicable-dropped.json` | githubApp: identity-a configured, bff-a notApplicable | both healthy fresh | githubApp configured, [] |
| `06-unreported.json` | localApps: agent-a unconfigured | agent-a health `stopped` fresh | localApps unreported, [] |
| `07-sorted-by-module.json` | storage: bff-a configured; ai: bff-a unconfigured; email: bff-a partial | bff-a healthy fresh | ai unconfigured; email partial; storage configured (that order) |

Example, `03-disagreement-is-partial.json`:

```json
{
  "name": "disagreement folds to partial and names every live node by state",
  "now": "2026-09-06T12:00:00Z",
  "reports": [
    {"module": "storage", "nodeId": "bff-a", "nodeType": "bff", "state": "configured", "core": true, "lanes": [], "reportedAt": "2026-09-06T11:58:00Z"},
    {"module": "storage", "nodeId": "bff-b", "nodeType": "bff", "state": "unconfigured", "core": true, "lanes": [], "reportedAt": "2026-09-06T11:58:00Z"},
    {"module": "storage", "nodeId": "agent-a", "nodeType": "agent", "state": "partial", "core": true, "lanes": [], "reportedAt": "2026-09-06T11:58:00Z"}
  ],
  "nodes": [
    {"nodeId": "bff-a", "health": "healthy", "lastSeen": "2026-09-06T11:59:40Z"},
    {"nodeId": "bff-b", "health": "healthy", "lastSeen": "2026-09-06T11:59:40Z"},
    {"nodeId": "agent-a", "health": "healthy", "lastSeen": "2026-09-06T11:59:40Z"}
  ],
  "expect": [
    {"module": "storage", "state": "partial", "disagreement": ["bff-b=unconfigured", "agent-a=partial", "bff-a=configured"]}
  ]
}
```

Disagreement order is by state, worst first, then by node id within a state.

- [ ] **Step 4: Write the package**

`component/readiness/readiness.go`:

```go
// Package readiness is the pure decision layer of configuration readiness
// (design record docs/superpowers/specs/2026-09-06-configuration-readiness-design.md,
// section 4.5): values in, verdicts out, no engine, no database, no provider.
//
// It is a package of the ROOT module on purpose. A nested module would trip a
// dozen gates, and the one importer that must reach it -- component/memql,
// where the writer lives because only allowlisted packages may stamp internal
// origin -- already depends on the root.
package readiness

import (
	"sort"
	"time"
)

// State is one node's, or the fold's, verdict on one module.
type State string

const (
	Configured    State = "configured"
	Partial       State = "partial"
	Unconfigured  State = "unconfigured"
	NotApplicable State = "notApplicable"
	// Unreported is the fold's word for "no live node reported this module".
	// It is never spelled Unconfigured: not knowing and not being configured
	// are different answers, and the OS draws nothing for this one.
	Unreported State = "unreported"
)

// SlotReport is one registry entry's presence and source. Never a value.
type SlotReport struct {
	Name     string `json:"name"`
	Present  bool   `json:"present"`
	Source   string `json:"source"`
	Optional bool   `json:"optional,omitempty"`
}

// LaneReport is one lane's completeness on one node.
type LaneReport struct {
	Name             string       `json:"name"`
	ConfigurableFrom string       `json:"configurableFrom"`
	Complete         bool         `json:"complete"`
	Slots            []SlotReport `json:"slots"`
}

// NodeReport is one row of v1:platform:moduleReadiness.
type NodeReport struct {
	Module     string       `json:"module"`
	NodeId     string       `json:"nodeId"`
	NodeType   string       `json:"nodeType"`
	State      State        `json:"state"`
	Core       bool         `json:"core"`
	Lanes      []LaneReport `json:"lanes"`
	ReportedAt time.Time    `json:"reportedAt"`
}

// NodeLiveness is what the fold needs from a v1:cluster:node row.
type NodeLiveness struct {
	NodeId   string    `json:"nodeId"`
	Health   string    `json:"health"`
	LastSeen time.Time `json:"lastSeen"`
}

// NodeVerdict is one live reporter's contribution to a Verdict.
type NodeVerdict struct {
	NodeId     string    `json:"nodeId"`
	NodeType   string    `json:"nodeType"`
	State      State     `json:"state"`
	ReportedAt time.Time `json:"reportedAt"`
}

// Verdict is the cluster-wide answer for one module.
type Verdict struct {
	Module       string        `json:"module"`
	State        State         `json:"state"`
	Core         bool          `json:"core"`
	Disagreement []string      `json:"disagreement"`
	Nodes        []NodeVerdict `json:"nodes"`
}

// NodeLiveWindow is how recently a cluster node must have been seen for its
// report to count. Three times the reconciler's 20s grace, so a slow
// heartbeat does not flicker a verdict. MIRRORED as NODE_LIVE_WINDOW_SECONDS
// in clients/os/src/system/readinessFold.ts and pinned by
// TestNodeLiveWindowMatchesTheClient in this package.
const NodeLiveWindow = 60 * time.Second

var liveHealth = map[string]bool{
	"healthy":    true,
	"connecting": true,
	"degraded":   true,
	"draining":   true,
}

// NodeIsLive reports whether a node's report may count: a live health word
// and a heartbeat inside the window. A zero LastSeen is never live.
func NodeIsLive(n NodeLiveness, now time.Time) bool {
	if !liveHealth[n.Health] || n.LastSeen.IsZero() {
		return false
	}
	return now.Sub(n.LastSeen) <= NodeLiveWindow
}

func rank(s State) int {
	switch s {
	case Unconfigured:
		return 2
	case Partial:
		return 1
	default:
		return 0
	}
}

// Fold turns every node's report into one verdict per module.
//
//  1. Keep reports from live nodes only, so a dead replica's stale row cannot
//     pin a verdict.
//  2. Drop notApplicable.
//  3. Worst state wins.
//  4. If the kept reports disagree, the verdict is partial and Disagreement
//     names every live reporter as nodeId=state, worst state first.
//  5. A module with no kept report is unreported.
//
// Modules appear in name order, so two folds over the same rows are equal.
func Fold(reports []NodeReport, nodes []NodeLiveness, now time.Time) []Verdict {
	live := map[string]bool{}
	for _, n := range nodes {
		if NodeIsLive(n, now) {
			live[n.NodeId] = true
		}
	}
	kept := map[string][]NodeReport{}
	core := map[string]bool{}
	seen := map[string]bool{}
	var order []string
	for _, r := range reports {
		if !seen[r.Module] {
			seen[r.Module] = true
			order = append(order, r.Module)
		}
		core[r.Module] = core[r.Module] || r.Core
		if !live[r.NodeId] || r.State == NotApplicable {
			continue
		}
		kept[r.Module] = append(kept[r.Module], r)
	}
	sort.Strings(order)
	out := make([]Verdict, 0, len(order))
	for _, module := range order {
		v := Verdict{Module: module, Core: core[module], Disagreement: []string{}, Nodes: []NodeVerdict{}}
		rs := kept[module]
		if len(rs) == 0 {
			v.State = Unreported
			out = append(out, v)
			continue
		}
		sort.Slice(rs, func(i, j int) bool {
			if rank(rs[i].State) != rank(rs[j].State) {
				return rank(rs[i].State) > rank(rs[j].State)
			}
			return rs[i].NodeId < rs[j].NodeId
		})
		states := map[State]bool{}
		for _, r := range rs {
			states[r.State] = true
			v.Nodes = append(v.Nodes, NodeVerdict{NodeId: r.NodeId, NodeType: r.NodeType, State: r.State, ReportedAt: r.ReportedAt})
		}
		if len(states) > 1 {
			v.State = Partial
			for _, r := range rs {
				v.Disagreement = append(v.Disagreement, r.NodeId+"="+string(r.State))
			}
		} else {
			v.State = rs[0].State
		}
		out = append(out, v)
	}
	return out
}
```

- [ ] **Step 5: Run the tests**

Run: `go test -count=1 ./component/readiness/ -v`
Expected: `TestFoldFixtures` with seven subtests PASS, `TestNodeIsLiveNeedsBothHealthAndRecency` PASS.

- [ ] **Step 6: Commit**

```bash
git add component/readiness/
git commit -m "readiness: the fold -- values in, one verdict per module out, fixtures both sides run"
```

---

## Task 3: the DSL and its generated artifacts

**Files:**
- Modify: `dsl/platform/concepts.memql` (append two concepts)
- Modify: `dsl/platform/shapes.memql` (append one shape)
- Modify: `dsl/platform/queries.memql` (append one query)
- Modify: `dsl/platform/mutations.memql` (append one mutation)
- Modify: `dsl/platform/builtins.memql` (append two builtins)
- Modify: `component/memql/engine_types.go` (constants)
- Modify: `component/memql/executor_builtin.go:17-119` (two handler entries)
- Modify: `component/node/routing.go:140-175` (two rules)
- Create: `component/node/routing_readiness_test.go`
- Regenerate: `sdk/go/client/generated_*.go`, `sdk/ts/src/client/generated_*.ts`

**Interfaces:**
- Produces: concept ids `v1:platform:moduleReadiness` and `v1:platform:moduleVerdict`; the
  query `moduleReadinessAll()`; the mutation `recordModuleReadiness(rowId, module, nodeId,
  nodeType, state, core, lanes, reportedAt)`; the builtins `moduleReadiness()` (on the SDK
  wire) and `readinessRecompute()` (not on the wire); Go constants
  `ModuleReadinessConcept`, `ModuleVerdictConcept`, `BuiltinExecutorModuleReadiness`,
  `BuiltinExecutorReadinessRecompute`; the SDK methods
  `connection.query.moduleReadinessAll({})` and `connection.query.moduleReadiness({})`.
- Consumes: nothing from Task 2 yet; the handler bodies arrive in Task 5 and this task
  registers them as stubs returning `nil, nil` so boot's unknown-executor check passes.

- [ ] **Step 1: Write the routing test first**

`component/node/routing_readiness_test.go`:

```go
package node

import "testing"

// The OS keeps a live feed of readiness rows on every replica, and the rows
// are written by whichever node evaluated. Without these rules default-deny
// leaves the feed correct on load and frozen after, which looks like it works.
func TestModuleReadinessRowsBroadcast(t *testing.T) {
	for _, topic := range []string{
		"graph.node.created.v1:platform:moduleReadiness",
		"graph.node.updated.v1:platform:moduleReadiness",
	} {
		d := evaluateRouting(defaultRoutingRules(), topic)
		if !d.Forward || !d.Broadcast {
			t.Errorf("%s: want forward+broadcast, got %+v", topic, d)
		}
	}
	// The negative control: a platform concept with no rule stays local.
	if d := evaluateRouting(defaultRoutingRules(), "graph.node.created.v1:platform:moduleVerdict"); d.Forward {
		t.Errorf("moduleVerdict is virtual and must not be routed, got %+v", d)
	}
}
```

Run: `go test -count=1 ./component/node/ -run TestModuleReadinessRowsBroadcast -v`
Expected: FAIL on the two created/updated topics.

- [ ] **Step 2: Add the routing rules**

In `component/node/routing.go`, after the `graph.node.deleted.v1:workbench:workspace` rule:

```go
		// MODULE READINESS (design record 2026-09-06-configuration-readiness,
		// section 4.4). Every node writes its own verdict rows; the OS folds
		// them from a live feed on whichever replica it is attached to. No
		// delete rule: rows are rewritten as versions, never removed.
		{Pattern: "graph.node.created.v1:platform:moduleReadiness", TargetType: ""},
		{Pattern: "graph.node.updated.v1:platform:moduleReadiness", TargetType: ""},
```

Run the test again. Expected: PASS.

- [ ] **Step 3: Declare the constructs**

Append to `dsl/platform/concepts.memql`:

```memql
/// One node's verdict on one module (design record 2026-09-06-configuration-readiness, section
/// 4.4). Rewritten as a new version of the same id, v1:platform:moduleReadiness:<module>--<nodeId>,
/// on every boot and after a providers reload or an integration configure, so a read collapses to
/// the latest. Carries presence and source only, never a value.
///
/// TIER: public, requiresIdentity. Any signed-in person reads it -- the setup surface has to tell a
/// viewer "not set up" as honestly as it tells an owner -- and nobody anonymous does. The write is
/// the engine's alone: recordModuleReadiness is @serverOnly.
@rowAuthz(public, requiresIdentity)
@displayCard(primary="module", secondary="nodeId", tertiary="nodeType", status="state")
concept moduleReadiness {
  module      string!  @description("The readiness module id from the env manifest's modules block: ai, storage, email, githubApp, campaigns, workbench, localApps.")
  nodeId      string!  @description("The reporting node's MEMQL_NODE_ID.")
  nodeType    string!  @description("The reporting node's type.")
  state       enum("configured", "partial", "unconfigured", "notApplicable")!  @description("This node's verdict; notApplicable means the node does not host the module.")
  core        bool     @description("Whether the module is one the first-run wizard walks.")
  lanes       []object @description("One entry per lane: { name, configurableFrom, complete, slots: [{ name, present, source, optional }] }. Presence and source only.")
  reportedAt  datetime!  @description("When this node evaluated, RFC3339.")
}

/// Virtual, never persisted: the cluster-wide fold of moduleReadiness rows, one row per module with
/// id = module name. The moduleReadiness builtin answers it; the OS computes the same fold from the
/// live feed with the mirrored function (component/readiness).
@displayCard(primary="module", secondary="state", tertiary="disagreement")
concept moduleVerdict {
  module        string!  @description("The module id.")
  state         enum("configured", "partial", "unconfigured", "unreported")!  @description("Worst state across live reporting nodes; partial with disagreement when they differ; unreported when no live node reported.")
  core          bool     @description("Whether the module is core.")
  disagreement  []string @description("nodeId=state for every live reporter when they disagree, worst first; empty otherwise.")
  nodes         []object @description("Per live reporting node: { nodeId, nodeType, state, reportedAt }.")
}
```

Append to `dsl/platform/shapes.memql`:

```memql
/// Full projection of v1:platform:moduleReadiness -- every field, because the OS folds them.
@row
shape moduleReadiness moduleReadinessFull {
  row.id
  module
  nodeId
  nodeType
  state
  core
  lanes
  reportedAt
  row.createdAt
}
```

Append to `dsl/platform/queries.memql`:

```memql
/// Every node's latest verdict on every module: the rows the OS folds live and the moduleReadiness
/// builtin folds on demand. Modules times nodes, consumed whole. Any signed-in caller (the concept's
/// tier); no caller term is written because public, requiresIdentity injects nothing.
@unbounded("module readiness -- modules times nodes, consumed whole by the fold")
query moduleReadiness moduleReadinessAll {
  shape   moduleReadinessFull
  asOf    latest
}
```

Append to `dsl/platform/mutations.memql`:

```memql
/// Record one node's verdict on one module as a new version of the deterministic row
/// v1:platform:moduleReadiness:<module>--<nodeId>. @serverOnly: readiness is the engine's own
/// observation, written under internal origin at boot and on a change; a client-reachable writer
/// would be a primitive for forging "configured".
@serverOnly
@description("Write one node's readiness verdict for one module as a new version of its deterministic row.")
mutate moduleReadiness recordModuleReadiness {
  args {
    rowId       string!
    module      string!
    nodeId      string!
    nodeType    string!
    state       string!  @enum("configured", "partial", "unconfigured", "notApplicable")
    core        boolean
    lanes       []object
    reportedAt  string!
  }
  insert {
    id:          args.rowId
    module:      args.module
    nodeId:      args.nodeId
    nodeType:    args.nodeType
    state:       args.state
    core:        args.core ?? false
    lanes:       args.lanes ?? []
    reportedAt:  args.reportedAt
  }
}
```

Append to `dsl/platform/builtins.memql`:

```memql
/// The cluster-wide readiness verdict per module (design record 2026-09-06-configuration-readiness,
/// section 4.5): reads every node's latest moduleReadiness row and the live cluster nodes, folds them
/// with component/readiness -- worst state wins, disagreement is partial with the nodes named, no live
/// reporter is unreported -- and answers one row per module on v1:platform:moduleVerdict.
@sdk
@executor("moduleReadiness")
@description("Fold every node's module readiness rows into one verdict per module: configured, partial, unconfigured or unreported, with disagreeing nodes named.")
builtin moduleReadiness { }

/// Re-evaluate this node's modules and rewrite its moduleReadiness rows. NO @sdk: this is the seam an
/// integration's configure path pulls after applying a slot (integration.email.configure runs
/// `builtin readinessRecompute()` through its ConfigWriter). The handler refuses any call that is
/// neither internal-origin nor a cluster owner.
@executor("readinessRecompute")
@description("Re-evaluate every module on this node and rewrite its moduleReadiness rows; answers one row naming how many modules were written.")
builtin readinessRecompute { }
```

Before appending each, confirm the name is free: `grep -rn "moduleReadiness\|moduleVerdict\|recordModuleReadiness\|readinessRecompute" dsl/` must show only the lines you just wrote.

- [ ] **Step 4: Register the executors as stubs and the constants**

In `component/memql/engine_types.go`, beside `BuiltinExecutorInferenceStatus`:

```go
	// BuiltinExecutorModuleReadiness folds every node's readiness rows into
	// one verdict per module (design record 2026-09-06-configuration-readiness,
	// section 4.5). See readiness_read.go.
	BuiltinExecutorModuleReadiness = "moduleReadiness"
	// BuiltinExecutorReadinessRecompute re-evaluates this node and rewrites
	// its rows; pulled by integration configure paths, never by a client.
	BuiltinExecutorReadinessRecompute = "readinessRecompute"
```

and, beside `InferenceStatusConcept` in `component/memql/fleet_catalog_read.go` or in
`engine_types.go`:

```go
	ModuleReadinessConcept = "v1:platform:moduleReadiness"
	ModuleVerdictConcept   = "v1:platform:moduleVerdict"
```

In `component/memql/executor_builtin.go`, inside `initBuiltinExecutorHandlers` beside the
`BuiltinExecutorInferenceStatus` entry:

```go
		BuiltinExecutorModuleReadiness: func(ctx context.Context, _ map[string]any, _ int) ([]memorynodes.MemoryNode, error) {
			return e.evaluateModuleReadinessExpression(ctx)
		},
		BuiltinExecutorReadinessRecompute: func(ctx context.Context, _ map[string]any, _ int) ([]memorynodes.MemoryNode, error) {
			return e.evaluateReadinessRecomputeExpression(ctx)
		},
```

and, until Task 5 replaces them, create `component/memql/readiness_read.go` with two stubs:

```go
package memql

import (
	"context"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
)

func (e *MemQLEngine) evaluateModuleReadinessExpression(ctx context.Context) ([]memorynodes.MemoryNode, error) {
	return nil, nil
}

func (e *MemQLEngine) evaluateReadinessRecomputeExpression(ctx context.Context) ([]memorynodes.MemoryNode, error) {
	return nil, nil
}
```

(Copy the exact `memorynodes` import path from `fleet_catalog_read.go`'s imports.)

- [ ] **Step 5: Lint, regenerate, and run the gates**

```bash
go run ./cmd/memqllint dsl/
make sdk-gen && make sdk-gen-check
make test 2>&1 | tail -20
```

Expected: memqllint clean; `sdk/ts/src/client/generated_builtins.ts` gains
`moduleReadiness` and `moduleReadinessAll` but NOT `readinessRecompute` (no `@sdk`) and NOT
`recordModuleReadiness` (`@serverOnly`); `generated_concepts.ts` gains
`PLATFORM_MODULE_READINESS` and `PLATFORM_MODULE_VERDICT`. In `make test`, watch these by name:
`TestUndeclaredRowAuthzPopulationOnlyShrinks` (must not grow: the concept declares its tier),
`TestPerRowAuthzClassification` (the mutation lands in `srvOnly`), `public_tier_test.go` (no
`@pii` fields, `requiresIdentity` present), `TestEmbeddedFileCountsAreStable` (unchanged:
no new files under `dsl/`).

- [ ] **Step 6: Commit**

```bash
git add dsl/platform/concepts.memql dsl/platform/shapes.memql dsl/platform/queries.memql \
  dsl/platform/mutations.memql dsl/platform/builtins.memql \
  component/memql/engine_types.go component/memql/executor_builtin.go component/memql/readiness_read.go \
  component/node/routing.go component/node/routing_readiness_test.go \
  sdk/go/client sdk/ts/src/client
git commit -m "platform: moduleReadiness rows, the verdict projection, the two builtins, broadcast routing"
```

## Task 4: the per-node evaluator

**Files:**
- Create: `component/memql/readiness_eval.go`
- Create: `component/memql/readiness_eval_test.go`
- Modify: `component/memql/fleet_catalog_read.go:117-188` (extract `inferenceDoors`)

**Interfaces:**
- Produces: `readinessResolvers{Env, Variable, Secret, IsSecret, Hosted, InferenceOpen,
  IntegrationState}`, `evaluateModules(ctx, r, mods, nodeId, nodeType, now) []readiness.NodeReport`,
  `(e *MemQLEngine) readinessResolvers() readinessResolvers`,
  `(e *MemQLEngine) inferenceDoors(ctx) inferenceDoors`.
- Consumes: `envregistry.Module` (Task 1), `readiness.NodeReport` (Task 2).

- [ ] **Step 1: Write the failing tests**

`component/memql/readiness_eval_test.go`:

```go
package memql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/envregistry"
	"github.com/znasllc-io/memql/component/readiness"
)

const sentinelValue = "SECRET-VALUE-THAT-MUST-NEVER-APPEAR"

func fakeResolvers(env map[string]string, vars map[string]string, secrets map[string]string) readinessResolvers {
	return readinessResolvers{
		Env: func(name string) (string, bool) { v, ok := env[name]; return v, ok },
		Variable: func(_ context.Context, name string) (string, error) {
			if v, ok := vars[name]; ok {
				return v, nil
			}
			return "", errNotFound
		},
		Secret: func(_ context.Context, name string) (string, error) {
			if v, ok := secrets[name]; ok {
				return v, nil
			}
			return "", errNotFound
		},
		IsSecret:      func(name string) bool { return strings.HasSuffix(name, "_SECRET") },
		Hosted:        func(envregistry.Module) bool { return true },
		InferenceOpen: func(context.Context) bool { return false },
		IntegrationState: func(_ context.Context, name string) (string, bool, bool, error) {
			return "", false, false, nil
		},
	}
}

var twoSlotModule = envregistry.Module{
	Name: "storage", Core: true, Description: "d",
	Lanes: []envregistry.Lane{{
		Name: "azure-blob", ConfigurableFrom: envregistry.ConfigurableFromDeployment,
		Slots:         []string{"MEMQL_A", "MEMQL_B_SECRET"},
		OptionalSlots: []string{"MEMQL_C"},
	}},
}

func evalOne(t *testing.T, r readinessResolvers, mod envregistry.Module) readiness.NodeReport {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	reports := evaluateModules(context.Background(), r, []envregistry.Module{mod}, "node-a", "bff", now)
	if len(reports) != 1 {
		t.Fatalf("want one report, got %d", len(reports))
	}
	return reports[0]
}

func TestLaneStates(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		secrets map[string]string
		want    readiness.State
	}{
		{"complete lane is configured", map[string]string{"MEMQL_A": "x"}, map[string]string{"MEMQL_B_SECRET": sentinelValue}, readiness.Configured},
		{"some required slots is partial", map[string]string{"MEMQL_A": "x"}, nil, readiness.Partial},
		{"nothing is unconfigured", nil, nil, readiness.Unconfigured},
		{"an optional slot alone is unconfigured", map[string]string{"MEMQL_C": "x"}, nil, readiness.Unconfigured},
		{"blank counts as absent", map[string]string{"MEMQL_A": "  "}, nil, readiness.Unconfigured},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evalOne(t, fakeResolvers(c.env, nil, c.secrets), twoSlotModule)
			if got.State != c.want {
				t.Fatalf("got %s want %s (%+v)", got.State, c.want, got.Lanes)
			}
		})
	}
}

func TestSlotSourceFollowsTheLadder(t *testing.T) {
	r := fakeResolvers(map[string]string{"MEMQL_A": "x"}, nil, map[string]string{"MEMQL_B_SECRET": sentinelValue})
	got := evalOne(t, r, twoSlotModule)
	slots := map[string]readiness.SlotReport{}
	for _, s := range got.Lanes[0].Slots {
		slots[s.Name] = s
	}
	if slots["MEMQL_A"].Source != "env" || slots["MEMQL_B_SECRET"].Source != "globalSecret" {
		t.Fatalf("sources: %+v", slots)
	}
	if !slots["MEMQL_C"].Optional || slots["MEMQL_C"].Present {
		t.Fatalf("optional absent slot: %+v", slots["MEMQL_C"])
	}
}

func TestReportsCarryNoValues(t *testing.T) {
	r := fakeResolvers(map[string]string{"MEMQL_A": sentinelValue}, nil, map[string]string{"MEMQL_B_SECRET": sentinelValue})
	got := evalOne(t, r, twoSlotModule)
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), sentinelValue) {
		t.Fatalf("a report carried a resolved value: %s", raw)
	}
}

func TestNotHostedIsNotApplicable(t *testing.T) {
	r := fakeResolvers(map[string]string{"MEMQL_A": "x"}, nil, map[string]string{"MEMQL_B_SECRET": "y"})
	r.Hosted = func(envregistry.Module) bool { return false }
	if got := evalOne(t, r, twoSlotModule); got.State != readiness.NotApplicable {
		t.Fatalf("got %s", got.State)
	}
}

func TestInferenceEvaluator(t *testing.T) {
	mod := envregistry.Module{Name: "ai", Core: true, Description: "d", Evaluator: envregistry.EvaluatorInferenceStatus}
	r := fakeResolvers(nil, nil, nil)
	if got := evalOne(t, r, mod); got.State != readiness.Unconfigured {
		t.Fatalf("no door open must be unconfigured, got %s", got.State)
	}
	r.InferenceOpen = func(context.Context) bool { return true }
	if got := evalOne(t, r, mod); got.State != readiness.Configured {
		t.Fatalf("a door open must be configured, got %s", got.State)
	}
}

func TestIntegrationEvaluator(t *testing.T) {
	mod := envregistry.Module{Name: "email", Core: true, Description: "d", Evaluator: "integration:email"}
	cases := []struct {
		state      string
		touched    bool
		registered bool
		want       readiness.State
	}{
		{"configured", true, true, readiness.Configured},
		{"unhealthy", true, true, readiness.Configured},
		{"needs_configuration", true, true, readiness.Partial},
		{"needs_configuration", false, true, readiness.Unconfigured},
		{"", false, false, readiness.NotApplicable},
	}
	for _, c := range cases {
		r := fakeResolvers(nil, nil, nil)
		r.IntegrationState = func(context.Context, string) (string, bool, bool, error) {
			return c.state, c.touched, c.registered, nil
		}
		if got := evalOne(t, r, mod); got.State != c.want {
			t.Errorf("%+v: got %s", c, got.State)
		}
	}
}
```

`errNotFound` is a package-level `errors.New("not found")` declared in the test file.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -count=1 ./component/memql/ -run 'TestLaneStates|TestSlotSource|TestReportsCarry|TestNotHosted|TestInferenceEvaluator|TestIntegrationEvaluator' -v`
Expected: compile failure on `readinessResolvers` and `evaluateModules`.

- [ ] **Step 3: Extract the inference doors**

In `component/memql/fleet_catalog_read.go`, split `evaluateInferenceStatusExpression` so
the door computation is reusable and has ONE implementation:

```go
// inferenceDoors is the shared reading behind inferenceStatus and the `ai`
// readiness module. One implementation, two readers -- the inferenceStatus
// concept says why a second would be a bug.
type inferenceDoors struct {
	LocalEligible    bool
	LocalModels      int
	EligibleModelIds []string
	CloudConfigured  bool
	Federation       bool
	Doors            []string
}

func (e *MemQLEngine) inferenceDoors(ctx context.Context) inferenceDoors {
	var d inferenceDoors
	if e == nil || e.providers == nil {
		return d
	}
	models, err := e.fleetCatalogForCaller(ctx)
	if err == nil {
		for _, m := range models {
			d.LocalModels++
			if m.Online() && m.StructuredOutput && m.ContextWindow >= MinimumContextWindow {
				d.LocalEligible = true
				d.EligibleModelIds = append(d.EligibleModelIds, m.ModelId)
			}
		}
	}
	sort.Strings(d.EligibleModelIds)
	d.CloudConfigured = e.providers.HasCloudProviderConfigured()
	d.Federation = e.providers.federationConfigured()
	if d.LocalEligible {
		d.Doors = append(d.Doors, InferenceDoorLocal)
	}
	if d.Federation {
		d.Doors = append(d.Doors, InferenceDoorFederation)
	}
	if d.CloudConfigured && !d.Federation {
		d.Doors = append(d.Doors, InferenceDoorApiKey)
	}
	return d
}
```

and make `evaluateInferenceStatusExpression` build its row from `d := e.inferenceDoors(ctx)`
(`eligible` = `len(d.Doors) > 0`, `doorsOpen` = `toAnySlice(d.Doors)`, the rest one-to-one).
Keep the existing comments; the existing `TestInferenceStatus*` tests must stay green.

- [ ] **Step 4: Write the evaluator**

`component/memql/readiness_eval.go`:

```go
package memql

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/znasllc-io/memql/component/envregistry"
	"github.com/znasllc-io/memql/component/readiness"
)

// Slot sources, spelled the way email's ConfigResolver spells them so the
// Set up group reads one vocabulary.
const (
	readinessSourceEnv      = "env"
	readinessSourceVariable = "globalVariable"
	readinessSourceSecret   = "globalSecret"
	readinessSourceUnset    = "unset"
)

// readinessResolvers is everything the evaluator needs from the node, as
// functions, so the decision is testable with no engine.
type readinessResolvers struct {
	Env      func(name string) (string, bool)
	Variable func(ctx context.Context, name string) (string, error)
	Secret   func(ctx context.Context, name string) (string, error)
	IsSecret func(name string) bool
	// Hosted reports whether this node hosts the module.
	Hosted func(mod envregistry.Module) bool
	// InferenceOpen reports whether any inference door is open here.
	InferenceOpen func(ctx context.Context) bool
	// IntegrationState asks integration.<name>.status in-process:
	// state is the report's own word, touched is "any slot present",
	// registered=false means the integration is not on this node.
	IntegrationState func(ctx context.Context, name string) (state string, touched bool, registered bool, err error)
}

func resolveSlot(ctx context.Context, r readinessResolvers, name string, optional bool) readiness.SlotReport {
	out := readiness.SlotReport{Name: name, Optional: optional, Source: readinessSourceUnset}
	if r.Env != nil {
		if v, ok := r.Env(name); ok && strings.TrimSpace(v) != "" {
			out.Present, out.Source = true, readinessSourceEnv
			return out
		}
	}
	if r.IsSecret != nil && r.IsSecret(name) {
		if r.Secret != nil {
			if v, err := r.Secret(ctx, name); err == nil && strings.TrimSpace(v) != "" {
				out.Present, out.Source = true, readinessSourceSecret
			}
		}
		return out
	}
	if r.Variable != nil {
		if v, err := r.Variable(ctx, name); err == nil && strings.TrimSpace(v) != "" {
			out.Present, out.Source = true, readinessSourceVariable
		}
	}
	return out
}

func evaluateLane(ctx context.Context, r readinessResolvers, lane envregistry.Lane) readiness.LaneReport {
	out := readiness.LaneReport{Name: lane.Name, ConfigurableFrom: lane.ConfigurableFrom, Complete: true, Slots: []readiness.SlotReport{}}
	for _, name := range lane.Slots {
		s := resolveSlot(ctx, r, name, false)
		if !s.Present {
			out.Complete = false
		}
		out.Slots = append(out.Slots, s)
	}
	for _, name := range lane.OptionalSlots {
		out.Slots = append(out.Slots, resolveSlot(ctx, r, name, true))
	}
	return out
}

// evaluateModule decides one node's verdict on one module. Presence and
// source only: no branch here ever keeps a resolved value.
func evaluateModule(ctx context.Context, r readinessResolvers, mod envregistry.Module, nodeId, nodeType string, now time.Time) readiness.NodeReport {
	out := readiness.NodeReport{Module: mod.Name, NodeId: nodeId, NodeType: nodeType, Core: mod.Core, Lanes: []readiness.LaneReport{}, ReportedAt: now}
	if r.Hosted != nil && !r.Hosted(mod) {
		out.State = readiness.NotApplicable
		return out
	}
	switch {
	case mod.Evaluator == envregistry.EvaluatorInferenceStatus:
		if r.InferenceOpen != nil && r.InferenceOpen(ctx) {
			out.State = readiness.Configured
		} else {
			out.State = readiness.Unconfigured
		}
	case strings.HasPrefix(mod.Evaluator, envregistry.EvaluatorIntegrationPrefix):
		name := strings.TrimPrefix(mod.Evaluator, envregistry.EvaluatorIntegrationPrefix)
		state, touched, registered, err := "", false, false, error(nil)
		if r.IntegrationState != nil {
			state, touched, registered, err = r.IntegrationState(ctx, name)
		}
		switch {
		case err != nil || !registered:
			out.State = readiness.NotApplicable
		case state == "configured" || state == "unhealthy":
			out.State = readiness.Configured
		case touched:
			out.State = readiness.Partial
		default:
			out.State = readiness.Unconfigured
		}
	default:
		touched, complete := false, false
		for _, lane := range mod.Lanes {
			lr := evaluateLane(ctx, r, lane)
			out.Lanes = append(out.Lanes, lr)
			if lr.Complete {
				complete = true
			}
			for _, s := range lr.Slots {
				if s.Present && !s.Optional {
					touched = true
				}
			}
		}
		switch {
		case complete:
			out.State = readiness.Configured
		case touched:
			out.State = readiness.Partial
		default:
			out.State = readiness.Unconfigured
		}
	}
	return out
}

func evaluateModules(ctx context.Context, r readinessResolvers, mods []envregistry.Module, nodeId, nodeType string, now time.Time) []readiness.NodeReport {
	out := make([]readiness.NodeReport, 0, len(mods))
	for _, mod := range mods {
		out = append(out, evaluateModule(ctx, r, mod, nodeId, nodeType, now))
	}
	return out
}

// integrationReport is the slice of integration.<name>.status's payload the
// evaluator reads. The report's own fields, nothing invented.
type integrationReport struct {
	State       string `json:"state"`
	Settings    []struct{ Source string `json:"source"` } `json:"settings"`
	Credentials []struct {
		Present bool   `json:"present"`
		Source  string `json:"source"`
	} `json:"credentials"`
}

// readinessResolvers wires the evaluator to this node: the environment, the
// two row tiers, the plug-in registry, the provider registry and the
// in-process capability map.
func (e *MemQLEngine) readinessResolvers() readinessResolvers {
	manifest, _ := envregistry.LoadManifest("")
	nodeType := envregistry.ResolveNodeType()
	return readinessResolvers{
		Env:      os.LookupEnv,
		Variable: e.ResolveSystemVariable,
		Secret:   e.ResolveSystemSecret,
		IsSecret: func(name string) bool { return manifest != nil && manifest.IsSecret(name) },
		Hosted: func(mod envregistry.Module) bool {
			if mod.HostedBy.Everywhere() {
				return true
			}
			for _, name := range mod.HostedBy.Integrations {
				if e.IntegrationByName(name) != nil {
					return true
				}
			}
			for _, nt := range mod.HostedBy.NodeTypes {
				if nt == nodeType {
					return true
				}
			}
			return false
		},
		InferenceOpen: func(ctx context.Context) bool { return len(e.inferenceDoors(ctx).Doors) > 0 },
		IntegrationState: func(ctx context.Context, name string) (string, bool, bool, error) {
			handler, ok := e.builtinExecutorHandlers["integration."+name+".status"]
			if !ok {
				return "", false, false, nil
			}
			nodes, err := handler(ctx, map[string]any{"probe": false}, 0)
			if err != nil {
				return "", false, true, err
			}
			for _, n := range nodes {
				var rep integrationReport
				if err := json.Unmarshal(n.Payload, &rep); err != nil {
					continue
				}
				if rep.State == "" {
					continue
				}
				touched := false
				for _, s := range rep.Settings {
					if s.Source != "" && s.Source != readinessSourceUnset {
						touched = true
					}
				}
				for _, c := range rep.Credentials {
					if c.Present {
						touched = true
					}
				}
				return rep.State, touched, true, nil
			}
			return "", false, true, nil
		},
	}
}

var _ = protojson.Marshal // used by readiness_read.go (Task 5); keeps the import stable
```

Drop the trailing `protojson` line if Task 5's file imports it itself; do not leave an
unused import. The email status handler's payload is one node per registered integration
(email first); the loop above takes the first node whose `state` is non-empty, which is
email's own report; an integration that publishes no report never reaches this evaluator
because no module names it.

- [ ] **Step 5: Run the tests**

Run: `go test -count=1 ./component/memql/ -run 'TestLaneStates|TestSlotSource|TestReportsCarry|TestNotHosted|TestInferenceEvaluator|TestIntegrationEvaluator|TestInferenceStatus' -v`
Expected: all PASS, the pre-existing inference status tests included.

- [ ] **Step 6: Commit**

```bash
git add component/memql/readiness_eval.go component/memql/readiness_eval_test.go component/memql/fleet_catalog_read.go
git commit -m "memql: evaluate module readiness per node -- lanes, the inference doors, an integration's own report"
```

---

## Task 5: the writer and the two builtin bodies

**Files:**
- Create: `component/memql/readiness_write.go`
- Replace: `component/memql/readiness_read.go` (the Task 3 stubs)
- Modify: `component/memql/engine.go` (two fields on `MemQLEngine`)
- Modify: `dsl/platform/concepts.memql` (one more virtual concept), then `make sdk-gen`

**Interfaces:**
- Produces: `(e *MemQLEngine) SetReadinessIdentity(nodeId, nodeType string)`,
  `(e *MemQLEngine) WriteModuleReadiness(ctx) (int, error)`, `readinessRowID(module, nodeId)`,
  `renderRecordModuleReadiness(r readiness.NodeReport, rowId string) (string, error)`, the
  real `evaluateModuleReadinessExpression` and `evaluateReadinessRecomputeExpression`.
- Consumes: Task 4's evaluator and resolvers; Task 3's mutation and query; Task 2's fold.

- [ ] **Step 1: Add the recompute reply concept**

Append to `dsl/platform/concepts.memql` and run `make sdk-gen`:

```memql
/// Virtual, never persisted: what readinessRecompute answers -- how many modules this node just
/// rewrote, and which node it was.
@displayCard(primary="nodeId", secondary="written")
concept readinessRecomputeResult {
  nodeId   string!  @description("The node that re-evaluated.")
  written  int!     @description("How many moduleReadiness rows were written.")
}
```

- [ ] **Step 2: Add the identity fields**

In `component/memql/engine.go`, on the `MemQLEngine` struct:

```go
	// readinessNodeId / readinessNodeType name this node in the readiness rows
	// it writes. Set once by app/run.go from the same values the startup event
	// carries; empty until then, in which case the writer falls back to
	// MEMQL_NODE_ID and the resolved node type.
	readinessNodeId   string
	readinessNodeType string
```

- [ ] **Step 3: Write the writer**

`component/memql/readiness_write.go`:

```go
package memql

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/envregistry"
	langparser "github.com/znasllc-io/memql/component/language/parser"
	"github.com/znasllc-io/memql/component/readiness"
)

// SetReadinessIdentity names this node in the rows it writes.
func (e *MemQLEngine) SetReadinessIdentity(nodeId, nodeType string) {
	e.readinessNodeId = strings.TrimSpace(nodeId)
	e.readinessNodeType = strings.TrimSpace(nodeType)
}

func (e *MemQLEngine) readinessIdentity() (string, string) {
	nodeId, nodeType := e.readinessNodeId, e.readinessNodeType
	if nodeId == "" {
		nodeId = strings.TrimSpace(os.Getenv("MEMQL_NODE_ID"))
	}
	if nodeId == "" {
		if h, err := os.Hostname(); err == nil {
			nodeId = h
		}
	}
	if nodeType == "" {
		nodeType = envregistry.ResolveNodeType()
	}
	return nodeId, nodeType
}

// readinessRowID is the deterministic id a rewrite versions.
func readinessRowID(module, nodeId string) string {
	return ModuleReadinessConcept + ":" + module + "--" + nodeId
}

// renderRecordModuleReadiness renders the @serverOnly call as MemQL TEXT. Every
// string goes through QuoteString (the lexer's escaping, memql#4256); the
// lanes ride as a JSON literal, which the parser accepts as a list of objects.
func renderRecordModuleReadiness(r readiness.NodeReport, rowId string) (string, error) {
	lanes := r.Lanes
	if lanes == nil {
		lanes = []readiness.LaneReport{}
	}
	raw, err := json.Marshal(lanes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"mutation recordModuleReadiness(rowId: %s, module: %s, nodeId: %s, nodeType: %s, state: %s, core: %t, lanes: %s, reportedAt: %s)",
		langparser.QuoteString(rowId),
		langparser.QuoteString(r.Module),
		langparser.QuoteString(r.NodeId),
		langparser.QuoteString(r.NodeType),
		langparser.QuoteString(string(r.State)),
		r.Core,
		string(raw),
		langparser.QuoteString(r.ReportedAt.UTC().Format(time.RFC3339)),
	), nil
}

// readinessWriteContext is the engine's own identity for these rows: internal
// origin (the @serverOnly gate) plus a synthetic, unranked actor so the row
// carries a createdBy and the rank rules do not govern it (D4 of the RANK
// epic). The shape mirrors component/automations' contextWithSystemActor.
func readinessWriteContext(ctx context.Context) context.Context {
	const actorId = "system:readiness"
	claims := map[string]any{"sub": actorId, "email": actorId, "role": "system"}
	ctx = auth.ContextWithClaims(ctx, claims)
	ctx = auth.ContextWithToken(ctx, auth.BuildTokenInfo(claims))
	ctx = auth.ContextWithAccess(ctx, &auth.AccessContext{
		UserId:    actorId,
		Role:      auth.RoleReader,
		Unranked:  true,
		Synthetic: true,
	})
	return auth.ContextWithInternalOrigin(ctx)
}

// WriteModuleReadiness evaluates every module and writes this node's rows as
// new versions of their deterministic ids. Returns how many were written; a
// failure stops at the first module, leaving the previous version standing.
func (e *MemQLEngine) WriteModuleReadiness(ctx context.Context) (int, error) {
	manifest, err := envregistry.LoadManifest("")
	if err != nil {
		return 0, fmt.Errorf("module readiness: manifest: %w", err)
	}
	nodeId, nodeType := e.readinessIdentity()
	reports := evaluateModules(ctx, e.readinessResolvers(), manifest.Modules, nodeId, nodeType, time.Now().UTC())
	wctx := readinessWriteContext(ctx)
	written := 0
	for _, r := range reports {
		call, err := renderRecordModuleReadiness(r, readinessRowID(r.Module, nodeId))
		if err != nil {
			return written, fmt.Errorf("module readiness: render %s: %w", r.Module, err)
		}
		if _, err := e.Execute(wctx, call); err != nil {
			return written, fmt.Errorf("module readiness: write %s: %w", r.Module, err)
		}
		written++
	}
	return written, nil
}
```

- [ ] **Step 4: Replace the read stubs**

`component/memql/readiness_read.go`:

```go
package memql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/znasllc-io/memql/component/auth"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/readiness"
)

// readModuleReadinessRows reads every node's latest row under the caller's
// own context. The concept is public, requiresIdentity, so any signed-in
// caller sees all of them and an anonymous one sees none.
func (e *MemQLEngine) readModuleReadinessRows(ctx context.Context) ([]readiness.NodeReport, error) {
	result, err := e.Execute(ctx, "query moduleReadinessAll()")
	if err != nil {
		return nil, fmt.Errorf("module readiness: rows: %w", err)
	}
	var out []readiness.NodeReport
	if result == nil || result.Bundle == nil {
		return out, nil
	}
	for _, n := range result.Bundle.Nodes {
		raw, err := protojson.Marshal(n.GetPayload())
		if err != nil {
			continue
		}
		var r readiness.NodeReport
		if err := json.Unmarshal(raw, &r); err != nil || r.Module == "" {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// readClusterNodeLiveness reads the latest non-stopped cluster nodes, the same
// set the reconciler prunes over.
func (e *MemQLEngine) readClusterNodeLiveness(ctx context.Context) ([]readiness.NodeLiveness, error) {
	result, err := e.Execute(ctx, "query staleClusterNodes()")
	if err != nil {
		return nil, fmt.Errorf("module readiness: cluster nodes: %w", err)
	}
	var out []readiness.NodeLiveness
	if result == nil || result.Bundle == nil {
		return out, nil
	}
	for _, n := range result.Bundle.Nodes {
		f := n.GetPayload().GetFields()
		id := strings.TrimPrefix(n.GetId(), "v1:cluster:node:")
		liveness := readiness.NodeLiveness{NodeId: id, Health: strings.ToLower(strings.TrimSpace(f["health"].GetStringValue()))}
		if ls := strings.TrimSpace(f["lastSeen"].GetStringValue()); ls != "" {
			if t, perr := time.Parse(time.RFC3339, ls); perr == nil {
				liveness.LastSeen = t
			}
		}
		out = append(out, liveness)
	}
	return out, nil
}

// evaluateModuleReadinessExpression is the moduleReadiness builtin: the fold,
// one row per module, id = module name so the id-keyed reply keeps every row.
func (e *MemQLEngine) evaluateModuleReadinessExpression(ctx context.Context) ([]memorynodes.MemoryNode, error) {
	reports, err := e.readModuleReadinessRows(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := e.readClusterNodeLiveness(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	verdicts := readiness.Fold(reports, nodes, now)
	out := make([]memorynodes.MemoryNode, 0, len(verdicts))
	for _, v := range verdicts {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out = append(out, memorynodes.MemoryNode{
			ID:        v.Module,
			Concept:   ModuleVerdictConcept,
			Type:      memorynodes.NodeTypeObject,
			Payload:   raw,
			CreatedAt: now,
		})
	}
	return out, nil
}

// evaluateReadinessRecomputeExpression is the readinessRecompute builtin. Not
// on the SDK wire, and refused unless the caller is internal or a cluster
// owner: a recompute is harmless, but a surface any signed-in person can hit
// in a loop is a way to make a node rewrite rows all day.
func (e *MemQLEngine) evaluateReadinessRecomputeExpression(ctx context.Context) ([]memorynodes.MemoryNode, error) {
	if !auth.OriginFromContext(ctx).IsInternal() && !rowAuthzIsClusterOwner(ctx) {
		return nil, fmt.Errorf("readinessRecompute is internal or owner-only")
	}
	written, err := e.WriteModuleReadiness(ctx)
	if err != nil {
		return nil, err
	}
	nodeId, _ := e.readinessIdentity()
	raw, err := json.Marshal(map[string]any{"nodeId": nodeId, "written": written})
	if err != nil {
		return nil, err
	}
	return []memorynodes.MemoryNode{{
		ID:        "current",
		Concept:   "v1:platform:readinessRecomputeResult",
		Type:      memorynodes.NodeTypeObject,
		Payload:   raw,
		CreatedAt: time.Now().UTC(),
	}}, nil
}
```

`rowAuthzIsClusterOwner` already exists (it gates `providerAuthStatus`). Match the exact
accessor names on the bundle node type (`GetPayload`, `GetFields`, `GetId`) to those the
reconciler uses in `component/node/reconciler.go:406-441`.

- [ ] **Step 5: Build and run the unit tests**

Run: `go build ./... && (cd component/memql && go build ./...) && go test -count=1 ./component/memql/ -run 'Readiness|TestLaneStates' -v`
Expected: builds; the Task 4 tests still PASS. The database-gated behaviour is Task 6.

- [ ] **Step 6: Commit**

```bash
git add component/memql/readiness_write.go component/memql/readiness_read.go component/memql/engine.go \
  dsl/platform/concepts.memql sdk/go/client sdk/ts/src/client
git commit -m "memql: write this node's readiness rows under internal origin; fold them on read"
```

## Task 6: the three hooks and the database-gated tests

**Files:**
- Modify: `app/run.go` (before `application.EmitSystemStartup()`, around line 221)
- Modify: `component/memql/provider_reload_propagate.go:84-128` (the subscriber closure)
- Modify: `integrations/email/configure.go:166-182` (after `invalidateSender()`)
- Create: `component/memql/readiness_db_test.go`

**Interfaces:**
- Consumes: `SetReadinessIdentity`, `WriteModuleReadiness` (Task 5); the
  `readinessRecompute` builtin (Task 3/5); email's `ConfigWriter` (exists).

- [ ] **Step 1: Write the database-gated test**

`component/memql/readiness_db_test.go`. `bootReadinessTestEngine` is a copy of the engine
boot helper `keyless_boot_test.go` uses (the function in that file that returns a
`*MemQLEngine` against `component/database/dbtest`); it skips when the database is
unreachable and `MEMQL_REQUIRE_DB` is unset, which is `dbtest`'s own behaviour. Copy the
helper's body into this file under the new name rather than calling across test files.

```go
package memql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/envregistry"
	"github.com/znasllc-io/memql/component/readiness"
)

func readinessRowsForTest(t *testing.T, e *MemQLEngine) []readiness.NodeReport {
	t.Helper()
	ctx := auth.ContextWithAccess(context.Background(), &auth.AccessContext{UserId: "v1:identity:user:reader", Role: auth.RoleReader})
	rows, err := e.readModuleReadinessRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestBootWritesOneRowPerModuleAndARewriteVersionsThem(t *testing.T) {
	e := bootReadinessTestEngine(t)
	ctx := context.Background()
	e.SetReadinessIdentity("readiness-test-node", "bff")

	manifest, err := envregistry.LoadManifest("")
	if err != nil {
		t.Fatal(err)
	}
	written, err := e.WriteModuleReadiness(ctx)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if written != len(manifest.Modules) {
		t.Fatalf("wrote %d rows, manifest declares %d modules", written, len(manifest.Modules))
	}

	first := readinessRowsForTest(t, e)
	mine := 0
	for _, r := range first {
		if r.NodeId == "readiness-test-node" {
			mine++
			switch r.State {
			case readiness.Configured, readiness.Partial, readiness.Unconfigured, readiness.NotApplicable:
			default:
				t.Errorf("%s: state %q is not one of the four", r.Module, r.State)
			}
		}
	}
	if mine != written {
		t.Fatalf("read back %d of my rows, wrote %d", mine, written)
	}

	// A rewrite is a new VERSION of the same ids, and the read collapses.
	if _, err := e.WriteModuleReadiness(ctx); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second := readinessRowsForTest(t, e)
	mine = 0
	for _, r := range second {
		if r.NodeId == "readiness-test-node" {
			mine++
		}
	}
	if mine != written {
		t.Fatalf("after a rewrite the read collapsed to %d rows, want %d", mine, written)
	}
}

func TestReadinessRowsCarryNoValues(t *testing.T) {
	e := bootReadinessTestEngine(t)
	t.Setenv("MEMQL_AZURE_BLOB_CONTAINER", "READINESS-SENTINEL-CONTAINER")
	e.SetReadinessIdentity("readiness-test-node", "bff")
	if _, err := e.WriteModuleReadiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range readinessRowsForTest(t, e) {
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), "READINESS-SENTINEL-CONTAINER") {
			t.Fatalf("a row carried a resolved value: %s", raw)
		}
	}
}

func TestFoldReportsUnreportedWhenNoClusterNodeIsLive(t *testing.T) {
	e := bootReadinessTestEngine(t)
	e.SetReadinessIdentity("readiness-ghost-node", "bff")
	if _, err := e.WriteModuleReadiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := auth.ContextWithAccess(context.Background(), &auth.AccessContext{UserId: "v1:identity:user:reader", Role: auth.RoleReader})
	nodes, err := e.evaluateModuleReadinessExpression(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("the fold answered no modules at all")
	}
	for _, n := range nodes {
		var v readiness.Verdict
		if err := json.Unmarshal(n.Payload, &v); err != nil {
			t.Fatal(err)
		}
		// No v1:cluster:node row names readiness-ghost-node, so its report
		// must not count; with no other live reporter the honest answer is
		// unreported, never unconfigured.
		if v.State == readiness.Unconfigured || v.State == readiness.Configured || v.State == readiness.Partial {
			for _, nv := range v.Nodes {
				if nv.NodeId == "readiness-ghost-node" {
					t.Fatalf("module %s counted a node with no live cluster row: %+v", v.Module, v)
				}
			}
		}
	}
}

func TestRecomputeRefusesAClientOrigin(t *testing.T) {
	e := bootReadinessTestEngine(t)
	ctx := auth.ContextWithAccess(context.Background(), &auth.AccessContext{UserId: "v1:identity:user:reader", Role: auth.RoleReader})
	if _, err := e.Execute(ctx, "builtin readinessRecompute()"); err == nil {
		t.Fatal("a reader over client origin must not be able to force a recompute")
	}
	if _, err := e.Execute(auth.ContextWithInternalOrigin(ctx), "builtin readinessRecompute()"); err != nil {
		t.Fatalf("internal origin must be admitted: %v", err)
	}
}
```

- [ ] **Step 2: Run it against the shared Postgres to verify the state**

```bash
export MEMQL_DATABASE_DSN='postgres://memql:memql_dev@localhost:15434/memql?sslmode=disable'
export MEMQL_REQUIRE_DB=1
go test -count=1 ./component/memql/ -run 'TestBootWrites|TestReadinessRowsCarry|TestFoldReportsUnreported|TestRecomputeRefuses' -v
```

Expected before the hooks: `--- PASS` for all four (the writer exists since Task 5; the
tests exercise it directly). Look for `--- PASS`, not `ok`: a skip is not a pass.

- [ ] **Step 3: The boot hook**

In `app/run.go`, immediately before `application.EmitSystemStartup()`:

```go
	// MODULE READINESS (design record 2026-09-06-configuration-readiness,
	// section 6): written here, after every dependency is Ready and the
	// plug-ins and providers have materialized, under the same node identity
	// the startup event carries. A failure keeps the previous boot's rows and
	// says so; it never stops the node.
	if eng := application.Engine(); eng != nil {
		eng.SetReadinessIdentity(application.startupNodeID(), application.startupNodeType())
		if n, err := eng.WriteModuleReadiness(context.Background()); err != nil {
			serviceLogger.Warn("module readiness: boot write failed; the previous boot's rows stand", "error", err)
		} else {
			serviceLogger.Info("module readiness: rows written", "modules", n)
		}
	}
```

`startupNodeID()` and `startupNodeType()` are two one-line accessors to add to
`app/cluster.go` beside `EmitSystemStartup`, returning the SAME `id` and `type` values that
function puts into its `node` payload (read them from wherever `EmitSystemStartup` reads
them; do not read the environment a second time). Use the logger variable the surrounding
code in `run.go` uses.

- [ ] **Step 4: The reload hook**

In `component/memql/provider_reload_propagate.go`, inside the subscriber closure, after
the `logger.Info("provider reload propagation: providers re-resolved from broadcast", ...)`
line:

```go
			// The ai module's verdict may have changed with the providers.
			if _, werr := e.WriteModuleReadiness(ctx); werr != nil {
				logger.Warn("module readiness: rewrite after providers reload failed", "error", werr)
			}
```

- [ ] **Step 5: The configure hook**

In `integrations/email/configure.go`, after `reresolves := i.invalidateSender()`:

```go
	// The email module's verdict may have changed with the slot. The builtin
	// carries no @sdk and is pulled through the same writer that just applied
	// the slot, so the rewrite happens on THIS node, like the invalidation.
	if _, err := writer.Execute(ctx, "builtin readinessRecompute()"); err != nil {
		i.logger.Warn("email.configure: readiness recompute failed; the mark updates on the next providers reload", "error", err)
	}
```

Use the logger field `Integration` already carries (whatever it is named in this file).
`NewConfigWriter` must stamp internal origin for the `@serverOnly` slot writes it already
performs; confirm by reading its body, and if it does not, the recompute builtin's gate
would refuse this call, which the Step 6 test would show.

- [ ] **Step 6: Run the gates**

```bash
go build ./... && (cd component/memql && go build ./...) && (cd integrations/email && go build ./...)
for t in "" identity mcp agent planner workbench edge bff; do go build -tags "$t" . || exit 1; done
make test 2>&1 | tail -30
export MEMQL_DATABASE_DSN='postgres://memql:memql_dev@localhost:15434/memql?sslmode=disable'; export MEMQL_REQUIRE_DB=1
go test -count=1 ./component/memql/ -run 'Readiness|TestBootWrites|TestFoldReportsUnreported|TestRecomputeRefuses' -v | grep -E '^(--- |ok|FAIL)'
make arch-model && make arch-model-check
```

Expected: every tag builds; `make test` green; the four db-gated cases `--- PASS`;
`arch-model-check` green after the regeneration (a new package changes the model).

- [ ] **Step 7: Commit**

```bash
git add app/run.go app/cluster.go component/memql/provider_reload_propagate.go \
  integrations/email/configure.go component/memql/readiness_db_test.go \
  component/architecture/embedded/topology.model.json
git commit -m "readiness: write at boot, after a providers reload and after an email configure"
```

---

## Task 7: amend the spec and open PR 1

**Files:**
- Modify: `docs/superpowers/specs/2026-09-06-configuration-readiness-design.md` (section 4.2,
  4.3 and 8)
- Modify: `docs/superpowers/plans/2026-09-06-configuration-readiness.md` (tick the boxes)

- [ ] **Step 1: Amend the spec for the planning deviations**

In section 4.2, replace the `email` entry of the YAML sketch with
`evaluator: "integration:email"` and delete the sentence beginning "Email's Go
`ConfigManifest` stays as it is"; add `hostedBy` and `optionalSlots` to the sketch and one
rule line each ("`hostedBy` names the integrations or node types that report; neither means
every node" and "`optionalSlots` count toward presence, not completeness"). In section 4.3,
add: "An `integration:<name>` evaluator asks `integration.<name>.status` in-process:
`configured` or `unhealthy` is configured, `needs_configuration` with any slot present is
partial, otherwise unconfigured, and an integration not registered on the node is not
applicable." In section 7, item 2, delete "A parity test pins email's Go manifest to the env
manifest's lanes." In section 8, add to Delivery: "The email configure hook runs the
`readinessRecompute` builtin through the integration's existing writer; the builtin carries
no `@sdk` and refuses any origin that is not internal or a cluster owner."

- [ ] **Step 2: Run the docs gates and the whole tree once more**

```bash
go test -count=1 -run 'TestNoVendorDomainLiterals|TestDocsFrontMatter|TestDocsRelativeLinks' .
make test 2>&1 | tail -5
go run ./cmd/memqllint dsl/ && make sdk-gen-check && make env-registry-check && make arch-model-check
```

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin epic/configuration-readiness
gh pr create --repo znasllc-io/memql --base main --title "configuration readiness: the engine knows what is set up (1 of 2)" --body-file - <<'BODY'
Epic 1 of the configuration-readiness program, engine half. The env manifest gains a
`modules` block; every node evaluates every module at boot and on a change and writes
`v1:platform:moduleReadiness` rows as versions of deterministic ids; `component/readiness`
folds them (worst state wins, disagreement is partial with nodes named, no live reporter is
unreported); the `moduleReadiness` builtin answers one verdict per module; two broadcast
routing rules keep the OS feed live.

Design record: docs/superpowers/specs/2026-09-06-configuration-readiness-design.md
Plan: docs/superpowers/plans/2026-09-06-configuration-readiness.md (PR 2 deletes it)

Gates that moved: `make env-registry-sync`, `make sdk-gen`, `make arch-model`.

Closes #<epic-task-1>
Closes #<epic-task-2>
Closes #<epic-task-3>
Closes #<epic-task-4>

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01V3meB9SBytpmvzaWo9aHLF
BODY
```

Replace the four placeholders with the numbers Task 0 printed. Then
`scripts/dev/merge-as-owner.sh --pr=<n> --check` once CI is green, and merge with the script.
PR 2 starts from `main` after this merges.

---

# PR 2: the OS

Branch from `main` AFTER PR 1 merges: `git checkout main && git pull && git checkout -b
epic/configuration-readiness-os`. Every command below runs from
`/home/znas/memql-projects/memql/clients/os` unless it says otherwise.

## Task 8: the manifest verbs, the module ids, and their contract tests

**Files:**
- Create: `clients/os/src/system/modules.ts`
- Modify: `clients/os/src/system/registry.ts` (`OsAppSection`, `OsAppManifest`,
  `OsWidgetManifest`, one new function)
- Create: `clients/os/test/system/readinessContract.test.ts`
- Create: `component/envregistry/os_modules_parity_test.go`

**Interfaces:**
- Produces: `READINESS_MODULES` (const tuple), `ModuleId`, `MODULE_NAMES`,
  `MODULE_SETTINGS_SECTION`, `isModuleId(id)`; `requires?` and `wants?` on `OsAppSection`,
  `OsAppManifest`, `OsWidgetManifest`; `readinessProblem(app): string | null`;
  `requirementsFor(app, sectionId): { requires: ModuleId[]; wants: ModuleId[] }`.

- [ ] **Step 1: Write the failing contract test**

`clients/os/test/system/readinessContract.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { OS_REGISTRY } from "../../src/apps/registry";
import { READINESS_MODULES, isModuleId } from "../../src/system/modules";
import { readinessProblem, requirementsFor, type OsAppManifest } from "../../src/system/registry";

// The readiness contract (design record 2026-09-06-configuration-readiness,
// section 5.1): every `requires` and `wants` id names a module the engine
// declares. The list lives in src/system/modules.ts and a Go gate pins it to
// the env manifest, so a typo here fails the build on both sides.

function fakeApp(over: Partial<OsAppManifest>): OsAppManifest {
  return {
    id: "test",
    name: "Test",
    icon: () => null,
    sections: [{ id: "main", name: "Main" }, { id: "logs", name: "Logs", roles: { min: "admin" } }, { id: "settings", name: "Settings" }],
    settingsSection: "settings",
    logsSection: "logs",
    component: () => null,
    ...over,
  };
}

describe("the readiness contract", () => {
  it("every shipped requirement names a declared module", () => {
    const problems = OS_REGISTRY.apps.map(readinessProblem).filter((p) => p !== null);
    expect(problems).toEqual([]);
    expect(READINESS_MODULES.length).toBeGreaterThan(0);
  });

  it("at least one shipped app declares a requirement, so the sweep examined something", () => {
    const declared = OS_REGISTRY.apps.filter(
      (a) => (a.requires?.length ?? 0) > 0 || (a.sections ?? []).some((s) => (s.requires?.length ?? 0) > 0),
    );
    expect(declared.length).toBeGreaterThan(0);
  });

  it("fails an app naming an unknown module", () => {
    expect(readinessProblem(fakeApp({ requires: ["storagee"] as never }))).toMatch(/storagee/);
    expect(readinessProblem(fakeApp({ sections: [{ id: "main", name: "Main", wants: ["nope"] as never }], settingsSection: "main", logsSection: "main" }))).toMatch(/nope/);
  });

  it("fails an app that requires on its settings or logs section", () => {
    const app = fakeApp({ sections: [{ id: "settings", name: "Settings", requires: ["storage"] }, { id: "logs", name: "Logs", roles: { min: "admin" } }] });
    expect(readinessProblem(app)).toMatch(/settings/);
  });

  it("folds app-level and section-level requirements for a section", () => {
    const app = fakeApp({ requires: ["email"], sections: [{ id: "main", name: "Main", requires: ["storage"], wants: ["ai"] }, { id: "logs", name: "Logs", roles: { min: "admin" } }, { id: "settings", name: "Settings" }] });
    expect(requirementsFor(app, "main")).toEqual({ requires: ["email", "storage"], wants: ["ai"] });
    expect(requirementsFor(app, "settings")).toEqual({ requires: [], wants: [] });
    expect(requirementsFor(app, "logs")).toEqual({ requires: [], wants: [] });
  });

  it("isModuleId is a real predicate", () => {
    expect(isModuleId("storage")).toBe(true);
    expect(isModuleId("Storage")).toBe(false);
  });
});
```

Run: `cd /home/znas/memql-projects/memql/clients/os && npx vitest run test/system/readinessContract.test.ts`
Expected: FAIL (module and functions missing).

- [ ] **Step 2: Write `modules.ts`**

`clients/os/src/system/modules.ts`:

```ts
// The readiness module ids the engine declares (scripts/secrets/manifest.yaml,
// `modules:`), as the shell spells them.
//
// IT IS A COPY, AND A GO GATE PINS IT. component/envregistry/os_modules_parity_test.go
// reads the tuple below and fails the build the moment it disagrees with the
// manifest. Keep the tuple on ONE line with double-quoted ids: the gate parses
// it by regexp, not by executing TypeScript.
export const READINESS_MODULES = ["ai", "storage", "email", "githubApp", "campaigns", "workbench", "localApps"] as const;

export type ModuleId = (typeof READINESS_MODULES)[number];

export function isModuleId(id: string): id is ModuleId {
  return (READINESS_MODULES as readonly string[]).includes(id);
}

/** What a person calls the module. Sentence case, no variable names. */
export const MODULE_NAMES: Record<ModuleId, string> = {
  ai: "AI providers",
  storage: "Storage",
  email: "Email sender",
  githubApp: "GitHub App",
  campaigns: "Campaign sending",
  workbench: "Workbenches",
  localApps: "Local apps",
};

/**
 * Where a module is configured from the OS, when it is. Null means the
 * deployment: the Set up group names the variables instead of offering a
 * button. The engine says which through each lane's `configurableFrom`;
 * this map only knows which Settings section to open.
 */
export const MODULE_SETTINGS_SECTION: Record<ModuleId, { section: string; name: string } | null> = {
  ai: { section: "providers", name: "AI providers" },
  email: { section: "integrations", name: "Integrations" },
  storage: null,
  githubApp: null,
  campaigns: null,
  workbench: null,
  localApps: null,
};
```

- [ ] **Step 3: Add the verbs and the two functions to `registry.ts`**

Add `requires?: readonly ModuleId[]; wants?: readonly ModuleId[];` to `OsAppSection`,
`OsAppManifest` and `OsWidgetManifest` (import `ModuleId`, `isModuleId` from `./modules`).
Then, beside `logsSectionProblem`:

```ts
/**
 * The readiness contract, as a function for the same reason the settings one
 * is: the apps index can report the defect it gates on.
 *
 * A requirement on the settings or logs section is refused: those two are the
 * exemptions the window frame keeps reachable while an app is unconfigured,
 * and a requirement there would lock a person out of the one place they can
 * fix it.
 */
export function readinessProblem(app: OsAppManifest): string | null {
  const bad = (ids: readonly string[] | undefined, where: string): string | null => {
    for (const id of ids ?? []) {
      if (!isModuleId(id)) return `${app.id}: ${where} names unknown module "${id}"`;
    }
    return null;
  };
  const top = bad(app.requires, "requires") ?? bad(app.wants, "wants");
  if (top) return top;
  for (const section of app.sections ?? []) {
    const p = bad(section.requires, `section ${section.id} requires`) ?? bad(section.wants, `section ${section.id} wants`);
    if (p) return p;
    const exempt = section.id === app.settingsSection || section.id === app.logsSection;
    if (exempt && ((section.requires?.length ?? 0) > 0 || (section.wants?.length ?? 0) > 0)) {
      return `${app.id}: section ${section.id} is the settings or logs section and cannot carry a requirement`;
    }
  }
  return null;
}

/** The requirements that gate one section: the app's, then the section's own. */
export function requirementsFor(app: OsAppManifest, sectionId: string): { requires: ModuleId[]; wants: ModuleId[] } {
  if (sectionId === app.settingsSection || sectionId === app.logsSection) return { requires: [], wants: [] };
  const section = (app.sections ?? []).find((s) => s.id === sectionId);
  const dedupe = (ids: readonly ModuleId[]) => Array.from(new Set(ids));
  return {
    requires: dedupe([...(app.requires ?? []), ...(section?.requires ?? [])]),
    wants: dedupe([...(app.wants ?? []), ...(section?.wants ?? [])]),
  };
}
```

`readinessProblem` will report nothing until Task 12 adds requirements, and the second
test in Step 1 stays red until then; that is the reachable positive, deliberately left red
across the PR's own history rather than satisfied by a fake.

- [ ] **Step 4: Write the Go parity gate**

`component/envregistry/os_modules_parity_test.go`:

```go
package envregistry

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const osModulesPath = "../../clients/os/src/system/modules.ts"

var osModulesTuple = regexp.MustCompile(`(?m)^\s*export\s+const\s+READINESS_MODULES\s*=\s*\[([^\]]*)\]\s*as\s+const\s*;`)
var quotedId = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9]*)"`)

// The shell's module ids are a copy of the manifest's, and this is the gate
// that keeps them one list. Both directions: a module the shell does not know
// cannot be required by an app, and a module the shell names that the engine
// never declares would be an app that is unconfigured forever.
func TestOSModuleIdsMatchTheManifest(t *testing.T) {
	raw, err := os.ReadFile(osModulesPath)
	if err != nil {
		t.Fatalf("the shell's module list is unreadable at %s: %v", osModulesPath, err)
	}
	m := osModulesTuple.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s does not export READINESS_MODULES as a one-line `as const` tuple", osModulesPath)
	}
	var shell []string
	for _, q := range quotedId.FindAllSubmatch(m[1], -1) {
		shell = append(shell, string(q[1]))
	}
	manifest, err := LoadManifestFromBytes(embeddedManifest, "embedded")
	if err != nil {
		t.Fatal(err)
	}
	var engine []string
	for _, mod := range manifest.Modules {
		engine = append(engine, mod.Name)
	}
	if len(engine) == 0 {
		t.Fatal("the manifest declares no modules; this gate would compare against nothing")
	}
	sort.Strings(shell)
	sort.Strings(engine)
	if strings.Join(shell, ",") != strings.Join(engine, ",") {
		t.Fatalf("module ids disagree\nshell:  %v\nengine: %v", shell, engine)
	}
}
```

Run: `cd /home/znas/memql-projects/memql && go test -count=1 ./component/envregistry/ -run TestOSModuleIds -v`
Expected: PASS.

- [ ] **Step 5: Run the contract test**

Run: `cd /home/znas/memql-projects/memql/clients/os && npx vitest run test/system/readinessContract.test.ts test/settings test/logs && npm run typecheck`
Expected: every case PASS except "at least one shipped app declares a requirement", which
stays red until Task 12; the settings and logs contracts unchanged; typecheck clean.

- [ ] **Step 6: Commit**

```bash
git add clients/os/src/system/modules.ts clients/os/src/system/registry.ts \
  clients/os/test/system/readinessContract.test.ts component/envregistry/os_modules_parity_test.go
git commit -m "os: requires and wants on the manifest, the module ids, and the gates that pin them"
```

---

## Task 9: the feed and the fold mirror

**Files:**
- Create: `clients/os/src/system/readinessFold.ts`
- Create: `clients/os/src/live/readiness.tsx`
- Modify: `clients/os/src/chrome/access.tsx` (`SessionFacts.readiness`)
- Modify: `clients/os/src/chrome/Shell.tsx:153-187` (`SessionScope`)
- Create: `clients/os/test/system/readinessFold.test.ts`
- Create: `component/readiness/os_parity_test.go`

**Interfaces:**
- Produces: `NODE_LIVE_WINDOW_SECONDS`, `nodeIsLive(row, now)`, `foldReadiness(reports,
  nodes, now): Verdict[]`, types `ReadinessState`, `NodeReport`, `NodeLiveness`, `Verdict`;
  `type Readiness = { loaded: boolean; state: LiveState; of(id: ModuleId): Verdict | null;
  reseed(): void }`; `useReadinessFeed(): Readiness`; `SessionFacts.readiness?: Readiness`.
- Consumes: `connection.query.moduleReadinessAll` and `connection.query.staleClusterNodes`
  (generated in PR 1); the fixtures under `component/readiness/testdata/fold/`.

- [ ] **Step 1: Write the failing parity test**

`clients/os/test/system/readinessFold.test.ts`:

```ts
import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { foldReadiness, nodeIsLive, NODE_LIVE_WINDOW_SECONDS, type NodeLiveness, type NodeReport } from "../../src/system/readinessFold";

// THE SAME FIXTURES THE ENGINE RUNS. component/readiness/fold_test.go reads
// this directory too; a case one side passes and the other fails is the
// drift this test exists to catch. The directory is resolved from this file,
// so it does not depend on the working directory vitest was started from.
const FIXTURES = resolve(__dirname, "../../../../component/readiness/testdata/fold");

interface Fixture {
  name: string;
  now: string;
  reports: NodeReport[];
  nodes: NodeLiveness[];
  expect: { module: string; state: string; disagreement: string[] }[];
}

describe("the fold mirrors component/readiness", () => {
  const files = readdirSync(FIXTURES).filter((f) => f.endsWith(".json")).sort();
  it("finds the shared fixtures", () => {
    expect(files.length).toBeGreaterThanOrEqual(7);
  });
  for (const file of files) {
    const fx = JSON.parse(readFileSync(resolve(FIXTURES, file), "utf8")) as Fixture;
    it(fx.name, () => {
      const got = foldReadiness(fx.reports, fx.nodes, new Date(fx.now));
      expect(got.map((v) => ({ module: v.module, state: v.state, disagreement: v.disagreement }))).toEqual(fx.expect);
    });
  }
  it("nodeIsLive needs a live health word and a recent heartbeat", () => {
    const now = new Date("2026-09-06T12:00:00Z");
    const fresh = new Date(now.getTime() - (NODE_LIVE_WINDOW_SECONDS / 2) * 1000).toISOString();
    const stale = new Date(now.getTime() - (NODE_LIVE_WINDOW_SECONDS + 1) * 1000).toISOString();
    expect(nodeIsLive({ nodeId: "a", health: "healthy", lastSeen: fresh }, now)).toBe(true);
    expect(nodeIsLive({ nodeId: "a", health: "stopped", lastSeen: fresh }, now)).toBe(false);
    expect(nodeIsLive({ nodeId: "a", health: "healthy", lastSeen: stale }, now)).toBe(false);
    expect(nodeIsLive({ nodeId: "a", health: "healthy", lastSeen: "" }, now)).toBe(false);
  });
});
```

Run: `npx vitest run test/system/readinessFold.test.ts`
Expected: FAIL (module missing).

- [ ] **Step 2: Write the mirror**

`clients/os/src/system/readinessFold.ts`:

```ts
// The readiness fold, restated for the shell (design record
// 2026-09-06-configuration-readiness, section 4.5). component/readiness is
// the source; this is the copy the live feed runs, and the shared fixtures
// under component/readiness/testdata/fold hold the two equal.
//
// THE LITERAL IS PARSED. component/readiness/os_parity_test.go extracts
// NODE_LIVE_WINDOW_SECONDS by regexp and fails the build when it disagrees
// with NodeLiveWindow. Keep it a plain numeric literal.
export const NODE_LIVE_WINDOW_SECONDS = 60;

export type ReadinessState = "configured" | "partial" | "unconfigured" | "notApplicable" | "unreported";

export interface SlotReport { name: string; present: boolean; source: string; optional?: boolean }
export interface LaneReport { name: string; configurableFrom: string; complete: boolean; slots: SlotReport[] }

export interface NodeReport {
  module: string;
  nodeId: string;
  nodeType: string;
  state: ReadinessState;
  core?: boolean;
  lanes?: LaneReport[];
  reportedAt: string;
}

export interface NodeLiveness { nodeId: string; health: string; lastSeen: string }

export interface NodeVerdict { nodeId: string; nodeType: string; state: ReadinessState; reportedAt: string }

export interface Verdict {
  module: string;
  state: ReadinessState;
  core: boolean;
  disagreement: string[];
  nodes: NodeVerdict[];
  /** The live reporters' lanes, for the Set up group; empty when unreported. */
  lanes: LaneReport[];
}

const LIVE_HEALTH = new Set(["healthy", "connecting", "degraded", "draining"]);

export function nodeIsLive(n: NodeLiveness, now: Date): boolean {
  if (!LIVE_HEALTH.has(n.health)) return false;
  if (!n.lastSeen) return false;
  const seen = Date.parse(n.lastSeen);
  if (Number.isNaN(seen)) return false;
  return now.getTime() - seen <= NODE_LIVE_WINDOW_SECONDS * 1000;
}

function rank(s: ReadinessState): number {
  if (s === "unconfigured") return 2;
  if (s === "partial") return 1;
  return 0;
}

export function foldReadiness(reports: NodeReport[], nodes: NodeLiveness[], now: Date): Verdict[] {
  const live = new Set(nodes.filter((n) => nodeIsLive(n, now)).map((n) => n.nodeId));
  const kept = new Map<string, NodeReport[]>();
  const core = new Map<string, boolean>();
  const order: string[] = [];
  for (const r of reports) {
    if (!core.has(r.module)) {
      order.push(r.module);
      core.set(r.module, false);
    }
    if (r.core) core.set(r.module, true);
    if (!live.has(r.nodeId) || r.state === "notApplicable") continue;
    const list = kept.get(r.module) ?? [];
    list.push(r);
    kept.set(r.module, list);
  }
  order.sort();
  const out: Verdict[] = [];
  for (const module of order) {
    const rs = kept.get(module) ?? [];
    if (rs.length === 0) {
      out.push({ module, state: "unreported", core: core.get(module) ?? false, disagreement: [], nodes: [], lanes: [] });
      continue;
    }
    rs.sort((a, b) => rank(b.state) - rank(a.state) || (a.nodeId < b.nodeId ? -1 : a.nodeId > b.nodeId ? 1 : 0));
    const states = new Set(rs.map((r) => r.state));
    const verdict: Verdict = {
      module,
      state: states.size > 1 ? "partial" : rs[0]!.state,
      core: core.get(module) ?? false,
      disagreement: states.size > 1 ? rs.map((r) => `${r.nodeId}=${r.state}`) : [],
      nodes: rs.map((r) => ({ nodeId: r.nodeId, nodeType: r.nodeType, state: r.state, reportedAt: r.reportedAt })),
      lanes: rs[0]!.lanes ?? [],
    };
    out.push(verdict);
  }
  return out;
}
```

Run the test again. Expected: all PASS.

- [ ] **Step 3: Write the Go window parity gate**

`component/readiness/os_parity_test.go`:

```go
package readiness

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

const osFoldPath = "../../clients/os/src/system/readinessFold.ts"

var windowPattern = regexp.MustCompile(`(?m)^\s*export\s+const\s+NODE_LIVE_WINDOW_SECONDS\s*=\s*(\d+)\s*;`)

func TestNodeLiveWindowMatchesTheClient(t *testing.T) {
	raw, err := os.ReadFile(osFoldPath)
	if err != nil {
		t.Fatalf("the shell's fold is unreadable at %s: %v", osFoldPath, err)
	}
	m := windowPattern.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s does not export NODE_LIVE_WINDOW_SECONDS as a numeric literal", osFoldPath)
	}
	got, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	if want := int(NodeLiveWindow.Seconds()); got != want {
		t.Fatalf("NODE_LIVE_WINDOW_SECONDS is %d in the shell and %d in Go", got, want)
	}
}
```

Run: `cd /home/znas/memql-projects/memql && go test -count=1 ./component/readiness/ -run TestNodeLiveWindow -v`
Expected: PASS.

- [ ] **Step 4: Write the feed**

`clients/os/src/live/readiness.tsx`:

```tsx
import { useMemo } from "react";
import type { Row } from "@znasllc-io/memql-sdk-core/client";

import type { ModuleId } from "../system/modules";
import { foldReadiness, type NodeLiveness, type NodeReport, type Verdict } from "../system/readinessFold";
import { useLiveCollection } from "./useLiveCollection";
import type { LiveState } from "@znasllc-io/memql-sdk-core/client";

// THE READINESS FEED (design record 2026-09-06-configuration-readiness,
// section 5.2). Retained ONCE, in SessionScope, and provided through the
// session facts -- never re-read by a window or a Set up group, for the
// reason AccountsApp gives for its one feed: two subscriptions over one
// concept are free to disagree, and here the two readings would decide
// whether a setup surface or an app renders.
//
// Two collections: the readiness rows, and the cluster nodes whose liveness
// decides which rows count. Both broadcast (component/node/routing.go), so
// a change on any replica reaches the fold here.

export const MODULE_READINESS_CONCEPT = "v1:platform:moduleReadiness";
export const CLUSTER_NODE_CONCEPT = "v1:cluster:node";

export interface Readiness {
  /** True once BOTH feeds have seeded. Nothing is gated or drawn before. */
  loaded: boolean;
  /** The worse of the two feeds' states, for a surface that wants to say "stale". */
  state: LiveState;
  of(id: ModuleId): Verdict | null;
  reseed(): void;
}

function reportFromRow(row: Row): NodeReport | null {
  const r = row as Record<string, unknown>;
  const module = typeof r.module === "string" ? r.module : "";
  if (module === "") return null;
  return {
    module,
    nodeId: String(r.nodeId ?? ""),
    nodeType: String(r.nodeType ?? ""),
    state: String(r.state ?? "unconfigured") as NodeReport["state"],
    core: r.core === true,
    lanes: Array.isArray(r.lanes) ? (r.lanes as NodeReport["lanes"]) : [],
    reportedAt: String(r.reportedAt ?? ""),
  };
}

function livenessFromRow(row: Row): NodeLiveness {
  const r = row as Record<string, unknown>;
  const id = String(r.id ?? "").replace(/^v1:cluster:node:/, "");
  return { nodeId: id, health: String(r.health ?? "").toLowerCase(), lastSeen: String(r.lastSeen ?? "") };
}

const worse = (a: LiveState, b: LiveState): LiveState => {
  const order: LiveState[] = ["live", "seeding", "degraded", "disconnected"];
  return order.indexOf(a) >= order.indexOf(b) ? a : b;
};

export function useReadinessFeed(): Readiness {
  const rows = useLiveCollection<Row>("readiness:rows", (connection) => ({
    concept: MODULE_READINESS_CONCEPT,
    seed: async (_cursor, signal) => {
      const result = await connection.query.moduleReadinessAll({}, { signal });
      return { rows: result.rows(), nextCursor: "" };
    },
    paged: false,
  }));
  const nodes = useLiveCollection<Row>("readiness:nodes", (connection) => ({
    concept: CLUSTER_NODE_CONCEPT,
    seed: async (_cursor, signal) => {
      const result = await connection.query.staleClusterNodes({}, { signal });
      return { rows: result.rows(), nextCursor: "" };
    },
    paged: false,
  }));

  const rowsVersion = rows.snapshot.version;
  const nodesVersion = nodes.snapshot.version;
  const loaded = rows.snapshot.state !== "seeding" && nodes.snapshot.state !== "seeding"
    && rows.source !== null && nodes.source !== null;

  return useMemo(() => {
    void rowsVersion;
    void nodesVersion;
    const reports = rows.snapshot.rows.map(reportFromRow).filter((r): r is NodeReport => r !== null);
    const liveness = nodes.snapshot.rows.map(livenessFromRow);
    const verdicts = loaded ? foldReadiness(reports, liveness, new Date()) : [];
    const byModule = new Map(verdicts.map((v) => [v.module, v]));
    return {
      loaded,
      state: worse(rows.snapshot.state, nodes.snapshot.state),
      of: (id) => byModule.get(id) ?? null,
      reseed: () => {
        rows.reseed();
        nodes.reseed();
      },
    };
  }, [loaded, rowsVersion, nodesVersion, rows, nodes]);
}
```

`staleClusterNodes` returns the latest-per-id non-stopped nodes and, like every OS feed
over `v1:cluster:node`, the fold tolerates duplicates because it keys on node id. The fold
runs with `new Date()` at recompute time; a node that goes stale without any row changing is
re-judged on the next event, which is the same staleness the Fleet's online dot accepts.

- [ ] **Step 5: Provide it through the session**

In `clients/os/src/chrome/access.tsx`, add to `SessionFacts`:

```ts
  /**
   * Module readiness (design record 2026-09-06-configuration-readiness,
   * section 5.2). Optional for the same reason ladderLoaded is: every harness
   * that builds SessionFacts by hand keeps compiling, and absent reads as
   * "not loaded", under which nothing is gated and nothing is drawn.
   */
  readiness?: Readiness;
```

(import `type { Readiness } from "../live/readiness"`). In `Shell.tsx`'s `SessionScope`:

```tsx
  const ladderLoaded = useRoleLadder();
  const readiness = useReadinessFeed();
  const value = useMemo(
    () => ({ access: resolved, config, ladderLoaded, readiness }),
    [resolved, config, ladderLoaded, readiness],
  );
```

- [ ] **Step 6: Typecheck and run the shell suites**

Run: `npm run typecheck && npx vitest run test/system test/settings test/cluster`
Expected: clean typecheck; every existing suite green. Suites that mount `Shell` with a
fake connection whose `executeNamed` answers `rows: () => []` for unknown queries keep
working: two more seeds answer empty and `loaded` becomes true with no verdicts.

- [ ] **Step 7: Commit**

```bash
git add clients/os/src/system/readinessFold.ts clients/os/src/live/readiness.tsx \
  clients/os/src/chrome/access.tsx clients/os/src/chrome/Shell.tsx \
  clients/os/test/system/readinessFold.test.ts component/readiness/os_parity_test.go
git commit -m "os: the readiness feed, retained once, folded with the engine's own fixtures"
```

## Task 10: the kit -- two dot tones, the setup surface, the Set up group

**Files:**
- Create: `clients/os/src/kit/ReadinessStates.tsx`
- Modify: `clients/os/src/kit/index.tsx:57-80` (`ProvenanceDot` prop type) and the export list
- Modify: `clients/os/src/styles/index.css:89-106` (two dot tones) and append the surface
  and group rules
- Create: `clients/os/test/kit/readinessStates.test.tsx`

**Interfaces:**
- Produces: `type DotTone = ProvenanceTone | "needsSetup" | "partlySetUp"`;
  `ProvenanceDot({ tone: DotTone, label })`; `gateFor(readiness, requires, wants): Gate`
  where `Gate = { state: "unknown" | "ready" | "partial" | "unconfigured"; unmet: ModuleId[];
  wanted: ModuleId[] }`; `SurfaceUnconfigured({ surface, unmet, descriptions, canSetUp,
  onSetUp })`; `SetupGroup({ app, requires, wants })`; `markToneFor(gate): DotTone | null`.
- Consumes: `Readiness` (Task 9), `MODULE_NAMES`, `MODULE_SETTINGS_SECTION` (Task 8),
  `useOsIfPresent` (exists), `useSession` (exists).

- [ ] **Step 1: Write the failing kit tests**

`clients/os/test/kit/readinessStates.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SessionProvider } from "../../src/chrome/access";
import { UNKNOWN_RUNTIME_CONFIG } from "../../src/cluster/config";
import { gateFor, markToneFor, SetupGroup, SurfaceUnconfigured } from "../../src/kit/ReadinessStates";
import type { Readiness } from "../../src/live/readiness";
import type { Verdict } from "../../src/system/readinessFold";

function verdict(module: string, state: Verdict["state"], lanes: Verdict["lanes"] = []): Verdict {
  return { module, state, core: false, disagreement: [], nodes: [], lanes };
}

function readiness(loaded: boolean, verdicts: Verdict[]): Readiness {
  const by = new Map(verdicts.map((v) => [v.module, v]));
  return { loaded, state: "live", of: (id) => by.get(id) ?? null, reseed: () => {} };
}

function withSession(node: React.ReactNode, clusterRole: string) {
  return (
    <SessionProvider value={{ access: { userId: "u", primaryEmail: "u@example.com", clusterRole }, config: UNKNOWN_RUNTIME_CONFIG, ladderLoaded: true }}>
      {node}
    </SessionProvider>
  );
}

describe("gateFor", () => {
  it("is unknown while the feed has not loaded, whatever is required", () => {
    expect(gateFor(readiness(false, []), ["storage"], []).state).toBe("unknown");
  });
  it("is unconfigured when a required module is unconfigured or unreported", () => {
    expect(gateFor(readiness(true, [verdict("storage", "unconfigured")]), ["storage"], []).unmet).toEqual(["storage"]);
    expect(gateFor(readiness(true, []), ["storage"], []).state).toBe("unconfigured");
  });
  it("is partial when a required module is partial or a wanted one is not configured", () => {
    expect(gateFor(readiness(true, [verdict("storage", "partial")]), ["storage"], []).state).toBe("partial");
    expect(gateFor(readiness(true, [verdict("storage", "configured"), verdict("ai", "unconfigured")]), ["storage"], ["ai"]).state).toBe("partial");
  });
  it("is ready when every required module is configured and nothing wanted is missing", () => {
    expect(gateFor(readiness(true, [verdict("storage", "configured")]), ["storage"], []).state).toBe("ready");
    expect(gateFor(readiness(true, []), [], []).state).toBe("ready");
  });
});

describe("markToneFor", () => {
  it("draws only while a person is needed", () => {
    expect(markToneFor({ state: "unknown", unmet: [], wanted: [] })).toBeNull();
    expect(markToneFor({ state: "ready", unmet: [], wanted: [] })).toBeNull();
    expect(markToneFor({ state: "partial", unmet: [], wanted: ["ai"] })).toBe("partlySetUp");
    expect(markToneFor({ state: "unconfigured", unmet: ["storage"], wanted: [] })).toBe("needsSetup");
  });
});

describe("SurfaceUnconfigured", () => {
  it("offers the act to an owner and the sentence to everyone else", () => {
    const onSetUp = vi.fn();
    const { rerender } = render(
      <SurfaceUnconfigured surface="Campaigns" unmet={["email"]} descriptions={{ email: "Sending mail needs a mailbox this cluster can send from." }} canSetUp onSetUp={onSetUp} />,
    );
    expect(screen.getByRole("heading", { name: "Campaigns is not set up yet" })).toBeTruthy();
    expect(screen.getByText("Sending mail needs a mailbox this cluster can send from.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Set up Campaigns" }));
    expect(onSetUp).toHaveBeenCalledTimes(1);
    rerender(<SurfaceUnconfigured surface="Campaigns" unmet={["email"]} descriptions={{}} canSetUp={false} onSetUp={onSetUp} />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("An owner or developer can set it up in Settings.")).toBeTruthy();
  });
});

describe("SetupGroup", () => {
  it("lists each module with its state and points at the one place it is configured", () => {
    const r = readiness(true, [
      verdict("ai", "unconfigured"),
      verdict("storage", "configured", [{ name: "azure-blob", configurableFrom: "deployment", complete: true, slots: [] }]),
    ]);
    render(withSession(<SetupGroup app="Materializer" requires={["ai", "storage"]} wants={[]} readiness={r} />, "owner"));
    expect(screen.getByRole("heading", { name: "Set up" })).toBeTruthy();
    expect(screen.getByText("AI providers")).toBeTruthy();
    expect(screen.getByText("Not set up")).toBeTruthy();
    expect(screen.getByText("Storage")).toBeTruthy();
    expect(screen.getByText("Set up")).toBeTruthy();
    // No OsProvider in this test, so the act is the words, not a button.
    expect(screen.getByText("Settings, under AI providers")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Open AI providers" })).toBeNull();
  });
  it("names the variables for a deployment-only lane", () => {
    const r = readiness(true, [
      verdict("storage", "unconfigured", [{ name: "azure-blob", configurableFrom: "deployment", complete: false, slots: [{ name: "MEMQL_AZURE_BLOB_CONTAINER", present: false, source: "unset" }] }]),
    ]);
    render(withSession(<SetupGroup app="Files" requires={[]} wants={["storage"]} readiness={r} />, "developer"));
    expect(screen.getByText("Set in the deployment")).toBeTruthy();
    expect(screen.getByText("MEMQL_AZURE_BLOB_CONTAINER")).toBeTruthy();
  });
  it("renders nothing for a viewer", () => {
    const r = readiness(true, [verdict("ai", "unconfigured")]);
    const { container } = render(withSession(<SetupGroup app="Nexus" requires={["ai"]} wants={[]} readiness={r} />, "viewer"));
    expect(container.textContent).toBe("");
  });
});
```

Run: `npx vitest run test/kit/readinessStates.test.tsx`
Expected: FAIL (module missing).

- [ ] **Step 2: Widen the dot**

In `clients/os/src/kit/index.tsx`, change `ProvenanceDot`'s prop to `tone: DotTone` and add,
above it:

```tsx
/**
 * The dot's full vocabulary. The three provenance tones are aliveness; the two
 * setup tones are STATE, drawn only while a person is needed (design record
 * 2026-09-06-configuration-readiness, D5). Named by meaning so amber here is
 * never read as "machine unreachable" in Fleet.
 */
export type DotTone = ProvenanceTone | "needsSetup" | "partlySetUp";
```

and export the new pieces:

```tsx
export { SetupGroup, SurfaceUnconfigured, gateFor, markToneFor, type Gate } from "./ReadinessStates";
```

In `clients/os/src/styles/index.css`, after the `off` rule:

```css
/* The two SETUP tones (design record 2026-09-06-configuration-readiness, D5):
   drawn on an app's Settings entry and gear only while a person is needed.
   Red is --os-error because an unconfigured app is the one state in this
   shell where a person must act before anything else works; amber is the
   partial state. Configured and unknown draw NOTHING. */
.os-dot[data-os-dot="needsSetup"] {
  background: var(--os-error);
}
.os-dot[data-os-dot="partlySetUp"] {
  background: var(--os-warn);
}
```

and append:

```css
/* ---- the setup surface (mirrors .os-rank-refused) ---- */

.os-setup {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  min-height: 100%;
  padding: 48px 32px;
  text-align: center;
}
.os-setup-mark {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 1.5px solid var(--os-muted);
}
.os-setup-head {
  margin: 0;
  font-size: var(--os-text-md);
  font-weight: 600;
  color: var(--os-ink);
}
.os-setup-body {
  margin: 0;
  max-width: 42ch;
  font-size: var(--os-text-base);
  color: var(--os-muted);
}
.os-setup-next {
  margin: 6px 0 0;
}

/* ---- the Set up group: one row per module, rule 8's container language ---- */

.os-setup-group-rows {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.os-setup-row {
  display: grid;
  grid-template-columns: minmax(10ch, 1fr) minmax(8ch, auto) minmax(12ch, auto);
  gap: 12px;
  align-items: center;
  padding: 6px 0;
  border-top: 1px solid var(--os-line);
}
.os-setup-row:first-child {
  border-top: 0;
}
.os-setup-state {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--os-text-sm);
  color: var(--os-muted);
}
.os-setup-vars {
  margin: 0;
  font-family: var(--os-font-mono);
  font-size: var(--os-text-xs);
  color: var(--os-muted);
  overflow-wrap: anywhere;
}

/* The mark on a rail entry and on the gear. The entry becomes a flex row so
   the dot sits at the end without moving the label. */
.os-window-nav-item[data-os-setup] {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.os-icon-button[data-os-setup] {
  position: relative;
}
.os-icon-button[data-os-setup] .os-dot {
  position: absolute;
  top: 3px;
  right: 3px;
}
```

- [ ] **Step 3: Write the pieces**

`clients/os/src/kit/ReadinessStates.tsx`:

```tsx
import { useSession } from "../chrome/access";
import { useOsIfPresent } from "../chrome/state";
import type { Readiness } from "../live/readiness";
import { MODULE_NAMES, MODULE_SETTINGS_SECTION, type ModuleId } from "../system/modules";
import { roleAdmits } from "../system/roles";
import type { Verdict } from "../system/readinessFold";
import { Button, Caption, Panel, Subhead } from "./controls";
import { ProvenanceDot, type DotTone } from "./index";

// THE SETUP SURFACE AND THE SET UP GROUP (design record
// 2026-09-06-configuration-readiness, sections 5.3 to 5.5).
//
// # Not an error, and not styled as one
//
// An unconfigured app has nothing wrong with it. Like SurfaceRefused, this
// is quiet chrome: the off dot as the mark, one headline, the module's own
// sentence from the manifest, and the one act that resolves it. Red and
// amber are STATUS in this shell and appear only on the Settings entry.
//
// # Three states that must not read the same
//
// "The feed has not loaded", "not set up" and "set up" hide and show the
// same body. gateFor answers "unknown" for the first, and the window frame
// draws NOTHING for it -- a configured cluster must never flash a setup
// screen for a frame (the Accounts first-run card's rule).

export interface Gate {
  state: "unknown" | "ready" | "partial" | "unconfigured";
  /** Required modules that are not configured: the ones that gate. */
  unmet: ModuleId[];
  /** Wanted modules that are not configured: the ones that only mark. */
  wanted: ModuleId[];
}

const configured = (v: Verdict | null): boolean => v !== null && v.state === "configured";

export function gateFor(readiness: Readiness | undefined, requires: readonly ModuleId[], wants: readonly ModuleId[]): Gate {
  if (!readiness || !readiness.loaded) return { state: "unknown", unmet: [], wanted: [] };
  const unmet = requires.filter((id) => !configured(readiness.of(id)));
  const wanted = wants.filter((id) => !configured(readiness.of(id)));
  const anyPartial = requires.some((id) => readiness.of(id)?.state === "partial");
  if (unmet.some((id) => readiness.of(id)?.state !== "partial")) return { state: "unconfigured", unmet, wanted };
  if (anyPartial || wanted.length > 0) return { state: "partial", unmet, wanted };
  return { state: "ready", unmet, wanted };
}

export function markToneFor(gate: Gate): DotTone | null {
  if (gate.state === "unconfigured") return "needsSetup";
  if (gate.state === "partial") return "partlySetUp";
  return null;
}

/** Sentence-case words for a verdict, the Set up group's vocabulary. */
export function stateWords(v: Verdict | null): string {
  if (v === null || v.state === "unreported") return "Not reported";
  if (v.state === "configured") return "Set up";
  if (v.state === "partial") return "Partly set up";
  return "Not set up";
}

export function SurfaceUnconfigured({
  surface,
  unmet,
  descriptions,
  canSetUp,
  onSetUp,
}: {
  /** What they opened, named as they saw it: "Campaigns", or "Workbenches". */
  surface: string;
  unmet: readonly ModuleId[];
  /** The manifest descriptions, by module id; a missing one renders the module's name. */
  descriptions: Partial<Record<ModuleId, string>>;
  canSetUp: boolean;
  onSetUp: () => void;
}) {
  return (
    <div className="os-setup" data-os-setup-surface>
      <span className="os-setup-mark" aria-hidden />
      <h2 className="os-setup-head">{surface} is not set up yet</h2>
      {unmet.map((id) => (
        <p key={id} className="os-setup-body">
          {descriptions[id] ?? `${MODULE_NAMES[id]} is needed first.`}
        </p>
      ))}
      {canSetUp ? (
        <Button tone="primary" onClick={onSetUp}>Set up {surface}</Button>
      ) : (
        <p className="os-caption os-setup-next">An owner or developer can set it up in Settings.</p>
      )}
    </div>
  );
}

/** Whether this actor may configure: owner or developer, as a SET (the Integrations gate). */
export function canConfigure(clusterRole: string): boolean {
  return clusterRole === "owner" || clusterRole === "developer";
}

export function SetupGroup({
  app,
  requires,
  wants,
  readiness,
}: {
  app: string;
  requires: readonly ModuleId[];
  wants: readonly ModuleId[];
  /** Injected so the group is testable without a session feed; apps pass useSession().readiness. */
  readiness: Readiness | undefined;
}) {
  const { access } = useSession();
  const os = useOsIfPresent();
  const role = access?.clusterRole ?? "";
  if (!canConfigure(role)) return null;
  const ids = Array.from(new Set([...requires, ...wants]));
  if (ids.length === 0) return null;
  return (
    <Panel label={`Set up ${app}`}>
      <Subhead>Set up</Subhead>
      {!readiness || !readiness.loaded ? (
        <Caption>Reading this cluster's setup.</Caption>
      ) : (
        <div className="os-setup-group-rows">
          {ids.map((id) => {
            const v = readiness.of(id);
            const tone: DotTone | null = v?.state === "configured" ? null : v?.state === "partial" ? "partlySetUp" : v === null || v.state === "unreported" ? null : "needsSetup";
            const target = MODULE_SETTINGS_SECTION[id];
            const lane = v?.lanes.find((l) => !l.complete) ?? v?.lanes[0];
            const deploymentOnly = target === null;
            const reachable = target !== null && os !== null && os.layout === "desktop"
              && roleAdmits(role, target.section === "providers" ? { min: "owner" } : { any: ["owner", "developer"] });
            return (
              <div key={id} className="os-setup-row">
                <span>{MODULE_NAMES[id]}</span>
                <span className="os-setup-state">
                  {tone ? <ProvenanceDot tone={tone} label={`${MODULE_NAMES[id]}: ${stateWords(v)}`} /> : null}
                  {stateWords(v)}
                  {v && v.disagreement.length > 0 ? ` (${v.disagreement.join(", ")})` : ""}
                </span>
                {deploymentOnly ? (
                  <span>
                    <span className="os-caption">Set in the deployment</span>
                    {lane && lane.slots.length > 0 ? (
                      <p className="os-setup-vars">{lane.slots.filter((s) => !s.present).map((s) => s.name).join(" ")}</p>
                    ) : null}
                  </span>
                ) : reachable ? (
                  <Button onClick={() => { os!.actions.openApp("settings", target!.section); }}>Open {target!.name}</Button>
                ) : (
                  <span className="os-caption">Settings, under {target!.name}</span>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Panel>
  );
}
```

`os.layout` is added to `OsContextValue` in Task 11; until then this file does not
typecheck, so Task 10 and Task 11 commit together, or add the field first (Task 11 Step 2)
and come back. The `providers` gate is written out because that section is owner-only
while the group is owner-or-developer: a developer sees "Settings, under AI providers" and
who can open it is what the Settings app itself will tell them.

- [ ] **Step 4: Run the kit tests**

Run: `npx vitest run test/kit/readinessStates.test.tsx`
Expected: all PASS (after Task 11 Step 2's `layout` field exists).

- [ ] **Step 5: Commit (with Task 11)**

Committed in Task 11 Step 6.

---

## Task 11: the window frame, the phone shell, and the reactivity test

**Files:**
- Modify: `clients/os/src/chrome/state.tsx:168-189` (`OsContextValue.layout`) and the
  `OsProvider` props
- Modify: `clients/os/src/chrome/Shell.tsx:189-236` (`ShellRoster` passes `layout`)
- Modify: `clients/os/src/chrome/WindowFrame.tsx` (gear, rail entry, body branch)
- Modify: `clients/os/src/chrome/PhoneShell.tsx:51-75` (strip entry, body branch)
- Create: `clients/os/test/system/readinessReactivity.test.tsx`

**Interfaces:**
- Produces: `OsContextValue.layout: "desktop" | "phone"`; the window frame renders
  `SurfaceUnconfigured` and the marks from `gateFor(readiness, requirementsFor(...))`.
- Consumes: Task 8's `requirementsFor`, Task 9's `useSession().readiness`, Task 10's
  pieces. Descriptions come from the readiness rows: the fold carries none, so the frame
  reads them from a small map in `modules.ts` (Step 3 adds `MODULE_DESCRIPTIONS` there,
  pinned to the manifest by extending the Task 8 Go gate to compare descriptions too).

- [ ] **Step 1: Write the failing reactivity test**

`clients/os/test/system/readinessReactivity.test.tsx`. It copies the connection double of
`ladderReactivity.test.tsx` (the `h.connection` hoist, `fakeConnection`, `memStorage`,
`mountShell`, `launcherApps`) verbatim, with one change: `executeNamed` answers by query
name and `moduleReadinessAll` is deferred behind a gate the test opens by hand.

```tsx
// (imports and the vi.mock of ../../src/live/connection exactly as in ladderReactivity.test.tsx)

function readinessRow(module: string, state: string) {
  return { id: `v1:platform:moduleReadiness:${module}--bff-a`, module, nodeId: "bff-a", nodeType: "bff", state, core: true, lanes: [], reportedAt: "2026-09-06T11:58:00Z" };
}
function nodeRow() {
  return { id: "v1:cluster:node:bff-a", nodeType: "bff", health: "healthy", lastSeen: new Date().toISOString() };
}

function fakeConnection(readinessRows: unknown[]) {
  let openReadiness!: () => void;
  const gate = new Promise<void>((res) => { openReadiness = res; });
  const stub = {
    getMyAccess: vi.fn(async () => summary("owner")),
    activeRoles: vi.fn(async () => ({ rows: () => ladderRows(), meta: () => ({ cursor: "" }) })),
    executeNamed: vi.fn(async (name: string) => {
      if (name === "moduleReadinessAll") { await gate; return { rows: () => readinessRows, meta: () => null }; }
      if (name === "staleClusterNodes") return { rows: () => [nodeRow()], meta: () => null };
      return { rows: () => [], meta: () => null };
    }),
  };
  return {
    connection: {
      query: Object.setPrototypeOf(stub, QueryClient.prototype) as QueryClient,
      dispatcher: { sendAndWait: vi.fn() },
      subscriptions: { subscribeGraph: () => () => {} },
    },
    openReadiness,
  };
}

async function openApp(name: string) {
  const open = await screen.findByRole("button", { name: "Launcher" });
  fireEvent.click(open);
  const dialog = await screen.findByRole("dialog", { name: "Launcher" });
  fireEvent.click(within(dialog).getByRole("button", { name }));
}

describe("an unconfigured app gates only once readiness has loaded", () => {
  it("renders the app body while unknown, then the setup surface, with the mark on Settings", async () => {
    const { connection, openReadiness } = fakeConnection([readinessRow("email", "unconfigured"), readinessRow("campaigns", "configured")]);
    h.connection = connection;
    setRoleLadder(SEEDED_LADDER);
    mountShell();
    await openApp("Campaigns");
    const win = await screen.findByRole("dialog", { name: "Campaigns" });
    // Unknown: nothing gated, nothing drawn.
    expect(within(win).queryByText(/is not set up yet/)).toBeNull();
    expect(within(win).queryByRole("img", { name: /Campaigns is/ })).toBeNull();

    openReadiness();

    await waitFor(() => {
      expect(within(win).getByRole("heading", { name: "Campaigns is not set up yet" })).toBeTruthy();
    });
    expect(within(win).getByRole("img", { name: "Campaigns is not set up" })).toBeTruthy();
    // The rail is intact and Settings still answers.
    fireEvent.click(within(win).getByRole("button", { name: "Settings" }));
    await waitFor(() => {
      expect(within(win).queryByText(/is not set up yet/)).toBeNull();
    });
    expect(within(win).getByRole("heading", { name: "Set up" })).toBeTruthy();
  });

  it("draws nothing on a configured app", async () => {
    const { connection, openReadiness } = fakeConnection([readinessRow("email", "configured"), readinessRow("campaigns", "configured")]);
    h.connection = connection;
    setRoleLadder(SEEDED_LADDER);
    mountShell();
    await openApp("Campaigns");
    openReadiness();
    const win = await screen.findByRole("dialog", { name: "Campaigns" });
    await waitFor(() => expect(connection.query.executeNamed).toHaveBeenCalledWith("moduleReadinessAll", expect.anything(), expect.anything()));
    expect(within(win).queryByText(/is not set up yet/)).toBeNull();
    expect(within(win).queryByRole("img", { name: /Campaigns is/ })).toBeNull();
  });
});
```

Run: `npx vitest run test/system/readinessReactivity.test.tsx`
Expected: FAIL (no surface, no mark).

- [ ] **Step 2: `layout` on the OS context**

In `state.tsx`, add `layout: "desktop" | "phone";` to `OsContextValue` with the comment
"which chrome is rendering: the Set up group offers a button only where a window can open",
add a `layout` prop to `OsProvider`, and pass it through `value`. In `Shell.tsx`,
`ShellRoster` gains a `layout` prop that `Shell` passes (`layout === "phone" ? "phone" :
"desktop"`) and hands to `OsProvider`. Every existing harness that renders `OsProvider`
passes `layout="desktop"`; grep `<OsProvider` under `test/` and add it.

- [ ] **Step 3: Descriptions beside the ids**

Add to `modules.ts`:

```ts
/** The manifest descriptions, verbatim; the setup surface renders them. Pinned by the Go gate. */
export const MODULE_DESCRIPTIONS: Record<ModuleId, string> = {
  ai: "Inference needs a provider: a machine on your fleet serving a model, or a federated cloud vendor.",
  storage: "Files, materialized outputs, deploy bundles and log archives live in blob storage.",
  email: "Sending mail needs a mailbox this cluster can send from.",
  githubApp: "Connecting a source through the GitHub App needs the app registered on the identity node.",
  campaigns: "Sending a campaign needs a one-click unsubscribe secret and a reachable unsubscribe address.",
  workbench: "Workbenches need a workbench node this agent can reach.",
  localApps: "Running a task in Claude Code or Codex on your machine needs the agent to mint a session credential.",
};
```

Extend `TestOSModuleIdsMatchTheManifest` with a second regexp over the
`MODULE_DESCRIPTIONS` block (`(\w+):\s*"([^"]*)"` per line) comparing each string to the
manifest module's `Description`; keep each entry on one line for the regexp.

- [ ] **Step 4: The window frame**

In `WindowFrame.tsx`, after `const sections = ...` and `const current = ...`:

```tsx
  const { readiness } = useSession();
  const reqs = requirementsFor(manifest, current?.id ?? "");
  const gate = gateFor(readiness, reqs.requires, reqs.wants);
  const appReqs = requirementsFor(manifest, "__app__");
  const appGate = gateFor(readiness, [...(manifest.requires ?? [])], [...(manifest.wants ?? [])]);
  const settingsTone = markToneFor(appGate);
  void appReqs;
```

(`requirementsFor` with an unknown section id returns the app-level lists; the `void` line
can go once `appGate` is computed directly from the manifest as shown.) Then:

- On the gear button, add `data-os-setup={settingsTone ? "" : undefined}` and, inside it
  after the icon, `{settingsTone ? <ProvenanceDot tone={settingsTone} label={`${manifest.name} is ${settingsTone === "needsSetup" ? "not set up" : "partly set up"}`} /> : null}`.
- On the rail entry whose `section.id === manifest.settingsSection`, add the same
  `data-os-setup` attribute and the same dot after `{section.name}`.
- In the body, make the branch three-way:

```tsx
          {!roleAdmits(actorRole, manifest.roles) ? (
            <SurfaceRefused surface={manifest.name} requirement={manifest.roles} actorRole={actorRole} />
          ) : gate.state === "unconfigured" && gate.unmet.some((id) => !(manifest.requires ?? []).includes(id)) ? (
            <SurfaceUnconfigured
              surface={current?.name ?? manifest.name}
              unmet={gate.unmet}
              descriptions={MODULE_DESCRIPTIONS}
              canSetUp={canConfigure(actorRole)}
              onSetUp={() => actions.navigateSection(win.id, manifest.settingsSection)}
            />
          ) : gate.state === "unconfigured" ? (
            <SurfaceUnconfigured
              surface={manifest.name}
              unmet={gate.unmet}
              descriptions={MODULE_DESCRIPTIONS}
              canSetUp={canConfigure(actorRole)}
              onSetUp={() => actions.navigateSection(win.id, manifest.settingsSection)}
            />
          ) : (
            <WindowErrorBoundary key={win.id} app={manifest.id} section={current?.id ?? ""}>
              <Body ... />
            </WindowErrorBoundary>
          )}
```

The first unconfigured arm names the SECTION when the unmet module is a section-level
requirement ("Workbenches is not set up yet"); the second names the app. `requirementsFor`
already returns empty lists for the settings and logs sections, so those two always fall
through to the body. Import `SurfaceUnconfigured`, `gateFor`, `markToneFor`,
`canConfigure`, `ProvenanceDot` from `../kit`, `MODULE_DESCRIPTIONS` and `requirementsFor`
from `../system/...`, and `useSession` from `./access`.

- [ ] **Step 5: The phone shell**

In `PhoneShell.tsx`, compute the same `gate` and `settingsTone` for `current` and
`activeSection`; add the dot on the strip entry whose id is `current.settingsSection`; and
wrap `<current.component ...>` in the same unconfigured branch, with `onSetUp={() =>
setSectionId(current.settingsSection)}`. This is the phone shell's first body-replacement
branch; role refusal stays as it is.

- [ ] **Step 6: Run everything and commit**

Run: `npm run typecheck && npx vitest run test/system test/kit test/settings test/cluster test/accounts`
Expected: the reactivity test PASS; the Task 10 kit tests PASS; every older suite green.

```bash
git add clients/os/src/kit/ReadinessStates.tsx clients/os/src/kit/index.tsx clients/os/src/styles/index.css \
  clients/os/src/system/modules.ts clients/os/src/chrome/state.tsx clients/os/src/chrome/Shell.tsx \
  clients/os/src/chrome/WindowFrame.tsx clients/os/src/chrome/PhoneShell.tsx \
  clients/os/test/kit/readinessStates.test.tsx clients/os/test/system/readinessReactivity.test.tsx \
  component/envregistry/os_modules_parity_test.go
git commit -m "os: the setup surface, the two dot tones and the Set up group, on both renderers"
```

## Task 12: the app requirements and their Set up groups

**Files:**
- Modify: `clients/os/src/apps/{campaigns,materializer,nexus,fleet,deployables,files,training,users,logs}/settings.ts`
  (export `<APP>_REQUIRES` / `<APP>_WANTS`; add `requires`/`wants` to section entries)
- Modify: `clients/os/src/apps/registry.tsx` (manifests and the Ask widget)
- Modify: each of those apps' `*App.tsx` settings section (the `SetupGroup` at the top)
- Create: `clients/os/test/system/readinessMapping.test.ts`

**Interfaces:**
- Consumes: Task 8's verbs, Task 10's `SetupGroup`, `useSession().readiness`.

- [ ] **Step 1: Write the failing mapping test**

`clients/os/test/system/readinessMapping.test.ts` pins the table from the spec (section
5.1) so a later edit to one app cannot silently drop a gate:

```ts
import { describe, expect, it } from "vitest";

import { OS_REGISTRY } from "../../src/apps/registry";
import { requirementsFor } from "../../src/system/registry";

const app = (id: string) => {
  const found = OS_REGISTRY.apps.find((a) => a.id === id);
  if (!found) throw new Error(`no app ${id}`);
  return found;
};

describe("the readiness mapping (design record section 5.1)", () => {
  it("Campaigns requires email and campaign sending on every section", () => {
    expect(requirementsFor(app("campaigns"), "campaigns")).toEqual({ requires: ["email", "campaigns"], wants: [] });
    expect(requirementsFor(app("campaigns"), "settings")).toEqual({ requires: [], wants: [] });
  });
  it("Materializer requires ai and storage", () => {
    expect(requirementsFor(app("materializer"), "composer").requires).toEqual(["ai", "storage"]);
  });
  it("Nexus requires ai on goals, runs and approvals, not on automations", () => {
    for (const s of ["goals", "runs", "approvals"]) expect(requirementsFor(app("nexus"), s).requires).toEqual(["ai"]);
    expect(requirementsFor(app("nexus"), "automations").requires).toEqual([]);
  });
  it("Fleet requires workbench on Workbenches and localApps on Apps only", () => {
    expect(requirementsFor(app("fleet"), "workbenches").requires).toEqual(["workbench"]);
    expect(requirementsFor(app("fleet"), "apps").requires).toEqual(["localApps"]);
    expect(requirementsFor(app("fleet"), "machines").requires).toEqual([]);
  });
  it("Deployables, Files, Training, Users and Logs only want", () => {
    expect(requirementsFor(app("deployables"), "map")).toEqual({ requires: [], wants: ["storage", "githubApp"] });
    expect(requirementsFor(app("files"), "browse")).toEqual({ requires: [], wants: ["storage"] });
    expect(requirementsFor(app("training"), "upload")).toEqual({ requires: [], wants: ["ai"] });
    expect(requirementsFor(app("users"), "invites")).toEqual({ requires: [], wants: ["email"] });
    expect(requirementsFor(app("users"), "people")).toEqual({ requires: [], wants: [] });
    expect(requirementsFor(app("logs"), "stream")).toEqual({ requires: [], wants: ["storage"] });
  });
  it("the Ask widget requires ai", () => {
    expect(OS_REGISTRY.widgets.find((w) => w.id === "ask")?.requires).toEqual(["ai"]);
  });
  it("apps that declare nothing declare nothing", () => {
    for (const id of ["stores", "accounts", "bin", "cluster", "concepts", "settings"]) {
      const a = app(id);
      expect(a.requires ?? []).toEqual([]);
      expect(a.wants ?? []).toEqual([]);
      for (const s of a.sections ?? []) expect([...(s.requires ?? []), ...(s.wants ?? [])]).toEqual([]);
    }
  });
});
```

Run: `npx vitest run test/system/readinessMapping.test.ts`
Expected: FAIL on every case.

- [ ] **Step 2: Declare the requirements beside each app's sections**

Each app's `settings.ts` already exports its `*_SECTIONS` const. Add the module lists next
to it and use them from both the manifest and the settings section, so the gear and the
manifest cannot disagree. Campaigns, `clients/os/src/apps/campaigns/settings.ts`:

```ts
import type { ModuleId } from "../../system/modules";

/** Sending needs a mailbox and the unsubscribe pair; authoring waits on them too (spec 5.1). */
export const CAMPAIGNS_REQUIRES: readonly ModuleId[] = ["email", "campaigns"];
export const CAMPAIGNS_WANTS: readonly ModuleId[] = [];
```

and in `registry.tsx` the campaigns manifest gains `requires: CAMPAIGNS_REQUIRES, wants:
CAMPAIGNS_WANTS`. The same shape for the others:

| App | `settings.ts` exports | Where it goes |
|---|---|---|
| materializer | `MATERIALIZER_REQUIRES = ["ai", "storage"]` | manifest `requires` |
| nexus | on the section entries: `{ id: "goals", ..., requires: ["ai"] }`, same for `runs` and `approvals` | `NEXUS_SECTIONS` |
| fleet | `{ id: "workbenches", ..., requires: ["workbench"] }`, `{ id: "apps", ..., requires: ["localApps"] }` | `FLEET_SECTIONS` |
| deployables | `DEPLOYABLES_WANTS = ["storage", "githubApp"]` | manifest `wants` |
| files | `FILES_WANTS = ["storage"]` | manifest `wants` |
| training | `TRAINING_WANTS = ["ai"]` | manifest `wants` |
| users | `{ id: "invites", ..., wants: ["email"] }` | `USERS_SECTIONS` |
| logs | `LOGS_WANTS = ["storage"]` | manifest `wants` |
| ask widget | `requires: ["ai"]` | the `askWidget` manifest in `registry.tsx` |

Type the section consts as `OsAppSection[]` where they are not already, so the new keys
typecheck.

- [ ] **Step 3: Put the Set up group at the top of each Settings section**

In each of the nine apps' settings section component, immediately after `<Head ... />`
and before the first `<Panel>`, render the group with the app's own lists and the
session's readiness. Campaigns, in `CampaignsApp.tsx`'s `CampaignsSettingsSection`:

```tsx
  const { readiness } = useSession();
  ...
      <Head title="Campaigns settings" />
      <SetupGroup app="Campaigns" requires={CAMPAIGNS_REQUIRES} wants={CAMPAIGNS_WANTS} readiness={readiness} />
      <Panel label="Campaigns settings">
```

For an app whose requirements are on sections (Nexus, Fleet, Users), pass the union of its
sections' lists: `requires={NEXUS_SECTIONS.flatMap((s) => s.requires ?? [])}`. The group
sits ABOVE the preferences on purpose: it is the reason a person was sent here, and rule 4's
"micro-preferences live in Settings" was never about ordering. Deployables keeps its
Sources group where it is, below the preferences; the Set up group goes above them.

- [ ] **Step 4: The Ask widget**

In the widget's body component (`AskWidgetBody`), read `useSession().readiness` and, when
`gateFor(readiness, ["ai"], []).state === "unconfigured"`, render
`<p className="os-caption">{MODULE_DESCRIPTIONS.ai} An owner or developer can set it up in Settings.</p>`
in place of the prompt field. Nothing while unknown.

- [ ] **Step 5: Run the suites**

Run: `npm run typecheck && npx vitest run`
Expected: the mapping test and the Task 8 "at least one shipped app declares a requirement"
case now PASS; every app suite green. An app suite that mounts a settings section with a
hand-built `SessionFacts` needs no change: `readiness` absent reads as not loaded, and the
group renders its one caption.

- [ ] **Step 6: Commit**

```bash
git add clients/os/src/apps clients/os/test/system/readinessMapping.test.ts
git commit -m "os: which modules each app needs, and a Set up group at the top of its Settings"
```

---

## Task 13: the readiness column in Cluster, Modules

**Files:**
- Modify: `clients/os/src/apps/cluster/modules/rows.ts` (one function)
- Modify: `clients/os/src/apps/cluster/modules/ModulesSection.tsx` (the row's chip)
- Modify: `clients/os/test/cluster/modules.test.tsx` (one case; the file exists for this section)

- [ ] **Step 1: Write the failing test**

Add to the Modules suite, using its harness's `withSession` (pass `readiness` in the
session value, which `SessionFacts` now accepts):

```tsx
  it("shows the cluster-wide readiness beside a module that has one", async () => {
    const readiness = {
      loaded: true, state: "live" as const, reseed: () => {},
      of: (id: string) => (id === "storage" ? { module: "storage", state: "partial" as const, core: true, disagreement: ["bff-b=unconfigured", "bff-a=configured"], nodes: [], lanes: [] } : null),
    };
    render(withSession(<ModulesSection />, { readiness }));
    expect(await screen.findByText("Partly set up")).toBeTruthy();
    expect(screen.getByText(/bff-b=unconfigured/)).toBeTruthy();
  });
```

(Extend `withSession`'s overrides with an optional `readiness` that is spread into the
provider value.) Seed the inventory with a component module named `storage` the way the
suite's existing cases seed modules. Run: `npx vitest run test/cluster/modules.test.tsx`
Expected: FAIL.

- [ ] **Step 2: The join and the chip**

In `rows.ts`:

```ts
import { isModuleId } from "../../../system/modules";
import type { Readiness } from "../../../live/readiness";
import type { Verdict } from "../../../system/readinessFold";

/**
 * The cluster-wide readiness for a module row, when the registry's name is
 * also a readiness module id. Two readings of different scope: the
 * inventory is one node's answer, the verdict is every live node's, and
 * the column says which by carrying the disagreement.
 */
export function readinessForModule(module: Module, readiness: Readiness | undefined): Verdict | null {
  if (!readiness || !readiness.loaded || !isModuleId(module.name)) return null;
  return readiness.of(module.name);
}
```

In `ModulesSection.tsx`, where a row renders its state chip, add beside it:

```tsx
{(() => {
  const v = readinessForModule(m, readiness);
  if (v === null) return null;
  return (
    <Chip tone={v.state === "configured" ? "quiet" : "warn"}>
      {stateWords(v)}{v.disagreement.length > 0 ? ` (${v.disagreement.join(", ")})` : ""}
    </Chip>
  );
})()}
```

with `const { readiness } = useSession();` at the top of the component and `stateWords`
imported from `../../../kit`. Use whichever `ChipTone` values `controls.tsx` declares; if
`"warn"` is not one, use the tone the section already uses for `moduleStateNeedsAttention`.

- [ ] **Step 3: Run and commit**

Run: `npx vitest run test/cluster && npm run typecheck`

```bash
git add clients/os/src/apps/cluster/modules/rows.ts clients/os/src/apps/cluster/modules/ModulesSection.tsx \
  clients/os/test/cluster/modules.test.tsx clients/os/test/cluster/harness.tsx
git commit -m "os: the cluster-wide readiness beside each module in Cluster, Modules"
```

---

## Task 14: screenshots, the operator page, and PR 2

**Files:**
- Create (temporary, deleted before commit): `clients/os/qa.html`, `clients/os/qa/main.tsx`,
  `clients/os/vite.qa.config.ts`
- Create: `docs/internal/ops/2026-09-XX-configuration-readiness-visual-qa.md` (dated the day
  it runs)
- Create: `docs/public/operate/configuration-readiness.md`
- Delete: `docs/superpowers/plans/2026-09-06-configuration-readiness.md` (an executed plan
  is deleted in its epic's own merge)

- [ ] **Step 1: Build the QA harness and take the screenshots**

Follow `docs/internal/ops/2026-09-06-os-operator-parity-visual-qa.md` lines 12-66: a Vite
harness mounting the real `Shell` over a stubbed `useOsConnection` that returns a
MODULE-LEVEL singleton, with the swap plugin on the resolved path and `enforce: "pre"`,
`setRoleLadder(SEEDED_LADDER)` at startup, no `vitest` import, and a builtin answered as an
id-keyed map of nodes. The stub's `executeNamed` answers `moduleReadinessAll` with three
rows (`email` unconfigured, `campaigns` configured, `ai` partial) and `staleClusterNodes`
with one healthy node. Capture at 1600x1100 in both modes, each headless Chrome run with
its own `--user-data-dir`:

1. Campaigns open on Audiences: the setup surface, the rail intact, the red dot on Settings
   and on the gear (owner).
2. The same window as a viewer (`clusterRole: "viewer"`): the sentence, no button, no
   Set up group under Settings.
3. Campaigns, Settings: the Set up group with "Email sender: Not set up" and "Open
   Integrations", "Campaign sending: Set up".
4. Nexus on Automations with `ai` partial: the body renders, the amber dot on Settings.
5. Cluster, Modules: the readiness chip beside `storage` with a disagreement.
6. The phone layout (viewport 420x860): the strip entry's dot and the surface.

Record the run in the ops note with the six images' names, the fixture, and anything the
pixels disagreed with. Delete the harness files before committing.

- [ ] **Step 2: Write the operator page**

`docs/public/operate/configuration-readiness.md`, with the front-matter every public doc
carries (`title`, `audience: public`, `status: stable`, `area: operate`, `sinceVersion`,
`owner: znas`), covering in prose: what the mark on Settings means and when it is absent;
what "not reported" means and that it is not "not set up"; how the verdict is folded across
nodes and why two replicas can disagree mid-rollout; where each module is configured
(`configurableFrom`), naming the env variables per deployment-only lane; that a saved but
unapplied provider change reads as unconfigured until applied; and the `modules` block's
rules for adding a module. Link it from `docs/public/operate/env-vars.md` beside the
integration-config link, and add a GLOSSARY.md line. Run the docs gates:
`go test -count=1 -run 'TestDocsFrontMatter|TestDocsRelativeLinks|TestNoVendorDomainLiterals' .`

- [ ] **Step 3: Delete the plan, run everything, open PR 2**

```bash
git rm docs/superpowers/plans/2026-09-06-configuration-readiness.md
cd clients/os && npm run typecheck && npx vitest run && npm run build && cd ../..
make test 2>&1 | tail -5
go test -count=1 ./component/envregistry/ ./component/readiness/ -v | grep -E '^(--- |ok|FAIL)'
git add docs/public/operate/configuration-readiness.md docs/public/operate/env-vars.md GLOSSARY.md docs/internal/ops/
git commit -m "docs: configuration readiness for operators; the executed plan goes with its epic"
git push -u origin epic/configuration-readiness-os
```

PR body, with the three OS task numbers:

```
Epic 1 of the configuration-readiness program, OS half. `requires` and `wants` on the
manifest; one readiness feed retained in the session scope and folded with the engine's
own fixtures; the setup surface, the two dot tones and the Set up group on both renderers;
the per-app mapping from the design record's section 5.1; the readiness chip in Cluster,
Modules. Screenshots in docs/internal/ops/<date>-configuration-readiness-visual-qa.md.

Design record: docs/superpowers/specs/2026-09-06-configuration-readiness-design.md

Closes #<os-task-1>
Closes #<os-task-2>
Closes #<os-task-3>
Closes #<epic>

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01V3meB9SBytpmvzaWo9aHLF
```

Then `scripts/dev/merge-as-owner.sh --pr=<n> --check` once CI is green, and merge with the
script.

---

## Plan self-review

**Spec coverage.** Section 4.1 vocabulary: Task 2. Section 4.2 declaration: Task 1, with
the email deviation recorded up top and amended into the spec in Task 7. Section 4.3
evaluators: Task 4. Section 4.4 rows, tier, `@serverOnly`, routing: Tasks 3 and 5. Section
4.5 fold and builtin: Tasks 2 and 5, TS mirror in Task 9. Section 5.1 manifest and mapping:
Tasks 8 and 12. Section 5.2 feed: Task 9. Sections 5.3 to 5.5 surface, mark, group: Tasks
10 and 11. Section 5.6 Modules column: Task 13. Section 6 failure modes: boot and reload
hooks in Task 6, unknown-gates-nothing in Task 11's test, dead node and unreported in Task
2's fixtures and Task 6's db test. Section 7 testing: every item has a task except the
parity test the email deviation removed. Section 8 delivery: Tasks 0, 7 and 14. Section 9
out-of-scope items are not implemented.

**Placeholders.** The four `#<...>` issue numbers in the two PR bodies and the ops note's
date are filled from Task 0's output and the day of the run; there is no other. One harness
is copied rather than written here: `bootReadinessTestEngine` in Task 6 copies the engine
boot helper of `keyless_boot_test.go`, because that helper's exact signature was not read
while planning; the test bodies that use it are complete.

**Type consistency.** `readiness.NodeReport` JSON tags (Task 2) are the shape
`renderRecordModuleReadiness` writes (Task 5), `readModuleReadinessRows` decodes (Task 5)
and `readinessFold.ts`'s `NodeReport` reads (Task 9); `Verdict.lanes` exists only on the TS
side, added for the Set up group, and is documented there. `gateFor` returns `Gate` with
`state` in `unknown | ready | partial | unconfigured` (Task 10) and Tasks 11 and 12 branch on
those four words. `MODULE_SETTINGS_SECTION` names `providers` and `integrations`, the two
section ids `registry.tsx` declares for Settings. `OsContextValue.layout` is added in Task
11 Step 2 and read in Task 10's `SetupGroup`; the two tasks commit together.
