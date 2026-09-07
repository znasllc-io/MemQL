import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { Row } from "@znasllc-io/memql-sdk-core/client";

import { boolOr } from "../../kit";
import { useSession } from "../../chrome/access";
import { useReading } from "../../cluster/reading";
import { useOsConnection } from "../../live/connection";
import { SetupFactsProvider } from "./context";
import { coreIsConfigured, stopsAreKnown, stopsFor, type PasskeyReading } from "./stops";

// WHETHER THE WIZARD IS ON THE DESK AT ALL (design record
// 2026-09-06-first-run-wizard, D1).
//
// ===========================================================================
// ABOVE THE FRAME, AND THAT IS THE WHOLE REASON IT EXISTS
// ===========================================================================
// A widget whose BODY returns null still draws a header, a name and an
// overflow menu. On a cluster that was set up months ago that is an empty
// card on the desk for one frame, or forever -- and "a configured cluster
// never flashes a wizard" is the requirement this surface was written
// against. So the decision sits above `WidgetFrame`, through the manifest's
// `gate` seam, and a cluster with nothing to set up draws NOTHING.
//
// ===========================================================================
// IT RETIRES ITSELF, AND HOW IT LEAVES DEPENDS ON WHETHER IT WAS EVER NEEDED
// ===========================================================================
// The readiness verdicts ARE the state (D1): nothing is dismissed, nothing is
// remembered in a browser, and there is no Skip -- there would be nothing to
// remember. So the widget takes itself off the desk once every core stop is
// settled, and comes back on a fresh desk, which is the honest answer to
// "where did it go".
//
// A cluster that was already configured when the shell booted gets NO exit at
// all: it never rendered, so there is nothing to animate away, and playing a
// fold-up of a card nobody saw is the flash this file exists to prevent.
// Somebody who FINISHED the last stop gets one beat with every mark lit
// before it folds -- the interface's answer to "did that work", and the only
// reward this surface offers, because a congratulation screen is a screen you
// have to dismiss.

/** How long a finished rail stays on screen before folding away. */
export const EXIT_HOLD_MS = 1200;

export function SetupGate({ retire, children }: { retire: () => void; children: ReactNode }) {
  const { access, config, readiness } = useSession();
  const connection = useOsConnection();
  const role = access?.clusterRole ?? "";

  // The role gate is the MANIFEST's (`roles: { any: ["owner", "developer"] }`)
  // and the desk enforces it, so this never re-states it. What it must not do
  // is ask the cluster about somebody's passkeys before there is a connection
  // to ask over.
  const readPasskeys = useCallback(
    async (signal: AbortSignal): Promise<Row[]> => {
      if (connection === null) throw new Error("not connected");
      const result = await connection.query.passkeysForSelf({}, { signal });
      return result.rows();
    },
    [connection],
  );
  // NOT ASKED AT ALL ON A CLUSTER WITH AUTHENTICATION OFF. The passkey stop
  // is `skipped` there and nothing reads the answer, so the read is one
  // request per boot that could not change a pixel.
  const passkeys = useReading<Row[]>(
    `setup:passkeys:${config.authEnabled ? "on" : "off"}`,
    connection === null || !config.authEnabled ? null : readPasskeys,
  );

  // A FAILED READ IS `unknown`, NEVER `none` (D6). `active` defaults TRUE on
  // the concept and a folded row carries only what a write touched, so it is
  // read through boolOr with that default rather than a bare truthiness test
  // that would count a revoked passkey as live.
  const reading: PasskeyReading =
    passkeys.value === null
      ? "unknown"
      : passkeys.value.filter((row) => boolOr(row, "active", true)).length > 0
        ? "held"
        : "none";

  const stops = useMemo(
    () => stopsFor({ readiness, passkeys: reading, authEnabled: config.authEnabled }),
    [readiness, reading, config.authEnabled],
  );
  const known = stopsAreKnown(stops);
  const configured = coreIsConfigured(stops);
  const draw = known && !configured;

  // Whether this widget was ever actually on screen. A ref rather than state:
  // it changes exactly once, and re-rendering the moment it does would be a
  // render that draws the same thing.
  const wasDrawn = useRef(false);
  if (draw) wasDrawn.current = true;

  const [exiting, setExiting] = useState(false);
  const retireRef = useRef(retire);
  retireRef.current = retire;

  useEffect(() => {
    // A MODULE CAN GO BACK. If the core stops being configured mid-beat --
    // a replica restarts, a lane empties -- the timer is cleared by this
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
    <SetupFactsProvider value={{ stops, readiness, identityUrl: config.identityUrl, role }}>
      <div className="os-setup-gate" data-exiting={exiting ? "true" : undefined}>
        {children}
      </div>
    </SetupFactsProvider>
  );
}
