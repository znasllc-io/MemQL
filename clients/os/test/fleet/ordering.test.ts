import { describe, expect, it } from "vitest";

import fixture from "../../src/apps/fleet/models/ordering.fixture.json";
import {
  eligibleFor,
  formatContext,
  formatParams,
  nextForTurn,
  orderModels,
  type RankedModel,
} from "../../src/apps/fleet/models/ordering";

// The client's half of the fleet ordering rule (epic memql#5096, design D5).
//
// The TABLE is shared with component/memql's fleet_ordering_parity_test.go, so
// a change to either implementation fails on the other's side. Drift here is
// invisible in the worst way: both sides keep working and this page simply
// names a different model from the one the router picks.

function model(over: Partial<RankedModel> & { modelId: string }): RankedModel {
  return {
    params: 0,
    contextWindow: 0,
    structuredOutput: false,
    embeddings: false,
    tools: false,
    online: true,
    ...over,
  };
}

describe("fleet model ordering", () => {
  it("declares cases -- a gate over nothing passes for the wrong reason", () => {
    expect(fixture.cases.length).toBeGreaterThan(0);
  });

  for (const tc of fixture.cases) {
    it(tc.name, () => {
      const models = tc.models.map((m) =>
        model({ modelId: m.modelId, params: m.params, contextWindow: m.contextWindow }),
      );
      expect(orderModels(models, tc.preference).map((m) => m.modelId)).toEqual(tc.want);
    });
  }

  it("does not mutate the array it was given", () => {
    const models = [model({ modelId: "b", params: 1 }), model({ modelId: "a", params: 2 })];
    const before = models.map((m) => m.modelId);
    orderModels(models, []);
    expect(models.map((m) => m.modelId)).toEqual(before);
  });
});

describe("eligibility", () => {
  const capable = model({
    modelId: "llama3.1:8b",
    params: 8_000_000_000,
    contextWindow: 8192,
    structuredOutput: true,
    embeddings: true,
    tools: true,
  });

  it("admits a model that advertises what the turn needs", () => {
    expect(eligibleFor(capable, {}).ok).toBe(true);
    expect(eligibleFor(capable, { structuredOutput: true }).ok).toBe(true);
    expect(eligibleFor(capable, { tools: true }).ok).toBe(true);
    expect(eligibleFor(capable, { embeddings: true }).ok).toBe(true);
  });

  it("refuses on the capability the model did not advertise, and says which", () => {
    const prose = model({ modelId: "tiny:1b", params: 1_000_000_000 });
    expect(eligibleFor(prose, { structuredOutput: true })).toEqual({
      ok: false,
      why: "does not advertise structured output",
    });
    expect(eligibleFor(prose, { tools: true }).why).toBe("does not advertise tool calling");
    expect(eligibleFor(prose, { embeddings: true }).why).toBe("does not advertise embeddings");
  });

  it("refuses an offline model before asking anything else", () => {
    const asleep = { ...capable, online: false };
    expect(eligibleFor(asleep, { tools: true })).toEqual({
      ok: false,
      why: "no machine offering it is online",
    });
  });
});

describe("the next model per kind of turn", () => {
  // A structured turn and an embedding turn legitimately resolve to DIFFERENT
  // models on one fleet, which is why the section prints four answers rather
  // than one.
  const fleet = [
    model({ modelId: "llama3.3:70b", params: 70_000_000_000, contextWindow: 8192, structuredOutput: true }),
    model({ modelId: "qwen2.5:7b", params: 7_000_000_000, contextWindow: 32768, structuredOutput: true, tools: true }),
    model({ modelId: "nomic-embed-text", params: 137_000_000, contextWindow: 8192, embeddings: true }),
  ];

  it("takes the strongest eligible model for each", () => {
    expect(nextForTurn(fleet, [], {})?.modelId).toBe("llama3.3:70b");
    expect(nextForTurn(fleet, [], { structuredOutput: true })?.modelId).toBe("llama3.3:70b");
    // The 70B does not advertise tools, so the tool turn falls to the 7B --
    // which is the whole reason the capability is a gate rather than a hint.
    expect(nextForTurn(fleet, [], { tools: true })?.modelId).toBe("qwen2.5:7b");
    expect(nextForTurn(fleet, [], { embeddings: true })?.modelId).toBe("nomic-embed-text");
  });

  it("answers null when nothing on the fleet can serve the turn", () => {
    expect(nextForTurn([fleet[2]!], [], { tools: true })).toBeNull();
  });

  it("honours the owner's preference", () => {
    expect(nextForTurn(fleet, ["qwen2.5:7b"], { structuredOutput: true })?.modelId).toBe("qwen2.5:7b");
  });
});

describe("the operator's vocabulary", () => {
  it("renders a parameter count the way a runtime reports one", () => {
    expect(formatParams(70_000_000_000)).toBe("70B");
    expect(formatParams(8_000_000_000)).toBe("8B");
    expect(formatParams(1_500_000_000)).toBe("1.5B");
    expect(formatParams(137_000_000)).toBe("137M");
  });

  it("renders an UNREPORTED size as nothing, never as zero", () => {
    // Zero parameters is not a thing. Printing "0B" would make the unmeasured
    // model look like the smallest rather than the one that did not say --
    // which is precisely the distinction the ordering rule turns on.
    expect(formatParams(0)).toBe("");
    expect(formatParams(-1)).toBe("");
    expect(formatParams(Number.NaN)).toBe("");
  });

  it("renders a context window in thousands", () => {
    expect(formatContext(131072)).toBe("131k");
    expect(formatContext(8192)).toBe("8k");
    expect(formatContext(512)).toBe("512");
    expect(formatContext(0)).toBe("");
  });
});
