// COMING BACK FROM THE IDENTITY SERVICE (design record
// 2026-09-06-first-run-wizard, D6 and section 5).
//
// ===========================================================================
// ONE ACT LEAVES THIS ORIGIN, AND ONLY THAT ONE PARKS A MARKER
// ===========================================================================
// Three of the wizard's acts open a WINDOW over the desk -- Fleet, Settings,
// Integrations -- and none of them parks anything. The desk is still there
// underneath, the stop follows the readiness feed live, and pulling somebody
// out of Fleet the moment their machine registers is exactly the ambush the
// Accounts first-run covenant forbids.
//
// The passkey act is different in kind: the ceremony lives on the identity
// origin (`/me/devices`), the OS runs no WebAuthn of its own, and following
// that link REPLACES this document. So the marker is parked before the
// browser leaves, survives the round trip, and is spent by the dispatcher on
// the next boot -- the `ConnectReturnDispatcher` shape, for the same reason:
// the surface that asked does not exist at the moment the answer arrives.
//
// SESSION STORAGE, NOT A MODULE VARIABLE. `connectReturn.ts` can park in
// memory because the marker arrives in the URL of the very load that reads
// it; here the marker is written by the load that LEAVES and read by the one
// that comes back, so it has to outlive a navigation. It must not outlive
// the tab: a marker in localStorage would send somebody to the desk a week
// later for a passkey they registered on Tuesday.

const KEY = "memql.os.setup.return";

/** What was parked: the stop somebody left from. */
export interface SetupReturn {
  stopId: string;
}

function storage(): Storage | null {
  try {
    return globalThis.sessionStorage ?? null;
  } catch {
    // A browser configured to refuse site data throws on the ACCESSOR. The
    // wizard works without a marker -- the person simply lands wherever the
    // shell restores them -- so this is a degradation, never a failure.
    return null;
  }
}

export function parkSetupReturn(stopId: string): void {
  try {
    storage()?.setItem(KEY, stopId);
  } catch {
    /* quota, private mode: see above */
  }
}

/**
 * Take the parked return, once.
 *
 * ONCE is the whole contract: the effect that consumes this runs again on a
 * StrictMode remount, and a value that survived would move somebody's desk
 * under them on a later render.
 */
export function takeSetupReturn(): SetupReturn | null {
  const store = storage();
  if (store === null) return null;
  let held: string | null = null;
  try {
    held = store.getItem(KEY);
    store.removeItem(KEY);
  } catch {
    return null;
  }
  return held === null || held === "" ? null : { stopId: held };
}

/** Tests only: forget anything parked, so one case cannot leak into the next. */
export function clearSetupReturn(): void {
  try {
    storage()?.removeItem(KEY);
  } catch {
    /* see above */
  }
}

// ---------------------------------------------------------------------------
// The one act that leaves
// ---------------------------------------------------------------------------

/**
 * The identity service's Devices page for this cluster, or "" when the OS
 * does not know its identity origin.
 *
 * The empty answer is not a fallback to a guessed URL. A shell that has not
 * loaded its runtime config cannot know where the identity service lives,
 * and composing one from the current host would send somebody to a page that
 * does not exist -- so the stop says where to look instead.
 */
export function devicesUrl(identityUrl: string): string {
  return identityUrl === "" ? "" : `${identityUrl.replace(/\/$/, "")}/me/devices`;
}

/**
 * Park the return and hand the browser to the identity origin.
 *
 * SAME TAB, deliberately. A first-run flow that spawns a tab leaves somebody
 * to find their way back among several, and the marker is what makes coming
 * back land on the desk they left. False when there is nowhere to send them,
 * so the caller can say so in words rather than offering a dead control.
 */
export function leaveForPasskey(identityUrl: string, win: Window): boolean {
  const url = devicesUrl(identityUrl);
  if (url === "") return false;
  parkSetupReturn("passkey");
  win.location.assign(url);
  return true;
}
