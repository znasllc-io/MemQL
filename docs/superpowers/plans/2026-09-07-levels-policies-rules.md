# Levels, policies and rules as DSL -- Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every call to a model declares a *level* instead of a model, goes through one router seam, and is decided by *rules* that map the call's metadata to a *policy*; the shipped rules express paid-last by construction and are re-seeded on every boot; every resolution is a decision record; and the work spine's disconnected failure path is wired.

**Architecture:** A new leaf package `core/airoute` holds the shared vocabulary (level, modality, needs, request, resolution, decision) so that both the engine (`component/memql`) and the router (`component/router`) — which are separate Go modules with a one-way dependency — can name it. `component/memql` gains a `rule` registry alongside its policy registry and an `AIResolver` seam it calls for every model call; `component/router` implements that seam, evaluating rules in precedence, expanding policy chains, resolving selectors, and writing one decision record per resolution.

**Tech Stack:** Go 1.26.1, MemQL DSL, PostgreSQL + TimescaleDB. Multi-module workspace (`go.work`, 49 modules).

**Spec:** `docs/superpowers/specs/2026-09-07-levels-policies-rules-design.md`
Program index: `docs/superpowers/specs/2026-09-07-local-first-routing-program.md`
Epic: memql#5127. Tasks: #5128-#5136. Branch: `epic/levels-policies-rules`. **One PR carrying all nine issues** (owner instruction, 2026-09-07, overriding the record's section 7 three-PR grouping).

---

## Global Constraints

Copied verbatim from the spec and from repo rules. Every task's requirements implicitly include this section.

- **Levels are a closed set of exactly four**: `fast`, `strong`, `reasoning`, `embeddings`. No fifth.
- **Degrade order is `reasoning` -> `strong` -> `fast`.** `embeddings` never degrades (a degraded embedder is a different vector space).
- **Policy entry grammar is closed**: a bare provider name; `fleet:strongest`, `fleet:fastest`, `fleet:<modelId>`; `app:*`, `app:<id>`; `federation:cheapest`, `federation:strongest`, `federation:<providerName>`; `policy:<name>`. **`fleet:*` is retired** and refuses load with `fleet:strongest` named in the message.
- **`@when` keys are closed**: `level`, `modality`, `prompt`, `role`, `actorRole`, `tag`, `touches`. Every key optional, all present keys ANDed.
- **Shipped policies after this epic are exactly three**: `localFirst`, `localOnly`, `federationStrongest`. `balancedChat`, `strongReasoning`, `cheapestCapable`, `fastCoding`, `backgroundExecution`, `backgroundEscalation` are deleted.
- **Shipped rules are exactly six**: `default`, `reasoningParks`, `embeddingsPark`, `backgroundLane`, `backgroundEscalation`, `operatorReasoning`.
- **`@maxLatencyMs`, `@maxTimeToFirstTokenMs`, `@preferredRole` are removed** from the grammar, the AST, the converter, the catalog projection and the docs map. `PolicyRegistry.DefaultForRole` and `byRole` are deleted.
- **`MinContextTokens` is never zero** on a request that reaches the router.
- **No `if env == "..."` in engine code** (`TestNoEnvironmentBranchingInEngineCode`).
- **No emojis** anywhere — docs, comments, CLI output, test names.
- **Stage files by explicit path** (`git add <file>`); never `git add -A` or `git add .`. The repo owner runs multiple sessions against this working tree.
- **Verify with `make test`, never `go test ./...`** — the bare pattern misses `component/memql`, `component/database` and `component/language`.
- **Commit format:** `Issue #<N>: <description>`, and every commit message ends with the two attribution lines in the epic's PR checklist (see Task 9).
- **Pre-release: no backwards-compat shims.** When a contract changes, fix both sides and delete what is no longer needed.

---

## Architecture corrections to the record

Two facts the record's section 9 did not list, found on 2026-09-07 at `0cd415a41`. Both go on issue #5127 as a comment and into section 4 of the record in this PR (Task 9).

### C1 -- The vocabulary lives in `core/airoute`, not `component/router/types.go`

`component/router` is its own Go module and **imports `component/memql`**; `component/memql` does not import it back, and cannot. Every call site the record re-points under D2 (`InvokeAI`, `InvokeAIStructured`, `InvokeAIChatWithFilteredTools`, `ai_tool_loop.go`, `ai_semantic_cache.go`, `ai_runtime.go`) lives **inside `component/memql`**. Declaring `ResolveRequest` in `component/router` would make D2 an import cycle.

Separately, `component/safety`, `component/fileprocessor`, `component/healing`, `component/work` and `component/planner` have **no dependency on `component/memql` at all** — they take an injected `common.ChatStructuredProvider` / `common.VisionAIProvider` from their caller. Putting the vocabulary in `component/memql` would add five module edges to name a four-value enum.

So the vocabulary goes in **`core/airoute`**, a new package inside the *existing* `core` module, which every module in the workspace already requires. No new `go.mod`; no module-boundary change. `component/router` type-aliases the names, so `router.ResolveRequest` keeps resolving for existing callers.

### C2 -- The engine reaches the router through an interface it owns

`component/memql` declares `AIResolver` and holds one, wired from `app/`. **Unwired means every model call refuses** with a typed refusal — never a silent fall-through to the retired resolution paths, and never a nil-provider panic. A test asserts `app/` performs the wiring, because a registered seam with an unwired setter is green and inert.

---

## File Structure

**New:**

| Path | Responsibility |
|---|---|
| `core/airoute/level.go` | `Level`, the closed four, parse, validate, `Degrade()`, `String()` |
| `core/airoute/request.go` | `Modality`, `Needs`, `ResolveRequest`, `Tag*` constants |
| `core/airoute/resolution.go` | `Resolution`, `ConsideredEntry`, `Decision` |
| `core/airoute/estimate.go` | `EstimateMinContextTokens` — the four-chars-per-token floor |
| `component/language/parser/rule_decl.go` | `ParseRuleDecl` |
| `component/language/parser/policy_entry.go` | `ValidatePolicyEntry` — the closed entry grammar, shared by the policy and rule parsers |
| `component/memql/unified_rule_loader.go` | `LoadUnifiedRules` |
| `component/memql/rule_registry.go` | `RuleRegistry`, `RuleConfig` |
| `component/memql/rule_converter.go` | `ruleDeclToRuleConfig` |
| `component/memql/policy_expand.go` | `policy:<name>` expansion + cycle detection |
| `component/memql/ai_resolver.go` | the `AIResolver` seam and its refusal when unwired |
| `component/memql/direct_registry_access_gate_test.go` | the Go AST gate |
| `component/router/rules.go` | rule evaluation in precedence, locked first |
| `component/router/selectors.go` | `strongest`, `fastest`, `cheapest` |
| `component/router/decision.go` | the decision record and its write |
| `component/routingrules/` | the deterministic rule renderer (the `component/emailrules` shape) |
| `dsl/rules/rules.memql` | the six shipped rules |
| `dsl/rules/README.md` | why the domain is core and sealed |
| `docs/public/operate/ai-routing.md` | levels, policies and rules for an operator |

**Modified (load-bearing only; each task names its own full list):**
`component/language/dslspec/constructs.go`, `component/language/ast/ast.go`, `component/language/annotations/registry.go`, `component/language/parser/{policy_decl,prompt_decl}.go`, `component/memql/{policy_parser,policy_loader,policy_converter,unified_policy_loader,unified_prompt_loader,ai_runtime,engine_ai,ai_tool_loop,ai_providers,ai_semantic_cache,authoring_sandbox}.go`, `component/router/{router,types,doors}.go`, `component/grpc/ai_handlers.go`, `dsl/policies/policies.memql`, `dsl/router/{concepts,queries,builtins}.memql`, every `dsl/**/prompts.memql`, `integrations/agent/{replier,nonstreaming,subagent}.go`, `integrations/{embedding,knowledge,similarity,harnessrecall,router,work}`, `app/`.

---

## Task order and dependencies

