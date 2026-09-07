# Local-first inference and routing -- the five-epic program

- **Date:** 2026-09-07
- **Status:** agreed with the owner in the 2026-09-07 brainstorm. Every fork below was put
  to the owner and answered, or is a recorded recommendation the owner did not overturn
  when asking for the program to be filed; the per-epic records carry the reasoning and
  what each choice rejected.
- **What it is:** the index for a program of five epics that together make a fresh MemQL
  cluster useful on the owner's own hardware without a paid model anywhere by default:
  the platform holds an owner until it has an inference door and tells everyone else to
  ask one (1), every call to a model declares a level and is decided by rules that put
  paid inference last (2), every level resolves to an open-weight model from a curated
  catalog and every modality has a local door (3), a machine says what it is and what it
  measured and can serve a team (4), and the screens teach the three nouns (5).
- **Predecessor:** the inference and setup program of 2026-09-06
  (`2026-09-06-inference-and-setup-program.md`), whose first-run wizard this program
  replaces with a gate and whose readiness model, federation doors, fleet inference and
  machine setup it builds on.
- **Repositories:** epics 1, 2 and 5 live in `memql`; epics 3 and 4 have an engine half in
  `memql` and a cockpit half in `memql-cockpit`. The engine halves carry the design
  records; the cockpit halves have no separate record, their contract is fixed in the
  engine's, and their epics say so.

---

## The five epics, in shipping order

| # | Epic | Repository | Record | Depends on |
|---|---|---|---|---|
| 1 | The core gate, honest readiness, honest install | memql | `2026-09-07-core-gate-and-honest-install-design.md` | nothing |
| 2 | Levels, policies and rules as DSL | memql | `2026-09-07-levels-policies-rules-design.md` | nothing (1 is convenient) |
| 3 | Open-weight defaults and the curated catalog | memql (engine) + memql-cockpit (cockpit) | `2026-09-07-open-weight-defaults-and-catalog-design.md` | 2; the halves share a proto change |
| 4 | The machine scanner, measured capability, and shared machines | memql (engine) + memql-cockpit (cockpit) | `2026-09-07-machine-scanner-and-shared-machines-design.md` | 3 and 2; the halves share a proto change |
| 5 | Settings, AI and the Fleet app, redesigned | memql | `2026-09-07-ai-settings-and-fleet-redesign-design.md` | the vocabulary of 2 to 4; its PR 1 may start beside 2 |

The order is dependencies first. Epic 1 is small and urgent and ships alone. Epics 2 and
3 are the substance. Epic 5's information architecture is drawn while epic 2 is designed,
so the nouns on the screen and the nouns in the DSL are the same words from the first
day.

Plans are not pre-written. Each epic's record is implementation-ready (decisions, the
change by path, failure modes, testing, delivery by PR and task), and the session that
picks the epic up writes the plan from it with `superpowers:writing-plans` before the
first task, then deletes the plan in the epic's merge (docs/CLAUDE.md).

## Issues, filed 2026-09-07

| Epic | Repository | Epic issue | Task issues |
|---|---|---|---|
| 1 The core gate, honest readiness, honest install | memql | #5118 | #5119-#5126 |
| 2 Levels, policies and rules as DSL | memql | #5127 | #5128-#5136 |
| 3 Open-weight defaults and the curated catalog, engine half | memql | #5137 | #5138-#5145 |
| 3 Open-weight defaults, cockpit half | memql-cockpit | #393 | #394-#395 |
| 4 The scanner, measured capability, shared machines, engine half | memql | #5146 | #5147-#5152 |
| 4 The scanner, the probe and shared machines, cockpit half | memql-cockpit | #396 | #397-#400 |
| 5 Settings, AI and the Fleet app, redesigned | memql | #5153 | #5154-#5159 |

Every task is a GitHub sub-issue of its epic; every epic body names its PR grouping, its
record and its branch; every task body carries its deliverable, acceptance and files, and
opens with its epic, its PR number and its record section. All carry the `claude` label
and the epic's `epic:<name>` label.

## The decisions that cut across epics

Recorded once here; the per-epic records restate the ones they implement.

1. **Paid inference is the last resort by construction.** The shipped default rule tries
   the fleet's strongest eligible model, then a signed-in subscription app, then the
   cheapest federated model that qualifies, under the ceilings. An owner may put a paid
   model first with a custom rule of higher precedence; it is explicit and every decision
   record says so.
