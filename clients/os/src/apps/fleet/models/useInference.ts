import { useCallback, useMemo } from "react";
import type { Row } from "@znasllc-io/memql-sdk-core/client";

import { useOsConnection } from "../../../live/connection";
import { useReading } from "../../../cluster/reading";
import { boolOr, stringsOf } from "../../../kit";
import { useRoutingPolicy } from "../routing/useRoutingPolicy";
import type { RankedModel } from "./ordering";

// The Models section's two readings (epic memql#5096).
//
// ===========================================================================
// NEITHER IS LIVE, AND THAT IS THE HONEST ANSWER
// ===========================================================================
// `fleetModels` and `inferenceStatus` are VIRTUAL projections -- the
// `v1:router:modelCatalog` pattern -- computed per request from which machines
// are awake right now and never persisted. There is no `graph.node.*` event
// for either, so a live collection over one would render "Loading from the
// cluster" and then a list that never moves: the exact failure this app's
// workbench notes call out.
//
// So they read once, say WHEN they looked, and offer to look again. That is
// the Cluster app's rule, and its reasoning carries: a screen where half the
// panels move and half do not is worse than one where none do, because the
// reader cannot tell which kind they are looking at.
//
// ===========================================================================
// THEY SETTLE SEPARATELY
// ===========================================================================
// Deliberately not one combined await. The doors reading and the catalog
// reading have different reasons to fail -- one asks a registry, the other a
// fleet -- and a combined one lets the read that WILL be refused decide the
// state of the one that succeeded.

export interface DoorsReading {
  eligible: boolean;
  doorsOpen: string[];
  localEligible: boolean;
  localModelCount: number;
  eligibleModelIds: string[];
  appEligible: boolean;
  runnableApps: string[];
  appSessionsInstalled: boolean;
  cloudConfigured: boolean;
  federationConfigured: boolean;
  fleetInferenceInstalled: boolean;
  minimumContextWindow: number;
}

/** One catalog row, plus the machines behind it. */
export interface CatalogModel extends RankedModel {
  quant: string;
  machineCount: number;
  onlineCount: number;
  machines: CatalogMachine[];
}

export interface CatalogMachine {
  registrationId: string;
  name: string;
  displayName: string;
  runtimes: string[];
  online: boolean;
  busy: boolean;
  activeCount: number;
  maxConcurrent: number;
}

function numberOf(row: Row | Record<string, unknown>, key: string): number {
  const raw = (row as Record<string, unknown>)[key];
  if (typeof raw === "number" && Number.isFinite(raw)) return raw;
  if (typeof raw === "string" && raw.trim() !== "") {
    const parsed = Number(raw);
    if (Number.isFinite(parsed)) return parsed;
  }
  return 0;
}

function stringOf(row: Row | Record<string, unknown>, key: string): string {
  const raw = (row as Record<string, unknown>)[key];
  return typeof raw === "string" ? raw : "";
}

/** Project one `v1:platform:fleetModel` row. */
export function catalogModelFromRow(row: Row): CatalogModel {
  const rawMachines = Array.isArray((row as Record<string, unknown>).machines)
    ? ((row as Record<string, unknown>).machines as Record<string, unknown>[])
    : [];
  return {
    // The row id IS the model id; `modelId` is on the payload too, and
    // preferring it means a row whose id was minted differently still reads
    // correctly.
    modelId: stringOf(row, "modelId") || stringOf(row, "id"),
    params: numberOf(row, "params"),
    contextWindow: numberOf(row, "contextWindow"),
    structuredOutput: boolOr(row, "structuredOutput", false),
    embeddings: boolOr(row, "embeddings", false),
    tools: boolOr(row, "tools", false),
    online: boolOr(row, "online", false),
    quant: stringOf(row, "quant"),
    machineCount: numberOf(row, "machineCount"),
    onlineCount: numberOf(row, "onlineCount"),
    machines: rawMachines.map((m) => ({
      registrationId: stringOf(m, "registrationId"),
      name: stringOf(m, "name"),
      displayName: stringOf(m, "displayName"),
      runtimes: Array.isArray(m.runtimes) ? (m.runtimes as unknown[]).filter((r): r is string => typeof r === "string") : [],
      online: m.online === true,
      busy: m.busy === true,
      activeCount: numberOf(m, "activeCount"),
      maxConcurrent: numberOf(m, "maxConcurrent"),
    })),
  };
}

/** Project the single `v1:platform:inferenceStatus` row. */
export function doorsFromRow(row: Row | null): DoorsReading | null {
  if (row === null) return null;
  return {
    eligible: boolOr(row, "eligible", false),
    doorsOpen: stringsOf(row, "doorsOpen"),
    localEligible: boolOr(row, "localEligible", false),
    localModelCount: numberOf(row, "localModelCount"),
    eligibleModelIds: stringsOf(row, "eligibleModelIds"),
    appEligible: boolOr(row, "appEligible", false),
    runnableApps: stringsOf(row, "runnableApps"),
    appSessionsInstalled: boolOr(row, "appSessionsInstalled", false),
    cloudConfigured: boolOr(row, "cloudConfigured", false),
    federationConfigured: boolOr(row, "federationConfigured", false),
    fleetInferenceInstalled: boolOr(row, "fleetInferenceInstalled", false),
    minimumContextWindow: numberOf(row, "minimumContextWindow"),
  };
}

export function useInference() {
  const connection = useOsConnection();

  const readCatalog = useCallback(
    async (signal: AbortSignal): Promise<CatalogModel[]> => {
      if (connection === null) throw new Error("not connected");
      const result = await connection.query.fleetModels({}, { signal });
      return result.rows().map(catalogModelFromRow);
    },
    [connection],
  );

  const readDoors = useCallback(
    async (signal: AbortSignal): Promise<DoorsReading | null> => {
      if (connection === null) throw new Error("not connected");
      const result = await connection.query.inferenceStatus({}, { signal });
      return doorsFromRow(result.single());
    },
    [connection],
  );

  const catalog = useReading<CatalogModel[]>(
    "fleet:models:catalog",
    connection === null ? null : readCatalog,
  );
  const doors = useReading<DoorsReading | null>(
    "fleet:models:doors",
    connection === null ? null : readDoors,
  );

  // The owner's model preference, off the LIVE routing policy -- so an edit
  // in the Routing section re-ranks this list without a re-read. The policy
  // broadcasts; the catalog does not, which is why only one of the two moves
  // on its own.
  const routing = useRoutingPolicy();
  const preference = useMemo(
    () => routing.policy?.modelPreference ?? [],
    [routing.policy],
  );

  return { catalog, doors, preference, policyError: routing.error };
}
