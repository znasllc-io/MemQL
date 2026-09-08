import { getRowByConceptAndId, type Row } from "@znasllc-io/memql-sdk-core/client";

import { useLiveCollection, type LiveCollectionHandle } from "../../live/useLiveCollection";
import { ACCOUNT_FRONT_DOOR_CONCEPT } from "./frontDoor";

// The account front-door feed: ONE LiveCollection over this cluster's doors.
//
// ===========================================================================
// LIVE IS THE WHOLE POINT OF THIS SURFACE
// ===========================================================================
// `component/node/routing.go` carries broadcast rules for both
// `graph.node.created.v1:platform:accountFrontDoor` and its `updated` sibling,
// and this stop is one of the two reasons they are there. The rows are written
// by the reconciliation sweep -- on whichever replica the automations cron
// leader elected -- and read on the bff-served window an operator is watching
// WHILE THEY CREATE THREE CNAMEs. Default-deny would leave the stop correct on
// load and frozen afterwards, which is the failure that looks like it is
// working: a person pastes three records, watches nothing happen for ten
// minutes, and concludes the records are wrong.
//
// Read the ROUTING RULES before deciding a concept's feed is live. The rule
// for this concept is in the same block as the custom-domain pair and cites
// this file.
//
// ===========================================================================
// THE KEY IS A CONSTANT, AND THE ACCOUNT IS NOT IN IT
// ===========================================================================
// A key must encode everything that changes what is READ and nothing that
// merely narrows what is shown. `accountFrontDoorsOpen` takes no arguments --
// the concept's clusterOwner tier decides how far "open" reaches -- so opening
// a different account changes no read at all, and folding the account id in
// would tear down the subscription and re-seed from empty every time somebody
// clicked another account.
//
// The stop narrows by account in the view, which is the deliberate re-baseline
// the arrival cue's own rule asks for: a filter change is not the cluster
// sending rows, and animating it would claim news that did not arrive.

/**
 * Every account front door this caller may read, live.
 *
 * NO ARGUMENTS. `accountFrontDoorsOpen` excludes only `removed` -- the one
 * terminal state -- which is deliberate here for the reason the query's own
 * doc gives: a seed narrowed further would give the browser a list that grows
 * a `live` row the first time one is touched and never has one on load.
 *
 * A REMOVED DOOR IS NOT IN THIS FEED, and the stop's `currentDoor` falls back
 * to the newest row it can see. On a cluster where a client's door was torn
 * down, that means the stop says "not held" with the reservation's own reason
 * rather than "we served this and stopped" -- which is the honest reading of a
 * withdrawn reservation, since the reservation is what was withdrawn.
 */
export function useAccountFrontDoors(): LiveCollectionHandle<Row> {
  return useLiveCollection<Row>("accounts:frontDoors", (connection) => ({
    concept: ACCOUNT_FRONT_DOOR_CONCEPT,
    seed: async (_cursor, signal) => {
      const result = await connection.query.accountFrontDoorsOpen({}, { signal });
      return { rows: result.rows(), nextCursor: "" };
    },
    // The re-read a `payload_omitted` event lands on, and the collection's gap
    // recovery. The concept is clusterOwner rather than granted, so the
    // omitted-payload path is not expected here -- it is wired because the
    // fold uses the same seam either way.
    reread: async (rowId, signal) => {
      const row = await getRowByConceptAndId(connection.query, ACCOUNT_FRONT_DOOR_CONCEPT, rowId, {
        signal,
      });
      return (row as Row) ?? null;
    },
    paged: false,
  }));
}
