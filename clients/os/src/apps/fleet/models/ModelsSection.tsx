import { useMemo, useState } from "react";

import {
  Button,
  Caption,
  Check,
  Chip,
  Chips,
  Fact,
  Facts,
  Head,
  Measure,
  Notice,
  Panel,
  Refine,
  Select,
  Subhead,
} from "../../../kit";
import { figureFrom, type Figure } from "../../../kit/measure";
import { eligibleFor, formatContext, formatParams, orderModels, type ModelNeeds } from "./ordering";
import { useInference, type CatalogModel, type DoorsReading } from "./useInference";

// Models: what this fleet can actually serve, in the order the router would
// pick from (epic memql#5096).
//
// ===========================================================================
// THE ORDER IS THE POINT
// ===========================================================================
// Every other list in this app answers "what do I have". This one answers
// "what will be used", which is a different question and the only one an
// operator asks when something is slow, expensive or refusing. A policy names
// `fleet:*`; the router ranks the caller's own catalog strongest-first and
// takes the first model that can serve THAT turn. So the list is rendered in
// exactly that order, the first eligible row is marked, and the ranking rule
// is stated ONCE above the list rather than restated per row.
//
// It is not alphabetical, and the shape of the screen is what says so.
//
// ===========================================================================
// WHY A TURN KIND CHANGES THE ANSWER
// ===========================================================================
// A structured turn needs a model that advertises structured output; a tool
// turn needs one that advertises tool calling; an embedding turn needs a
// different model entirely. So there is no single "next model" -- there is one
// per kind of turn, and printing a single one would be confidently wrong for
// two thirds of the traffic. The header row names all four.

/**
 * What has been measured about a model, or the reason nothing has.
 *
 * `measured` is filled by epic memql#5146's probe and is ABSENT everywhere
 * until it lands. That absence is a value, not a gap: a machine nobody has
 * probed showing "0 tok/s" is worse than showing nothing, because a zero says
 * "we measured, and the answer is none" -- which for a throughput figure is a
 * claim that the model does not work.
 *
 * `figureFrom` reads an absent key as `unmeasured` rather than as a number, so
 * this is total against a wire that does not carry the field yet.
 */
function measuredOf(model: CatalogModel): Figure {
  return figureFrom(model as unknown as Record<string, unknown>, "measuredTokensPerSecond");
}

const CAPABILITY_LABEL: Record<string, string> = {
  structured: "structured output",
  tools: "tool calling",
  embeddings: "embeddings",
};

/** The turns the platform makes, and what each needs of a model. */
const TURNS: Array<{ id: string; label: string; needs: ModelNeeds }> = [
  { id: "chat", label: "Chat", needs: {} },
  { id: "structured", label: "Structured", needs: { structuredOutput: true } },
  { id: "tools", label: "Tool calling", needs: { tools: true } },
  { id: "embedding", label: "Embeddings", needs: { embeddings: true } },
];

