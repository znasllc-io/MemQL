// The curated catalog, joined against what this fleet actually serves
// (epic memql#5137, task memql#5140).
//
// ===========================================================================
// THIS ANSWERS A DIFFERENT QUESTION FROM THE LIST ABOVE IT
// ===========================================================================
// `ordering.ts` answers "what WILL be used", ranked in the order the router
// picks from. This answers "what SHOULD this fleet run, and what can it not
// run -- and why". The second question is the one an operator has when a
// capability is missing, and the ranked list cannot answer it at all: a model
// nobody pulled is not IN the ranked list, so its absence is silent.
//
// ===========================================================================
// THE GAP IS THE INFORMATION
// ===========================================================================
// A full alphabetical dump of the catalog beside a full dump of the fleet
// leaves the reader to diff two lists. What they came for is the difference,
// so the join computes it: every profile is either SERVED (and by which
// machines) or BLOCKED with one of three reasons, and every fleet model the
// catalog does not know is reported rather than hidden.
//
// ===========================================================================
// EXACT ID EQUALITY, NEVER A FUZZY MATCH
// ===========================================================================
// The model id is byte-identical from the cockpit's label through the catalog
// row to a policy naming `fleet:<modelId>`, and that is deliberate so this
// comparison can be a string equality. `qwen3.5:9b` and `qwen3.5:9b-q4` are
// different models -- different weights, possibly different capabilities --
// and matching one to the other would tell an operator their fleet serves a
// model it does not have.

/** One `v1:models:modelProfile` row, as the surface reads it. */
export interface ModelProfile {
  modelId: string;
  category: string;
  runtime: string;
  family: string;
  params: number;
  quant: string;
  sizeBytes: number;
  contextWindow: number;
  /** The capability names that are TRUE. Absence is false. */
  flags: string[];
  dimensions: number;
  license: string;
  recommendedFor: string[];
  minMachineClass: string;
  offeredOn: string[];
  notes: string;
  curated: boolean;
  unavailable: boolean;
}

/** What a machine on this fleet reports, reduced to what the join needs. */
export interface FleetMachineFacts {
  name: string;
  runtimes: string[];
  online: boolean;
  /** macos | linux | "" when the machine has not said. */
  platform: string;
  /** Unified memory or VRAM in gigabytes; 0 when the machine has not said. */
  memoryGb: number;
}

/** A fleet model, reduced to what the join needs. */
export interface FleetModelFacts {
  modelId: string;
  online: boolean;
  machineCount: number;
}

export type BlockedKind = "no-machine-of-class" | "runtime-missing" | "not-offered-on-platform";

export interface BlockedReason {
  kind: BlockedKind;
  /** One sentence an operator can act on. Never an icon, never a code. */
  detail: string;
}

export interface CatalogRow {
  profile: ModelProfile;
  /** True when a fleet model matches this profile's id exactly. */
  served: boolean;
  /** Machine names serving it, empty when not served. */
  servedBy: string[];
  /**
   * Why this fleet cannot serve it, or null.
   *
   * NULL WHEN SERVED, and also null when the fleet COULD serve it and simply
   * has not pulled it -- those are different states and only the second is an
   * invitation. A blocked profile is one no machine here could run even after
   * a pull, which is the only case where "why" is a question with an answer.
   */
  blocked: BlockedReason | null;
}

/** A model the fleet serves that the catalog has never heard of. */
export interface UncataloguedModel {
  modelId: string;
  online: boolean;
  machineCount: number;
}

export interface CatalogReading {
  rows: CatalogRow[];
  /**
   * Fleet models with no catalog entry.
   *
   * REPORTED, NOT HIDDEN. An operator who pulled a model by hand is entitled
   * to see it acknowledged; a page that silently omitted it would read as
   * though the pull had failed.
   */
  uncatalogued: UncataloguedModel[];
}

/** Machine classes in ascending order. minMachineClass is a FLOOR. */
const MACHINE_CLASSES = ["16", "24", "32", "64", "128"];

