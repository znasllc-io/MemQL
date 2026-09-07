import { useEffect } from "react";

import { useOs } from "../../chrome/state";
import { takeSetupReturn } from "./returns";

// Brings somebody back to the desk they left from (epic memql#5106).
//
// ===========================================================================
// IT RENDERS NOTHING, AND IT OPENS NOTHING
// ===========================================================================
// A person who followed the passkey stop's link left this origin; coming back
// reloads the shell, which restores whichever desk they were last on. That is
// USUALLY the desk holding the setup widget and occasionally is not, so the
// whole act is: switch to the desk that holds the widget, and spend the
// marker.
//
// A return with NO WIDGET TO RETURN TO -- they registered the passkey, came
// back, and the core had finished configuring while they were away -- opens
// nothing and clears the marker, which is the design record's own failure
// mode. Landing them on a desk to look at a widget that has just retired
// would be a worse answer than landing them where they were.
//
// It lives inside `OsProvider` because switching desks is a shell act, and in
// the SETUP tree because knowing which widget matters is this app's knowledge
// and not the shell's.

export function SetupReturnDispatcher() {
  const { actions, state } = useOs();
  useEffect(() => {
    // TAKE, not read: the parked value is consumed here and this effect is
    // free to run again -- a StrictMode remount does -- and every later run
    // correctly finds nothing.
    const held = takeSetupReturn();
    if (held === null) return;
    const desk = state.shell.desks.find((d) =>
      Object.values(state.surfaces[d.id]?.items ?? {}).some(
        (item) => item.kind === "widget" && item.widgetId === "setup",
      ),
    );
    if (!desk || desk.id === state.shell.activeDeskId) return;
    actions.switchDesk(desk.id);
    // The desks and surfaces are read ONCE, at mount, and are deliberately
    // not dependencies: this consumes a marker from a previous page load, and
    // re-running it as the document settles would be a second consume of a
    // value that is already gone.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [actions]);
  return null;
}
