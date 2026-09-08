import { capabilityScriptPath, runCapabilityScript, type RunScript } from "../install/runner.js";

// THE FOURTH PRESENCE SIGNAL: what k3d says is on this machine (memql#5118, D8).
//
// ===========================================================================
// A MODULE OF ITS OWN, BECAUSE presence.ts MUST NOT SHELL OUT
// ===========================================================================
// `clusters/presence.ts` states as one of its three load-bearing properties
// that rendering a menu must not require Docker, must not shell out, and must
// not wait on `k3d cluster list`. That property still holds: presence takes
// this as an INJECTED function and asks it on exactly one path -- the one that
// would otherwise offer to install over a cluster somebody already had.
//
// Keeping the spawn here rather than inside presence is what makes that
// checkable: every test in `clusterPresence.test.ts` runs with no listing or a
// fake one, and none of them can accidentally reach a real k3d.
//
// ===========================================================================
// THROUGH THE DETECT CAPABILITY, NOT `k3d` DIRECTLY
// ===========================================================================
// `install.detect` is already the installer's read-only inventory of this
// machine, it already refuses an unsupported platform, and it already answers
// in the capability envelope every other caller here parses. Spawning `k3d`
// from TypeScript would be a second way to ask the same machine the same
// question -- with its own PATH assumptions, its own output parsing and its
// own failure modes -- for a signal that only has to be right about one name.

/** How long the listing may take before the answer is "we could not ask". */
export const LISTING_TIMEOUT_MS = 4_000;

export interface ListClustersOptions {
  /** The repository root the capability scripts live under. */
  root: string;
  /** Injectable for tests; the real one spawns the script. */
  run?: RunScript;
  timeoutMs?: number;
}

/**
 * The k3d cluster names on this machine, or an empty list.
 *
 * NEVER REJECTS, and every failure is the empty list: k3d may not be
 * installed, Docker may be down, the envelope may be unparseable, the platform
 * may be refused. None of those is evidence of a cluster, and the caller reads
 * an empty list as "install is still safe to offer" -- which is the direction
 * that cannot destroy anything.
 */
export async function listK3dClusters(opts: ListClustersOptions): Promise<string[]> {
  const run = opts.run ?? runCapabilityScript;
  try {
    const outcome = await run({
      scriptPath: capabilityScriptPath("install.detect", opts.root),
      params: {},
      capability: "install.detect",
      cwd: opts.root,
      env: process.env,
      timeoutMs: opts.timeoutMs ?? LISTING_TIMEOUT_MS,
    });
    return namesFrom(outcome.envelope?.result);
  } catch {
    return [];
  }
}

/**
 * `result.clusters` from detect's envelope, defensively.
 *
 * The envelope is JSON a script produced, so `result` can be anything at all.
 * Anything that is not an array of non-empty strings contributes nothing --
 * and a malformed answer therefore reads as "no cluster", not as a cluster
 * named `[object Object]`.
 */
function namesFrom(result: unknown): string[] {
  if (result === null || typeof result !== "object") return [];
  const raw = (result as Record<string, unknown>).clusters;
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((n): n is string => typeof n === "string")
    .map((n) => n.trim())
    .filter((n) => n !== "");
}