function classIndex(value: string): number {
  return MACHINE_CLASSES.indexOf(value);
}

/**
 * The largest machine class this fleet has, as an index into MACHINE_CLASSES,
 * or -1 when no machine has said how much memory it has.
 *
 * A MACHINE THAT HAS NOT SAID DOES NOT COUNT AS SMALL. It counts as unknown,
 * and an unknown fleet blocks nothing -- reporting "no machine of this class"
 * to somebody whose 64 GB laptop simply has not reported its memory yet would
 * be confidently wrong, and the operator has no way to tell that from the
 * truth.
 */
function largestClass(machines: FleetMachineFacts[]): number {
  let best = -1;
  for (const m of machines) {
    if (m.memoryGb <= 0) continue;
    for (let i = MACHINE_CLASSES.length - 1; i >= 0; i--) {
      if (m.memoryGb >= Number(MACHINE_CLASSES[i])) {
        if (i > best) best = i;
        break;
      }
    }
  }
  return best;
}

function platforms(machines: FleetMachineFacts[]): Set<string> {
  const out = new Set<string>();
  for (const m of machines) {
    if (m.platform) out.add(m.platform);
  }
  return out;
}

function runtimes(machines: FleetMachineFacts[]): Set<string> {
  const out = new Set<string>();
  for (const m of machines) {
    for (const r of m.runtimes) out.add(r);
  }
  return out;
}

function plural(n: number, one: string, many: string): string {
  return n === 1 ? one : many;
}

/**
 * Join the catalog against the fleet.
 *
 * The three blocked reasons are checked in the order an operator would act on
 * them -- platform first (nothing to be done), then runtime (installable),
 * then machine class (needs hardware) -- so the sentence they read names the
 * cheapest thing that would fix it last.
 */
export function joinCatalog(
  profiles: ModelProfile[],
  fleetModels: FleetModelFacts[],
  machines: FleetMachineFacts[],
): CatalogReading {
  const byId = new Map<string, FleetModelFacts>();
  for (const m of fleetModels) {
    if (m.modelId) byId.set(m.modelId, m);
  }

  const fleetPlatforms = platforms(machines);
  const fleetRuntimes = runtimes(machines);
  const biggest = largestClass(machines);

  const rows: CatalogRow[] = profiles.map((profile) => {
    const hit = byId.get(profile.modelId);
    if (hit) {
      const servedBy = machines
        .filter((m) => m.runtimes.includes(profile.runtime))
        .map((m) => m.name)
        .filter((n) => n !== "");
      return { profile, served: true, servedBy, blocked: null };
    }

    // Not pulled. Could this fleet run it at all?
    if (profile.offeredOn.length > 0 && fleetPlatforms.size > 0) {
      const anyMatch = profile.offeredOn.some((os) => fleetPlatforms.has(os));
      if (!anyMatch) {
        const where = profile.offeredOn.join(" or ");
        return {
          profile,
          served: false,
          servedBy: [],
          blocked: {
            kind: "not-offered-on-platform",
            detail: `Runs on ${where}. No machine on your fleet is one.`,
          },
        };
      }
    }

    if (fleetRuntimes.size > 0 && !fleetRuntimes.has(profile.runtime)) {
      return {
        profile,
        served: false,
        servedBy: [],
        blocked: {
          kind: "runtime-missing",
          detail: `Needs the ${profile.runtime} runtime. No machine on your fleet has it installed.`,
        },
      };
    }

    const floor = classIndex(profile.minMachineClass);
    if (floor >= 0 && biggest >= 0 && biggest < floor) {
      return {
        profile,
        served: false,
        servedBy: [],
        blocked: {
          kind: "no-machine-of-class",
          detail: `Needs ${profile.minMachineClass} GB. Your largest machine has ${MACHINE_CLASSES[biggest]} GB.`,
        },
      };
    }

    // Could be served, and has not been pulled. Not blocked -- an invitation.
    return { profile, served: false, servedBy: [], blocked: null };
  });

  const known = new Set(profiles.map((p) => p.modelId));
  const uncatalogued: UncataloguedModel[] = fleetModels
    .filter((m) => m.modelId && !known.has(m.modelId))
    .map((m) => ({ modelId: m.modelId, online: m.online, machineCount: m.machineCount }));

  return { rows, uncatalogued };
}