export function ModelsSection() {
  const { catalog, doors, preference } = useInference();
  const models = catalog.value ?? [];
  const reading = catalog.state === "reading" || doors.state === "reading";

  // REFINE, not a standing filter strip (DESIGN.md rule 2): collapsed until
  // asked, active constraints as removable chips beside it, and never shown
  // over an empty list.
  //
  // THESE ARE THE FACETS THIS BRANCH CAN SERVE. Epic memql#5137's catalog adds
  // category, runtime and "what this fleet lacks" to the same control; they are
  // deliberately not stubbed here, because a facet that narrows nothing is
  // worse than one that is absent -- it reads as a fleet with no entries in
  // that category rather than as a control that does not work yet.
  const [search, setSearch] = useState("");
  const [capability, setCapability] = useState("");
  const [onlineOnly, setOnlineOnly] = useState(false);

  // ORDERED ONCE, here, and rendered in that order. Every "which model" answer
  // below reads this array rather than re-sorting, so the marks and the list
  // cannot disagree about the ranking.
  const ranked = useMemo(() => orderModels(models, preference), [models, preference]);

  // THE RANKS ARE THE FULL LIST'S, NOT THE FILTERED VIEW'S. A model's rank is
  // its position in what the router would pick from, so renumbering a narrowed
  // view would print a different answer to the question this screen exists to
  // answer -- "rank 1" under a filter would name a model the router reaches
  // third. The filter hides rows; it never renumbers them.
  const shown = useMemo(
    () =>
      ranked.filter((m) => {
        if (onlineOnly && !m.online) return false;
        if (capability === "structured" && !m.structuredOutput) return false;
        if (capability === "tools" && !m.tools) return false;
        if (capability === "embeddings" && !m.embeddings) return false;
        const q = search.trim().toLowerCase();
        return q === "" || m.modelId.toLowerCase().includes(q);
      }),
    [ranked, onlineOnly, capability, search],
  );

  // Whether ANY model on this fleet has been measured. When none has, the
  // column is absent and one sentence says why -- a measured column of
  // forty-four identical absence marks is forty-four things to read past. When
  // SOME have, an unmeasured row shows its absence mark, because somebody
  // scanning a column of figures for the one that is missing has to see the
  // gap.
  const anyMeasured = useMemo(
    () => ranked.some((m) => measuredOf(m).kind === "measured"),
    [ranked],
  );

  // The first eligible model per turn kind. `null` when the fleet cannot serve
  // that kind at all, which is a state the header states rather than hides.
  const nextByTurn = useMemo(() => {
    const out = new Map<string, string>();
    for (const turn of TURNS) {
      const hit = ranked.find((m) => eligibleFor(m, turn.needs).ok);
      if (hit) out.set(turn.id, hit.modelId);
    }
    return out;
  }, [ranked]);

  return (
    <div className="os-fleet">
      <Head
        title="Models"
        meta={models.length === 0 ? undefined : `${models.length} on your fleet`}
      >
        {/* A REFRESH CONTROL BELONGS HERE, unlike on the live sections: both
            readings are on-demand projections that are never broadcast, so
            offering to look again is the honest affordance rather than a
            contradiction of a feed that arrives on its own. */}
        <Button
          tone="quiet"
          busy={reading}
          busyLabel="Reading"
          onClick={() => {
            catalog.reread();
            doors.reread();
          }}
        >
          Read again
        </Button>
      </Head>

      <DoorsPanel doors={doors.value} state={doors.state} error={doors.error} />

      {catalog.state === "failed" ? (
        <Notice
          tone="info"
          sentence="We could not read your fleet's catalog."
          next="That is not the same as a fleet with no models -- try again, or read the agent node's logs."
          detail={catalog.error}
        />
      ) : null}

      <Subhead>Ranked for your fleet</Subhead>

      {/* NOTHING ABOUT THE ORDER IS SHOWN OVER AN EMPTY LIST. The ranking
          rule, the preference chips and the four turn lines all describe an
          ordering of models, and printing them above nothing describes an
          order of nothing -- the same reason a section never shows filter
          chrome over no content (rule 2). The empty notice carries the whole
          message on its own. */}
      {models.length === 0 ? null : (
        <>
          <Caption>
            A policy that names <span className="os-mono">fleet:*</span> takes the first model
            here that can serve the turn it is making. The order is your preference first, then
            parameters, then context window, then model id — and a model that did not report its
            size sorts last, never first.
          </Caption>

          {preference.length > 0 ? (
            <Chips label="Your preferred order">
              {preference.map((id, i) => (
                <Chip key={`${id}:${i}`} tone="accent">
                  {id}
                </Chip>
              ))}
            </Chips>
          ) : null}

          <Caption>What each kind of turn would land on right now:</Caption>
          <NextForEachTurn next={nextByTurn} known />

          <div className="os-fleet-models-scope">
            <Refine
              label="Refine models"
              search={search}
              onSearch={setSearch}
              placeholder="Search"
              chips={[
                ...(capability === ""
                  ? []
                  : [
                      {
                        id: "capability",
                        label: CAPABILITY_LABEL[capability] ?? capability,
                        onRemove: () => setCapability(""),
                      },
                    ]),
                ...(onlineOnly
                  ? [{ id: "online", label: "online now", onRemove: () => setOnlineOnly(false) }]
                  : []),
              ]}
            >
              <Select
                id="models-facet-capability"
                label="Capability"
                value={capability}
                onChange={setCapability}
              >
                <option value="">Any capability</option>
                <option value="structured">Structured output</option>
                <option value="tools">Tool calling</option>
                <option value="embeddings">Embeddings</option>
              </Select>
              <Check checked={onlineOnly} onChange={setOnlineOnly}>
                Online now
              </Check>
            </Refine>
          </div>
        </>
      )}

      {catalog.state === "read" && ranked.length === 0 ? (
        <Notice
          tone="info"
          sentence="Your fleet offers no models."
          next="Pair a machine in Machines, install a runtime on it, and pull a model. Ollama and any OpenAI-compatible endpoint are discovered automatically."
        />
      ) : null}

      {ranked.length > 0 && shown.length === 0 ? (
        <Caption>No model matches that.</Caption>
      ) : null}

      <ul className="os-fleet-models">
        {shown.map((model) => (
          <ModelLine
            key={model.modelId}
            model={model}
            rank={ranked.indexOf(model) + 1}
            preferred={preference.includes(model.modelId)}
            serves={TURNS.filter((t) => nextByTurn.get(t.id) === model.modelId).map((t) => t.label)}
            measured={measuredOf(model)}
            showMeasured={anyMeasured}
          />
        ))}
      </ul>

      {/* SAID ONCE, UNDER THE LIST, rather than as a column of identical
          absence marks. When the probe lands (epic memql#5146) the column
          appears and this line goes away on its own. */}
      {ranked.length > 0 && !anyMeasured ? (
        <Caption>
          Nothing on this fleet has been measured yet, so there are no figures
          to compare. That is not the same as a model that measured badly.
        </Caption>
      ) : null}

      {catalog.at === null ? null : (
        <Caption>Read {catalog.at.toLocaleTimeString()}.</Caption>
      )}
    </div>
  );
}

