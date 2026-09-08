import { useCallback, useEffect, useState } from "react";
import { Concepts, type Row } from "@znasllc-io/memql-sdk-core/client";

import { useOsConnection } from "../../live/connection";
import { grantFromRow, roleFromRow, type GrantRow, type RoleRow } from "./rows";

// The role catalog and its grants: ONE on-demand read of each, re-read when
// either concept broadcasts (design record, D5).
//
// ===========================================================================
// WHY A READ AND A SUBSCRIPTION RATHER THAN A LIVE COLLECTION
// ===========================================================================
// The two catalogs are small registries consumed whole -- `activeRoles` and
// `activeCapabilities` both declare `@unbounded` on exactly that grounds -- and
// the app needs them JOINED: the grid is a function of both, so a row arriving
// on one feed while the other is unchanged has nothing useful to say on its
// own. A pair of LiveCollections would give two snapshots to reconcile at
// every render for no arrival cue anybody wants: nothing in this app announces
// "a capability arrived".
//
// What it does need is not to go stale while somebody is editing a role in
// another window, which is what the subscription is for. It carries no payload
// into the state -- it invalidates, and the read is the authority. That is the
// same shape `desktopGateway` uses, and it is why a `payload_omitted` event
// (a row this caller may not read) is handled correctly for free: the re-read
// runs under their own authority and simply returns what they may have.

export interface RoleCatalog {
  roles: RoleRow[];
  grants: GrantRow[];
  /** "loading" until the first read settles; "error" leaves the last rows up. */
  state: "loading" | "ready" | "error";
  /** The server's own sentence, verbatim. */
  error: string;
  reload: () => void;
}

export function useRoleCatalog(): RoleCatalog {
  const connection = useOsConnection();
  const [roles, setRoles] = useState<RoleRow[]>([]);
  const [grants, setGrants] = useState<GrantRow[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState("");
  const [nonce, setNonce] = useState(0);

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  useEffect(() => {
    const query = connection?.query ?? null;
    if (query === null) return;
    const controller = new AbortController();
    let live = true;
    setState((held) => (held === "ready" ? held : "loading"));

    void (async () => {
      try {
        // TOGETHER, deliberately: the grid is a function of both, and settling
        // them independently would draw a ladder with every cell dashed for
        // however long the second read took -- which is a confident picture of
        // a role holding nothing.
        const [roleResult, grantResult] = await Promise.all([
          query.activeRoles({}, { signal: controller.signal }),
          query.activeCapabilities({}, { signal: controller.signal }),
        ]);
        if (!live) return;
        setRoles((roleResult.rows() as Row[]).map(roleFromRow).filter((r) => r.slug !== ""));
        setGrants((grantResult.rows() as Row[]).map(grantFromRow).filter((g) => g.roleSlug !== ""));
        setState("ready");
        setError("");
      } catch (err: unknown) {
        if (!live) return;
        // THE ROWS ALREADY HELD STAY UP. A refused re-read after a successful
        // first one means the catalog on screen is the last known answer, and
        // emptying it would redraw every role page as a role that holds
        // nothing.
        setState("error");
        setError(err instanceof Error ? err.message : String(err));
      }
    })();

    return () => {
      live = false;
      controller.abort();
    };
  }, [connection, nonce]);

  // Re-read on either concept's broadcast. Both carry created and updated to
  // browser subscribers (component/node/routing.go).
  useEffect(() => {
    const subscriptions = connection?.subscriptions ?? null;
    if (subscriptions === null) return;
    const offs = [Concepts.RBAC_ROLE, Concepts.RBAC_CAPABILITY].map((concept) =>
      subscriptions.subscribeGraph(() => reload(), { concept }),
    );
    return () => {
      for (const off of offs) off();
    };
  }, [connection, reload]);

  return { roles, grants, state, error, reload };
}

/**
 * The ladder, strongest rung first -- the order the Roles list draws.
 *
 * Rank descending because that is how a ladder is read when the question is
 * "who is above whom", and it puts the rung a reader is most likely looking
 * for -- the one they cannot assign -- at the top rather than at the bottom of
 * a scroll.
 */
export function ladderDescending(roles: readonly RoleRow[]): RoleRow[] {
  return [...roles].sort((a, b) => b.rank - a.rank || a.slug.localeCompare(b.slug));
}

/** How many people hold a role, from the roster this app already has. */
export function holderCount(people: readonly { role: string }[], role: RoleRow): number {
  return people.filter((p) => p.role === role.slug || role.aliases.includes(p.role)).length;
}
