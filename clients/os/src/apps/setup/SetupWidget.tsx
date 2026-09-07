import { useState } from "react";

import { Rail, type Stop } from "../../kit";
import { InferenceStop } from "./InferenceStop";
import { ModuleStop } from "./ModuleStop";
import { PasskeyStop } from "./PasskeyStop";
import { useSetupFacts } from "./context";
import { PASSKEY_STOP, drawnState, openStopFor, type SetupStop } from "./stops";
import type { ModuleId } from "../../system/modules";

// THE FIRST-RUN WIZARD (design record 2026-09-06-first-run-wizard, D1 to D6).
//
// ===========================================================================
// STOPS THAT LIGHT UP, NOT STEPS WITH NEXT
// ===========================================================================
// There is no Next, no Back and no step number, because only ONE ordering
// here is a law and the widget states it in one sentence: a passkey before
// anything else, since a sign-in link is the only way back into an account
// without one. The rest are three independent decisions, and a stepper over
// three independent decisions is a form wearing a sequence's clothes
// (interface rule 12).
//
// So the rail opens the first stop still outstanding, every other reachable
// stop is one click away, and a stop somebody opens themselves stays open
// until it is settled -- at which point the rail moves on by itself, which is
// the law and the only motion this surface has.
//
// ===========================================================================
// THE BODY IS ONLY EVER HALF THE WIDGET
// ===========================================================================
// Whether it draws at all is `SetupGate`'s decision, above the frame. This
// component is never mounted while the reading is unsettled and never mounted
// once the core is configured, so it has no "loading" state and no "all done"
// state -- both would be a card on the desk saying nothing.

export function SetupWidget() {
  const facts = useSetupFacts();
  const [override, setOverride] = useState<string | null>(null);
  if (facts === null) return null;

  const { stops, readiness, identityUrl, role } = facts;
  const openStop = openStopFor(stops, override);
  const outstanding = stops.filter((s) => s.state === "waiting").length;

  const drawn: Stop[] = stops.map((stop) => ({
    id: stop.id,
    name: stop.name,
    state: drawnState(stop, openStop),
    sentence: stop.sentence,
    answer: stop.answer,
    body: bodyFor(stop),
  }));

  return (
    <div className="os-setup-widget">
      <p className="os-setup-widget-lead">{lead(outstanding)}</p>
      <Rail
        stops={drawn}
        label="Set up this cluster"
        openStop={openStop}
        onOpenStop={(id) => setOverride(id)}
      />
    </div>
  );

  /**
   * The one sentence, in three readings.
   *
   * ZERO IS THE EXIT BEAT, not an impossible state: the gate keeps this
   * mounted for a moment after the last stop lights, with every mark lit,
   * before the card folds away. "0 things" in that moment would be the last
   * thing a person read before it went.
   */
  function lead(left: number): string {
    if (left === 0) return "The core is set up. This card goes now.";
    if (left === 1) return "One thing left before this cluster can do useful work. This card goes when it is done.";
    return `Enough to make this cluster useful -- ${left} things, in any order after the first. This card goes when they are done.`;
  }

  function bodyFor(stop: SetupStop) {
    if (stop.id === PASSKEY_STOP) {
      // A skipped passkey stop (authentication off) has no act at all -- its
      // sentence on the rail line is the whole answer.
      if (stop.state === "skipped") return undefined;
      return <PasskeyStop done={stop.state === "done"} identityUrl={identityUrl} />;
    }
    if (stop.state === "done" || stop.state === "skipped") return undefined;
    const id = stop.id as ModuleId;
    if (id === "ai") return <InferenceStop role={role} />;
    return <ModuleStop id={id} verdict={readiness?.of(id) ?? null} role={role} />;
  }
}