/**
 * Which doors this cluster can reach, in the order the default chain tries
 * them. It is supporting context for the list below, so it sits in a Panel.
 */
function DoorsPanel({
  doors,
  state,
  error,
}: {
  doors: DoorsReading | null;
  state: string;
  error: string;
}) {
  return (
    <Panel label="Doors">
      {state === "failed" ? (
        <Notice
          tone="info"
          sentence="We could not ask this cluster which doors are open."
          next="That is not the same as a cluster with no inference."
          detail={error}
        />
      ) : null}
      {doors === null ? (
        state === "reading" ? <Caption>Asking the cluster.</Caption> : null
      ) : (
        <>
          <p className="os-cluster-fact">{doorSentence(doors)}</p>
          <div className="os-fleet-doors">
            <DoorState
              name="Local model"
              open={doors.localEligible}
              detail={
                doors.localEligible
                  ? `${doors.eligibleModelIds.length} of ${doors.localModelCount} meet the ${doors.minimumContextWindow.toLocaleString()}-token floor`
                  : doors.fleetInferenceInstalled
                    ? doors.localModelCount === 0
                      ? "your fleet offers no models"
                      : "nothing meets the floor with structured output"
                    : "this node cannot place fleet calls at all"
              }
            />
            <DoorState
              name="Signed-in app"
              open={doors.appEligible}
              detail={
                doors.appEligible
                  ? doors.runnableApps.join(", ")
                  : doors.appSessionsInstalled
                    ? "no machine has one allowed, signed in and online here"
                    : "this node cannot open app sessions at all"
              }
            />
            <DoorState
              name="Federation"
              open={doors.federationConfigured}
              detail={
                doors.federationConfigured
                  ? "workload-identity federation"
                  : doors.cloudConfigured
                    ? // A callable cloud provider that is not federated cannot
                      // happen since epic memql#5088, and this is the one line
                      // that would notice if that stopped being true.
                      //
                      // THE INVARIANT IT RESTS ON LIVES IN ANOTHER FILE: a
                      // vendor entry becomes Available only after resolvedAuth
                      // and newAIProvider both succeed
                      // (component/memql/unified_kinds_loader.go), and with the
                      // key tier deleted federation is the only path either can
                      // take -- so Available implies federated. Before that
                      // deletion this was NOT unreachable but ORDINARY: a
                      // developer running `make up` with a static key had
                      // cloudConfigured and no federation, and would have read
                      // this on every load.
                      //
                      // So if a static-key path ever returns -- a local-dev
                      // break-glass is the likely shape -- this line starts
                      // warning about clusters that are fine. Restore one and
                      // you owe this sentence an edit.
                      "a cloud provider is callable but not through federation"
                    : "not configured"
              }
            />
          </div>
          <Caption>
            The chain tries them in this order: your own hardware, then a subscription you already
            pay for, then anybody's money. Work parks only when every one is shut.
          </Caption>
        </>
      )}
    </Panel>
  );
}

function DoorState({ name, open, detail }: { name: string; open: boolean; detail: string }) {
  return (
    <div className="os-fleet-door" data-open={open || undefined}>
      <span className="os-fleet-door-name">{name}</span>
      <span className="os-fleet-door-state">{open ? "open" : "shut"}</span>
      <span className="os-caption">{detail}</span>
    </div>
  );
}

/**
 * One line per kind of turn, naming the model that would serve it.
 *
 * FOUR ANSWERS RATHER THAN ONE, because a structured turn and an embedding
 * turn legitimately resolve to different models on the same fleet, and a
 * single "next model" would be confidently wrong for most of the traffic.
 */
function NextForEachTurn({ next, known }: { next: Map<string, string>; known: boolean }) {
  if (!known) return null;
  return (
    <div className="os-fleet-turns">
      {TURNS.map((turn) => {
        const model = next.get(turn.id);
        return (
          <div className="os-fleet-turn" key={turn.id} data-served={model ? true : undefined}>
            <span className="os-fleet-turn-kind">{turn.label}</span>
            <span className={model ? "os-mono" : "os-caption"}>
              {model ?? "nothing on your fleet can serve this"}
            </span>
          </div>
        );
      })}
    </div>
  );
}

