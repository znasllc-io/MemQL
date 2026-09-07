// The readiness fold, restated for the shell (design record
// 2026-09-06-configuration-readiness, section 4.5). component/readiness is
// the source; this is the copy the live feed runs, and the shared fixtures
// under component/readiness/testdata/fold hold the two equal -- a case added
// on either side is a case both must pass.
//
// THE LITERAL IS PARSED. component/readiness/os_parity_test.go extracts
// NODE_LIVE_WINDOW_SECONDS by regexp and fails the build when it disagrees
// with NodeLiveWindow. Keep it a plain numeric literal.
export const NODE_LIVE_WINDOW_SECONDS = 60;

export type ReadinessState =
  | "configured"
  | "partial"
  | "unconfigured"
  | "notApplicable"
  | "unreported";

export interface SlotReport {
  name: string;
  present: boolean;
  source: string;
  optional?: boolean;
}

export interface LaneReport {
  name: string;
  configurableFrom: string;
  complete: boolean;
  slots: SlotReport[];
}

export interface NodeReport {
  module: string;
  nodeId: string;
  nodeType: string;
  state: ReadinessState;
  core?: boolean;
  lanes?: LaneReport[];
  reportedAt: string;
}

export interface NodeLiveness {
  nodeId: string;
  health: string;
  lastSeen: string;
}

export interface NodeVerdict {
  nodeId: string;
  nodeType: string;
  state: ReadinessState;
  reportedAt: string;
}

export interface Verdict {
  module: string;
  state: ReadinessState;
  core: boolean;
  disagreement: string[];
  nodes: NodeVerdict[];
  /**
   * The worst live reporter's lanes, for the Set up group; empty when
   * unreported. Not part of the Go Verdict, which the group does not read --
   * the shell needs the slot names to say what to set, and the engine's
   * consumer of the fold does not.
   */
  lanes: LaneReport[];
}

const LIVE_HEALTH = new Set(["healthy", "connecting", "degraded", "draining"]);

/**
 * Whether a node's report may count: a live health word and a heartbeat
 * inside the window. A missing or unparseable lastSeen is never live --
 * "we do not know when this node was last seen" is not evidence that it is up.
 */
export function nodeIsLive(n: NodeLiveness, now: Date): boolean {
  if (!LIVE_HEALTH.has(n.health)) return false;
  if (!n.lastSeen) return false;
  const seen = Date.parse(n.lastSeen);
  if (Number.isNaN(seen)) return false;
  return now.getTime() - seen <= NODE_LIVE_WINDOW_SECONDS * 1000;
}

function rank(s: ReadinessState): number {
  if (s === "unconfigured") return 2;
  if (s === "partial") return 1;
  return 0;
}

/**
 * Every node's report to one verdict per module:
 *
 *  1. Keep reports from live nodes only, so a dead replica's stale row cannot
 *     pin a verdict.
 *  2. Drop notApplicable.
 *  3. Worst state wins.
 *  4. If the kept reports disagree, the verdict is partial and disagreement
 *     names every live reporter as nodeId=state, worst first.
 *  5. A module with no kept report is unreported -- never unconfigured. Not
 *     knowing and not being set up are different answers, and only one of
 *     them should send somebody to a form.
 *
 * Modules come back in name order, so two folds over the same rows are equal.
 */
export function foldReadiness(reports: NodeReport[], nodes: NodeLiveness[], now: Date): Verdict[] {
  const live = new Set(nodes.filter((n) => nodeIsLive(n, now)).map((n) => n.nodeId));
  const kept = new Map<string, NodeReport[]>();
  const core = new Map<string, boolean>();
  const order: string[] = [];
  for (const r of reports) {
    if (!core.has(r.module)) {
      order.push(r.module);
      core.set(r.module, false);
    }
    if (r.core) core.set(r.module, true);
    if (!live.has(r.nodeId) || r.state === "notApplicable") continue;
    const list = kept.get(r.module) ?? [];
    list.push(r);
    kept.set(r.module, list);
  }
  order.sort();
  const out: Verdict[] = [];
  for (const module of order) {
    const rs = kept.get(module) ?? [];
    if (rs.length === 0) {
      out.push({
        module,
        state: "unreported",
        core: core.get(module) ?? false,
        disagreement: [],
        nodes: [],
        lanes: [],
      });
      continue;
    }
    rs.sort(
      (a, b) => rank(b.state) - rank(a.state) || (a.nodeId < b.nodeId ? -1 : a.nodeId > b.nodeId ? 1 : 0),
    );
    const states = new Set(rs.map((r) => r.state));
    out.push({
      module,
      state: states.size > 1 ? "partial" : rs[0]!.state,
      core: core.get(module) ?? false,
      disagreement: states.size > 1 ? rs.map((r) => `${r.nodeId}=${r.state}`) : [],
      nodes: rs.map((r) => ({
        nodeId: r.nodeId,
        nodeType: r.nodeType,
        state: r.state,
        reportedAt: r.reportedAt,
      })),
      lanes: rs[0]!.lanes ?? [],
    });
  }
  return out;
}