/** The nine categories, in the order the surface shows them. */
export const CATEGORY_ORDER = [
  "text",
  "reasoning",
  "omni",
  "vision",
  "audioIn",
  "audioOut",
  "imageGen",
  "videoGen",
  "embeddings",
] as const;

/** What each category is called on screen, in the reader's words. */
export const CATEGORY_LABEL: Record<string, string> = {
  text: "Everyday work",
  reasoning: "Hard problems",
  omni: "Everything at once",
  vision: "Seeing",
  audioIn: "Listening",
  audioOut: "Speaking",
  imageGen: "Making images",
  videoGen: "Making video",
  embeddings: "Search and memory",
};

export interface CategoryGroup {
  category: string;
  label: string;
  rows: CatalogRow[];
  /** How many of this category's entries the fleet serves. */
  servedCount: number;
  /**
   * Whether this fleet has any machine at all.
   *
   * IT CHANGES WHAT AN UNPULLED ENTRY MEANS, which is why the group carries it
   * rather than the sentence guessing. With machines, "not pulled" is an
   * invitation one command away. With none, an unknown fleet blocks nothing --
   * so every entry reads as pullable, and saying "runs on a machine you already
   * have" to somebody who has paired nothing is a claim about hardware that
   * does not exist.
   */
  fleetHasMachines: boolean;
}

/**
 * Group the join by category, in CATEGORY_ORDER, dropping categories the
 * catalog has no entry for.
 *
 * A category with no entries is DROPPED rather than rendered empty: `vision`
 * has none by design -- the text models see, so a separate vision pull would
 * be a second copy of weights the fleet already holds -- and an empty heading
 * would read as a gap in the fleet rather than a decision about the catalog.
 */
export function groupByCategory(
  reading: CatalogReading,
  fleetHasMachines = true,
): CategoryGroup[] {
  const out: CategoryGroup[] = [];
  for (const category of CATEGORY_ORDER) {
    const rows = reading.rows.filter((r) => r.profile.category === category);
    if (rows.length === 0) continue;
    out.push({
      category,
      label: CATEGORY_LABEL[category] ?? category,
      rows,
      servedCount: rows.filter((r) => r.served).length,
      fleetHasMachines,
    });
  }
  return out;
}

/**
 * One sentence for a category group's scope line.
 *
 * IT NAMES THE STATE, not a count on its own. "3 of 4" tells a reader nothing
 * about whether that is fine; "Served by your fleet" and "Nothing here runs on
 * your fleet yet" are answers.
 */
export function categorySentence(group: CategoryGroup): string {
  if (group.servedCount === group.rows.length) return "Your fleet serves every recommendation here.";
  if (group.servedCount > 0) {
    return `Your fleet serves ${group.servedCount} of ${group.rows.length}.`;
  }
  // A FLEET WITH NO MACHINES GETS NO PER-CATEGORY SENTENCE AT ALL.
  //
  // The state is a property of the FLEET, not of each category, so saying it
  // per group printed "Pair a machine and these become available" nine times
  // down one screen -- rule 7, say it once. The section says it once above the
  // groups instead, and every group falls silent. The empty string is what the
  // surface checks for.
  //
  // It matters that this is not just repetition. Without a machine, every entry
  // is unblocked -- an unknown fleet blocks nothing -- so the honest per-group
  // sentence would have been "these run on a machine you already have", said to
  // somebody who has paired none.
  if (!group.fleetHasMachines) return "";
  const blocked = group.rows.filter((r) => r.blocked !== null).length;
  if (blocked === group.rows.length) {
    return "Nothing here runs on your fleet, and each entry says why.";
  }
  const pullable = group.rows.length - blocked;
  return `Nothing here is pulled yet. ${pullable} of them ${plural(pullable, "runs", "run")} on a machine you already have.`;
}
