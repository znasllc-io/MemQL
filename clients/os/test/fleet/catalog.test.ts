import { describe, expect, it } from "vitest";

const { applyFacets, categorySentence, groupByCategory, hasUncheckableClass, joinCatalog } = await import(
  "../../src/apps/fleet/models/catalog"
);
type FleetMachineFacts = import("../../src/apps/fleet/models/catalog").FleetMachineFacts;
type FleetModelFacts = import("../../src/apps/fleet/models/catalog").FleetModelFacts;
type ModelProfile = import("../../src/apps/fleet/models/catalog").ModelProfile;

// The catalog join (epic memql#5137, task memql#5140).
//
// The join is the whole feature and it is PURE, so it is tested on fixtures
// with no DOM. What the component adds on top is layout; what goes wrong in a
// join is an operator told their fleet serves a model it does not have, or told
// nothing at all about one it cannot.

function profile(over: Partial<ModelProfile> = {}): ModelProfile {
  return {
    modelId: "qwen3.5:9b",
    category: "text",
    runtime: "ollama",
    family: "qwen3.5",
    params: 9_000_000_000,
    quant: "Q4_K_M",
    sizeBytes: 6_600_000_000,
    contextWindow: 262_144,
    flags: ["structured", "tools", "vision", "streaming"],
    dimensions: 0,
    license: "qwen",
    recommendedFor: ["fast", "strong"],
    minMachineClass: "16",
    offeredOn: ["macos", "linux"],
    notes: "The default local pick.",
    curated: true,
    unavailable: false,
    ...over,
  };
}

function machine(over: Partial<FleetMachineFacts> = {}): FleetMachineFacts {
  return {
    name: "studio",
    runtimes: ["ollama"],
    online: true,
    platform: "macos",
    memoryGb: 32,
    ...over,
  };
}

function served(modelId: string): FleetModelFacts {
  return { modelId, online: true, machineCount: 1 };
}

describe("joinCatalog", () => {
  it("marks a profile served when a fleet model matches its id exactly", () => {
    const r = joinCatalog([profile()], [served("qwen3.5:9b")], [machine()]);
    expect(r.rows[0]!.served).toBe(true);
    expect(r.rows[0]!.blocked).toBeNull();
    expect(r.rows[0]!.servedBy).toContain("studio");
  });

  it("does not fuzzy-match: qwen3.5:9b and qwen3.5:9b-q4 are different models", () => {
    // Different weights, possibly different capabilities. Matching them would
    // tell an operator their fleet serves a model it does not have.
    const r = joinCatalog([profile()], [served("qwen3.5:9b-q4")], [machine()]);
    expect(r.rows[0]!.served).toBe(false);
    expect(r.uncatalogued.map((m) => m.modelId)).toEqual(["qwen3.5:9b-q4"]);
  });

  it("blocks with no-machine-of-class when every machine is below the floor", () => {
    const r = joinCatalog(
      [profile({ modelId: "qwen3.5:122b", minMachineClass: "128" })],
      [],
      [machine({ memoryGb: 32 })],
    );
    expect(r.rows[0]!.blocked?.kind).toBe("no-machine-of-class");
    expect(r.rows[0]!.blocked?.detail).toContain("128 GB");
    expect(r.rows[0]!.blocked?.detail).toContain("32 GB");
  });

  it("blocks with runtime-missing when no machine reports the runtime", () => {
    const r = joinCatalog(
      [profile({ modelId: "kokoro-82m", runtime: "kokoro", category: "audioOut" })],
      [],
      [machine({ runtimes: ["ollama"] })],
    );
    expect(r.rows[0]!.blocked?.kind).toBe("runtime-missing");
    expect(r.rows[0]!.blocked?.detail).toContain("kokoro");
  });

  it("blocks with not-offered-on-platform for a linux-only entry on an all-macos fleet", () => {
    const r = joinCatalog(
      [profile({ modelId: "wan2.2:5b", category: "videoGen", offeredOn: ["linux"], runtime: "comfyui" })],
      [],
      [machine({ platform: "macos" })],
    );
    expect(r.rows[0]!.blocked?.kind).toBe("not-offered-on-platform");
    expect(r.rows[0]!.blocked?.detail).toContain("linux");
  });

  it("reports a fleet model with no catalog hit rather than hiding it", () => {
    // An operator who pulled a model by hand is entitled to see it
    // acknowledged; silently omitting it reads as though the pull failed.
    const r = joinCatalog([profile()], [served("qwen3.5:9b"), served("mistral-small:24b")], [machine()]);
    expect(r.uncatalogued).toHaveLength(1);
    expect(r.uncatalogued[0]!.modelId).toBe("mistral-small:24b");
  });

  it("does not block a profile the fleet could run and simply has not pulled", () => {
    // Blocked and not-yet-pulled are different states, and only the second is
    // an invitation. Conflating them would tell an operator to buy hardware
    // when the answer is one pull.
    const r = joinCatalog([profile()], [], [machine()]);
    expect(r.rows[0]!.served).toBe(false);
    expect(r.rows[0]!.blocked).toBeNull();
  });

  it("a machine that has not reported its memory blocks nothing", () => {
    // Unknown is not small. Telling somebody with an unreported 64 GB laptop
    // that they have no machine of the class would be confidently wrong, and
    // they have no way to tell that from the truth.
    const r = joinCatalog(
      [profile({ minMachineClass: "128" })],
      [],
      [machine({ memoryGb: 0 })],
    );
    expect(r.rows[0]!.blocked).toBeNull();
  });

  it("an empty fleet blocks nothing, because there is nothing to compare against", () => {
    const r = joinCatalog([profile({ minMachineClass: "128", runtime: "mflux" })], [], []);
    expect(r.rows[0]!.blocked).toBeNull();
    expect(r.rows[0]!.served).toBe(false);
  });

  it("platform is checked before runtime, so the sentence names the thing that cannot change", () => {
    // A linux-only model on a mac fleet with no comfyui: both are true, and
    // "install comfyui" is advice that would not help.
    const r = joinCatalog(
      [profile({ category: "videoGen", offeredOn: ["linux"], runtime: "comfyui" })],
      [],
      [machine({ platform: "macos", runtimes: ["ollama"] })],
    );
    expect(r.rows[0]!.blocked?.kind).toBe("not-offered-on-platform");
  });
});

