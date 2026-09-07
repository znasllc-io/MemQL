import { useSession } from "../chrome/access";
import { useOsIfPresent } from "../chrome/state";
import type { Readiness } from "../live/readiness";
import { MODULE_NAMES, MODULE_SETTINGS_SECTION, type ModuleId } from "../system/modules";
import type { Verdict } from "../system/readinessFold";
import { Button, Panel, Subhead } from "./controls";
import { Caption } from "./Caption";
import { ProvenanceDot, type DotTone } from "./index";

// THE SETUP SURFACE AND THE SET UP GROUP (design record
// 2026-09-06-configuration-readiness, sections 5.3 to 5.5).
//
// # Not an error, and not styled as one
//
// An unconfigured app has nothing wrong with it -- nobody has told it what to
// talk to yet. So this is quiet chrome, like SurfaceRefused beside it: the
// `off` dot at display size as the mark, one headline, the module's own
// sentence from the env manifest, and the one act that resolves it. Red and
// amber are STATUS in this shell, and they appear on the Settings entry where
// the fix is, never across the body of an app that is merely waiting.
//
// # Three states that must not read the same
//
// "The feed has not loaded", "not set up" and "set up" all decide whether the
// app body renders. gateFor answers "unknown" for the first and the window
// frame draws NOTHING for it -- a configured cluster must never flash a setup
// screen for a frame, and a shell that does not yet know must show the app
// rather than a screen the person cannot dismiss.

export interface Gate {
  state: "unknown" | "ready" | "partial" | "unconfigured";
  /** Required modules that are not configured: the ones that gate. */
  unmet: ModuleId[];
  /** Wanted modules that are not configured: the ones that only mark. */
  wanted: ModuleId[];
}

const configured = (v: Verdict | null): boolean => v !== null && v.state === "configured";

/**
 * What one surface's requirements come to.
 *
 * `partial` rather than `unconfigured` when every unmet requirement is itself
 * partial: mid-rollout, one replica has the setting and another does not, and
 * throwing up a setup screen over a deploy in progress would tell somebody to
 * fix something that is already fixing itself.
 */
export function gateFor(
  readiness: Readiness | undefined,
  requires: readonly ModuleId[],
  wants: readonly ModuleId[],
): Gate {
  if (!readiness || !readiness.loaded) return { state: "unknown", unmet: [], wanted: [] };
  const unmet = requires.filter((id) => !configured(readiness.of(id)));
  const wanted = wants.filter((id) => !configured(readiness.of(id)));
  const anyPartial = requires.some((id) => readiness.of(id)?.state === "partial");
  if (unmet.some((id) => readiness.of(id)?.state !== "partial")) {
    return { state: "unconfigured", unmet, wanted };
  }
  if (anyPartial || wanted.length > 0) return { state: "partial", unmet, wanted };
  return { state: "ready", unmet, wanted };
}

/**
 * The mark's tone, or null for no mark at all.
 *
 * Null for both `unknown` and `ready`, which is the restraint that keeps this
 * from becoming furniture: a dot that sits on every app forever is chrome, and
 * this one draws only while a person is actually needed.
 */
export function markToneFor(gate: Gate): DotTone | null {
  if (gate.state === "unconfigured") return "needsSetup";
  if (gate.state === "partial") return "partlySetUp";
  return null;
}

/** Sentence-case words for a verdict, the Set up group's vocabulary. */
export function stateWords(v: Verdict | null): string {
  if (v === null || v.state === "unreported") return "Not reported";
  if (v.state === "configured") return "Set up";
  if (v.state === "partial") return "Partly set up";
  return "Not set up";
}

