import { roleHolds, type GrantRow } from "./rows";

// The verb and resource grid: what a role holds, drawn as a table.
//
// ===========================================================================
// THE VOCABULARY IS PINNED TO THE SEEDS
// ===========================================================================
// `ROLE_GRID_VOCABULARY` is written as ONE LINE on purpose:
// component/memql's role_grid_os_parity_test.go reads it out of this file with
// a regexp and holds it equal to the (verb, resourceType) pairs
// dsl/rbac/seeds.memql names -- the way OFFERED_KINDS is pinned to
// v1:platform:site.kind. A pair the OS offers that no seed names is a checkbox
// nothing can store: the write succeeds, the row lands, and the resolver never
// asks that question, so the person sees a permission that does nothing.
//
// The drift is invisible in both directions, which is why it is gated rather
// than reviewed: a resource kind the seeds grow and the grid does not offer is
// a permission nobody can grant from the shell.
//
// The form is `<resource>:<verb>,<verb>|<resource>:...`, resources in the
// order they are drawn down the grid and verbs in the order they are drawn
// across it.
export const ROLE_GRID_VOCABULARY = "principal:read,create,update,delete|role:read,create,update,delete|group:read,create,update|admission:create|data:read,create,update,delete|construct:read,create,update,delete,execute|agent:read,create|deployment:execute";

/**
 * The five verbs, in the order they are drawn, with the words a person reads.
 *
 * `update` is "Edit" and `execute` is "Run" because those are what somebody
 * looking at a permissions table says. The VALUE stays the engine's verb --
 * relabelling the column and renaming the value are different things, and only
 * the first one is a display decision.
 */
export const GRID_VERBS = [
  { verb: "read", label: "Read" },
  { verb: "create", label: "Create" },
  { verb: "update", label: "Edit" },
  { verb: "delete", label: "Delete" },
  { verb: "execute", label: "Run" },
] as const;

export type GridVerb = (typeof GRID_VERBS)[number]["verb"];

/** One row of the grid: a resource kind and the verbs that exist for it. */
export interface GridResource {
  /** The engine's `resourceType`. */
  resource: string;
  /** What it is called, in the reader's words. */
  label: string;
  /** The verbs any seeded role names for it. Everything else is a dash. */
  verbs: readonly string[];
}

/**
 * What each resource kind IS, in a person's words.
 *
 * Kept beside the vocabulary rather than in it, because the parity test
 * compares the pairs and has nothing to say about the copy. A kind with no
 * entry falls back to its own slug, which is honest for a product layer's own
 * resource kind arriving from a bundle this build has never heard of.
 */
const RESOURCE_LABELS: Record<string, string> = {
  principal: "People",
  role: "Roles",
  group: "Groups",
  admission: "Invitations",
  data: "Data",
  construct: "Constructs",
  agent: "Agents",
  deployment: "Deploys",
};

/** The grid's rows, parsed from the pinned vocabulary. */
export function gridResources(): GridResource[] {
  return ROLE_GRID_VOCABULARY.split("|").map((entry) => {
    const [resource = "", verbs = ""] = entry.split(":");
    return {
      resource,
      label: RESOURCE_LABELS[resource] ?? resource,
      verbs: verbs.split(",").filter((v) => v !== ""),
    };
  });
}

/** Every (resource, verb) pair the grid offers, as `resource:verb`. */
export function gridPairs(): string[] {
  return gridResources()
    .flatMap((row) => row.verbs.map((verb) => `${row.resource}:${verb}`))
    .sort();
}

/** The same set, built once: `cellState` is called for every cell of the grid. */
const GRID_PAIRS: ReadonlySet<string> = new Set(gridPairs());

/**
 * What one cell is.
 *
 * `absent` is a pair nothing gates -- drawn as a dash, never as an empty
 * checkbox, because an empty checkbox says "off" and the truth is "this
 * question is not asked".
 *
 * `lockedOn` is the one worth being exact about: the role being drawn holds a
 * permission the CALLER does not. It is SHOWN and cannot be toggled, because
 * hiding it would misreport what the role holds, and offering it would let
 * somebody hand on an authority they do not have -- which is the guard
 * `roleUpdate` applies server-side (every grant one the creator holds).
 *
 * A predefined role's whole grid is locked, in both values: it is the seeds'
 * row and immutable at runtime by the engine's own guard, so an editable
 * control on it would be one whose every write is refused.
 */
export type CellState = "absent" | "on" | "off" | "lockedOn" | "lockedOff";

export function cellState({
  resource,
  verb,
  held,
  callerHolds,
  editable,
}: {
  resource: string;
  verb: string;
  /** Whether the role being drawn holds this pair. */
  held: boolean;
  /** Whether the CALLER holds it, and may therefore grant it. */
  callerHolds: boolean;
  /** Whether this grid is editable at all: a custom role, and update on role. */
  editable: boolean;
}): CellState {
  if (!GRID_PAIRS.has(`${resource}:${verb}`)) return "absent";
  if (!editable) return held ? "lockedOn" : "lockedOff";
  if (held && !callerHolds) return "lockedOn";
  if (!callerHolds) return "lockedOff";
  return held ? "on" : "off";
}

/** Whether a cell state draws a tick. */
export function cellIsHeld(state: CellState): boolean {
  return state === "on" || state === "lockedOn";
}

/** Why a cell cannot be toggled, as a title, or "" when it can. */
export function cellLockReason(state: CellState, editable: boolean): string {
  if (state !== "lockedOn" && state !== "lockedOff") return "";
  if (!editable) return "A predefined role's permissions are the cluster's, not this window's.";
  return state === "lockedOn"
    ? "This role holds it and you do not, so you cannot hand it on or take it away."
    : "You do not hold this permission, so you cannot grant it.";
}

/** The pairs a role holds, as `resource:verb`, for a grid or a diff. */
export function heldPairs(grants: readonly GrantRow[], slug: string): string[] {
  return gridPairs().filter((pair) => {
    const [resource = "", verb = ""] = pair.split(":");
    return roleHolds(grants, slug, verb, resource);
  });
}
