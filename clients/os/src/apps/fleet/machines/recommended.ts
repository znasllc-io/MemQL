import { absent, figureOf, type Figure } from "../../../kit/measure";
import { rowArray, rowNumber, rowString, type Row } from "@znasllc-io/memql-sdk-core/client";

// The recommended set and the measured figures (epic memql#5146, D2 and D4).
//
// ===========================================================================
// THE ENGINE COMPUTES THE SET; THIS FILE ONLY READS IT
// ===========================================================================
// The page has the catalog and the machine and could work the recommendation
// out itself. It does not, because that would be a second implementation of
// RecommendedSet in a second language, and the two would drift -- presenting as
// a page that offers to pull a model the act then refuses, or lists a set the
// act does not take. `fleetRecommended` is the act's own answer without the
// act.
//
// ===========================================================================
// THE `measured` FLAG IS THE DISCRIMINANT, NOT KEY PRESENCE
// ===========================================================================
// The kit's `figureFrom(row, key)` reads KEY PRESENCE and is the natural thing
// to reach for here. It is wrong for this shape: a figure of ours may carry
// `{measured: false, median: 12.4, absentReason: "..."}` -- a median left
// beside a flag that says it means nothing -- and `figureFrom` would return a
// MEASURED 12.4. That is the exact lie the type exists to prevent, produced by
// the helper.
//
// So `figureFromMeasured` reads the FLAG first and never the median beside it.
// The Go side asserts the same rule at the row and at the wire.

/** One entry in a machine's recommended set. */
export interface Recommendation {
  modelId: string;
  /** fast / strong / reasoning / embeddings. */
  level: string;
  runtime: string;
  category: string;
  params: number;
  sizeBytes: number;
  notes: string;
  /** Empty when the model can be pulled; otherwise the SENTENCE saying what is
   *  in the way, written once on the engine and rendered here verbatim. */
  blocked: string;
  pullable: boolean;
}

/** What `fleetRecommended` answers for one machine. */
export interface RecommendedSet {
  workerId: string;
  /** "" when the cockpit has not reported, "unsupported" below the floor,
   *  otherwise the rung. */
  machineClass: string;
  usableBytes: number;
  /** False when the cockpit has not reported its hardware. */
  reported: boolean;
  entries: Recommendation[];
  /** The runtimes the set NEEDS and the machine does not have, so the page can
   *  say "install Kokoro" once rather than once per blocked row. */
  runtimeGap: string[];
}

export const EMPTY_SET: RecommendedSet = {
  workerId: "",
  machineClass: "",
  usableBytes: 0,
  reported: false,
  entries: [],
  runtimeGap: [],
};

export function recommendedSetFrom(input: Row | null | undefined): RecommendedSet {
  if (!input) return EMPTY_SET;
  const row = input;
  const entries: Recommendation[] = (rowArray(row, "entries") ?? []).map((item) => {
    const entry = item as Row;
    return {
      modelId: rowString(entry, "modelId"),
      level: rowString(entry, "level"),
      runtime: rowString(entry, "runtime"),
      category: rowString(entry, "category"),
      params: rowNumber(entry, "params"),
      sizeBytes: rowNumber(entry, "size"),
      notes: rowString(entry, "notes"),
      blocked: rowString(entry, "blocked"),
      pullable: entry["pullable"] === true,
    };
  });
  const gap = (rowArray(row, "runtimeGap") ?? [])
    .map((v) => (typeof v === "string" ? v : ""))
    .filter((v) => v !== "");
  return {
    workerId: rowString(row, "workerId"),
    machineClass: rowString(row, "machineClass"),
    usableBytes: rowNumber(row, "usableBytes"),
    reported: row["reported"] === true,
    entries,
    runtimeGap: gap,
  };
}

/** One model's measured figures on one machine. */
export interface Measurement {
  machineId: string;
  modelId: string;
  suiteVersion: string;
  measuredAt: string;
  structuredValidity: Figure;
  toolCallCorrectness: Figure;
  throughputTps: Figure;
  ttftMs: Figure;
  /** The machine's own sentence when the probe itself failed. Present ALONGSIDE
   *  figures when some cases ran and some did not, which is a real state and
   *  not a contradiction. */
  probeError: string;
}

export function measurementFrom(row: Row): Measurement {
  return {
    machineId: rowString(row, "machineId"),
    modelId: rowString(row, "modelId"),
    suiteVersion: rowString(row, "suiteVersion"),
    measuredAt: rowString(row, "measuredAt"),
    structuredValidity: figureFromMeasured(row["structuredValidity"]),
    toolCallCorrectness: figureFromMeasured(row["toolCallCorrectness"]),
    throughputTps: figureFromMeasured(row["throughputTps"]),
    ttftMs: figureFromMeasured(row["ttftMs"]),
    probeError: rowString(row, "probeError"),
  };
}

/**
 * Read one stored figure.
 *
 * THE FLAG DECIDES, and the median beside a false flag is never read. See the
 * header: the kit's `figureFrom` reads key presence, which on this shape
 * returns a measured number for a figure that says it is not one.
 *
 * A missing object, a malformed one, or a `measured: true` with no median all
 * read as ABSENT. A number is what gets ranked on and what a person acts on, so
 * a figure nobody can read must not become one.
 */
export function figureFromMeasured(v: unknown): Figure {
  if (v === null || typeof v !== "object") return absent("unmeasured");
  const fig = v as Record<string, unknown>;
  if (fig["measured"] !== true) {
    const reason = typeof fig["absentReason"] === "string" ? fig["absentReason"] : "";
    const detail = typeof fig["absentDetail"] === "string" ? fig["absentDetail"] : "";
    switch (reason) {
      case "refused":
        return absent("refused", detail);
      case "failed":
        return absent("failed", detail);
      default:
        return absent("unmeasured", detail);
    }
  }
  const median = fig["median"];
  if (typeof median !== "number" || !Number.isFinite(median)) return absent("unmeasured");
  return figureOf(median);
}

/** A validity or correctness figure, as a percentage. */
export function formatRate(value: number): string {
  return `${Math.round(value * 100)}%`;
}

/** Tokens per second, whole. */
export function formatTps(value: number): string {
  return String(Math.round(value));
}

/** Time to first token, in the unit that reads. */
export function formatTtft(value: number): string {
  if (value >= 1000) return `${(value / 1000).toFixed(1)} s`;
  return `${Math.round(value)} ms`;
}

/** A level, as the page writes it. */
export function levelLabel(level: string): string {
  switch (level) {
    case "fast":
      return "Fast";
    case "strong":
      return "Strong";
    case "reasoning":
      return "Reasoning";
    case "embeddings":
      return "Embeddings";
    default:
      return level;
  }
}
