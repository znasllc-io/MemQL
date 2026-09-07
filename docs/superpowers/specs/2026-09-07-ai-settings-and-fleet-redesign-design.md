# Settings, AI and the Fleet app, redesigned -- Design

- **Date:** 2026-09-07
- **Status:** approved in the 2026-09-07 brainstorm as epic 5 of the local-first
  inference and routing program (`2026-09-07-local-first-routing-program.md`), the last
  to ship. The owner's words: the AI providers screen is words all over; the Fleet
  routing screen needs the same care; what we build for policies must be extremely
  good and extremely manageable; straightforward, minimal, nothing lost; custom rules
  from a prompt. D1-D7 are rulings recorded for the owner to overturn. Layouts are not
  decided here: they are decided in front of the owner, at real size, under the
  `frontend-design` process, which the owner designated as the process for every MemQL
  UI task.
- **Owner areas:** `clients/os` only: `src/apps/settings`, `src/apps/fleet`,
  `src/apps/nexus` (the run's decision), `src/kit`, `DESIGN.md` if a rule is added.
- **Depends on:** the vocabulary of epics 2, 3 and 4. Its information architecture is
  drawn alongside epic 2, so the nouns on the screen and in the DSL are the same words
  from the first day; its implementation lands after the reads it needs exist.

---

## 1. Problem

Settings, AI providers is a page of sentences: federated identity ids, a verify act, the
fleet door, save-then-apply, and prose explaining each. Fleet, Routing edits a machine
strategy row with five fields and no sentence about what a strategy is. AI-provider
policies have no surface at all, and the word "Policy" under Cluster already means
identity policy. Epics 2 to 4 add levels, policies, rules, described rules, decision
records, a catalog, hardware, measurements and sharing. Without one structure those
become ten more paragraphs.

## 2. What the tree already has

- The interface language, twelve rules in `clients/os/DESIGN.md`: every section opens
  with the Head and at most one primary act; filters behind one affordance; sort is
  quiet text; micro-preferences in Settings; one control line; actions are verbs; say it
  once; one container language; real estate belongs to content; in-surface state is
  never a checkbox; a list and its detail never share a scroll column; acts follow the
  state, in one place, and an act that is not legal is absent.
- The kit: `Head`, `Refine`, `SortControl`, `Panel`, `Subhead`, `Field`, `ActionBar`,
  `Rail`, `ChoiceStack`, `SetupGroup`, `SurfaceUnconfigured`, the dot tones.
- `src/apps/settings/ProvidersSection.tsx` and `providerFacts.ts` (the vendor forms,
  the verify act, the save-then-apply rule), `PolicyPanel.tsx` (identity policy, stays
  where it is), the Settings app's `intent` plumbing from the wizard epic.
- `src/apps/fleet/`: machines, `machines/ModelsGroup.tsx`, `models/ModelsSection.tsx`,
  `routing/RoutingSection.tsx`, `apps/`, the delegation policy editor.
- From the program: `routerDecisionsRecent`, `modelProfiles`, the described-rule
  builtins, `v1:platform:modelMeasurement`, `registration.hardware` and `sharing`, the
  machine class and recommended set.
- The owner's taste, recorded across sessions: simple, subtle, one restrained
  recommendation rather than a carousel of variants, refined at real size.

## 3. Decisions

### D1 -- Settings, AI is three sections named for the three nouns

`Doors`, `Levels`, `Rules`, replacing `AI providers`. Chosen over one long page and over
folding rules under Cluster. A door is a place inference can come from; a level is how
much a call needs; a rule is who gets what. Three words a non-technical person can hold.

- **Doors**: one row per door, Fleet, Apps, Anthropic, OpenAI, with its state dot and
  word and at most one act. The fleet row's act opens Fleet, Add machine with the
  inference intent; the apps row names the signed-in apps and opens Fleet, Apps; a
  vendor row's act opens that vendor's form in a `Panel` below the list, the same form
  as today with its verify act, save-then-apply rule and no key field anywhere. Nothing
  else on the page.
- **Levels**: a four-row table, one per level, each row saying what the level resolves
  to right now, per machine class present in the fleet, live from the feed and the
  catalog: "strong: qwen3.8:27b on studio-01, measured 0.98 structured". A level with
  no local door says which door serves it, or that it parks. No act on this page; the
  act is a rule.
- **Rules**: the precedence-ordered list, shipped rules marked with a lock and not
  editable, custom rules editable and removable, each row one line in the rule's own
  sentence. One primary act, "Describe a rule", which opens a `Panel` with one text
  field, the compiled rule in plain words beneath it as it is compiled, the simulation
  ("over the last 200 calls this would have changed 12"), and Activate. A structured
  editor for people who prefer fields sits behind "Edit as fields". `Refine` filters by
  level, door and origin.

### D2 -- Decisions are a section, and every run shows its own

`Decisions` under Settings, AI: the recent decision records, one line each, level, rule,
door, model, cost, with `Refine` by level, door, rule and outcome and a quiet count on
the Head. A row expands in place to the considered list. Nexus's run page shows each
step's decision on the step row. This is how a described rule is checked and how a
person learns what the rules do. Chosen over a dashboard: a list of what happened,
filtered, is the honest form.

### D3 -- Fleet keeps its shape and gains what the program produced

The machine page gains three groups in the container language: `Hardware` (the
inventory as facts, the machine class as one word), `Models` (the recommended set with
the one pull act, each model's flags and measured figures, an unmeasured model saying
so, the probe act for owners), `Sharing` (the mode, both consents shown, the week's
count, one act). `Models` fleet-wide gains the catalog by category behind `Refine`
(category, runtime, what this fleet lacks) and a measured column. `Routing` is
restyled, not redesigned: a sentence naming what a machine strategy decides and that
model choice is Settings, Rules, then the five fields as `Field`s. `Apps` unchanged.

### D4 -- The process is frontend-design, judged at real size, approved by the owner before implementation

PR 1 produces mockups of the five surfaces (Doors, Levels, Rules with the describe
panel, Decisions, the machine page) in both modes, empty and populated, presented to
the owner with one restrained recommendation per surface, and the owner's approval on
the epic issue is the gate for PR 2. Layouts, spacing, type and the describe panel's
behaviour are decided there. The acceptance for every later PR is rendered screenshots
in both modes, empty and populated, as the interface language demands.

### D5 -- Nothing lost

Every act the current screens offer survives: the vendor forms and verify, the fleet
door, the machine strategy fields, model pull, rename, labels, revoke. A test enumerates
the current acts by accessible name and fails if one is missing after the redesign.

### D6 -- Names on the screen

`Doors`, `Levels`, `Rules`, `Decisions`, and "Describe a rule" for the entry. Two
alternatives for the last are offered in the mockup round, "A rule in your words" and
"New rule", and the owner picks. "Smart" is not used anywhere.

### D7 -- Roles

Doors and Rules are owner or developer, the set the provider writes and the rule writes
use. Levels and Decisions are readable by owner, developer and admin, since a decision
record carries no prompt content and an admin answering "why did this go to a vendor"
needs it. The Fleet page keeps its per-machine owner gates.

## 4. The change

- `src/apps/settings/`: `DoorsSection.tsx`, `LevelsSection.tsx`, `RulesSection.tsx` with
  `DescribeRulePanel.tsx` and `RuleFieldsPanel.tsx`, `DecisionsSection.tsx`;
  `ProvidersSection.tsx` retired, its vendor forms moved under Doors; the settings
  manifest and `intent` plumbing re-pointed.
- `src/apps/fleet/machines/`: `HardwareGroup.tsx`, `ModelsGroup.tsx` grown,
  `SharingGroup.tsx`; `models/ModelsSection.tsx` with the catalog `Refine`;
  `routing/RoutingSection.tsx` restyled.
- `src/apps/nexus/`: the step row's decision.
- `src/kit/`: a `Measure` piece for a figure with provenance, promoted on its second use
  (the measured column and the machine page), and nothing else new unless the mockups
  demand it.
- Screenshots under the existing acceptance path; `docs/public/operate/ai-routing.md`
  gains the screens; `first-run.md` cross-references.

## 5. Failure modes

- A feed not loaded: every section draws its empty state, never a setup screen.
- A described rule the compiler cannot parse: the panel shows the compiler's sentence
  and keeps the text; nothing activates.
- A decision record read refused for a role: the section is absent for that role, never
  an empty list.
- A machine with no inventory: the Hardware group says the cockpit predates the field
  and names the version that reports it.

## 6. Testing

- The acts-survive test (D5).
- Each section's role gate; empty and populated renders; the describe panel's three
  states (typing, compiled, simulated) and Activate only after simulation.
- Decisions `Refine` and the expand-in-place.
- The machine page groups with and without an inventory, measured and unmeasured.
- Screenshots, both modes, empty and populated, for every surface, attached to the PR.

## 7. Delivery

Three PRs.

- **PR 1, structure and mockups.** Task 1: the information architecture and mockups of
  the five surfaces under `frontend-design`, both modes, one recommendation each, the
  owner's approval recorded on the epic.
- **PR 2, Settings, AI.** Task 2: Doors and Levels. Task 3: Rules with the describe and
  fields panels. Task 4: Decisions and the run's step decision.
- **PR 3, Fleet.** Task 5: the machine page's Hardware, Models and Sharing groups and
  the fleet-wide catalog. Task 6: Routing restyled, the acts-survive test, screenshots
  and docs.

PR 1 may start as soon as epic 2's record is agreed; PRs 2 and 3 land after the reads
they need exist. The picking-up session writes the plan from this record with
`superpowers:writing-plans` and deletes it in the epic's merge.

## 8. Out of scope

- Any engine change; every read and write this epic uses ships in epics 2 to 4.
- A theme or wallpaper change; themes sit on top of the rules.
- The Cluster app's Readiness section, which stays a signpost.

## 9. Facts to re-verify before starting

The names of the builtins and queries epics 2 to 4 shipped; the kit's current pieces
and the promotion rule in `src/kit/controls.tsx`; the twelve rules; the Settings
manifest's section ids and role gates. Read on 2026-09-07 at commit 907e385fb, before
those epics landed.
