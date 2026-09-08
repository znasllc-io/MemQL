---
title: Local models on the fleet
audience: public
status: stable
area: operate
sinceVersion: 0.9.0
owner: znas
---

# Local models on the fleet

Running the platform's own operations — planning, suggestions, embeddings —
on models the machines in your fleet already host, instead of on a metered API.

The premise is simple: if a machine you own can run the model, there is no
reason to pay per token for work a 7–8B model handles well. Cloud providers
stay available, reserved for the work that genuinely needs a stronger model —
and reaching one is always a decision somebody made, never a fallback that
happens quietly. Epic
[memql#4676](https://github.com/znasllc-io/memql/issues/4676).

---

## The hardware floor

A machine below this floor is **not offered as an inference machine**. It
remains a full worker for everything else — shell, filesystem, HTTP fetch,
computer use, local apps — and nothing about it is degraded; it simply does not
appear in the model catalog, and the cockpit's discovery names the reason.

| Platform | Minimum (supported) | Recommended |
|---|---|---|
| macOS | Apple Silicon (M1+), 16 GB unified memory, macOS 13+ | M2 Pro+ / 32 GB for 8B-class at comfortable latency |
| Linux | x86_64 + discrete GPU with >= 8 GB VRAM (CUDA/ROCm) | 12–16 GB VRAM |
| CPU-only / Intel Mac | Not supported as an inference machine (still a full worker for everything else) | — |

The floor is checked by the cockpit's model discovery, on the machine itself.
That placement is deliberate: only the machine can see its own GPU, and a
central check would be guessing from a hostname.

**"My laptop does not appear in the model list."** In order of likelihood: it
is below the floor above; it has no model runtime installed; its
`policy.yaml` `models.allow` does not list the model; it is not signed in; or
it is simply asleep. The Providers page distinguishes the last one — an offline
machine is **listed and marked offline**, not hidden, precisely so this
question has a visible answer.

---

## The model floor

The default operational class is a **7–8B instruct model with structured
output** — `llama3.1:8b` or `qwen2.5:7b` class — plus a **small embeddings
model** where embeddings route locally.

Two different things enforce and recommend that, and it is worth keeping them
apart:

- **The floor is what onboarding recommends installing.** It is a sentence in
  a dialog and in this document.
- **The catalog's capability gating is what enforces it, per call.** A model
  is routed a structured-output prompt only if the machine hosting it
  advertised structured-output capability for that model; embeddings route
  only to models that advertised embeddings. A capability miss is treated as
  an **availability** miss — the provider is simply unavailable — because
  "no machine offers this model" and "none that offers it can do what this
  prompt needs" are the same answer to whoever is waiting.

Capabilities default to **absent**. A machine that says nothing about
structured output is not selected for a structured prompt. That direction is
deliberate: a model that quietly answers prose to a structured-output turn
produces a parse failure three layers away, naming nothing.

---

## The four inference doors

Using the console's AI surfaces requires configured inference. **Starting MemQL does not** —
the engine boots, serves and migrates with no provider configured anywhere,
and the installer asks for no key. The requirement lives at the console, and
after sign-in the first-run gate runs in order:

1. **Passkey**, when you have none enrolled. It is what gets you back in
   without a link in your inbox.
2. **Inference**, when the cluster has no eligible source. Four doors, local
   first -- and none of them is an API key, because the product has none.
3. The console.

### Door 1 — run a local model (the default)

Pair a machine through Fleet → Machines with **"This machine will run local
models"** ticked, and the one-line install command grows a `--inference` flag:
the installer checks the hardware floor, sets up a runtime and pulls a starting
model in the same terminal, before you have opened anything else. MemQL then
uses it for planning, routing, suggestions and embeddings. Nothing is billed
per token and no prompt leaves your hardware.

A machine already paired is set up the same way from its own terminal:

```bash
memql worker setup --inference
memql worker setup --inference --model llama3.1:8b --model nomic-embed-text
```

Supported runtimes: **Ollama**, discovered natively at its default endpoint;
and **any OpenAI-compatible endpoint** declared in the machine's `policy.yaml`
— which covers LM Studio, vLLM and llamafile. There are no other per-vendor
integrations.

#### Pulling a model from MemQL OS

A machine's page in Fleet → Machines carries a **Models** group: what the
machine runs, with each model's parameter count, quantization and context
window, and a **Pull** control for the machine's owner.

```
Fleet → Machines → (a machine) → Models
```

Four things about that surface are deliberate, and each of them is the answer
to a question the naive version gets wrong.

**The act belongs to one machine.** A pull writes gigabytes to one disk and
edits one `policy.yaml`, so it is offered on that machine's page and nowhere
else — there is no fleet-wide "pull a model" that would have to ask which
machine. It is offered to the machine's **owner only**, and to anybody else the
control is absent rather than disabled: `fleetModelPull` refuses a machine that
is not the caller's, and a greyed button would advertise an act nobody on that
page can reach.

**Every refusal happens before anything is written.** A machine that is not
yours, is revoked, or is not connected to the cluster right now is refused at
the press, with a sentence naming which. There is no second machine to fall
through to, so a refusal you can act on beats a progress bar that fails five
minutes later for a reason that was knowable at the start.

**The progress numbers describe one STEP, never the whole download.** A
runtime fetches a model as a set of blobs and counts each from zero, and states
no whole-pull total anywhere — so the bar is labelled "in this step", the
counters legitimately jump backwards at each layer boundary, and there is no
overall percentage. When the runtime states no size for a step at all, the bar
is **absent** rather than empty: an empty track claims the download has moved
nothing, when what happened is that nobody said how far there is to go. The
runtime's own status line ("pulling 8eeb52dfb3bb", "verifying sha256 digest")
carries the rest, verbatim.

**Leaving the page does not stop the pull.** The download runs on the agent
replica holding the machine's connection, and its record is a
`v1:worker:modelPull` row — so it survives the tab closing, and the same rows
answer "why is this model here" long afterwards. A failed pull is the reason
that history is shown at all: a successful one is already visible as a model,
while a failed one leaves nothing behind and the machine's model list simply
does not change.

**A pulled model may not be visible to the cluster yet.** The cockpit
re-advertises its label set at once after a pull it drove, so the model is
normally routable immediately; when it could not, the record says
`sees it when this machine next reconnects` rather than reporting plain
success. Those are different facts and the surface keeps them apart.

#### When a pull does not finish

| What you see | What happened | What to do |
|---|---|---|
| "…is not connected to the cluster right now" | No agent replica holds that machine's stream. The pull was refused and nothing was written. | Wake the machine, or check its cockpit is running, and try again. |
| "…was revoked" | The registration survives revocation as audit history, so it still appears. | Pair the machine again; its old worker token can never be used. |
| "No agent replica is driving this pull" | The replica that claimed it is gone — scaled down or killed. `workerModelPullStaleSweep` closes the record within two minutes. | Start the pull again. Whatever was fetched stays on the machine, so it resumes rather than restarting. |
| A runtime error ("no space left on device") | A pull can fail inside a successful HTTP response; the runtime's own words are carried through. | Fix what the message names, then pull again. |
| The model does not appear after a success | The machine could not re-advertise, so the cluster has not seen the new label set. | It appears when the machine next reconnects; nothing needs doing. |

### Door 2 — a signed-in Claude Code or Codex on one of your machines

A subscription you already pay for, used as an inference door (epic
memql#5096). A machine that has the app **allowed** in its own `policy.yaml`
and **signed in** advertises it, and a policy naming `app:claude-code` — or
`app:*` for any of them — reaches it. MemQL hands over a prompt and takes back
an answer; nothing is billed to MemQL, and the ledger records the call as
`subscription` rather than as free.

**It does not serve MemQL's own tool-calling turns**, and that is the design
rather than a gap. On a tool turn MemQL is driving, and an app is an agent
that drives itself: it reaches MemQL's tools through MCP, in the other
direction. A tool turn therefore walks past every app door and lands on the
next entry in the chain.

A machine whose stream a **sibling replica** holds leaves the door shut on
this one — the app-session envelope has no cross-node forward yet.

### Door 3 — Anthropic workload identity federation

No key at rest anywhere: each pod exchanges its own projected Kubernetes token
for a bearer that lives at most an hour. Configured outside the console; once
it is complete this step passes silently. See
[anthropic-federation.md](auth/anthropic-federation.md).

### Door 4 — OpenAI workload identity federation

The same shape for the other vendor, on the same `memql-engine` ServiceAccount
with its own projected token and audience (epic memql#5088). Calls are billed
to the Platform service account the mapping targets. See
[openai-federation.md](auth/openai-federation.md).

### There is no fourth door, and no API-key door

A manually entered vendor API key is not a way to configure inference on this
cluster. There is no field for one in Settings → AI providers, no env name the
engine reads, and nothing seeds one into a Secret. Federation and the fleet are
the whole list.

**A local cluster therefore reaches cloud models through door 1 only.** k3d's
OIDC issuer is private, so neither vendor can federate with it, and doors 2 and
3 are unavailable there by construction rather than by omission. Streaming
transcription and Whisper are OpenAI calls and are off on a local cluster for
the same reason; there is no local substitute today.

### What the gate does and does not do

- It gates **the console**, never the cluster. Nothing here is enforced
  server-side, and features that need a model refuse or park with a typed
  reason regardless.
- **A machine that goes offline later produces a notice, not an eviction.**
  Work that needs a model pauses and says why; you are not thrown out of a
  session because a laptop closed.
- **Auth-disabled clusters** (`MEMQL_IDENTITY_ENABLED=false`, troubleshooting
  only) skip the passkey step — there is no identity — and the inference step
  is skippable. That is the only mode with a skip.

---

## The chain: local, then an app, then anybody's money

Every shipped policy tries the doors in **cost order** (epic memql#5096):

```
@primary("fleet:*")      a model on hardware you already own. Electricity.
@fallback("app:*")       a signed-in Claude Code or Codex on one of your
                         machines. A subscription you already pay for.
@fallback("streamClaudeSonnet")   … and then the vendor entries, which cost
@fallback("stream54Pro")          money and are gated by the cost ceiling.
```

`fleet:*` and `app:*` are **wildcards**, and that is what makes this shippable
as a default. A policy naming one model id would park on every fleet running
something else, and which weights you pulled is not knowable in advance. The
wildcard resolves **per call**, among the models that can serve *that* call:

- **strongest first** — parameters, then context window, then model id;
- **your explicit preference wins**, if you set one (Fleet → Routing,
  `modelPreference`);
- **a model that does not say how big it is sorts LAST**, never first. It stays
  eligible for everything it advertised; it simply does not win by silence.

### The ceiling gates the federation hop

Falling back to a **paid** provider consults the cost ceiling first
(`MEMQL_LLM_MAX_TOTAL_COST_USD`, `MEMQL_LLM_MAX_TOTAL_CALLS`, and their
per-scope siblings). When it is reached, the call is refused with
`ceiling_reached` rather than spending past it.

A chain an operator wrote that **starts** at a vendor is unaffected: the
ceiling governs falling back to paid inference, not choosing it.

---

## Park, never fall back

This is the decision the whole feature rests on, so it is stated plainly:

**Work parks when EVERY door is shut. It does not quietly run on a paid API
that no policy named.**

The refusal is typed and names every door it tried and why each did not open —
and, for the local doors, every machine considered: offline, revoked, does not
offer the model, missing a capability, busy. That machine-level detail is the
half you can act on.

The run **parks** rather than failing: it becomes a `v1:work:approval` of kind
`inferenceUnavailable` on the run, and it resumes when

- **a door opens** — the sweep re-checks a parked run every five minutes, so a
  laptop you open or an app you sign into releases it with nobody deciding
  anything; or
- **you decide the approval** — "use a paid provider for this run", or stop
  the run.

A **ceiling** park carries no re-check: only a person changes a ceiling, so
polling would burn a dispatch every five minutes to rediscover a number nobody
touched.

Nothing about the work was wrong, which is why it parks: failing it would throw
away a compiled template and a journal because somebody closed a lid.

### A pinned policy still refuses

Park-not-fallback is unchanged for a policy that names ONE fleet model and
authors no fallback: it refuses exactly as it did, and a person's one-shot
consent is still the only way past it. What changed is the **default**, not the
rule.

### The four seeded local-only policies are gone

`localPlanner`, `localConductor`, `localSuggest` and `localEmbeddings` were
seeded local-first with no fallback and **named by nothing**. They are deleted:
the purposes they stood for are retired (the planner loop, memql#5052; there is
no conductor in the engine) or covered by the chain above. A policy nothing
names is not a default; it is a decoration that reads like one.

---

## Accounting

A local call costs no dollars, and the ledger says so honestly rather than
pretending it is free of consequence.

- `v1:router:call.billing` gains **`local`**, beside `metered`,
  `subscription` and `unknown`. It is stamped explicitly by the fleet path and
  is **never inferred** — inferring it would claim work ran on somebody's
  hardware when nobody established that. Absence still reads as `metered`.
- `executionSurface` names the machine, as `fleet:<registrationId>`, so
  "which of my machines did this" is answerable from the ledger alone.
- `plan.tokenSpentLocal` sits beside `tokenSpentSubscription`.

The two caps want opposite answers, exactly as they do for subscription spend:

- the **dollar ceiling excludes** local tokens — nobody was billed, and
  charging them would mean the more you used your own machine, the sooner your
  plans stopped;
- the **loop caps include** the calls — a runaway loop on a free model is
  still a runaway loop, burning a laptop's battery and occupying the machine
  its owner is trying to work on.

Unreported usage is **absent, not zero**: "the model ran and used nothing" and
"the model ran and nobody counted" are different facts, and only one of them is
ever true.

The same guards apply. The process-wide rate ceiling, the identical-request
breaker and the per-plan budgets sit at the provider seam, so a fleet call
passes the gates an HTTP provider call passes, sharing the same state. See
[LLM cost control](../ai/llm-cost-control.md).

---

## Routing, and who may use whose machine

Selection is the existing Fleet router asked for a `model:<id>` label, under
the strategies it already has. Two properties are security-load-bearing:

- **A model call carries your prompts, so it routes only to YOUR machines.**
  There is no cross-user routing path. The read that finds candidates is
  caller-scoped, so another user's machine is never in the result to begin
  with.
- **System work** — automations and cluster maintenance, with no acting user
  — reaches only machines whose **owner opted in**, by setting
  `sharedInference=true` in the machine's **operator labels** on
  Fleet → Machines. It must be the operator half: the labels a cockpit
  reports are overwritten on every reconnect, so an opt-in stored there would
  be granted by the machine rather than by its owner, and revoked roughly
  whenever the lid closed.

`leastLoaded` rations by the concurrency ceiling a machine declared **for that
model**, so a machine advertising one slot for a 70B and eight for a 1B is
described correctly for each.

---

## Install-time

Local installs probe for a model runtime and, when one is present, offer to
wire it up. **The probe is inference-optional in the strong sense**: a machine
with no runtime, no model and no key completes install, uninstall, repair and
update identically, and sees no prompt at all — an install is not the moment
to sell somebody a capability they did not ask for.

```bash
# The probe, runnable on its own. "Not found" is exit 0.
scripts/install/detect-ollama.sh
scripts/install/detect-ollama.sh --endpoint=http://127.0.0.1:11434 --timeout=3
```

The VS Code extension runs the same capability. That install, uninstall,
repair and update make **no** inference call is an assertion test, not a
review note: the flows run with every outbound network seam replaced by a
function that throws.

---

## What is not here

- **Transcription stays on its cloud path.** No local STT.
- **No models on workbenches.** Machines only.
- Machine-side discovery, serving and usage reporting live in the
  `memql-cockpit` repo. This repo fixes the wire contract and the engine side,
  so a cockpit that advertises no models changes nothing.

---

## Related

- [Workers runbook](workers-runbook.md) — pairing a machine, tokens, scope
- [Local apps as execution surfaces](local-apps.md) — the sibling delegation surface
- [LLM cost control](../ai/llm-cost-control.md) — the guard layers
- [Anthropic federation](auth/anthropic-federation.md) — door 2

---

## The curated catalog

Epic [memql#5137](https://github.com/znasllc-io/memql/issues/5137) added a
**catalog**: a short list of open-weight models worth running, organised by what
you would use them for, seeded with the cluster and readable at **Fleet →
Models**.

It is a recommendation and it **gates nothing**. A model your machines already
serve is used whether or not the catalog lists it; a model the catalog lists and
nobody has pulled is not available to anything. What the catalog knows is what a
machine class *should* pull, which is a different question from what the fleet
*can* serve — and the Fleet page shows the two beside each other, so the gap is
the thing you read rather than something you work out.

### Nine categories

| Category | What it is for |
|---|---|
| `text` | Everyday work: chat, tools, structured output |
| `reasoning` | Problems worth spending thinking on |
| `omni` | Every modality from one set of weights |
| `vision` | Deliberately empty — the text entries see |
| `audioIn` | Transcription |
| `audioOut` | Speech |
| `imageGen` | Making images |
| `videoGen` | Listed, recommended nowhere |
| `embeddings` | Search and memory |

Two of those rows are decisions rather than gaps.

**`vision` has no entries by design.** Every text model in the catalog carries
vision, so a separate vision pull would be a second copy of weights the fleet
already holds.

**`videoGen` is recommended nowhere.** The entries are listed so you can see
they exist and were considered; every one is a Linux GPU job measured in
minutes, and putting one behind a level a chat turn resolves would be a
multi-minute job answering a question somebody asked in a sentence.

### Runtimes

A model needs its runtime installed on a machine before that machine can serve
it. `ollama` covers most of the catalog; the others are small separate installs
the cockpit performs on consent.

| Runtime | Serves |
|---|---|
| `ollama` | Text, reasoning, embeddings, and (macOS, experimental) image generation |
| `mlx` | Apple Silicon models outside the Ollama library |
| `whispercpp` | Whisper transcription |
| `nemo` | NVIDIA Parakeet and Canary transcription |
| `kokoro` | Speech synthesis |
| `mflux` | Image generation on Apple Silicon |
| `comfyui` | Video generation, Linux and a GPU |

### What to pull, by machine class

`minMachineClass` on each entry is a **floor** in gigabytes of unified memory or
VRAM, so a machine runs everything at or below its own class. A 32 GB machine
gets the 16 GB recommendations as well as its own.

| Class | Text and reasoning | Embeddings |
|---|---|---|
| 16 GB | `qwen3.5:9b`, `gpt-oss:20b` | `qwen3-embedding:0.6b` |
| 24 GB | adds `gemma4:12b`, `gemma4:e4b` | — |
| 32 GB | adds `qwen3.8:27b`, `gemma4:26b` | adds `qwen3-embedding:4b` |
| 64 GB | adds `qwen3.5:35b` | adds `qwen3-embedding:8b` |
| 128 GB | adds `qwen3.5:122b`, `gpt-oss:120b` | — |

A machine that has not reported its memory blocks nothing: unknown is not small,
and telling somebody with an unreported 64 GB laptop that they have no machine
of the class would be confidently wrong with no way for them to tell.

### Adding one yourself

`modelProfileAdd` takes a model id and marks the entry `curated: false`. It
gates nothing either — a machine still has to advertise the model before
anything routes to it — so the blast radius of a wrong entry is a recommendation
nobody can act on.

Removing a **curated** entry is refused rather than performed. Curated rows are
re-seeded on every boot, so a removal would succeed, look correct, and be undone
at the next restart with nothing anywhere to explain it. Retiring one is a
release.

---

## The embedder is a binding, not a setting

The embedding model used to be a string in five files and the vector column was
declared at that model's width. Changing it meant editing five files **and** a
migration, and doing either without the other produced a table of vectors at the
wrong width — which is not an error anywhere. It is a search space that quietly
returns the wrong neighbours.

One row now says which embedder is active
(`v1:platform:embedderBinding` at the id `active`), and the width belongs to the
provider: a provider record declares it, and a fleet model's comes from its
catalog row. Vectors live in one table per width, `node_vectors_<dims>`.

**Switching is not an edit.** It creates the new width's table, records the
binding as a plan, re-embeds the corpus, and flips `active` only when the counts
match. Until then every read follows the binding that is still active — so a
switch interrupted half way leaves a cluster that still works, rather than one
whose vectors half mean one thing and half another.

Two consequences worth knowing:

- **Same width is not same meaning.** `bge-m3` and `qwen3-embedding:0.6b` are
  both 1024 dimensions and share nothing else. Switching between them still
  rebuilds the whole corpus.
- **A model the catalog does not know cannot be bound**, even when a machine
  offers it. This is a real state rather than a hypothetical: a cockpit
  advertises the embedding capability for any model whose runtime reports it. The
  model still serves embedding calls that name it; what it cannot be is the
  cluster-wide binding, because a binding creates its table before the first
  vector exists and a table at a guessed width returns wrong neighbours without
  ever erroring. The refusal says so, and says the machine is fine.

---

## What this does and does not yet prove

The proving suite
([overview/proving](../overview/proving.md)) carries a scenario in which a goal
whose steps are all deterministic is served end to end with **no provider call at
all**, against a control — a goal with a step that must reason — which makes
some. A call never made is never paid for, which is the load-bearing half of
running locally.

<!-- proving-pending: metric=amortizedCost.federationCalls reason=the CI tier replays from a cassette through a fake step registry, so no call reaches the router and no decision record names a door -->

What is **not** proven yet is the other half: that of the calls a cluster *does*
make, none went to a paid vendor. That needs a decision record naming the door
each call took, and the replay tier has no door — both arms play recorded
responses, so "no call went to a paid vendor" and "no call went anywhere" are the
same zero. Reporting it would be a number that reads as the headline result and
measures something else. The live tier can answer it and ships disarmed.