describe("groupByCategory", () => {
  it("drops a category the catalog has no entry for", () => {
    // `vision` has none by design: the text models see. An empty heading would
    // read as a gap in the fleet rather than a decision about the catalog.
    const groups = groupByCategory(joinCatalog([profile()], [], [machine()]));
    expect(groups.map((g) => g.category)).toEqual(["text"]);
  });

  it("orders categories by CATEGORY_ORDER, not by the input order", () => {
    const groups = groupByCategory(
      joinCatalog(
        [
          profile({ modelId: "qwen3-embedding:0.6b", category: "embeddings" }),
          profile({ modelId: "gpt-oss:20b", category: "reasoning" }),
          profile({ modelId: "qwen3.5:9b", category: "text" }),
        ],
        [],
        [machine()],
      ),
    );
    expect(groups.map((g) => g.category)).toEqual(["text", "reasoning", "embeddings"]);
  });

  it("counts what the fleet serves per category", () => {
    const groups = groupByCategory(
      joinCatalog(
        [profile({ modelId: "a" }), profile({ modelId: "b" })],
        [served("a")],
        [machine()],
      ),
    );
    expect(groups[0]!.servedCount).toBe(1);
  });
});

describe("categorySentence", () => {
  it("says the state rather than only a count", () => {
    // "3 of 4" tells a reader nothing about whether that is fine.
    const all = groupByCategory(joinCatalog([profile({ modelId: "a" })], [served("a")], [machine()]));
    expect(categorySentence(all[0]!)).toContain("every recommendation");

    const none = groupByCategory(joinCatalog([profile({ modelId: "a" })], [], [machine()]));
    expect(categorySentence(none[0]!)).toContain("runs on a machine you already have");
  });

  it("does not claim a model runs on hardware whose size nobody has reported", () => {
    // THE PRODUCTION STATE, and the fixture default hides it: `machine()`
    // reports memoryGb 32, and no real machine reports anything until the
    // scanner in epic memql#5146 lands. So every test machine had a known
    // class and this path was never taken.
    //
    // With memory unknown the floor cannot be checked, so the row is
    // deliberately NOT blocked -- guessing would tell an operator their
    // machine is too small when nobody has asked it yet. But counting it as
    // pullable turns "we could not check" into "it runs on a machine you
    // already have", which is a positive claim about somebody's hardware built
    // from the absence of data about it, and wrong in the direction that gets
    // a 122B pull started on a laptop.
    const big = profile({ modelId: "qwen3.5:122b", minMachineClass: "128" });
    const unreported = machine({ memoryGb: 0 });
    const group = groupByCategory(joinCatalog([big], [], [unreported]))[0]!;

    expect(group.rows[0]!.blocked).toBeNull();
    expect(group.rows[0]!.classKnown).toBe(false);
    expect(categorySentence(group)).not.toContain("machine you already have");
    // The group says only its own half. The MEMORY half is a fact about the
    // fleet -- the same under every category -- so the section says it once
    // above the list (rule 7) and `hasUncheckableClass` is what it asks.
    expect(categorySentence(group)).toBe("Nothing here is pulled yet.");
    expect(hasUncheckableClass([group])).toBe(true);
  });

  it("still counts pullable entries when the class IS known", () => {
    // The control for the case above. Without it, the fix could report the
    // "not reported" sentence for every fleet and pass -- which would replace
    // a wrong claim with a useless one.
    const fits = profile({ modelId: "qwen3.5:9b", minMachineClass: "16" });
    const known = machine({ memoryGb: 32 });
    const group = groupByCategory(joinCatalog([fits], [], [known]))[0]!;

    expect(group.rows[0]!.classKnown).toBe(true);
    expect(categorySentence(group)).toContain("runs on a machine you already have");
    expect(hasUncheckableClass([group])).toBe(false);
  });

  it("a fleet with no machines is told to pair one, not that these run on hardware it has", () => {
    // Without this branch the page tells somebody who has paired nothing that
    // these models run on a machine they already have -- because an unknown
    // fleet blocks nothing, so every entry reads as pullable.
    const empty = groupByCategory(joinCatalog([profile({ modelId: "a" })], [], []), false);
    // The group falls SILENT and the section says it once above the list --
    // saying it per category printed the same sentence nine times down one
    // screen (rule 7).
    expect(categorySentence(empty[0]!)).toBe("");
  });

  it("says each entry explains itself when the whole category is blocked", () => {
    const blocked = groupByCategory(
      joinCatalog(
        [profile({ category: "videoGen", offeredOn: ["linux"], runtime: "comfyui" })],
        [],
        [machine({ platform: "macos" })],
      ),
    );
    expect(categorySentence(blocked[0]!)).toContain("says why");
  });
});

