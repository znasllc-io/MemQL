# First-Run Core Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An owner or developer who opens MemQL OS on a cluster whose core is not set up
finds a setup widget on the first desk: a rail of the core modules from the readiness
feed, the passkey first, then inference, storage and the email sender as stops that light
up, each opening the one place it is configured and lighting when the feed says so; the
widget removes itself when the core is configured.

**Architecture:** PR 1 promotes the Deployables rail to the kit, adds the pure
`stops.ts` mapping and the `setup` widget with its manifest, gate, seed placement and
retire rule. PR 2 adds the inference stop's three doors with their intents into Fleet and
Settings, the return dispatcher, the intent plumbing in the two apps it opens, screenshots
and the operator page.

**Tech Stack:** React + vitest (`clients/os`); no engine change (the feed, the flag and
the gates come from epics 1, 2 and 4).

**Spec:** `docs/superpowers/specs/2026-09-06-first-run-wizard-design.md`. Read D1 to D6
first, then `clients/os/DESIGN.md` rules 1, 8, 9, 11, 12 and the Accounts first-run card's
header (`clients/os/src/apps/accounts/FirstRunCard.tsx`). Depends on epics 1 to 4 merged.

**Closes:** the epic issue and its task issues, filed by Task 0.

---

## Global constraints

- **No modal, no banner, no badge, no flag in a browser** (the Accounts covenant): the
  readiness verdicts are the state; the widget renders nothing while the feed is not
  loaded and nothing once the core is configured.
