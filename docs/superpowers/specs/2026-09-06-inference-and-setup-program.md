# Inference and setup -- the five-epic program

- **Date:** 2026-09-06
- **Status:** agreed with the owner in the 2026-09-06 brainstorm. Every fork below was put
  to the owner as selectable options and answered; the per-epic records carry the
  reasoning and what each choice rejected.
- **What it is:** the index for a program of five epics that together make a fresh
  MemQL cluster useful without a person ever entering a vendor API key: the platform
  knows what is configured (1), reaches Anthropic and OpenAI by workload identity only (2),
  serves inference from the owner's own machines and the apps signed in on them (3, 4), and
  walks the owner through the core setup once (5).
- **Repositories:** epics 1, 2 and 5 live in `memql`; epic 3 has an engine half in `memql`
  and a cockpit half in `memql-cockpit`; epic 4 lives in `memql-cockpit` with a small engine
  half in `memql`. Each half has its own design record and implementation plan in its
  repository, under `docs/superpowers/specs/` and `docs/superpowers/plans/`.

---

## The five epics, in shipping order

| # | Epic | Repository | Record | Plan | Depends on |
|---|---|---|---|---|---|
| 1 | Configuration readiness -- the platform knows what is set up, and the OS says so | memql | `2026-09-06-configuration-readiness-design.md` | `2026-09-06-configuration-readiness.md` | nothing |
| 2 | OpenAI workload identity federation, the official SDK, and no more API keys | memql | `2026-09-06-openai-federation-and-key-removal-design.md` | `2026-09-06-openai-federation-and-key-removal.md` | nothing (1 is convenient, not required) |
| 3 | Fleet inference completion, and subscription apps as an inference door | memql (engine) + memql-cockpit (cockpit) | `2026-09-06-fleet-inference-and-app-door-design.md` in each repo | `2026-09-06-fleet-inference-and-app-door.md` in each repo | 2 for the key door's removal; the cockpit half and the engine half share a proto change |
| 4 | Runtime install and model pull on the cockpit, and the guided inference machine | memql-cockpit (cockpit) + memql (Fleet app) | `2026-09-06-inference-machine-setup-design.md` in each repo | shipped; the engine half's plan is deleted (epic memql#5103) | 3's model attributes |
| 5 | The first-run core wizard | memql | `2026-09-06-first-run-wizard-design.md` | `2026-09-06-first-run-wizard.md` | 1, 2, 3, 4 |

The order was chosen as "foundations first, wizard last": the wizard is the core modules'
Set up groups walked in order on first run, so it consumes the readiness model, both
federation doors, the fleet doors and the cockpit's runtime setup. Epic 1's Set up groups
give owners a guided path in the meantime.

## Issues, filed 2026-09-06

| Epic | Repository | Epic issue | Task issues |
|---|---|---|---|
| 1 Configuration readiness | memql | #5077 | #5078-#5084 |
| 2 OpenAI federation and key removal | memql | #5088 | #5089-#5095 |
| 3 Fleet inference and the app door, engine half | memql | #5096 | #5097-#5102 |
| 3 Fleet inference and the app door, cockpit half | memql-cockpit | #382 | #383-#386 |
| 4 Inference machine setup, engine half | memql | #5103 | #5104, #5105 |
| 4 Inference machine setup, cockpit half | memql-cockpit | #387 | #388-#390 |
| 5 The first-run wizard | memql | #5106 | #5107, #5108 |

Every epic issue names its PR grouping; every task issue opens with its epic, its PR
number and its plan path; all carry the `claude` label and the epic's `epic:<name>` label.

## The decisions that cut across epics

Recorded once here; the per-epic records restate the ones they implement.

1. **Manual vendor API keys are removed everywhere, the local cluster included.** Chosen
   over "refuse keys in the cloud, keep the env key local-only" and over "remove only the OS
   key entry". Consequences accepted: a developer on a local cluster reaches cloud models
   only through a fleet machine's signed-in app, and streaming transcription and whisper
   lose their bearer on a local cluster until a fleet machine can transcribe. The Anthropic
   federation record's D4 and D6 ("local keeps the key") are reversed. Executed in epic 2,
   because the OpenAI provider has no credential source until OpenAI federation exists.
2. **Inference has three doors: the fleet, Anthropic federation, OpenAI federation.** The
   fleet is the default and the first offer; any combination is allowed. A signed-in
   Claude Code or Codex on a fleet machine becomes a door once epic 3 builds the router
   path; until then it is a task-execution surface only.
3. **The default routing order is local-strongest, then a fleet app, then federation,
   automatically, under the cost ceilings.** Among local models the STRONGEST eligible one
   wins rather than the cheapest, since local models cost nothing either way; the federation
   hop is governed by the existing dollar ceiling and loop caps; work parks only when every
   door is shut. This replaces park-not-fallback (D2 of the local-models record) as the
   DEFAULT; an authored policy may still pin any order; person-tunable routing rules are a
   later epic.
4. **MCP cannot start the loop from the cluster's side.** A server may make a request of a
   client only while that client is calling it (MCP 2026-07-28), and Claude Code does not
   implement sampling. The worker stream carries requests to a machine; MCP carries the
   app's calls back; each app is driven through its own harness protocol (Codex app-server
   or its MCP tools, Claude Code resumable headless sessions). An app the person started
   themselves and pointed at our MCP server may pull work through a `nextTask` and `submit`
   pair as a non-default option.
5. **The official OpenAI Go SDK replaces the community one**, as long as it does not limit
   functionality; it carries workload identity federation natively.
6. **"Strongest" needs a ranking the fleet does not advertise yet:** the cockpit will
   advertise parameter count and quantization as Ollama reports them; the engine ranks by
   parameter count then context window by default; a policy may override.
7. **Docker is always a requirement for a runtime the cockpit installs**, and the cockpit
   gains runtime install and model pull; models come from the Ollama library and Hugging
   Face GGUF repositories through Ollama's own pull.
8. **The wizard covers the core, not the whole system:** identity and the owner's passkey,
   inference (fleet first), storage, the email sender. Everything else stays with each
   app's Set up group.

## How another session picks an epic up

1. Read the epic's design record, then its plan; the plan's header names the skill to run
   it with (`superpowers:subagent-driven-development` in the same session, or
   `superpowers:executing-plans` in a parallel one) and the plan file is the brief source
   for every task. Do not start implementation on `main`; branch as `epic/<name>` (the
   engine) and `epic/<name>` (the cockpit), one PR per plan half at most two per epic.
2. Every task issue carries the `claude` label and `epic:<name>`; the epic issue lists the PR
   grouping; each PR body closes its tasks with one `Closes #n` line per issue.
3. **Epic 1 is already in flight.** Branch `epic/configuration-readiness` in `memql` holds
   the record, the plan and Tasks 1 to 3 of PR 1, reviewed; its SDD ledger under the
   worktree's `.superpowers/sdd/2026-09-06-configuration-readiness/progress.md` says where
   to resume (Task 4). A session resuming it recreates the worktree from that branch and
   starts the ledger's next task; the ledger's rulings stand.
4. A plan is deleted in its epic's own merge (docs/CLAUDE.md); a record is kept and cited.
5. The facts the records rest on were verified on 2026-09-06 against the vendors' current
   documentation and both trees; each record's last section lists them so a later reader
   can re-verify what has moved.