export function SurfaceUnconfigured({
  surface,
  unmet,
  descriptions,
  canSetUp,
  onSetUp,
}: {
  /** What they opened, named as they saw it: "Campaigns", or "Workbenches". */
  surface: string;
  unmet: readonly ModuleId[];
  /** The manifest descriptions, by module id; a missing one renders the module's name. */
  descriptions: Partial<Record<ModuleId, string>>;
  canSetUp: boolean;
  onSetUp: () => void;
}) {
  return (
    <div className="os-setup" data-os-setup-surface>
      {/* The `off` dot at display size: an empty ring, which reads as
          "nothing here yet" rather than "something is wrong". */}
      <span className="os-setup-mark" aria-hidden />
      <h2 className="os-setup-head">{surface} is not set up yet</h2>
      {unmet.map((id) => (
        <p key={id} className="os-setup-body">
          {descriptions[id] ?? `${MODULE_NAMES[id]} is needed first.`}
        </p>
      ))}
      {canSetUp ? (
        // "Set up <surface>" -- the same verb the group's rows use for the
        // finished state, so the word a person clicks is the word they see
        // afterwards.
        <Button tone="primary" onClick={onSetUp}>
          Set up {surface}
        </Button>
      ) : (
        <p className="os-caption os-setup-next">An owner or developer can set it up in Settings.</p>
      )}
    </div>
  );
}

/** Whether this actor may configure: owner or developer, as a SET (the Integrations gate). */
export function canConfigure(clusterRole: string): boolean {
  return clusterRole === "owner" || clusterRole === "developer";
}

export function SetupGroup({
  app,
  requires,
  wants,
  readiness,
}: {
  app: string;
  requires: readonly ModuleId[];
  wants: readonly ModuleId[];
  /** Injected so the group is testable without a session feed; apps pass useSession().readiness. */
  readiness: Readiness | undefined;
}) {
  const { access } = useSession();
  const os = useOsIfPresent();
  const role = access?.clusterRole ?? "";
  if (!canConfigure(role)) return null;
  const ids = Array.from(new Set([...requires, ...wants]));
  if (ids.length === 0) return null;
  return (
    <Panel label={`Set up ${app}`}>
      <Subhead>Set up</Subhead>
      {!readiness || !readiness.loaded ? (
        <Caption>Reading this cluster&apos;s setup.</Caption>
      ) : (
        <div className="os-setup-group-rows">
          {ids.map((id) => {
            const v = readiness.of(id);
            const tone: DotTone | null =
              v === null || v.state === "unreported" || v.state === "configured"
                ? null
                : v.state === "partial"
                  ? "partlySetUp"
                  : "needsSetup";
            const target = MODULE_SETTINGS_SECTION[id];
            // The lane that still needs something, else the first: what a
            // person has to set is the incomplete one.
            const lane = v?.lanes.find((l) => !l.complete) ?? v?.lanes[0];
            // `!== "phone"` rather than `=== "desktop"`: the iPad chrome
            // carries windows, and an act that opens one works there.
            const canOpen = target !== null && os !== null && os.layout !== "phone";
            return (
              <div key={id} className="os-setup-row">
                <span className="os-setup-name">{MODULE_NAMES[id]}</span>
                <span className="os-setup-state">
                  {tone ? (
                    <ProvenanceDot tone={tone} label={`${MODULE_NAMES[id]}: ${stateWords(v)}`} />
                  ) : null}
                  {stateWords(v)}
                  {v && v.disagreement.length > 0 ? ` (${v.disagreement.join(", ")})` : ""}
                </span>
                {target === null ? (
                  <span className="os-setup-act">
                    <span className="os-caption">Set in the deployment</span>
                    {lane && lane.slots.length > 0 ? (
                      <p className="os-setup-vars">
                        {lane.slots
                          .filter((s) => !s.present)
                          .map((s) => s.name)
                          .join(" ")}
                      </p>
                    ) : null}
                  </span>
                ) : canOpen ? (
                  <span className="os-setup-act">
                    <Button
                      onClick={() => {
                        os.actions.openApp("settings", target.section);
                      }}
                    >
                      Open {target.name}
                    </Button>
                  </span>
                ) : (
                  // No window to open into, so the destination is named in
                  // words. A button that could not go anywhere would be worse
                  // than a sentence that says where to look.
                  <span className="os-setup-act os-caption">Settings, under {target.name}</span>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Panel>
  );
}
