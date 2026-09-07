# The first-run core wizard -- Design

- **Date:** 2026-09-06
- **Status:** approved in the 2026-09-06 brainstorm as epic 5 of the inference and setup
  program (`2026-09-06-inference-and-setup-program.md`), the last to ship. The owner set
  the scope ("the core, not the whole system"), the audience (owner or developer) and the
  inference offer (fleet first, then Anthropic federation, then OpenAI federation, any
  combination). D1-D6 are rulings recorded for the owner to overturn.
- **Owner areas:** `clients/os` (a widget, a promoted rail, the Settings and Fleet sections
  it opens), `component/memql` (one read the widget needs), `dsl/platform`; the identity
  service's pages are linked, not changed.
- **Depends on:** epic 1 (the readiness feed and the `core` flag), epic 2 (the federation
  forms for both vendors and the owner-or-developer gate on them), epics 3 and 4 (the
  fleet door and the guided inference machine).

---

## 1. Problem

A fresh cluster boots ownerless; an owner claims it through the identity service's
`/setup` page and a magic link; from then on MemQL OS opens every app as though the
platform were configured. The portal used to carry a first-run gate over inference and
the owner's passkey; it was client-only and session-latched, enforced nothing and stood
in front of everybody, and the portal-removal record lists it, alone, as the thing not
rebuilt. What remains is a Readiness signpost in the Cluster app that names three fixes in
prose and opens nothing, and an Accounts first-run card that asks one question about the
owner's company. The owner wants the first thing an owner or developer sees on a fresh
cluster to be a guided setup of the core: enough to make the platform useful, not the
whole system, which stays with each app's Set up group.

## 2. What the tree already has

- **Ownership is a stamp.** `bootstrappedAt` on `v1:identity:clusterSettings` means
  "ownership claimed", set by the magic-link verifier; `/setup` answers 404 from then on.
  `HasClaimedOwner` asks about credentials, not rows. There is no "core setup completed"
  field, `clusterSettingsCurrent` declares no tier, and its editable slice in
  `adminops` is closed.
- **The shell's session facts** carry `access` (with `clusterRole`), `ladderLoaded`, and,
  after epic 1, `readiness`. The seeded first desk is one desk, the Ask widget top right,
  Settings pinned. The desktop document roams per person and is the wrong home for a
  per-cluster flag; the Accounts card's covenant says why: nothing is dismissed and nothing
  is remembered in a browser.
