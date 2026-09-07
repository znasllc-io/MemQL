import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  foldReadiness,
  nodeIsLive,
  NODE_LIVE_WINDOW_SECONDS,
  type NodeLiveness,
  type NodeReport,
} from "../../src/system/readinessFold";

// THE SAME FIXTURES THE ENGINE RUNS. component/readiness/fold_test.go reads
// this directory too; a case one side passes and the other fails is the
// drift this test exists to catch. The directory is resolved from this file,
// so it does not depend on the working directory vitest was started from.
const FIXTURES = resolve(__dirname, "../../../../component/readiness/testdata/fold");

interface Fixture {
  name: string;
  now: string;
  reports: NodeReport[];
  nodes: NodeLiveness[];
  expect: { module: string; state: string; disagreement: string[] }[];
}

describe("the fold mirrors component/readiness", () => {
  const files = readdirSync(FIXTURES)
    .filter((f) => f.endsWith(".json"))
    .sort();

  // Without this, a mistyped path reads as a suite with no cases and passes.
  it("finds the shared fixtures", () => {
    expect(files.length).toBeGreaterThanOrEqual(7);
  });

  for (const file of files) {
    const fx = JSON.parse(readFileSync(resolve(FIXTURES, file), "utf8")) as Fixture;
    it(fx.name, () => {
      const got = foldReadiness(fx.reports, fx.nodes, new Date(fx.now));
      expect(
        got.map((v) => ({ module: v.module, state: v.state, disagreement: v.disagreement })),
      ).toEqual(fx.expect);
    });
  }

  it("nodeIsLive needs a live health word and a recent heartbeat", () => {
    const now = new Date("2026-09-06T12:00:00Z");
    const fresh = new Date(now.getTime() - (NODE_LIVE_WINDOW_SECONDS / 2) * 1000).toISOString();
    const stale = new Date(now.getTime() - (NODE_LIVE_WINDOW_SECONDS + 1) * 1000).toISOString();
    expect(nodeIsLive({ nodeId: "a", health: "healthy", lastSeen: fresh }, now)).toBe(true);
    expect(nodeIsLive({ nodeId: "a", health: "stopped", lastSeen: fresh }, now)).toBe(false);
    expect(nodeIsLive({ nodeId: "a", health: "healthy", lastSeen: stale }, now)).toBe(false);
    expect(nodeIsLive({ nodeId: "a", health: "healthy", lastSeen: "" }, now)).toBe(false);
    expect(nodeIsLive({ nodeId: "a", health: "healthy", lastSeen: "not a date" }, now)).toBe(false);
  });

  // Every live health word counts, and the set is the one the Go side holds.
  // A node mid-rollout is `draining` and still reporting; dropping it would
  // make a rolling restart look like a cluster that forgot its configuration.
  it("counts every live health word", () => {
    const now = new Date("2026-09-06T12:00:00Z");
    const seen = new Date(now.getTime() - 1000).toISOString();
    for (const health of ["healthy", "connecting", "degraded", "draining"]) {
      expect(nodeIsLive({ nodeId: "a", health, lastSeen: seen }, now)).toBe(true);
    }
    for (const health of ["stopped", "", "HEALTHY"]) {
      expect(nodeIsLive({ nodeId: "a", health, lastSeen: seen }, now)).toBe(false);
    }
  });
});