```
T1 core/airoute vocabulary  (no deps)
 |
 +-- T2 language: @level on prompt, the rule construct, the entry grammar   (needs T1)
 |     |
 |     +-- T3 loaders: rule registry, policy expansion, duplicate refusal, re-seed  (needs T2)
 |     |     |
 |     |     +-- T4 the seeded corpus: dsl/rules, dsl/policies, @level on every prompt  (needs T3)
 |     |     +-- T8 runtime authoring: sandbox kinds + routingRuleActivate  (needs T3, T4)
 |     |
 +-- T5 router: rule evaluation, selectors, needs, degrade or park  (needs T1, T3)
 |     |
 |     +-- T6 decision records + routerDecisionsRecent  (needs T5)
 |           |
 |           +-- T7 every call site through the seam + the AST gate + the chat handlers  (needs T5, T6)
 |                 |
 |                 +-- T9 the work failure path + the proving scenario  (needs T7)
 |
 +-- T10 docs, CLAUDE.md, the record correction, the architecture model  (last, needs everything)
```

Issue mapping: T1+T2 -> #5128 and #5129 (language half); T3 -> #5129; T4 -> #5130; T5 -> #5131; T6 -> #5132; T7 -> #5133; T8 -> #5135; T9 -> #5134; T10 -> #5136.

---

## Task 1: The `core/airoute` vocabulary

**Files:**
- Create: `core/airoute/level.go`, `core/airoute/request.go`, `core/airoute/resolution.go`, `core/airoute/estimate.go`
- Test: `core/airoute/level_test.go`, `core/airoute/estimate_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: everything below. **Every later task uses these exact names.** `core/airoute` imports nothing outside the standard library — assert that in a test, the way `component/proving`'s pure sub-packages do.

- [ ] **Step 1: Write the failing tests**

```go
// core/airoute/level_test.go
package airoute

import "testing"