describe("applyFacets", () => {
  // The seam epic memql#5153's Refine control narrows through. It is tested
  // here rather than left to that epic because a prop with no exerciser is
  // inert code that reads as a working feature -- and the sentence trap below
  // is invisible from the control's side.

  function threeCategories() {
    return groupByCategory(
      joinCatalog(
        [
          profile({ modelId: "a", category: "text", runtime: "ollama" }),
          profile({ modelId: "b", category: "text", runtime: "ollama" }),
          profile({ modelId: "k", category: "audioOut", runtime: "kokoro" }),
          profile({ modelId: "w", category: "videoGen", runtime: "comfyui", offeredOn: ["linux"] }),
        ],
        [served("a")],
        [machine({ platform: "macos", runtimes: ["ollama"] })],
      ),
    );
  }

  it("no facets renders exactly what no narrowing renders", () => {
    // "The control is closed" and "the control is open with nothing chosen"
    // must be one page, not two.
    const groups = threeCategories();
    const same = applyFacets(groups, {});
    expect(same).toBe(groups);
    expect(applyFacets(groups, { category: "", runtime: "" }).length).toBe(groups.length);
  });

  it("narrows to one category", () => {
    const out = applyFacets(threeCategories(), { category: "audioOut" });
    expect(out.map((g) => g.category)).toEqual(["audioOut"]);
  });

  it("narrows by runtime and drops categories left empty", () => {
    const out = applyFacets(threeCategories(), { runtime: "kokoro" });
    expect(out.map((g) => g.category)).toEqual(["audioOut"]);
    expect(out[0]!.shown).toHaveLength(1);
  });

  it("lackingOnly means BLOCKED, not merely unpulled", () => {
    // An entry that is simply not pulled is an invitation -- one command away.
    // Folding it in with the blocked ones would put "run one command" in the
    // same list as "buy hardware".
    const out = applyFacets(threeCategories(), { lackingOnly: true });

    // audioOut needs the kokoro runtime and videoGen is linux-only; the
    // fixture's machine is macos with ollama, so both are genuinely blocked.
    expect(out.map((g) => g.category)).toEqual(["audioOut", "videoGen"]);
    for (const group of out) {
      for (const row of group.shown) {
        expect(row.blocked).not.toBeNull();
      }
    }

    // THE PROPERTY THAT MATTERS: `text` is excluded. Its entry "b" is not
    // pulled and WOULD run on a machine this fleet already has -- an invitation
    // one command away, which does not belong in a list of things the fleet
    // cannot do.
    expect(out.map((g) => g.category)).not.toContain("text");
  });

  it("the category sentence still describes the CATEGORY, not the filter", () => {
    // The trap, and the reason `rows` and `shown` are separate fields. The text
    // group serves 1 of 2; narrowing to what the fleet lacks must not make that
    // sentence report 0 of 0.
    const all = threeCategories();
    const text = all.find((g) => g.category === "text")!;
    expect(categorySentence(text)).toContain("1 of 2");

    const narrowed = applyFacets(all, { runtime: "ollama", lackingOnly: false });
    const narrowedText = narrowed.find((g) => g.category === "text")!;
    expect(categorySentence(narrowedText)).toBe(categorySentence(text));
  });

  it("combines facets rather than taking the last one", () => {
    const out = applyFacets(threeCategories(), { category: "text", runtime: "kokoro" });
    expect(out).toHaveLength(0);
  });
});