- **Every configuration surface the wizard needs already exists and takes no props:**
  Settings, AI providers (owner-only, because the engine's provider writes are owner-only);
  Settings, Integrations (owner or developer; the email slots, env-supplied ones not
  editable); Settings, Keys (admin); Cluster, Readiness (signpost); Fleet, Machines and
  "Add machine" (token mint, one-line install command, success detected as the population
  growing). Passkey registration lives on the identity origin (`/me/devices`; `/enroll` for
  a person with no credential yet) and is deliberately not duplicated in the OS.
- **The interface rules refuse a stepper.** A sequence is right only when the order is a
  law (the Deployables rail: stage, roll, publish); three decisions with no ordering law
  are a form; an act that is not legal is absent, never disabled; the next unanswered stop
  is the open one and there is no Next. The kit has no stepper; the Deployables `Rail` is
  local to that app and may be promoted on second use.
- **A widget takes no props and gets everything from hooks**; `openApp(appId, sectionId,
  payload)` opens another app at a section; `ConnectReturnDispatcher` is the precedent for
  sending a person out and bringing them back.

## 3. Decisions

### D1 -- The wizard is a first-desk widget, gated owner or developer, that retires itself

Chosen over a Settings section (standing surfaces there never retire), a Cluster section
(the Readiness header says the gate is deliberately not rebuilt there), a dock fixture
(by its own charter a permanent destination) and a modal (forbidden by the Accounts
covenant). `OsWidgetManifest` gains a `setup` widget beside `ask`, roles
`{ any: ["owner", "developer"] }` as the Integrations gate is, placed by `seedDocument` on
the first desk under the Ask widget for people who clear the gate. It renders while the
core is not configured and renders nothing, then removes itself from the desk, once every
core module reports configured; the readiness verdicts ARE the state, so nothing is
dismissed and nothing is remembered in a browser. A person may remove the widget from the
desk like any widget; it comes back on a fresh desk, which is the honest answer to "where
did it go".

### D2 -- Stops that light up, not steps with Next

The widget is a rail of the core modules from the readiness feed (`core: true`): the
owner's passkey, inference, storage, the email sender. Only one ordering is a law and it
is stated: a passkey before anything else, because a sign-in link is the only way back
into an account without one. The rest light up in the order the manifest declares and
may be opened in any order; the next unanswered stop is open; there is no Next, no Back
and no step number. The Deployables `Rail` is promoted to the kit for this second use,
its stop vocabulary generalised from deploy stages to module ids.

### D3 -- A stop is the module's Set up group, opened in place

Each stop renders the same `SetupGroup` epic 1 puts at the top of an app's Settings
(module name, state, the act), with two additions: the module's description as the stop's
sentence, and for the inference stop the three doors as a `ChoiceStack` in the prose
voice (a machine on your fleet, Anthropic, OpenAI; any combination). An act opens the
existing surface through `openApp`, the person configures there, and the stop's state
follows the readiness feed live when they return; a return marker parked before opening
brings them back to the desk, the `ConnectReturnDispatcher` precedent. Nothing is
duplicated: the federation forms, the email slots and the machine pairing keep their one
home each.

### D4 -- The inference stop offers the fleet first, and the offer is a machine

The fleet door's act opens Fleet, Add machine with the "this machine will run local
models" checkbox pre-selected (epic 4), so the person leaves with one install line and
comes back when the machine registers and its model appears. The two federation doors
open Settings, AI providers at the vendor's form (epic 2). A developer sees the fleet door
in full; the federation doors are offered to a developer only once epic 2 widens the
provider writes to the owner-or-developer set, which this record asks epic 2 to do, and
until then a developer's federation act is the words "an owner can federate this
cluster in Settings".

### D5 -- Storage is honest about being deployment-only

Storage has no OS surface and none is invented: its stop names the two variables and
says "set in the deployment", exactly as epic 1's Set up group does, and lights up when
the readiness feed says the lane is complete. The stop is core because five apps sit on
it, not because the wizard can configure it.

### D6 -- The passkey stop links to the identity origin and re-reads

`/me/devices` for a person who can sign in, `/enroll` with an enrolment token for one
who cannot yet; the OS never runs a WebAuthn ceremony of its own (a second implementation
of a security ceremony is the last thing that should have two). The stop re-reads
`passkeysForSelf` on return and turns green; an unreadable read is "not known", never
"none", the Readiness section's rule.

## 4. The change

- `dsl/platform`: no new persisted rows. The readiness feed carries `core`; the widget's
  own reads are `passkeysForSelf` and `inferenceStatus` (both ungated), which the
  Readiness section already reads.
- `clients/os/src/kit/Rail.tsx`: the promoted rail, `Stop { id, name, state, sentence,
  body }`, `Rail({ stops, openStop, onOpenStop })`, the "next unanswered is open" rule in
  one place; Deployables re-pointed at it, its tests kept.
- `clients/os/src/apps/setup/`: `SetupWidget` (the manifest, the roles gate, the
  retire-when-done rule), `stops.ts` (the pure mapping from readiness verdicts and the two
  reads to stops, testable without a DOM), `InferenceStop.tsx` (the three-door choice and
  its acts), `returns.ts` (the parked return marker and the dispatcher).
- `clients/os/src/chrome/state.tsx`: `seedDocument` places the setup widget for a role
  that clears its gate; `Shell.tsx` mounts `SetupReturnDispatcher` beside the two existing
  dispatchers.
- `clients/os/src/apps/fleet/addMachine/AddMachine.tsx`: accepts an intent that
  pre-selects the local-models checkbox (epic 4's flag).
- `clients/os/src/apps/settings/SettingsApp.tsx`: forwards `intent` to the providers and
  integrations sections so a stop can open the right vendor form.
- Epic 2 amendment: `providerAuthStatus`, `providerFederationSet`, `providerVerify`,
  `providersReload` gated owner-or-developer as a set, and the `providers` section's role
  follows.
- Docs: `docs/public/operate/first-run.md` (what an owner sees first, what the widget
  covers and does not, where it goes).

## 5. Failure modes

- The readiness feed not loaded: the widget renders nothing, exactly as an app gates
  nothing; a configured cluster never flashes a wizard.
- A stop's surface refuses the actor (a developer on the providers section before epic 2's
  gate change): the stop shows the words, never a button that refuses.
- A return marker with no widget to return to (the person configured everything while
  away): the dispatcher opens nothing and clears the marker.
- Auth disabled (`MEMQL_IDENTITY_ENABLED=false`): the passkey stop reports "not
  applicable" and the rail starts at inference, the retired gate's own carve-out.

## 6. Testing

- `stops.ts` on fixtures: every combination of the four verdicts plus the passkey read,
  including unknown; the passkey-first law; the retire condition.
- The widget renders nothing while unknown and after completion; renders the rail for an
  owner and a developer; the launcher and desk tests see it only for the two roles.
- The rail promotion: Deployables' existing rail tests pass unchanged against the kit
  piece.
- `InferenceStop`: three doors, each act opens the right app and section with the right
  intent, the developer variant shows words for federation until the gate widens.
- Return dispatcher: a parked marker opens the desk once and is consumed.
- Screenshots, both modes: fresh cluster as owner, as developer, mid-way with two stops
  lit, and the empty desk after completion.

## 7. Delivery

Two PRs. PR 1: the rail promotion, `stops.ts`, the widget and its manifest, the seed, the
retire rule, tests. PR 2: the inference stop's doors and intents, the return dispatcher,
the AddMachine and Settings intent plumbing, screenshots, the doc. Both branch from
`main` after epics 1 to 4 have merged.

## 8. Out of scope

- Everything that is not core: each app's own setup stays with its Set up group.
- Any server-side gate: the wizard gates nothing and never redirects; the engine's own
  refusals are the enforcement, as the retired gate's header said.
- A "skip" or "remind me later": there is nothing to remember; the widget is the state.

## 9. Facts to re-verify before starting

That epic 1's feed carries `core`; that epic 2 widened the provider gates; that epic 4's
`--inference` flag and checkbox exist; the identity pages' paths (`/me/devices`,
`/enroll`); the Deployables rail's current props. All were read on 2026-09-06.