func TestParseLevel_ClosedSet(t *testing.T) {
	for _, ok := range []string{"fast", "strong", "reasoning", "embeddings"} {
		if _, err := ParseLevel(ok); err != nil {
			t.Fatalf("ParseLevel(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "smart", "Fast", "cheap", "fast "} {
		_, err := ParseLevel(bad)
		if err == nil {
			t.Fatalf("ParseLevel(%q) was accepted; the set is closed", bad)
		}
		// The message must name all four, because it is what an author sees.
		for _, want := range []string{"fast", "strong", "reasoning", "embeddings"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("ParseLevel(%q) error %q does not name %q", bad, err, want)
			}
		}
	}
}

func TestLevelDegrade_ReasoningWalksDownAndEmbeddingsDoesNot(t *testing.T) {
	if next, ok := LevelReasoning.Degrade(); !ok || next != LevelStrong {
		t.Fatalf("reasoning degrades to %v (ok=%v); want strong", next, ok)
	}
	if next, ok := LevelStrong.Degrade(); !ok || next != LevelFast {
		t.Fatalf("strong degrades to %v (ok=%v); want fast", next, ok)
	}
	if _, ok := LevelFast.Degrade(); ok {
		t.Fatal("fast degraded; it is the floor")
	}
	// An embedder that degrades answers in a different vector space, so the
	// result is not a worse answer -- it is a wrong one.
	if _, ok := LevelEmbeddings.Degrade(); ok {
		t.Fatal("embeddings degraded; a degraded embedder is a different vector space")
	}
}
```

```go
// core/airoute/estimate_test.go
func TestEstimateMinContextTokens_IsNeverZero(t *testing.T) {
	if got := EstimateMinContextTokens("", 0); got <= 0 {
		t.Fatalf("estimate on an empty prompt is %d; it must never be zero -- a zero floor "+
			"admits every entry and is indistinguishable from 'not measured'", got)
	}
}

func TestEstimateMinContextTokens_CountsPromptAndCompletion(t *testing.T) {
	// Four characters per token, rounded UP, plus the completion budget.
	got := EstimateMinContextTokens(strings.Repeat("x", 401), 100)
	if want := 101 + 100; got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./core/airoute/...`
Expected: FAIL, package does not exist.

- [ ] **Step 3: Write `core/airoute/level.go`**

```go
// Package airoute is the shared vocabulary of MemQL's AI routing seam: the
// LEVEL a call declares, the MODALITY derived from it, the NEEDS it must be
// served with, the REQUEST that carries them, and the DECISION that comes back.
//
// It lives in core rather than beside the router because the two halves of the
// seam are separate Go modules pointing one way: component/router imports
// component/memql, and the call sites that build a request live inside
// component/memql. component/safety, component/fileprocessor, component/healing
// and component/work depend on neither and take an injected provider from their
// caller. core is the one module all of them already require.
//
// It imports nothing outside the standard library, asserted by a test.
package airoute

import (
	"fmt"
	"strings"
)

// Level is how much intelligence a call needs. It is a CLOSED set of four:
// an abstraction a person cannot hold in their head is not one (design D1).
type Level string

const (
	// LevelFast is triage, intake, classification and suggestion work.
	LevelFast Level = "fast"
	// LevelStrong is an agent's reply, a conductor turn, an authoring design.
	LevelStrong Level = "strong"
	// LevelReasoning is emitting or repairing a construct, and re-planning.
	LevelReasoning Level = "reasoning"
	// LevelEmbeddings is every embedding call. It is a level rather than a
	// modality flag so a rule can park it without naming a model.
	LevelEmbeddings Level = "embeddings"
)

// Levels is the closed set, in the order a person reads them.
func Levels() []Level { return []Level{LevelFast, LevelStrong, LevelReasoning, LevelEmbeddings} }

// ParseLevel accepts exactly the four values, exactly as spelled. The error
// names all four because it is what a DSL author and an operator both see.
func ParseLevel(s string) (Level, error) {
	switch Level(s) {
	case LevelFast, LevelStrong, LevelReasoning, LevelEmbeddings:
		return Level(s), nil
	}
	return "", fmt.Errorf("unknown level %q: a level is one of fast, strong, reasoning, embeddings", s)
}

// Degrade returns the next level down and whether there is one. The walk is
// reasoning -> strong -> fast, and it stops there. LevelEmbeddings never
// degrades: a degraded embedder answers in a different vector space, which is
// a wrong answer rather than a cheaper one.
func (l Level) Degrade() (Level, bool) {
	switch l {
	case LevelReasoning:
		return LevelStrong, true
	case LevelStrong:
		return LevelFast, true
	}
	return "", false
}

func (l Level) String() string { return string(l) }

// Valid reports whether l is one of the four.
func (l Level) Valid() bool { _, err := ParseLevel(string(l)); return err == nil }

// LevelNames is the four values as a comma-joined string, for error messages.
func LevelNames() string {
	out := make([]string, 0, 4)
	for _, l := range Levels() {
		out = append(out, string(l))
	}
	return strings.Join(out, ", ")
}
```

- [ ] **Step 4: Write `core/airoute/request.go`**

```go
package airoute

// Modality is DERIVED from the call, never declared (design D1). It says which
// provider interface must be satisfied, not which model.
type Modality string

const (
	ModalityChat           Modality = "chat"
	ModalityStreamingChat  Modality = "streamingChat"
	ModalityTools          Modality = "tools"
	ModalityStreamingTools Modality = "streamingTools"
	ModalityStructured     Modality = "structured"
	ModalityVision         Modality = "vision"
	ModalityEmbedding      Modality = "embedding"
	ModalitySpeech         Modality = "speech"
	ModalityTranscribe     Modality = "transcribe"
)

// Needs are the capability floors a provider must clear to serve the call.
// Vision, AudioIn, AudioOut and Image are declared here and consumed from
// epic 3; this epic sets them only where the call plainly has them.
type Needs struct {
	Structured bool
	Tools      bool
	Vision     bool
	AudioIn    bool
	AudioOut   bool
	Image      bool
	// MinContextTokens is the floor an entry's context window must clear. It
	// is NEVER zero on a request that reaches the router: a zero floor admits
	// every entry and is indistinguishable from "not measured".
	MinContextTokens int
}

// Call tags a rule can branch on. Tags are open (an author may set any
// string), but these are the ones the shipped rules name.
const (
	// TagBackground is a turn no human is waiting on.
	TagBackground = "background"
	// TagBackgroundEscalation is the one stronger continuation the background
	// lane swaps to when the cheap tier is stuck.
	TagBackgroundEscalation = "backgroundEscalation"
)

// ResolveRequest carries everything a rule may branch on and everything the
// decision record must say. Built by every call site that reaches a model.
type ResolveRequest struct {
	// Level is what the call declares it needs. Required: a request with no
	// level is refused rather than defaulted, because a guessed level is a
	// silent policy decision.
	Level Level
	// Modality is derived from the call site, not declared by an author.
	Modality Modality
	// Needs are the capability floors. MinContextTokens is never zero.
	Needs Needs

	// PromptName is the DSL prompt this call renders, empty for a Go call
	// site with no prompt. A rule may branch on it.
	PromptName string
	// Tags are call tags a rule may branch on (TagBackground, ...).
	Tags []string
	// Role is the AGENT's role slug. ActorRole is the calling human's cluster
	// role. They are different questions and a rule names them separately.
	Role      string
	ActorRole string
	// Touches is the call's footprint: a work step's footprint union, or an
	// agent turn's knowledge-domain concept ids. Empty otherwise. A rule
	// matches it with startsWith semantics.
	Touches []string

	// ExplicitProvider pins one registry entry and still wins over everything
	// (design D2). @defaultProvider rides this field.
	ExplicitProvider string

	// Attribution, unchanged in meaning from the pre-rules router.
	RequestId string
	UserId    string
	AgentId   string
	Partition string

	// CloudConsent is one person's explicit yes for THIS call.
	CloudConsent bool
}

// HasTag reports whether the request carries tag.
func (r ResolveRequest) HasTag(tag string) bool {
	for _, t := range r.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Write `core/airoute/resolution.go`**

```go
package airoute

// Door names where inference came from. The three values mirror
// component/router's door classification; they are declared here so a
// decision record can be read without importing the router.
const (
	DoorLocal      = "local"
	DoorApp        = "app"
	DoorFederation = "federation"
)

// ConsideredEntry is one line of the door report: an entry the chain walk
// passed over, or the one it took, and why. Kept on SUCCESS as well as on
// refusal (design D10) -- a rule is falsifiable only if the decisions it made
// can be read, and "what it did not pick" is half of that.
type ConsideredEntry struct {
	Entry  string `json:"entry"`
	Door   string `json:"door"`
	Reason string `json:"reason"`
}

// Resolution is what the router returns: the entry it picked and the whole
// decision that led there.
type Resolution struct {
	ProviderName string
	Vendor       string
	Model        string

	// Decision is what gets written to v1:router:call.
	Decision Decision
}

// Decision is one resolution, in the words the record uses. Every field is
// filled on success AND on refusal, so a park is as legible as a hit.
type Decision struct {
	Level Level
	// ServedLevel is what actually served. It differs from Level only when
	// Degraded is true. No degradation is silent (design D9).
	ServedLevel Level
	Degraded    bool

	// Rule is the name of the rule that matched, Policy the policy it named
	// (after expansion), Door the door the winning entry belongs to.
	Rule   string
	Policy string
	Door   string

	// Considered is the door report, kept on success too.
	Considered []ConsideredEntry

	// Touches is the request's footprint, copied through.
	Touches []string

	// MachineOwnerUserId names whose machine served a local call. Empty until
	// epic 4 (shared machines) fills it.
	MachineOwnerUserId string

	// Outcome is "ok" on a resolution and the refusal code on a park.
	Outcome string
}
```

- [ ] **Step 6: Write `core/airoute/estimate.go`**

```go
package airoute

// DefaultCompletionTokens is the completion budget assumed when a call
// declares none. Deliberately generous: the estimate is a FLOOR an entry must
// clear, and under-counting it admits a model that will truncate.
const DefaultCompletionTokens = 4096

// charsPerToken is the fixed estimate (design D8). It is not a tokenizer and
// is not meant to be one: a floor that is roughly right and never zero is
// worth more here than an exact count that costs a vendor round-trip.
const charsPerToken = 4

// EstimateMinContextTokens returns the context-window floor for a call whose
// rendered prompt is promptText and whose completion budget is
// completionTokens (pass 0 for the default).
//
// It is NEVER zero. A zero floor admits every entry, which reads on the
// decision record exactly like a floor that was measured and cleared.
func EstimateMinContextTokens(promptText string, completionTokens int) int {
	if completionTokens <= 0 {
		completionTokens = DefaultCompletionTokens
	}
	prompt := (len(promptText) + charsPerToken - 1) / charsPerToken
	if prompt < 1 {
		prompt = 1
	}
	return prompt + completionTokens
}
```

- [ ] **Step 7: Add the leaf-purity test**

```go
// core/airoute/purity_test.go -- the package's only guarantee that it stays
// nameable from every module. Mirrors component/proving's pure sub-packages.
func TestAirouteImportsOnlyTheStandardLibrary(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/znasllc-io/memql") && dep != "github.com/znasllc-io/memql/core/airoute" {
			t.Fatalf("core/airoute imports %s; it must stay a leaf so every module can name a level", dep)
		}
	}
}
```

- [ ] **Step 8: Run the tests**

Run: `go test ./core/airoute/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add core/airoute
git commit -F- <<'EOF'
Issue #5128: the core/airoute vocabulary -- level, modality, needs, request, decision

The four levels, the derived modality, the capability needs, the request every
call site builds and the decision every resolution produces, in a leaf package
inside the existing core module.

It is core rather than component/router because the seam's two halves are
separate modules pointing one way: component/router imports component/memql,
and the call sites that build a request live inside component/memql. Declaring
the request beside the router would make the whole re-pointing an import cycle.
component/safety, component/fileprocessor, component/healing and component/work
depend on neither and take an injected provider from their caller, so a home in
component/memql would add five module edges to name a four-value enum.
EOF
```

---

## Task 2: The language front end -- `@level`, the `rule` construct, the entry grammar

**Issues:** #5128 (the `@level` half), #5129 (the construct and grammar half).

**Files:**
- Create: `component/language/parser/rule_decl.go`, `component/language/parser/policy_entry.go`
- Create: `component/language/parser/rule_decl_test.go`, `component/language/parser/policy_entry_test.go`
- Modify: `component/language/dslspec/constructs.go` (a `rule` entry beside `policy`)
- Modify: `component/language/ast/ast.go` (`RuleDecl`; `Level` on `PromptDecl`; remove `MaxLatencyMs`, `MaxTimeToFirstTokenMs`, `PreferredRoles` from `PolicyDecl`)
- Modify: `component/language/annotations/registry.go` (`Rule` receiver; `level` on `Prompt` and `Rule`; delete the three policy annotations and their docs)
- Modify: `component/language/parser/policy_decl.go` (entry validation; refuse the three removed annotations)
- Modify: `component/language/parser/prompt_decl.go` (`@level`, required)

**Interfaces:**
- Consumes: `core/airoute.ParseLevel`, `airoute.Level`, `airoute.LevelNames`.
- Produces:

```go
// component/language/ast
type RuleDecl struct {
	Name          string
	Description   string
	DocComment    string
	When          RuleWhen
	Policy        string   // required
	Level         string   // optional; overrides the call's declared level
	Precedence    int
	OnUnavailable string   // "degrade" | "park"; empty means degrade
	Excludes      []string // repeatable @exclude("fleet:<modelId>")
	Locked        bool
	Enabled       bool
	Disabled      bool
}

type RuleWhen struct {
	Level     string
	Modality  string
	Prompt    string
	Role      string
	ActorRole string
	Tag       string
	Touches   string
	// Present records which keys the author actually wrote, so an absent key
	// and a key written empty are different conditions.
	Present map[string]bool
}

// PromptDecl gains:
//   Level string // required, one of the four

// component/language/parser
func ParseRuleDecl(src string) (*ast.RuleDecl, error)
func ValidatePolicyEntry(entry string) error   // the closed grammar, used by policy AND rule parsing
```

- [ ] **Step 1: Write the failing parser tests**

`component/language/parser/policy_entry_test.go` — a table over every accepted form and every refusal, with the `fleet:*` case asserting the hint:

```go
func TestValidatePolicyEntry_ClosedGrammar(t *testing.T) {
	for _, ok := range []string{
		"streamClaudeSonnet", "chat54Mini",
		"fleet:strongest", "fleet:fastest", "fleet:qwen3.8:27b",
		"app:*", "app:claude-code",
		"federation:cheapest", "federation:strongest", "federation:streamClaudeSonnet",
		"policy:localFirst",
	} {
		if err := ValidatePolicyEntry(ok); err != nil {
			t.Fatalf("ValidatePolicyEntry(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "  ", "fleet:", "app:", "federation:", "policy:", "cloud:cheapest", "fleet:strongest:extra"} {
		if err := ValidatePolicyEntry(bad); err == nil {
			t.Fatalf("ValidatePolicyEntry(%q) was accepted; the grammar is closed", bad)
		}
	}
}

func TestValidatePolicyEntry_FleetStarIsRetiredAndSaysWhatToWrite(t *testing.T) {
	err := ValidatePolicyEntry("fleet:*")
	if err == nil {
		t.Fatal("fleet:* was accepted; it is retired")
	}
	// The message must carry the replacement. An author who wrote fleet:*
	// meant "the best local model", and the new spelling for that is
	// fleet:strongest -- saying only "invalid" sends them to the source.
	if !strings.Contains(err.Error(), "fleet:strongest") {
		t.Fatalf("the refusal %q does not name fleet:strongest", err)
	}
}
```

`component/language/parser/rule_decl_test.go` — the full annotation surface, plus refusals:

```go
func TestParseRuleDecl_FullSurface(t *testing.T) {
	src := `@description("Operators reason at the reasoning level")
@when(prompt="agentReply", role="operator")
@level("reasoning")
@policy("localFirst")
@precedence(50)
@onUnavailable("park")
@exclude("fleet:qwen3.5:7b")
rule operatorReasoning { }`
	decl, err := ParseRuleDecl(src)
	// assert every field, including When.Present["prompt"] and When.Present["role"]
	// true and When.Present["level"] false.
}

func TestParseRuleDecl_RefusesUnknownWhenKey(t *testing.T) {
	// @when(model="gpt-5") -- "model" is not a @when key, and the message must
	// list the closed set, because the whole point of levels is that a call
	// site never names a model.
}

func TestParseRuleDecl_RequiresPolicy(t *testing.T)          {}
func TestParseRuleDecl_RefusesUnknownLevel(t *testing.T)     {}
func TestParseRuleDecl_RefusesUnknownOnUnavailable(t *testing.T) {}
func TestParseRuleDecl_RefusesNonEmptyBody(t *testing.T)     {}
func TestParseRuleDecl_RefusesRepeatedWhenKey(t *testing.T)  {
	// @when(level="fast", level="strong") collapses last-wins in a map, so the
	// value a reader sees is not the value the engine uses -- the same reason
	// a repeated argument name is refused everywhere else in the DSL.
}
```

And on the prompt side:

```go
func TestParsePromptDecl_RequiresLevel(t *testing.T) {
	// A prompt with no @level refuses, and the message names all four values.
}
func TestParsePromptDecl_RefusesUnknownLevel(t *testing.T) {}
```

And the removals:

```go
func TestParsePolicyDecl_RefusesTheThreeInertAnnotations(t *testing.T) {
	for _, ann := range []string{`@maxLatencyMs(60000)`, `@maxTimeToFirstTokenMs(500)`, `@preferredRole("operator")`} {
		// Each must refuse, naming the annotation. They steered nothing; an
		// author who writes one is telling the router something it will not
		// act on, and silence there is worse than a refusal.
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./component/language/parser/... -run 'Rule|PolicyEntry|PromptDecl_Requires|ThreeInert'`
Expected: FAIL — `ParseRuleDecl` / `ValidatePolicyEntry` undefined.

- [ ] **Step 3: Write `policy_entry.go`**

The grammar, in one function, with `fleet:*` refused by name before the generic path so its message carries the replacement. Selector sets: `fleet` -> `strongest|fastest|<modelId>`; `app` -> `*|<id>`; `federation` -> `cheapest|strongest|<providerName>`; `policy` -> `<name>`. A bare name (no colon) is a provider name and must be a valid identifier. **A `fleet:` model id may itself contain a colon** (`qwen3.8:27b`), so split on the FIRST colon only.

- [ ] **Step 4: Write `rule_decl.go`**

Model it on `policy_decl.go`. Read `@when`'s keyword arguments through the same mechanism `@trigger` and `@handler` use; refuse an unknown key naming the closed set; refuse a repeated key. `@policy` is required. `@exclude` is repeatable and each value goes through `ValidatePolicyEntry`. `@precedence` must parse as an integer. `@locked` is a bare flag — **the parser accepts it; refusing it outside the embedded tree is the loader's job** (Task 3), because the parser does not know which tree it is reading.

- [ ] **Step 5: Wire the construct into `dslspec` and `annotations`**

`dslspec/constructs.go`: a `rule` entry with `Keyword: "rule"`, `AnnotationReceiver: "Rule"`, mirroring `policy`'s shape exactly.
`annotations/registry.go`:
```go
"Rule": {
	"description", "enabled", "disabled",
	"when", "policy", "level", "precedence", "onUnavailable", "exclude", "locked",
},
```
`"Prompt"` gains `"level"`. `"Policy"` loses `"maxLatencyMs"`, `"maxTimeToFirstTokenMs"`, `"preferredRole"` and keeps the rest. Add a `Docs` entry for every new name and DELETE the three removed docs — `TestEveryAnnotationHasDoc` fails on a missing one, and a leftover doc for a removed annotation is what makes the editor offer something the parser refuses.

- [ ] **Step 6: Update the AST**

Add `RuleDecl` and `RuleWhen`. Add `Level` to `PromptDecl`. Remove the three fields from `PolicyDecl`. **Check `Registry.clone()` (or its equivalent) for a field-by-field copy** — a clone that lists fields by name silently drops a new one.

- [ ] **Step 7: Run the tests**

Run: `go test ./component/language/...`
Expected: PASS. Existing policy tests that assert the three removed fields must be DELETED, not adapted — pre-release, no shims.

- [ ] **Step 8: Commit**

```bash
git add component/language
git commit -m "Issue #5129: the rule construct, the closed policy-entry grammar, @level on prompt

..."
```

---

## Task 3: The loaders -- rule registry, policy expansion, duplicate refusal, boot re-seed

**Issue:** #5129.

**Files:**
- Create: `component/memql/rule_registry.go`, `component/memql/rule_converter.go`, `component/memql/unified_rule_loader.go`, `component/memql/policy_expand.go`
- Create: `component/memql/unified_rule_loader_test.go`, `component/memql/policy_expand_test.go`
- Modify: `component/memql/policy_parser.go` (drop the three fields), `component/memql/policy_loader.go` (delete `byRole` and `DefaultForRole`; add duplicate refusal), `component/memql/policy_converter.go`, `component/memql/unified_policy_loader.go`, `component/memql/unified_prompt_loader.go` (require `@level`), and the engine `Init` path that calls the loaders.

**Interfaces:**
- Consumes: `ast.RuleDecl`, `parser.ParseRuleDecl`, `parser.ValidatePolicyEntry`, `airoute.Level`.
- Produces:

```go
// component/memql
type RuleConfig struct {
	Name          string
	Description   string
	When          RuleWhen        // the same seven keys, with Present
	Policy        string
	Level         airoute.Level   // empty means "do not override"
	Precedence    int
	OnUnavailable string          // "degrade" | "park"
	Excludes      []string
	Locked        bool
	SourceFile    string          // for the duplicate message
}

type RuleWhen struct {
	Level, Modality, Prompt, Role, ActorRole, Tag, Touches string
	Present map[string]bool
}

type RuleRegistry struct{ /* mu, byName, ordered */ }

func NewRuleRegistry() *RuleRegistry
func (r *RuleRegistry) Lookup(name string) (*RuleConfig, bool)
// Ordered returns every enabled rule in EVALUATION order: locked rules first
// (in their own precedence order), then unlocked, precedence descending. The
// order is computed once at load, not per call.
func (r *RuleRegistry) Ordered() []*RuleConfig
func (r *RuleRegistry) Count() int

func LoadUnifiedRules(logger *slog.Logger, registry *RuleRegistry, report ...*LoadReport) (int, error)

// PolicyConfig gains the expanded chain. ProviderChain() returns it.
func (p PolicyConfig) ProviderChain() []string
```

- [ ] **Step 1: Write the failing loader tests**

```go
func TestLoadUnifiedRules_RegistersTheShippedSix(t *testing.T)  // after Task 4 seeds them
func TestRuleRegistry_OrderedPutsLockedFirstRegardlessOfPrecedence(t *testing.T) {
	// A locked rule at precedence 0 evaluates BEFORE an unlocked one at 900.
	// This is what "locked" means operationally: an owner may add rules and
	// give them any precedence they like, and the shipped ones still run first.
}
func TestRuleRegistry_TiedPrecedenceIsALoadError(t *testing.T) {
	// Two unlocked rules at one precedence: refused, naming BOTH rules and
	// BOTH files. A tie resolved by map order is a routing decision nobody
	// wrote and nobody can reproduce.
}
func TestPolicyExpansion_ExpandsPolicyColonAtLoad(t *testing.T)
func TestPolicyExpansion_RefusesACycleNamingTheLoop(t *testing.T) {
	// a -> b -> a. The message must print the loop, because an author with
	// four policies cannot find it from "cycle detected".
}
func TestPolicyExpansion_RefusesADepthBomb(t *testing.T)
func TestDuplicateRefusal_AcrossNamespaces(t *testing.T) {
	// The SAME policy name in two files refuses, naming both paths. The
	// pre-existing behaviour was bare-name last-wins, so a product bundle
	// could silently replace a shipped policy.
}
func TestBootReseed_RestoresAShippedPolicyEditedAtRuntime(t *testing.T)
func TestPromptLoader_RefusesAPromptWithNoLevel(t *testing.T)
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./component/memql/ -run 'Rule|PolicyExpansion|DuplicateRefusal|BootReseed|PromptLoader_Refuses'`
Expected: FAIL.

- [ ] **Step 3: Write the rule registry, converter and loader**

`unified_rule_loader.go` mirrors `unified_policy_loader.go` exactly: `baseloader.ReadAll(logger)` -> `ExtractKeywordSlices(raw.Content, "rule")` -> `languageParser.ParseRuleDecl(slice.Source)` -> `ruleDeclToRuleConfig` -> registry. Two differences:
1. **`@locked` is refused when `raw.Path` is not from the embedded tree.** The loader knows the origin; the parser does not.
2. **Registration REFUSES a duplicate** rather than overwriting, and the error names both files. This is the same change the policy registry gets.

`RuleRegistry.Ordered()` computes the evaluation order once at load: partition by `Locked`, sort each partition by `Precedence` descending, concatenate locked-then-unlocked. Stable, so the order is reproducible across replicas.

- [ ] **Step 4: Write `policy_expand.go`**

After every policy is registered, walk each chain and replace `policy:<name>` with the named policy's expanded chain, depth-first, tracking the visit stack. A repeat in the stack is a cycle: refuse, printing the stack joined by ` -> `. Cap the depth at 16 and refuse past it. The expanded chain is what `ProviderChain()` returns; **the router never sees a `policy:` entry**.

Validate every entry through `parser.ValidatePolicyEntry` at this point too, so a mounted bundle's chain is held to the grammar.

- [ ] **Step 5: Delete `DefaultForRole` and `byRole`**

Delete the field, the method, the map maintenance in the loader, and every caller. `integrations/agent/replier.go` is the only production caller and Task 4 removes it. Deleting the method with a caller still standing is a compile error, which is the order to want.

- [ ] **Step 6: Require `@level` on every prompt at load**

In `unified_prompt_loader.go`, a prompt whose `Level` is empty or unparseable is a **skip on the LoadReport**, which strict boot refuses. `MEMQL_DSL_ALLOW_SKIPS` remains the break-glass. This is what holds a bundle mounted at `MEMQL_DSL_PATH` to the same rule as the embedded tree.

- [ ] **Step 7: Wire `LoadUnifiedRules` into engine Init**

Beside `LoadUnifiedPolicies`. **Re-seed on every boot**: the loaders already re-read the embedded tree at Init, so what has to be added is that a runtime-authored row can never win — registration refuses a name a shipped construct owns (Task 8 relies on this).

- [ ] **Step 8: Run the tests**

Run: `MEMQL_REQUIRE_DB=1 go test ./component/memql/ -run 'Rule|Policy|Prompt'` then `make test`.

- [ ] **Step 9: Commit**

---

## Task 4: The seeded corpus

**Issue:** #5130.

**Files:**
- Create: `dsl/rules/rules.memql`, `dsl/rules/README.md`
- Rewrite: `dsl/policies/policies.memql`
- Modify: every `dsl/**/prompts.memql` (add `@level`)
- Modify: `integrations/agent/replier.go`, `nonstreaming.go`, `subagent.go`, `component/memql/ai_providers.go`
- Test: `test/dslconformance/rules_test.go` (new), `test/dslconformance/local_first_policies_test.go` (rewritten)

**Interfaces:**
- Consumes: the `rule` construct and the entry grammar from Tasks 2-3.
- Produces: the six rule names and three policy names every later task and every conformance gate asserts by name.

- [ ] **Step 1: Write `dsl/policies/policies.memql`**

Exactly three policies, each with a comment saying what it is FOR (the file's own standing rule: a policy nothing names is a decoration):

```memql
/// The default chain. Local strongest, then a signed-in app on the person's own
/// machine, then the cheapest federated model that qualifies. The three steps are
/// three kinds of cost in increasing order, and the federation hop is the one the
/// cost ceiling gates.
@primary("fleet:strongest")
@fallback("app:*")
@fallback("federation:cheapest")
policy localFirst { }

/// Local only. Named by a rule for work that must not leave the person's hardware.
/// It has no fallback on purpose: when the fleet cannot serve the call, the rule's
/// onUnavailable decides, and every door being shut is the honest answer.
@primary("fleet:strongest")
policy localOnly { }

/// Local strongest, an app, then the STRONGEST federated model rather than the
/// cheapest. Named by a rule for work where a wrong answer costs more than a
/// dear one.
@primary("fleet:strongest")
@fallback("app:*")
@fallback("federation:strongest")
policy federationStrongest { }
```

Delete the six retired policies and the long header comment's now-false paragraphs; keep the paragraph explaining the three kinds of cost, updated to the new spellings.

- [ ] **Step 2: Write `dsl/rules/rules.memql`**

The six, with precedence chosen so the ordering is readable and every value is distinct:

```memql
/// The floor. It matches every call and cannot be removed: a call that matches no
/// rule is impossible by construction rather than by care.
@when()
@policy("localFirst")
@precedence(0)
@onUnavailable("degrade")
@locked
rule default { }

/// Reasoning parks rather than degrades. A degraded reasoning call returns a
/// confident answer produced by a model that could not do the work, which is worse
/// than no answer because nothing downstream can tell the difference.
@when(level="reasoning")
@policy("federationStrongest")
@precedence(100)
@onUnavailable("park")
@locked
rule reasoningParks { }

/// Embeddings park. A degraded embedder answers in a DIFFERENT VECTOR SPACE, so a
/// degraded embedding is not a worse vector -- it is one that does not belong in
/// the index it is about to be written to.
@when(level="embeddings")
@policy("localFirst")
@precedence(110)
@onUnavailable("park")
@locked
rule embeddingsPark { }

/// Background work: nobody is waiting, so degrade freely.
@when(tag="background")
@policy("localFirst")
@precedence(40)
@onUnavailable("degrade")
@locked
rule backgroundLane { }

/// The one stronger continuation the background lane swaps to when the cheap tier
/// is stuck. It raises the LEVEL rather than naming a model.
@when(tag="backgroundEscalation")
@level("strong")
@policy("localFirst")
@precedence(50)
@onUnavailable("degrade")
@locked
rule backgroundEscalation { }

/// An operator's agent reply reasons. This is the one shipped rule that exists to
/// be READ as an example: it shows a rule raising a call's level from what the
/// prompt declared.
@when(prompt="agentReply", role="operator")
@level("reasoning")
@policy("federationStrongest")
@precedence(60)
@onUnavailable("degrade")
@locked
rule operatorReasoning { }
```

- [ ] **Step 3: Add `@level` to every prompt in the corpus**

Per the record's D3 assignment, by name:
- `fast`: triage, intake, `classifySymptom`, `docSummary`, `askSpecialist`, `seedDomainBridge`, `seedDomainContent`, every suggest domain, the safety classifier, healing's patch proposal.
- `strong`: `agentReply`, `agentFactoryAnalyze`, `reactiveConductor`, `authoringDesign`, `trainerAgent`, the Ask surface.
- `reasoning`: `authoringEmit`, `authoringRepair`, `replanGap`, `compileGoal`.
- `embeddings`: every embedding site.

Any prompt the record's list does not name gets a level chosen from what it does, and **the choice is recorded in the conformance test by name** so it is a decision rather than a default.

- [ ] **Step 4: Delete the Go literals**

- `integrations/agent/replier.go`: the role-to-policy string literal. The replier sets `Role` on the request instead; `operatorReasoning` is what makes the operator case work.
- `integrations/agent/subagent.go`: the lane constants. The subagent sets `TagBackground` / `TagBackgroundEscalation` instead.
- `component/memql/ai_providers.go`: the hard-coded preferred list (`chat54, chat54Mini, chat54Nano, chat54Pro, chat53Latest`). Every one is a paid vendor model unreachable on a local cluster; the chain is what picks now.

- [ ] **Step 5: Write the conformance gates**

```go
// test/dslconformance/rules_test.go
func TestEveryShippedRuleNamesAShippedPolicy(t *testing.T)
func TestShippedRulesAreExactlyTheSix(t *testing.T)          // by name
func TestEveryShippedRuleIsLocked(t *testing.T)
func TestPrecedenceValuesAreDistinct(t *testing.T)
func TestLocalFirstTriesLocalThenAppThenFederation(t *testing.T) {
	// Assert the ORDER by door, not the entry strings -- the point of the
	// policy is the order of the kinds of cost.
}
func TestEveryPromptCarriesALevel(t *testing.T)
func TestTheD3LevelAssignmentByName(t *testing.T)            // the table, spelled out
func TestNoGoSourceNamesAPolicyByStringLiteral(t *testing.T) {
	// Outside tests and outside dsl/. This is the gate that keeps the Go
	// literals from growing back.
}
func TestNoDslFileWritesFleetStar(t *testing.T)
```

- [ ] **Step 6: Run**

Run: `go test ./test/dslconformance/...` then `make test` and `go run ./cmd/memqllint`.

- [ ] **Step 7: Commit**

---

## Task 5: The selection seam

**Issue:** #5131.

**Files:**
- Create: `component/router/rules.go`, `component/router/selectors.go`
- Create: `component/router/rules_test.go`, `component/router/selectors_test.go`, `component/router/degrade_test.go`
- Modify: `component/router/router.go` (`resolveChain` becomes rule-driven), `component/router/types.go` (aliases to `core/airoute`), `component/router/doors.go` (report entries become `airoute.ConsideredEntry`)

**Interfaces:**
- Consumes: `airoute.*` from Task 1; `memql.RuleRegistry`, `memql.PolicyRegistry`, `memql.ProviderRegistry` from Task 3.
- Produces:

```go
// component/router
type ResolveRequest = airoute.ResolveRequest   // alias; router.ResolveRequest still resolves
type Resolution = airoute.Resolution

// New wires the rule registry in. A nil rule registry is a REFUSAL, not a
// fallback: without rules there is no default rule, and the pre-rules
// precedence would silently re-appear.
func New(providers *memql.ProviderRegistry, policies *memql.PolicyRegistry, rules *memql.RuleRegistry, engine Engine, logger *slog.Logger) *Router

// MatchRule returns the first rule that matches, in evaluation order.
// It never returns nil: `default` matches everything and refuses removal.
func (r *Router) MatchRule(req ResolveRequest) *memql.RuleConfig

// ResolveStructured, ResolveVision, ResolveEmbedding, ResolveSpeech,
// ResolveTranscribe join the three existing Resolve* entry points.
```

- [ ] **Step 1: Write the failing tests**

```go
func TestMatchRule_LockedFirstThenPrecedence(t *testing.T)
func TestMatchRule_AllPresentWhenKeysAreANDed(t *testing.T)
func TestMatchRule_TouchesUsesStartsWith(t *testing.T)
func TestMatchRule_DefaultIsTheFloorAndAlwaysMatches(t *testing.T)
func TestMatchRule_RoleAndActorRoleAreDifferentQuestions(t *testing.T) {
	// A rule on role="operator" must NOT match a call whose ACTOR is an
	// operator driving a non-operator agent. Collapsing the two would route on
	// who is watching rather than on what is acting.
}

func TestSelectorStrongest_ParametersThenContextWindowMissingLast(t *testing.T)
func TestSelectorFastest_FewestParametersWhenNothingIsMeasured(t *testing.T)
func TestSelectorCheapest_InputPlusOutputAscending(t *testing.T)
func TestSelectorCheapest_ARecordWithNoCostFiguresSortsLastAndIsReported(t *testing.T) {
	// A vendor record missing its prices must be VISIBLE rather than silently
	// free -- sorting it first is how a missing figure becomes the cheapest
	// thing in the catalog.
}
func TestSelectorsSkipEntriesUnderTheContextFloor(t *testing.T)
func TestSelectorsSkipEntriesThatCannotServeTheNeeds(t *testing.T)

func TestDegrade_WalksDownAndRecordsServedLevel(t *testing.T)
func TestDegrade_FastDoesNotDegradeFurther(t *testing.T)
func TestPark_ReturnsTheRefusalWithTheDoorReport(t *testing.T)
func TestExplicitProviderStillWins(t *testing.T)
func TestExcludeRemovesAConcreteModelFromResolution(t *testing.T)
func TestEmptyChainAfterExclude_OnUnavailableDecides(t *testing.T)
func TestNilRuleRegistryIsARefusalNotAFallback(t *testing.T)
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./component/router/...`

- [ ] **Step 3: Write `rules.go`**

`MatchRule` walks `rules.Ordered()` and returns the first whose every PRESENT `@when` key matches. Matching per key: `level` / `modality` / `prompt` / `role` / `actorRole` compare equal to the request's field; `tag` is membership in `req.Tags`; `touches` matches when ANY of `req.Touches` has the rule's value as a prefix.

- [ ] **Step 4: Write `selectors.go`**

Three resolvers over the candidate set, each returning an ordered list plus a reason for every entry it drops:
- `strongest` — measured capability when present (absent this epic), else parameters descending, then context window descending, missing attributes LAST.
- `fastest` — measured throughput when present (absent this epic), else fewest parameters.
- `cheapest` — among FEDERATED records: input + output cost per million ascending; **a record with no cost figures sorts LAST and the reason says so.**

`fleet:<modelId>` / `app:<id>` / `federation:<providerName>` resolve to that one entry. `app:*` resolves per acting user via the existing `EntryForUser`.

- [ ] **Step 5: Rewrite `resolveChain`**

New order, keeping every existing property:
1. `ExplicitProvider` wins — single-entry chain, no rule consulted.
2. Otherwise `MatchRule(req)` gives the rule; the rule's `@level` overrides `req.Level` when present; the rule's `@policy` gives the (already expanded) chain; `@exclude` removes concrete models from resolution.
3. Walk the chain at the effective level, classifying each entry by door, keeping the existing **federation-hop-asks-the-ceiling-only-after-a-local-door** condition unchanged, and recording a reason for every entry passed over.
4. On exhaustion: if the rule says `degrade` and the level has a next one down, walk again at that level and set `ServedLevel` + `Degraded`. If it says `park`, or the level is `embeddings`, or `fast` has nowhere to go, return the refusal with the report.
5. `CloudConsent` stays exactly where it is — the one escape, after every door is shut.

The door report becomes `[]airoute.ConsideredEntry` and is **kept on success**.

- [ ] **Step 6: Add the missing modality entry points**

`ResolveStructured`, `ResolveVision`, `ResolveEmbedding`, `ResolveSpeech`, `ResolveTranscribe`, each interface-checking the matching `common.*` interface and reporting "does not serve <modality>" for entries that do not.

- [ ] **Step 7: Run, then commit**

Run: `go test ./component/router/... && make test`

---

## Task 6: Decision records and `routerDecisionsRecent`

**Issue:** #5132.

**Files:**
- Create: `component/router/decision.go`, `component/router/decision_test.go`
- Modify: `dsl/router/concepts.memql` (the nine new fields on `call`), `dsl/router/queries.memql` (`routerDecisionsRecent`), `dsl/router/shapes.memql`
- Modify: `component/router/router.go` (`buildRouterCallArgs`), `integrations/router/integration.go`
- Test: `test/dslconformance` + a db-gated read test

**Interfaces:**
- Consumes: `airoute.Decision` from Task 1.
- Produces: `v1:router:call` fields `level`, `servedLevel`, `degraded`, `rule`, `policy`, `door`, `considered`, `touches`, `machineOwnerUserId`; query `routerDecisionsRecent(limit, since, level, door, rule, outcome)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDecisionFieldsPresentOnSuccess(t *testing.T)
func TestDecisionFieldsPresentOnRefusal(t *testing.T)   // a park writes a row too
func TestConsideredIsKeptOnSuccess(t *testing.T)
func TestRouterDecisionsRecent_GateIsOwnerOrDeveloper(t *testing.T)
func TestRouterDecisionsRecent_EveryFilter(t *testing.T)
func TestRouterCallHasNoBroadcastRoutingRuleAndHereIsWhy(t *testing.T) {
	// v1:router:call is deliberately ABSENT from component/node/routing.go, on
	// the same volume grounds that exclude v1:worker:invocation. This test is
	// the note: a later session adding a broadcast rule "for consistency"
	// fails here and reads the reason.
}
```

- [ ] **Step 2-4: Add the concept fields, the query, and fill them in the writer**

`routerDecisionsRecent` is `@requiresRank("developer")` (developer outranks admin on this ladder, so that floor admits owner and developer and refuses admin — which is what "owner or developer" means here). Add `@unbounded` NOT at all: the query paginates.

`considered` is an array of objects; a nested object in a mutation template **needs commas** or the block lexes wrong.

- [ ] **Step 5: Run, commit**

---

## Task 7: Every call site through the seam

**Issue:** #5133.

**Files:**
- Create: `component/memql/ai_resolver.go`, `component/memql/direct_registry_access_gate_test.go`, `component/memql/call_site_levels_test.go`
- Modify: `component/memql/{ai_runtime,engine_ai,ai_tool_loop,ai_providers,ai_semantic_cache}.go`, `component/grpc/ai_handlers.go`, `integrations/{embedding,knowledge,similarity,harnessrecall}`, the callers that construct `component/safety/llm`, `component/fileprocessor` and `component/healing`, `app/` (the wiring)

**Interfaces:**
- Consumes: everything above.
- Produces:

```go
// component/memql
type AIResolver interface {
	ResolveFor(ctx context.Context, req airoute.ResolveRequest) (ResolvedProvider, error)
}
type ResolvedProvider struct {
	Client     any             // one of the common.* provider interfaces
	Resolution airoute.Resolution
}
func (e *MemQLEngine) SetAIResolver(r AIResolver)
```

- [ ] **Step 1: Write the failing tests**

```go
func TestEveryCallSiteCarriesTheExpectedLevelAndModality(t *testing.T) {
	// A table over every site. Each row: the entry point, the level it must
	// declare, the modality it must derive. This is the test the record's
	// section 6 asks for, and it is what makes a re-pointed call site a
	// CHECKED fact rather than a diff somebody read.
}
func TestUnwiredResolverRefusesRatherThanFallingBack(t *testing.T) {
	// The "green and inert" trap: a seam with an unwired setter that returns
	// zero values looks exactly like a working one.
}
func TestAppWiresTheResolver(t *testing.T)   // in app/, an AST or runtime check
func TestDirectRegistryAccessGate_PassesOnTheTree(t *testing.T)
func TestDirectRegistryAccessGate_FailsOnAFixture(t *testing.T) {
	// The negative control. Run it against the FIXTURE, and confirm the
	// failure names the accessor -- a gate that fails for a compile error
	// looks identical to one that caught the thing.
}
func TestChatHandlerRefusesWithFailedPrecondition(t *testing.T)
```

- [ ] **Step 2: Write the AST gate**

Walk every tracked `.go` file with `go/parser` (not regex — regex misses an aliased import and a method value). Flag a selector expression naming `Entry`, `ChatProvider`, `StructuredChatProvider`, `SuggestChatProvider`, `ChatStreamProvider`, `VisionProvider` or `EmbeddingProvider` on a `*ProviderRegistry`, outside `component/router` and `component/memql`'s registry file itself. Use `git ls-files` so the walk sees only tracked files, and **name the escape hatch in the failure message** — how to declare a legitimate new router-internal accessor.

- [ ] **Step 3: Re-point the call sites**

Each site builds an `airoute.ResolveRequest` with its level, its derived modality and its needs (including `MinContextTokens` from `airoute.EstimateMinContextTokens` over the RENDERED prompt), and calls the resolver. `@defaultProvider` rides `ExplicitProvider`.

The embedding sites keep their pinned model id as the explicit pin — **epic 3 owns re-pointing that binding**, so nothing changes on the wire here.

- [ ] **Step 4: The chat handlers**

`AiChatMsg` builds level `strong`, modality chat or streaming chat, prompt name `ask`. On refusal: `status.Error(codes.FailedPrecondition, ...)` carrying the refusal code and the door report. Delete the three "no provider available" spellings.

- [ ] **Step 5: Run the whole tree, commit**

Run: `make test`, then `go build -tags agent .`, `-tags planner`, `-tags bff`, `-tags identity`, `-tags workbench`, `-tags mcp`, `-tags edge` — **tagged files are invisible to `make test`**, and a re-pointed call site inside one is a compile error nothing else catches.

---

## Task 8: Runtime-authored rules and policies

**Issue:** #5135.

**Files:**
- Create: `component/routingrules/` (renderer + activation, the `component/emailrules` shape), `dsl/router/builtins.memql`
- Modify: `component/memql/authoring_sandbox.go`, the authoring pipeline resolver

**Interfaces:**
- Produces: builtin `routingRuleActivate(conditions, policy, precedence, onUnavailable, excludes)`, owner or developer.

- [ ] **Step 1: Failing tests** — the activation round-trip (db-gated), the two refusals (a custom rule naming a shipped one; a custom rule carrying `@locked`), and that the rule is in the registry with no restart **and survives one**.
- [ ] **Step 2:** Compile paths for `rule` and `policy` in `authoring_sandbox.go` (which today refuses `policy`).
- [ ] **Step 3:** The deterministic renderer — a structured form to a construct, no LLM, the `emailrules` shape. **The generated construct runs under `AuthorContext`**, so anything cluster-wide it needs must be asked from Go.
- [ ] **Step 4:** Run, commit.

---

## Task 9: The work failure path

**Issue:** #5134.

**Files:**
- Modify: `integrations/work/` (the dispatcher's failure path), `app/integrations_work_dispatch.go`, the agent wiring for the healer
- Create: a `component/proving` scenario

- [ ] **Step 1: Failing tests** — a transient error reaches the table and costs **zero** model calls; a novel error costs **exactly one**; a precondition miss raises a `planReview`; a plan miss invokes `replanGap` with the completed prefix kept.
- [ ] **Step 2:** Wire `ClassifyByRules` first, `classifySymptom` at level `fast` only on a miss, `ActFor` to act.
- [ ] **Step 3:** Subscribe `NewWorkHealer` in the agent wiring.
- [ ] **Step 4:** The proving scenario, **with its negative control being the classifier actually being reached** — a counter that never rises on any path reads as zero forever.
- [ ] **Step 5:** Update `docs/public/ai/llm-cost-control.md` to account for the new call. Run, commit.

---

## Task 10: Docs, CLAUDE.md, the record correction, the architecture model

**Issue:** #5136.

- [ ] **Step 1:** `docs/public/language/memql.md` — the `rule` construct, `@level`, the entry grammar.
- [ ] **Step 2:** `docs/public/ai/llm-cost-control.md` — the seam, and what stayed in Go and why.
- [ ] **Step 3:** `docs/public/operate/ai-routing.md` (new) — levels, policies and rules for an operator.
- [ ] **Step 4:** `CLAUDE.md` — the AI Integration section and the Policies section, plus `rule` in the DSL dependency tree. **Verify with `go test -count=1 .`** — the root CLAUDE.md is gate-scanned.
- [ ] **Step 5:** Add C1 and C2 to section 4 of the design record, and post them as a comment on #5127.
- [ ] **Step 6:** Delete this plan file (docs/CLAUDE.md: a plan is spent when it ships).
- [ ] **Step 7:** `make arch-model-check`, regenerate if it drifts.
- [ ] **Step 8:** Full verification, then the PR.

---

## Final verification (before the PR)

Run in this order, and paste the output rather than summarizing it:

```bash
go build ./...
for t in agent planner bff identity workbench mcp edge; do go build -tags "$t" -o /dev/null . || echo "FAILED: $t"; done
make test
go test -count=1 .                       # the root repo-walking gates, uncached
go run ./cmd/memqllint
make arch-model-check
make frontdoor-hosts-check && make frontdoor-paths-check
scripts/ci/db-gated-packages.sh --trees  # then run them against a real Postgres
```

Known traps for this tree, from prior sessions:
- `go test ./...` does NOT reach `component/memql`, `component/database` or `component/language`. Only `make test` does.
- Repo-walking gates are served stale from the Go cache; `-count=1` is required for the root package.
- Do not edit files while a full test run is in flight — mid-run edits red the tree gates.
- A new doc file reds the docs front-matter gate and the vendor-domain gate; a new `.memql` construct reds memqllint, the sdk-gen check, dslconformance, the arch model and the sense-surface parity test.

---

## Appendix A: verified enumeration sites for the `rule` construct

Read on 2026-09-07 at `0cd415a41`. A `rule` construct is a distinct Go type, so "supporting policy" is implemented as N exhaustive type-switches rather than one dispatch table. Every site below is a hand-written literal.

**Silent failure if missed:**

| # | Site | Edit |
|---|---|---|
| 1 | `dsl/embed.go:47` — the `//go:embed all:<dir> ...` list | **Add `all:rules`.** The directive is an explicit space-separated list, not a wildcard. A new `dsl/rules/` directory is silently omitted from `embedFS`: no build error, the files simply are not there, and every downstream walk never sees them. This is the single most dangerous omission in the task. |
| 2 | `component/language/parser/parser.go:602-615` `topLevelDeclParsers` | Add `"rule"`. `TopLevelDeclKeywords` derives from it, so the drift tests then pass on their own. |
| 3 | `component/language/parser/doc_comments.go:~147` `attachDocComment` | `case *RuleDecl: d.DocComment = doc`. Without it `///` doc comments silently never attach. |
| 4 | `component/language/dslspec/spec.go:296-297` `receiverKeyToConstructKeywords` | `case "Rule": return []string{"rule"}`. Without it a new receiver key falls through to a fake self-named construct. |
| 5 | `component/memql/dslgate/imports.go:86-90` `flatKinds` | `"rule": true`. |
| 6 | `component/memql/dslgate/imports.go:97` `declLineRe` | Add `rule` to the keyword alternation, or the cross-namespace-import gate cannot recognise a `rule NAME {` line and every reference reads as unresolved. |
| 7 | `component/language/parser/ast.go:136` type-alias block | `RuleDecl = ast.RuleDecl`. |

**Red gate if missed (loud, but fix them in the same commit):**

| Site | Edit |
|---|---|
| `component/language/dslspec/spec_test.go:36-42` hard-coded `want` map | `"rule": true` |
| `test/dslconformance/naming_conventions_test.go:165-169` `declKeywordsPinned` | `"rule"`, plus the prose/count in `docs/public/language/naming-conventions.md` |
| `component/memql/dslimports/integrity.go:319-355` `declNameAndKind` | `case *languageAst.RuleDecl: return d.Name, "rule"` |
| `component/memql/dslimports/symbols.go:30-44, 200-231` | `SymbolRule` + its `String()` arm |
| `component/memql/duplicate_detector.go:~308` | `case *languageAst.RuleDecl: return "rule"` |
| `component/memql/construct_catalog.go:67-155, 805-819` | `ConstructKindRule`, the `constructKeyword` entry, and a `catalogRules` method wired into the catalog assembly |
| `component/packages/analyze.go:~308` | the same `case` arm |
| `component/memql/sense/authoring_rules.go:~605-661` | the description-length diagnostic arm |
| `component/language/parser/rewrite_doc_comments.go:~448-468` `resolvedConstructDescs` | `case *RuleDecl: put("rule", ...)` — the `///`-wins-over-`@description` precedence verifier |
| `component/language/annotations/registry.go` `Docs` | one entry per new annotation name; `TestEveryAnnotationHasDoc` fails on a missing one, and a leftover doc for a REMOVED annotation makes the editor offer what the parser refuses |

**Not applicable, traced and confirmed:** `cmd/memqllint` (fully generic), SDK generation (reads registries, not construct kinds), the architecture model (Go signatures, not DSL kinds), `dsl/pack_validation.go` and `dsl/runtime_mount.go` (domain-namespace guards, construct-kind-agnostic), `component/language/compiler` (AST to automation JSON; a rule is not an automation), `MEMQL_DSL_PATH` runtime mounting (already generic — only the compile-time embed list needs the manual edit).

## Appendix B: three corrections to the task issues

Found while verifying; each goes in the PR body and on the issue.

### B1 -- `@when`'s keys must be validated by the rule parser itself

`annotations.KeywordArgs` looks like the closed-key mechanism and is not one: its only two consumers are `component/memql/sense/complete.go:122` and `hover.go:279`, so it drives **completion and hover only**. The generic `parseAttribute` accepts any `key=value` into `Attribute.Args` with no key-name validation at all — `@trigger`, `@handler` and `@relationship` do not reject an unknown key today either. So `parseRuleDecl` must validate the seven `@when` keys explicitly, and a `KeywordArgs["when"]` entry is added for the editor on top of that, not instead of it.

### B2 -- the work failure seam is `component/automations/journal.go`, not `integrations/work/dispatch.go`

Issue #5134 names `integrations/work/dispatch.go`. That file's `FailRun` handles only the two PRE-execution failures (an automation that will not resolve, and a run refused on its arguments) — both happen before the executor opens its journal. The per-step failure path that actually decides what a terminal `failed` run does is `component/automations/journal.go`'s `closeRun` / `parkOnInference`, which today consults `work.DoorsFrom` for the every-door-shut park and writes `status=failed` + `errorMessage` for everything else. That "everything else" branch is where `ClassifyByRules` goes.

### B3 -- `v1:router:call` has no read surface to extend

`dsl/router/shapes.memql` declares only `budgetFull`; `dsl/router/queries.memql` declares only `routerBudgets`. There is **no shape and no query over `call`** today, so `routerDecisionsRecent` ships a new shape as well as a new query. Note that adding a shape to a query nils the bundle — check what the caller reads back.

Also: `v1:router:call` has **two** writers, and both must fill the new fields or default them — `component/router/router.go`'s `recordCall` (actor `system:router`) and `integrations/agent/worker/cockpitapp_ledger.go`'s `RecordAppSession` (actor: the owning user, `billing=subscription`). The second is agent-tagged, so `make test` does not compile it.

## Appendix C: the prompt corpus, counted

18 prompts in 9 files under `dsl/`. The record's D3 assignment names 15 of them. **Three are named nowhere in D3** and must each be assigned deliberately, with the choice asserted by name in the conformance test: `consolidateMemory` (`dsl/memory/prompts.memql`), `plannerAgent` (`dsl/agents/prompts.memql`) and `forgeMentoringExplanation` (`dsl/forge/prompts.memql`).

`agentReply` — the one D3 calls `strong` — **is not in this repo.** It lives in the product pack's runtime DSL, mounted at `MEMQL_DSL_PATH`. The pack loader holds it to the same rule, so the pack repo needs the same sweep; this PR cannot make that edit and says so in its body.

Four places must learn `level` in lockstep or the parser refuses it after one of them is changed: `annotations.ByReceiver["Prompt"]`, `annotations.Docs`, the `PromptDecl` AST doc comment, and `component/memql/reject_unknown_annotations_test.go`'s `TestPromptDeclToPromptDecl_AcceptsSupported` fixture.