2. **A call declares a level, never a model.** Four levels, `fast`, `strong`, `reasoning`,
   `embeddings`; modality is derived from the call. More levels than a person can hold in
   their head defeats the abstraction.
3. **Three nouns: level, policy, rule.** A policy chain may name a provider, a selector or
   another policy, which is how a rule delegates to logic that picks the best of what is
   available. "Smart" is not a word on any screen.
4. **A described rule is compiled once, locally, into a rule the person confirms.** The
   owner's cache argument is adopted: the sentence never changes at request time, so its
   hash is the cache key and the compiled rule is the cached value. What the program adds
   is that the cached value is a selector rather than a model, so it cannot go stale when
   a machine sleeps, that invalidation is an event rather than a clock, and that a
   recompile is offered and never applied silently. Request-time classification is
   reserved for free-form entries, rules first, then a small local model with a cached
   verdict.
5. **The gate keys on inference only.** Storage is always configured locally and mail is
   log-only there; the wizard still walks every core stop. The gate lives in the OS,
   computed from the readiness feed every signed-in person can read, never in the engine.
6. **Inference readiness is configuration, not presence, and it is computed from rows.**
   A door is configured when a registration holds a qualifying model, a signed-in app, or
   federation ids are present; whether it is open now is a second fact on the same row.
   A signed-in Claude Code or Codex with no local model is a door and the gate says which
   door opened while the wizard still recommends a local model.
7. **Locked means core-first, re-seeded on every boot, and never redefined.** Custom rules
   and policies add and may take precedence; they cannot carry `@locked` or a shipped
   name. The policy registry's bare-name last-wins is closed first.
8. **Mechanism stays in Go.** Door resolution, availability, the ceiling arithmetic,
   refusal codes, and the rate, repeat and cumulative-spend guards. A kill switch a policy
   can author around is not a kill switch.
9. **Measurement ranks and gates nothing, this release.** A probe that refuses a working
   model on a bad run is worse than no probe. Demotion is proposed from evidence as an
   approval and never applied silently.
10. **Shared team machines are in this program.** Without them the cost principle applies
    to a person, not to a business. Two consents, the owner's and the machine's own
    policy, and a ledger of counts, never content.
11. **Video generation is in the catalog as a Linux GPU job and is a default nowhere.**
    Speech out and image generation are separate small runtimes the cockpit installs,
    consented and idempotent.
12. **An uninstall that keeps a cluster says "kept", keeps its receipt, and asks k3d
    before offering Install.** The one destructive act, deleting the cluster and its data,
    is offered behind a typed phrase.

## What was verified before filing

The findings that shaped the program were verified on 2026-09-07 against the tree at
commit 907e385fb and the owner's live k3d cluster: the wizard's seed runs before the role
resolves; an adopted desk row replaces the seed; only 2 of 30 inference call sites reach
the router; every concrete provider record is a paid vendor model; `MinContextWindow` has
no producer; the fleet wire carries chat and embedding only; only agent binaries hold the
fleet and app seams; the readiness rows are rewritten on four triggers and no timer; the
policy registry is keyed by bare name; the work spine's rules table, symptom classifier,
healer and replan prompt have no production caller; the installer adopts and preserves a
pre-existing cluster while reporting a removal. The model facts were checked the same day
against the Ollama library pages for gemma4, qwen3.5, qwen3.8, gpt-oss and
qwen3-embedding and against vendor pages for audio, image and video models; each record's
last section lists what to re-verify before starting.

## How another session picks an epic up

1. Read the epic's design record in full. Do not start implementation on `main`; branch
   as `epic/<name>` (the engine) and `epic/<name>` (the cockpit), one PR per grouping the
   record's section 7 names.
2. Write the plan from the record with `superpowers:writing-plans`, run it with
   `superpowers:subagent-driven-development` in the same session or
   `superpowers:executing-plans` in a parallel one, and delete the plan in the epic's
   merge.
3. Every task issue carries the `claude` label and `epic:<name>`; each PR body closes its
   tasks with one `Closes #n` line per issue; the epic closes when its last PR merges.
4. A correction to a record goes on the epic issue as a comment and into the record in the
   epic's own PR; a record is kept and cited, never deleted with the plan.
5. Epic 5's PR 1 needs the owner's approval of the mockups as a comment on its epic before
   PR 2 starts. Every OS surface in this program is judged at real size, in both modes,
   under the `frontend-design` process.