function ModelLine({
  model,
  rank,
  preferred,
  serves,
  measured,
  showMeasured,
}: {
  model: CatalogModel;
  rank: number;
  preferred: boolean;
  serves: string[];
  measured: Figure;
  /** Whether ANY model on this fleet is measured -- see `anyMeasured`. */
  showMeasured: boolean;
}) {
  const size = formatParams(model.params);
  const window = formatContext(model.contextWindow);

  return (
    <li className="os-fleet-model" data-offline={model.online ? undefined : true}>
      <div className="os-fleet-model-head">
        <span className="os-fleet-model-rank" aria-hidden="true">
          {rank}
        </span>
        <span className="os-fleet-model-id os-mono">{model.modelId}</span>
        {/* The ONE standing mark on this screen. It names what the model is
            about to be used for, which is the question the whole section
            answers; everything else here is quiet. */}
        {serves.length > 0 ? (
          <span className="os-fleet-model-next">next for {serves.join(", ").toLowerCase()}</span>
        ) : null}
        {preferred ? <Chip tone="accent">preferred</Chip> : null}
        {model.online ? null : <Chip tone="muted">offline</Chip>}
      </div>

      <Facts>
        {/* SIZE IS NOT PRINTED AS ZERO. Zero parameters is not a thing, and a
            "0" here would make the unmeasured model look like the smallest
            rather than the one that did not say -- which is precisely the
            distinction the ordering rule turns on. */}
        <Fact
          label="Size"
          value={size === "" ? "not reported — sorts last" : size}
          mono={size !== ""}
        />
        <Fact label="Context" value={window === "" ? "not reported" : `${window} tokens`} />
        {model.quant === "" ? null : <Fact label="Quantization" value={model.quant} mono />}
        <Fact
          label="Machines"
          value={
            model.machineCount === 0
              ? "none"
              : `${model.onlineCount} of ${model.machineCount} online`
          }
        />
        {/* MEASURED, and only once something on this fleet has been. An
            unmeasured row here draws `Measure`'s absence mark rather than a
            zero -- the gap is visible on purpose, because somebody scanning
            the column for what is missing has to be able to see it. */}
        {showMeasured ? (
          <Fact label="Measured" value={<Measure figure={measured} suffix=" tok/s" />} />
        ) : null}
      </Facts>

      <Chips label="Capabilities">
        <Chip tone={model.structuredOutput ? "accent" : "muted"}>
          {model.structuredOutput ? "structured output" : "no structured output"}
        </Chip>
        <Chip tone={model.tools ? "accent" : "muted"}>
          {model.tools ? "tool calling" : "no tool calling"}
        </Chip>
        <Chip tone={model.embeddings ? "accent" : "muted"}>
          {model.embeddings ? "embeddings" : "no embeddings"}
        </Chip>
      </Chips>

      {model.machines.length === 0 ? null : (
        <ul className="os-fleet-model-machines">
          {model.machines.map((machine) => (
            <li key={machine.registrationId} className="os-fleet-model-machine">
              <span className="os-mono">{machine.displayName || machine.name || machine.registrationId}</span>
              <span className="os-caption">
                {machine.online ? (machine.busy ? "busy" : "online") : "offline"}
                {machine.maxConcurrent > 0
                  ? ` · ${machine.activeCount} of ${machine.maxConcurrent} calls`
                  : ""}
                {machine.runtimes.length > 0 ? ` · ${machine.runtimes.join(", ")}` : ""}
              </span>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

/** The doors in a sentence, because the first question is not a list. */
function doorSentence(doors: DoorsReading): string {
  const open = doors.doorsOpen.map(doorWord);
  if (open.length === 0) {
    return "No door to a model is open, so anything that needs one will refuse or park.";
  }
  if (open.length === 1) return `This cluster reaches a model through ${open[0]}.`;
  return `This cluster reaches a model through ${open.slice(0, -1).join(", ")} and ${open[open.length - 1]}, in that order.`;
}

/** The doors in the reader's words. An unrecognised value is printed as it
 *  came, never dropped: a door this build has no name for is still a door. */
function doorWord(door: string): string {
  switch (door) {
    case "local":
      return "a model on your own machines";
    case "app":
      return "a signed-in app on one of your machines";
    case "federation":
      return "workload-identity federation";
    // `apiKey` has no case, because it has no producer: the door went with the
    // vendor keys (epic memql#5088). An older node still reporting it falls
    // through to the pass-through below and is printed as it came, which is
    // the right treatment for a value from a build this one does not know.
    default:
      return door;
  }
}
