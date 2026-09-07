---
title: "Visual QA: the setup surface, the marks and the Set up group"
audience: internal
status: stable
area: ops
sinceVersion: 0.21.0
owner: znas
---

# Visual QA: the setup surface, the marks and the Set up group

- **Date:** 2026-09-07
- **Epic:** memql#5077 (memql#5082 the feed and contract, memql#5083 the kit and
  both renderers, memql#5084 the mapping and the Modules column)
- **Method:** a temporary Vite QA harness (`clients/os/qa.html` + `qa/` +
  `vite.qa.config.ts`) mounting the REAL `Shell` over a stubbed
  `useOsConnection`, driven in headless Chrome at 1600x1100 (420x860 for the
  phone) in both modes. Deleted after the sweep, as its three predecessors
  were.

DESIGN.md's closing note makes rendered screenshots the acceptance for any
surface change under its rules. This sweep earned that twice: **it found one
copy defect and one fixture defect that 2,416 passing tests did not.**

## The recorded traps, and which fired

All six from the previous sweeps were handled up front. Two still fired.

1. **The module swap is decided on the RESOLVED path.** Handled: the plugin
   resolves with `skipSelf` and compares absolute paths.
2. **`setRoleLadder(SEEDED_LADDER)`.** Handled at harness startup.
3. **No real vitest import.** Handled: the harness builds its own fixture and
   touches none of the suites' `vi.fn` fakes.
4. **The stubbed `useOsConnection` must return a MODULE-LEVEL SINGLETON.**
   Handled; every capture returned non-zero bytes on the first run, which is
   the signal that it was.
5. **Each capture needs its own `--user-data-dir`.** Handled.
6. **A wrong FIXTURE reads as a broken SURFACE.** **FIRED.** The first Cluster
   capture rendered "The cluster did not answer the module inventory /
   `list modules: unexpected reply payload`" -- which reads exactly like a
   defect in the page. The harness answered `modulesInventory`; `ModulesClient`
   expects `modulesListResult`. The suite's own harness had it right, and the
   fix was to copy that shape rather than to guess a second one.

A seventh, new to this sweep:

7. **The phone shell has no launcher.** Its home IS the tile grid, so the
   desktop-shaped opener left the capture on the home screen with every app
   listed and none open -- which reads as a phone layout that cannot open an
   app. The opener now branches on layout.

## The defect the browser found

**A configured module was still being told where to configure it.** The Set up
group's third column rendered "Set in the deployment" for `campaigns`, on a row
whose own state column said "Set up".

Nothing was wrong with it in a test's terms -- the act column is derived from
`configurableFrom`, and `campaigns` is a deployment lane whatever its state.
But an instruction beside a finished row is an instruction with nothing behind
it, and a column of them beside every finished module is exactly the standing
chrome DESIGN.md rule 10 exists to keep out. The act is now ABSENT for a
configured module, and the row reads "Campaign sending -- Set up", which is
the whole answer. `readinessStates.test.tsx` gained the case.

This is the kind of defect only the pixels raise: the words were individually
correct, and no assertion about either one on its own would have failed.

## Scenes checked

| Scene | Reading |
|---|---|
| Campaigns, Audiences, owner (dark + light) | The empty RING as the mark, not an error tone; "Campaigns is not set up yet"; the email module's own sentence verbatim; one primary act "Set up Campaigns". The rail is intact and every section still reachable. The red dot sits on BOTH the title-bar gear and the rail's Settings entry |
| Campaigns, Audiences, viewer | The same surface with the act replaced by "An owner or developer can set it up in Settings." No button, and no Set up group behind the Settings entry |
| Campaigns, Settings | The Set up group ABOVE the preferences: "Email sender / ● Not set up / [Open Integrations]" and "Campaign sending / Set up" with no act. The mark stays lit on Settings while standing in it |
| Nexus, Automations, `ai` partial | The body RENDERS -- partial never gates -- and the mark is amber, visibly distinct from the red. Automations declares no requirement of its own, so nothing gates it even at `unconfigured` |
| Cluster, Modules (light) | The cluster-wide chip beside `storage`: "Partly set up (bff-b=unconfigured, bff-a=configured)", next to that node's own `built_in` and `node` chips. Two readings of different scope, side by side. No chip at all on `identity`, `shopify` or `referencepack`, which have no readiness counterpart |
| Campaigns, phone (420x860) | The surface centres in the phone body with the section strip above it; the strip scrolls, so the Settings entry and its dot sit off the right edge at this width, reachable by scrolling as every other section is |
| Both modes | The setup surface resolves in the light set; the error and warn tones survive; no hardcoded dark value leaked |

## Decisions taken at the pixels, not changed

**The mark is drawn for a viewer too.** A viewer cannot set anything up, and
rule 12's "an act that is not legal is ABSENT" argues for hiding it. It is
kept because a mark is STATE, not an act: without it a viewer sees an app that
will not open and has nothing anywhere on the chrome saying why. The surface
already names who can fix it, which is the part that would otherwise be
missing.

**The sentence wraps to two lines in the Campaigns case.** `os-setup-body`
keeps `max-width: 42ch`, the measure its sibling `os-rank-refused-body` uses.
Widening it would fit this one description on one line and would not help the
`ai` one, which is twice as long. Consistency with the surface beside it is
worth more than one sentence's wrap.