- **Stops, not steps** (interface rule 12 and the Deployables rail's own header): the next
  unanswered stop is open; there is no Next, no Back, no step number; an act that is not
  legal is absent; one `Head` per surface.
- **The passkey-first law is the only ordering law** and the widget says it in one
  sentence; the other stops may be opened in any order.
- **Nothing is duplicated:** every act opens the existing surface (`openApp(appId,
  sectionId, payload)` through `useOsIfPresent`); the passkey ceremony stays on the
  identity origin (`/me/devices`, `/enroll`).
- **Gate `{ any: ["owner", "developer"] }`** on the widget manifest; role checks name
  `ladderLoaded` and `readiness.loaded` in their deps.
- **OS tests run from `clients/os`**; acceptance is screenshots in both modes; a widget
  takes no props and reads everything from hooks.
- **Stage by explicit path; commit trailers; no emojis; `example.com` hostnames.**

---

## Task 0: file the epic and its tasks

**DONE on 2026-09-06:** epic #5106; tasks #5107 (PR 1), #5108 (PR 2). Do not file them again; verify with `gh issue list --repo znasllc-io/memql --label epic:first-run-wizard`.

Label `epic:first-run-wizard`; the epic issue (`epic`, label, `feature`, `area/portal`,
`claude`); task issues (`task`, label, `area/portal`, `claude`):

| Title | PR |
|---|---|
| Kit: the rail promoted from Deployables; `stops.ts`; the setup widget, its gate, seed and retire rule | 1 of 2 |
| The inference stop's three doors and their intents; the return dispatcher; intent plumbing in Fleet and Settings; screenshots; the first-run page | 2 of 2 |

---

# PR 1: the rail, the stops, the widget

## Task 1: promote the rail

**Files:**
- Create: `clients/os/src/kit/Rail.tsx` (from `src/apps/deployables/page/Rail.tsx`, the
  stop vocabulary generalised: `Stop { id: string; name: string; state: "done" | "open" |
  "waiting" | "unknown"; sentence?: string; body?: ReactNode }`, `Rail({ stops, openStop,
  onOpenStop, label, compact? })`, the "next unanswered is open" rule as `nextOpen(stops)`)
- Modify: `src/apps/deployables/page/Rail.tsx` becomes an adapter from `RailInput` to
  `Stop[]` over the kit piece; its tests pass unchanged
- Modify: `src/kit/index.tsx` (export), `src/styles/index.css` (the rail's classes move
  under `.os-rail-*` if they are not already generic)

- [ ] **Step 1: Deployables' rail tests run against the adapter** before and after.
- [ ] **Step 2: Commit**

```bash
git add clients/os/src/kit/Rail.tsx clients/os/src/kit/index.tsx clients/os/src/apps/deployables/page/Rail.tsx clients/os/src/styles/index.css
git commit -m "kit: the rail, promoted on its second use"
```

## Task 2: `stops.ts` and the widget

**Files:**
- Create: `clients/os/src/apps/setup/stops.ts`, `stops.test.ts`
- Create: `clients/os/src/apps/setup/SetupWidget.tsx`, `manifest.ts`
- Modify: `clients/os/src/apps/registry.tsx` (`widgets: [askWidget, setupWidget]`),
  `src/chrome/state.tsx` (`seedDocument` places `setup` under `ask` when the role clears
  the gate), `src/chrome/LauncherOverlay.tsx` if widgets are listed there
- Tests: `test/setup/widget.test.tsx`, `test/system/desktop.test.ts` (the seed)

**Interfaces:**

```ts
export type StopId = "passkey" | ModuleId;   // ModuleId from src/system/modules
export interface StopFacts { readiness: Readiness | undefined; passkeys: Reading<Row[]>; authEnabled: boolean }
export function stopsFor(facts: StopFacts): Stop[]   // passkey first; then the core modules in manifest order; unknown while not loaded
export function coreIsConfigured(stops: Stop[]): boolean
```

`SetupWidget` renders `null` while any stop is `unknown` or when `coreIsConfigured`, else
`<Rail stops openStop onOpenStop />` with the sentence "A passkey first: a sign-in link is
the only way back into an account without one." on the passkey stop; each module stop's
body is `<SetupGroup app="Setup" requires={[id]} wants={[]} readiness={readiness} />`.

- [ ] **Step 1: Tests first** — `stopsFor` over every combination (unknown feed; passkey
  absent; all configured; auth disabled means passkey `done` with the sentence "not
  applicable"); the widget renders nothing in the two silent states and the rail
  otherwise; a viewer session has no widget; the seed places it for an owner.
- [ ] **Step 2: Implement, run, commit, push, PR 1**

Run: `cd clients/os && npm run typecheck && npx vitest run test/setup test/system test/deployables`

```bash
git add clients/os/src/apps/setup clients/os/src/apps/registry.tsx clients/os/src/chrome/state.tsx clients/os/test/setup clients/os/test/system
git commit -m "os: the setup widget -- the core modules as stops that light up, gone when they are all lit"
git push -u origin epic/first-run-wizard
```

---

# PR 2: the doors, the returns, the pages

## Task 3: the inference stop, the return dispatcher, the intents

**Files:**
- Create: `clients/os/src/apps/setup/InferenceStop.tsx` (a `ChoiceStack` in the prose
  voice: "A machine on your fleet" (recommended), "Anthropic", "OpenAI"; each act opens
  `fleet`/`machines` with `{ addMachine: { inference: true } }`, or `settings`/`providers`
  with `{ vendor: "anthropic" | "openai" }`; a developer sees words for the federation
  doors until `providers` admits them)
- Create: `clients/os/src/apps/setup/returns.ts` (`parkSetupReturn()`,
  `takeSetupReturn()`), `SetupReturnDispatcher.tsx` (null-rendering, mounted in
  `Shell.tsx` beside `ConnectReturnDispatcher`; focuses the first desk)
- Modify: `src/apps/fleet/machines/MachinesSection.tsx` and `addMachine/AddMachine.tsx`
  (consume the intent: open Add machine with the local-models checkbox pre-selected),
  `src/apps/settings/SettingsApp.tsx` (forward `intent` to `ProvidersSection` and
  `IntegrationsSection`), `ProvidersSection.tsx` (scroll to and open the named vendor's
  form), `IntegrationsSection.tsx` (open the email card)
- Tests: `test/setup/inferenceStop.test.tsx`, `test/setup/returns.test.tsx`,
  `test/fleet/addMachineIntent.test.tsx`, `test/settings/providersIntent.test.tsx`

- [ ] **Step 1: Tests first** — each door's act calls `openApp` with the right triple; a
  parked return is consumed once; the two apps act on their intents and consume them by
  id; a developer's federation door is words.
- [ ] **Step 2: Implement, run, commit**

```bash
git add clients/os/src/apps/setup clients/os/src/chrome/Shell.tsx clients/os/src/apps/fleet clients/os/src/apps/settings clients/os/test
git commit -m "setup: the three inference doors open the one place each is configured, and bring the person back"
```

## Task 4: screenshots, the page, PR 2

- The Vite QA harness recipe from `docs/internal/ops/2026-09-06-os-operator-parity-visual-qa.md`
  (its six traps); captures in both modes: fresh cluster as owner, as developer, two stops
  lit, the empty desk after completion; the ops note; the harness deleted before commit.
- `docs/public/operate/first-run.md` with front-matter: what an owner sees first, the four
  stops, what the widget does not cover (each app's Set up group), where it goes, the
  passkey-first sentence; linked from `docs/public/overview/quickstart.md`; `GLOSSARY.md`.
- Delete this plan; `make os-test`, `npm run build`, the docs gates
  (`go test -count=1 -run 'TestDocsFrontMatter|TestDocsRelativeLinks|TestNoVendorDomainLiterals' .`).

```bash
git add docs/public/operate/first-run.md docs/public/overview/quickstart.md GLOSSARY.md docs/internal/ops/
git rm docs/superpowers/plans/2026-09-06-first-run-wizard.md
git commit -m "docs: what an owner sees first; the executed plan goes with its epic"
git push -u origin epic/first-run-wizard-2
```

Closes the PR 2 task issue and the epic.

---

## Plan self-review

**Spec coverage.** D1: Task 2. D2: Tasks 1 and 2. D3: Task 2's stop bodies. D4: Task 3.
D5: Task 2 (the storage stop is the Set up group's deployment-only row). D6: Task 2's
passkey stop (a link and a re-read). Section 5 failure modes: the unknown state in Task
2, the refused actor in Task 3, the empty return in Task 3, auth disabled in Task 2.
Section 6 tests are named in each task.

**Placeholders.** None; the identity page paths are the current ones and are re-verified
by the executing session per the spec's section 9.

**Type consistency.** `Stop` from Task 1 is what `stopsFor` returns in Task 2 and
`InferenceStop` fills in Task 3; the intent payload shapes `{ addMachine: { inference } }`
and `{ vendor }` are written once in `InferenceStop.tsx` and read in the two apps.
