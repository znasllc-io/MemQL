---
title: "Visual QA: the Models group and the local-models checkbox"
audience: internal
status: stable
area: ops
sinceVersion: 0.21.0
owner: znas
---

# Visual QA: the Models group and the local-models checkbox

- **Date:** 2026-09-07
- **Epic:** memql#5103 (memql#5104 the wire, the hop and the builtin; memql#5105
  the Fleet app surfaces)
- **Method:** a temporary Vite QA harness (`clients/os/qa.html` + `qa/` +
  `vite.qa.config.ts`) mounting the REAL `Shell` over a stubbed
  `useOsConnection`, driven in a real Chrome at 1920x1020 in both modes.
  Deleted after the sweep, as its five predecessors were.

DESIGN.md's closing note makes rendered screenshots the acceptance for any
surface change under its rules. This sweep earned it: **the Models group
shipped 2,583 passing tests with its content spread across three lines of a
maximized window, and nothing in the suite could have said so.**

## The recorded traps, and which fired

All seven from the previous sweeps were handled up front, and an eighth is
added here.

1. **The module swap is decided on the RESOLVED path.** Handled: the plugin
   resolves with `skipSelf` and compares absolute paths.
2. **`setRoleLadder(SEEDED_LADDER)`.** Handled at harness startup.
3. **No real vitest import.** Handled: every fake is a plain function.
4. **The stubbed `useOsConnection` must return a MODULE-LEVEL SINGLETON.**
   Handled. Every capture returned non-zero bytes on the first run, which is
   the signal that it was.
5. **Each capture needs its own `--user-data-dir`.** Handled.
6. **A wrong FIXTURE reads as a broken SURFACE.** Handled: the rows were copied
   from `clients/os/test/fleet/harness.tsx` rather than invented.
7. **The connection stub answers only the reads it knows.** Handled with a
   Proxy defaulting every other generated method to an empty result -- a
   throw there blanks the whole shell, and the capture then shows a defect in
   a surface nobody was judging.

The eighth is new, and it cost the first ten minutes:

8. **The swap replaces the WHOLE module, so the stub must export the real
   one's entire surface.** `Shell` imports `OsConnectionProvider` from
   `src/live/connection` alongside the hook being stubbed, and a stub without
   it fails Vite's dependency scan with an esbuild error naming the import --
   which reads as a problem with the Shell rather than with the harness.

A ninth, cheap and worth writing down for whoever drives the next one:

9. **`computer.scroll_to` applies a page ZOOM.** The next capture then shows a
   magnified fragment that looks like a broken layout. Scroll with
   `element.scrollIntoView()` through `javascript_tool` instead, and read the
   element's `getBoundingClientRect()` back to aim the zoom region -- remembering
   that the screenshot's pixel space is scaled against the page's.

## What the browser found

**The group's content was spread across three lines with a hand's width of
nothing between.** Each model rendered as `id`, then `size · quant · context`,
then the capability list right-aligned at the far edge -- three readings about
one model, reading as three unrelated lines. The live pull put
`pulling 8eeb52dfb3bb` roughly thirteen hundred pixels from the
`llama3.1:70b` it describes, and the history did the same to each row's model,
date and outcome.

One cause, three sites: `margin-left: auto`, copied from the `.os-fleet-app`
row directly above in the same panel. That row works because its children are
short. A model's are a 38-character id, a size and three capability words, so
at a maximized window the auto margin threw them apart.

**It was invisible at the width I first looked at.** In a default-sized window
the panel is ~530px and the rows read correctly; the defect only appears
maximized, which is how anybody actually reads a machine's page. A test could
not have found it at any width, jsdom having no layout at all.

The fix is a stack rather than a spread: the model id keeps its own line --
which it needs, being the one string here a person may have to copy and the one
an ellipsis would ruin -- with its readings beneath it, left-aligned. The pull's
status line sits beside the model name rather than at the window's edge, and the
history rows read left to right. It is correct at every width instead of at one.

**The help text was on the control line.** `os-form-row` put a two-sentence
caption in the same row as the input and the Pull button, crowding both and
pushing the act away from the field it acts on. Rule 5 puts controls on one
line at one height; a sentence is not a control, and it now sits under them.

**The two states an empty models list would have collapsed both read
correctly.** "No model runtime on this machine" names the command that installs
one; "Running ollama with no models yet" invites the pull. That distinction was
built deliberately and the browser confirmed it lands.

## What was checked and was right

- **Both modes.** The accent progress track, the pull card's accent border and
  the error red all read on the dark plate as well as the light one.
- **The unmeasured model.** `hf.co/TheBloke/Llama-2-70B-GGUF:Q4_K_M` reports
  `4k context   no capabilities advertised` -- never a zero, never a blank.
- **The bar is a fraction of the STEP.** `3.9 GiB of 36.3 GiB in this step`, in
  monospace tabular figures, under a bar at 11%. There is no whole-pull
  percentage anywhere on the surface, because the protocol states no whole-pull
  total.
- **The Pull act is absent, not disabled**, while a pull runs and for anybody
  who is not the machine's owner.
- **A failed pull carries the runtime's own words** in the error tone, and a
  succeeded-but-unadvertised one says "The cluster sees it when this machine
  next reconnects" rather than reporting plain success.

## Related

- [Local models on the fleet](../../public/operate/local-models.md)
- [Workers runbook](../../public/operate/workers-runbook.md)
- `clients/os/DESIGN.md` -- the twelve rules this sweep is the acceptance for
