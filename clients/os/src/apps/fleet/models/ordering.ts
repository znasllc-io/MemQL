// The fleet's model ordering, on the client (epic memql#5096, design D5).
//
// ===========================================================================
// WHY THERE ARE TWO IMPLEMENTATIONS, AND WHAT KEEPS THEM IN STEP
// ===========================================================================
// `component/memql`'s `orderModels` is the rule the router applies when a
// policy names `fleet:*`. This is the same rule, restated, because the Models
// section's entire job is to show WHICH MODEL WILL BE USED -- and a list the
// engine did not order is a list the reader has to re-derive the ordering of
// in their head, which is exactly the question they came to the page with.
// `fleetModels` returns rows sorted by id, deliberately, so that a client
// rendering a plain catalog gets a stable order; ranking is a second question
// and this is its answer.
//
// Neither side can be deleted in favour of the other: the engine cannot ship
// a per-caller ranking on a projection every reader shares, and the page
// cannot ask the engine per render.
//
// So the risk is DRIFT, and drift here is invisible in the worst way -- both
// sides keep working and the page simply names a different model from the one
// the router picks. `ordering.fixture.json` is the shared table both
// implementations are asserted against (component/memql's
// fleet_ordering_parity_test.go and this app's ordering.test.ts), so a change
// to either rule fails on the other's side.

/** One model as the Models section reasons about it. */
export interface RankedModel {
  modelId: string;
  /** Parameter count, or 0 when no machine reported one. */
  params: number;
  /** Largest context window any machine behind it advertises, in tokens. */
  contextWindow: number;
  structuredOutput: boolean;
  embeddings: boolean;
  tools: boolean;
  online: boolean;
}

/** What a particular kind of turn needs of a model. */
export interface ModelNeeds {
  structuredOutput?: boolean;
  embeddings?: boolean;
  tools?: boolean;
}

/**
 * Rank a catalog strongest-first.
 *
 * The order is: the owner's explicit preference for the ids it names, then
 * PARAMETERS descending, then CONTEXT WINDOW descending, then model id.
 *
 * MISSING SIZE SORTS LAST, NEVER FIRST, and that direction is the whole of the
 * rule. A model that does not say how big it is must not win by silence: a
 * cockpit that predates the attribute would otherwise become the fleet's
 * strongest model on every machine it runs on.
 *
 * The sort is STABLE over the input, so two models tying on every signal keep
 * the catalog's own order -- which is `modelId` ascending, and therefore the
 * same on every replica and in every browser.
 */
export function orderModels<T extends RankedModel>(models: readonly T[], preference: readonly string[]): T[] {
  const rank = new Map<string, number>();
  preference.forEach((id, i) => {
    const key = id.trim();
    if (key !== "" && !rank.has(key)) rank.set(key, i);
  });
  // A model the preference does not name sorts after every one it does.
  const prefRank = (m: RankedModel) => rank.get(m.modelId) ?? preference.length + 1;

  return [...models].sort((a, b) => {
    const ra = prefRank(a);
    const rb = prefRank(b);
    if (ra !== rb) return ra - rb;
    // Unknown size last, in both directions: it is not "zero parameters", it
    // is "the machine did not say".
    const aKnown = a.params > 0;
    const bKnown = b.params > 0;
    if (aKnown !== bKnown) return aKnown ? -1 : 1;
    if (a.params !== b.params) return b.params - a.params;
    if (a.contextWindow !== b.contextWindow) return b.contextWindow - a.contextWindow;
    return a.modelId < b.modelId ? -1 : a.modelId > b.modelId ? 1 : 0;
  });
}

/**
 * Whether a model can serve a call with these needs, and the miss when it
 * cannot. It mirrors `FleetModel.eligibleFor` on the engine side.
 *
 * The reason is a SENTENCE rather than a code, because its only reader is a
 * person looking at a row and asking why it is not the one being used.
 */
export function eligibleFor(m: RankedModel, needs: ModelNeeds): { ok: boolean; why: string } {
  if (!m.online) return { ok: false, why: "no machine offering it is online" };
  if (needs.structuredOutput && !m.structuredOutput) {
    return { ok: false, why: "does not advertise structured output" };
  }
  if (needs.embeddings && !m.embeddings) return { ok: false, why: "does not advertise embeddings" };
  if (needs.tools && !m.tools) return { ok: false, why: "does not advertise tool calling" };
  return { ok: true, why: "" };
}

/**
 * The model `fleet:*` would pick for a turn with these needs, or null when
 * nothing is eligible.
 */
export function nextForTurn<T extends RankedModel>(
  models: readonly T[],
  preference: readonly string[],
  needs: ModelNeeds,
): T | null {
  for (const m of orderModels(models, preference)) {
    if (eligibleFor(m, needs).ok) return m;
  }
  return null;
}

/**
 * A parameter count in the vocabulary the operator's own runtime uses: "8B",
 * "70B", "137M". Zero is NOT rendered as a number -- the caller says "size not
 * reported", because zero parameters is not a thing and printing it would make
 * the model look like the smallest rather than the unmeasured one.
 */
export function formatParams(params: number): string {
  if (!Number.isFinite(params) || params <= 0) return "";
  const units: Array<[number, string]> = [
    [1_000_000_000_000, "T"],
    [1_000_000_000, "B"],
    [1_000_000, "M"],
    [1_000, "K"],
  ];
  for (const [scale, suffix] of units) {
    if (params >= scale) {
      const value = params / scale;
      // One decimal only when it says something: 8B, not 8.0B; 1.5B, not 2B.
      const rendered = value >= 100 || Number.isInteger(value) ? Math.round(value).toString() : value.toFixed(1);
      return `${rendered}${suffix}`;
    }
  }
  return params.toString();
}

/**
 * A context window in the operator's vocabulary: "128k", "32k", "8k".
 *
 * DIVIDED BY 1024, NOT 1000, and that is not pedantry. Context windows are
 * powers of two and every model card in the world names them that way: 32768
 * is the model an operator knows as 32k, and rounding 32.768 up to "33k"
 * prints a number they have never seen next to a model they recognise, which
 * reads as this page having got something wrong.
 */
export function formatContext(tokens: number): string {
  if (!Number.isFinite(tokens) || tokens <= 0) return "";
  if (tokens >= 1024) return `${Math.round(tokens / 1024)}k`;
  return tokens.toString();
}
