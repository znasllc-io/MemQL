---
title: "Visual QA: Settings, AI providers after key removal"
audience: internal
status: stable
area: ops
sinceVersion: 0.9.38
owner: znas
---

# Visual QA: Settings, AI providers after key removal

- **Date:** 2026-09-07
- **Epic:** memql#5088
- **Method:** a temporary Vite QA harness (`clients/os/qa.html` + `qa/main.tsx`
  + `qa/connection.tsx` + `qa/fake.ts` + `vite.qa.config.ts`) mounting the REAL
  `SettingsApp` at `sectionId="providers"` over a fake connection shaped like
  the one `test/settings/providers.test.tsx` drives, in headless Chrome at
  1600x1100, in both themes. Deleted after the sweep, as its two predecessors
  were.

The section was rebuilt for this epic: the API-key box is gone, both vendors
get a federation form, and each door reports one of three states. jsdom takes
none of the measurements that decide whether that works -- it performs no
layout and resolves no custom properties, so the 204 green settings tests say
nothing about how the screen reads. DESIGN.md's closing note makes rendered
screenshots the acceptance, and this is that pass.

## What the harness had to get right

Both traps from the previous sweeps fired again, which is the argument for
having written them down.

1. **The module swap is decided on the RESOLVED path.** `src/live/connection`
   is imported by five different relative spellings, and a specifier alias
   cannot cover `./connection` from inside `src/live` without also catching
   `chrome/connection`, a different module. The plugin resolves with
   `skipSelf` and compares absolute paths.
2. **`setRoleLadder(SEEDED_LADDER)`.** The ladder starts EMPTY and the shell
   fills it from `v1:rbac:role`; without it the app renders entirely
   read-only. Correct product behaviour, silent to debug.

A third, specific to this section: **the swapped module must export all four
names** (`useOsConnection`, `bridgePathFor`, `osBridgePath`,
`OsConnectionProvider`). Missing one fails the dependency scan for the whole
bundle -- a good failure, since the alternative is a partially swapped tree.

## What it found

**One defect, and it was invisible to the suite by construction: the two
service-account placeholders were swapped.** The OpenAI panel's Service
account id box read `svac_...`, which is ANTHROPIC's prefix (documented on
`deploy/k8s/base/anthropic-federation.yaml`), while Anthropic's own -- the one
that really is `svac_` -- had no hint at all. jsdom renders no placeholder, so
no assertion could have caught it, and in code review the two lines read as
plausible neighbours.

Fixed in `providerFacts.ts`: Anthropic's service account gets `svac_...`, and
BOTH OpenAI hints are now empty. Nothing in this repo knows the shape of
OpenAI's identity-provider or service-account ids -- its ConfigMap deliberately
names none -- and a guessed prefix in an empty box is worse than an empty box:
an operator who pastes a correct id that does not match it goes looking for the
mistake in their own console.

## What it confirmed

- **The three door states are distinct at a glance, in both themes.** `Open`
  carries a 3px accent rule and a soft accent ground; `Not set up` is a
  hairline with no colour and no alert role; `Half set up` is the only one
  carrying warn. The state word sits in a shared 104px column so the three
  panels' words stack and the cluster reads down one column. The specific
  uncertainty the implementer flagged -- whether `--os-accent-soft` under an
  open door reads as intended in light mode -- is answered: it reads as a quiet
  tint, clearly distinct from the unset hairline without competing with the
  warn rule.
- **`Half set up` renders the engine's own sentence verbatim**, in monospace,
  naming which ids are set and which are missing, above the instruction that
  matters most: re-enter every id, not only the missing one, because the write
  stores the set whole.
- **The local-cluster case is right, and it is the one most easily got wrong.**
  At `memql.localhost` both vendors read `Closed` with the reason -- a local
  cluster's OIDC issuer is private, so no id typed there would ever be accepted
  -- **the forms are absent rather than disabled**, `Your machines` is promoted
  to the top of the section, and a footnote says this is not a gap waiting to
  be closed.

## What it did not measure

The fleet panel showed `Not set up` throughout, because `fleetDoorFrom` reads
`localModelCount` / `localEligible` / `fleetInferenceInstalled` and the
harness's fake supplied none of them. That is a fixture gap, not a defect --
verified by reading the function, which distinguishes "cannot place fleet calls
at all" from "no machine is offering a model" deliberately, because the two
look identical on a page and have entirely different fixes. A sweep of the
fleet door's own states belongs to the epic that owns it.
