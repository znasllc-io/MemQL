import { createContext, useContext } from "react";

import type { Readiness } from "../../live/readiness";
import type { SetupStop } from "./stops";

// What the gate has already worked out, handed to the body beneath it.
//
// The gate reads the feed and the passkeys to decide whether the widget is
// drawn AT ALL; the body then needs the same answer to draw it. Passing it
// down rather than reading twice is not an optimisation -- `passkeysForSelf`
// is an on-demand read, and a second hook would open a second read of it on
// every boot and let the two disagree.

export interface SetupFacts {
  stops: SetupStop[];
  readiness: Readiness | undefined;
  identityUrl: string;
  role: string;
}

const Ctx = createContext<SetupFacts | null>(null);

export const SetupFactsProvider = Ctx.Provider;

/** Null outside the gate, which is what a widget body rendered alone gets. */
export function useSetupFacts(): SetupFacts | null {
  return useContext(Ctx);
}
