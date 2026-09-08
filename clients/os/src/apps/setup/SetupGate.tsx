import { useEffect, useRef, useState, type ReactNode } from "react";

import { useSetupFacts } from "./context";

// WHETHER THE WIZARD DRAWS ONCE IT IS ON THE DESK (design record
// 2026-09-06-first-run-wizard, D1).
//
// ===========================================================================
// ABOVE THE FRAME, AND THAT IS THE WHOLE REASON IT EXISTS
// ===========================================================================
// A widget whose BODY returns null still draws a header, a name and an
// overflow menu. On a cluster that was set up months ago that is an empty card
// on the desk for one frame, or forever -- and "a configured cluster never
// flashes a wizard" is the requirement this surface was written against. So
// the decision sits above `WidgetFrame`, through the manifest's `gate` seam,
// and a cluster with nothing to set up draws NOTHING.
//
// ===========================================================================
// IT NO LONGER DECIDES WHETHER THE WIDGET IS ON A DESK
// ===========================================================================
// That question moved to SetupPresence (epic memql#5118, D4), which has to be
// able to answer it while this component is unmounted -- which is exactly the
// case where the widget is absent. What is left here is the half that can only
// be decided from inside: whether the card that IS on the desk draws, and the
// beat it holds before it goes.
//
// The reading itself comes from SetupFactsScope now, so this surface, the core
// gate and the presence effect cannot disagree about one person's passkeys.
//
// ===========================================================================
// HOW IT LEAVES DEPENDS ON WHETHER IT WAS EVER NEEDED
// ===========================================================================
// The readiness verdicts ARE the state (D1): nothing is dismissed, nothing is
// remembered in a browser, and there is no Skip -- there would be nothing to
// remember.
//
// A cluster that was already configured when the shell booted gets NO exit at
// all: it never rendered, so there is nothing to animate away, and playing a
// fold-up of a card nobody saw is the flash this file exists to prevent.
// Somebody who FINISHED the last stop gets one beat with every mark lit before
// it folds -- the interface's answer to "did that work", and the only reward
// this surface offers, because a congratulation screen is a screen you have to
// dismiss.

/** How long a finished rail stays on screen before folding away. */
export const EXIT_HOLD_MS = 1200;

export function SetupGate({ retire, children }: { retire: () => void; children: ReactNode }) {
  const facts = useSetupFacts();
  // NULL IS "NOT KNOWN", never "configured". A widget mounted outside the
  // scope -- a preview, a test harness that forgot the provider -- draws
  // nothing rather than retiring itself off somebody's desk.
  const configured = facts?.configured ?? false;
  const draw = facts !== null && facts.known && !configured;

  // Whether this widget was ever actually on screen. A ref rather than state:
  // it changes exactly once, and re-rendering the moment it does would be a
  // render that draws the same thing.
  const wasDrawn = useRef(false);
  if (draw) wasDrawn.current = true;

  const [exiting, setExiting] = useState(false);
  const retireRef = useRef(retire);
  retireRef.current = retire;

  useEffect(() => {
    // A MODULE CAN GO BACK. If the core stops being configured mid-beat -- a
    // replica restarts, a lane empties -- the timer is cleared by this
    // effect's own cleanup, and without this line `exiting` would stay true
    // and leave a faded, unclickable card on the desk with nothing left to
    // remove it.
    if (!configured) {
      setExiting(false);
      return;
    }
    if (!wasDrawn.current) {
      retireRef.current();
      return;
    }
    setExiting(true);
    const timer = setTimeout(() => retireRef.current(), EXIT_HOLD_MS);
    return () => clearTimeout(timer);
  }, [configured]);

  if (!draw && !exiting) return null;

  return (
    <div className="os-setup-gate" data-exiting={exiting ? "true" : undefined}>
      {children}
    </div>
  );
}
