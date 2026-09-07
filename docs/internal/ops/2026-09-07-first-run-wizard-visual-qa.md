---
title: "Visual QA: the first-run wizard"
audience: internal
status: stable
area: ops
sinceVersion: 0.21.0
owner: znas
---

# Visual QA: the first-run wizard

- **Date:** 2026-09-07
- **Epic:** memql#5106 (memql#5107 the kit rail, the stops and the widget;
  memql#5108 the doors, the intents and the returns)
- **Method:** a temporary Vite QA harness (`clients/os/qa.html` + `qa/` +
  `vite.qa.config.ts`) mounting the REAL `Shell` over a stubbed
  `useOsConnection`, driven in headless Chrome at 1600x1100 in both modes.
  Deleted after the sweep, as its four predecessors were.

DESIGN.md's closing note makes rendered screenshots the acceptance for any
surface change under its rules. This sweep earned that: **the widget shipped
2,493 passing tests with its one act below the fold, and nothing in the suite
could have said so.**

## The recorded traps

All seven from the previous sweeps were handled up front and none fired. The
harness resolved the connection swap on absolute paths, seeded the role
ladder, imported no vitest, returned a module-level singleton connection, gave
each capture its own `--user-data-dir`, and copied its fixture shapes from the
suites' own harnesses rather than guessing a second set. Every capture
returned non-zero bytes on the first run, which is the signal that trap 4 was
handled.

An eighth, new here and cheap to avoid:

8. **`--hide-scrollbars` makes an overflowing card look truncated rather than
   scrollable.** That is the right flag for a capture -- a scrollbar in a
   screenshot is chrome nobody is judging -- but it means an overflow reads as
   a rendering defect. Both readings were wrong in the same direction here, so
   it cost nothing; on a surface that is MEANT to scroll it would send
   somebody looking for a bug.

## What the browser found

**The one act on the card was below the fold.** With the inference stop open,
three named choice cards each carrying two lines of prose ran to 210px, and
the card ended between the doors and the button that acts on them. A first-run
wizard whose reader has to scroll to find its only control has not done its
job, and every one of the surface's 60 tests passed while it was true --
jsdom has no layout, so "does this fit" is not a question any of them can ask.

Two changes, and the second is the better one:

- **The card is four cells tall, not three.** Sized for its tallest stop, so
  nothing scrolls in the state a person actually meets.
- **The doors carry no prose; the chosen one's sentence sits under them.**
  Three names read at a glance, and a person reads the sentence for the door
  they are considering. That is what a selection is for, and it is a better
  surface at any height.

The alternative -- shrinking the choice pill's padding inside this one widget
-- was rejected: interface rule 5 puts every control on one line at one size,
and a widget that quietly runs a smaller one is the drift the rules exist to
stop.

A second, smaller finding fell out of the same capture: with the doors in
place, the stop was ALSO carrying the module's own sentence ("Inference needs
a provider: a machine on your fleet serving a model, or a federated cloud
vendor") directly above a list reading exactly that. Rule 7 -- the sentence is
now the doors, and only the doors.

## Scenes checked

| Scene | Reading |
|---|---|
| Fresh cluster, owner | Four stops, the passkey open and accent-ringed, three waiting at full strength. The lead says how much is left and that the card goes when it is done |
| Fresh cluster, reader | **No wizard at all.** The seed's role gate holds, and the desk carries Ask alone |
| Half set up, owner | The passkey ticked, inference open with its three doors, the chosen door's sentence and `Open Fleet`; Storage and Email sender ticked below. Nothing scrolled |
| Half set up, developer | Identical, which is the point of D4: Fleet's Machines section has no role floor, so the fleet door is offered in full. The federation doors' words are unit-tested, not capturable without a click |
| Storage open, owner | "Set in the deployment, not from here." and the two variables that are still missing, in mono. The one already set is not named, because it is not work |
| Configured cluster, owner | **Nothing.** No card, no frame, no flash -- and the Ask widget beside it has its input rather than its setup sentence, which is the same feed answering two surfaces |
| Both modes | Every state resolves in the light set; the accent ring, the ticked marks and the chosen pill all keep their contrast, and no hardcoded dark value leaked |

## What was NOT changed

**The card keeps its slack on short states.** With only the storage stop open,
roughly a third of the four-cell card is empty. A widget cannot resize itself
without moving every icon around it on the desk, so it is sized for its
tallest stop -- the Ask widget's own idle state has the same slack, and this
is the shell's normal rather than an oversight here.

**The two widgets do not share a width.** Ask is three cells and this is four,
so their left edges are ragged where their right edges align. Matching them
would cost the doors a cell they need. Noted so the next sweep reads it as a
decision.
