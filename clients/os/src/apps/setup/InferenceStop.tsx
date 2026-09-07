import { useState } from "react";

import { Button, Caption, ChoiceStack, useAppReach, type ChoiceOption } from "../../kit";

// THE INFERENCE STOP: three doors, and the fleet one first (design record
// 2026-09-06-first-run-wizard, D4).
//
// ===========================================================================
// DOORS, NOT A LIST OF WHAT IS OPEN
// ===========================================================================
// Which doors this cluster can already walk through is the readiness feed's
// answer, and the rail line above carries it. These are the three ACTS a
// person can take to open one, in the order a fresh cluster should try them:
// a machine they already own costs nothing and keeps the model on their
// hardware, and the two vendors need an account and a bill. Any combination
// is legal, so the stop is a choice a person makes once and can come back to
// -- not a decision that forecloses the others.
//
// ===========================================================================
// AN ACT THAT IS NOT LEGAL IS THE WORDS INSTEAD (interface rule 12)
// ===========================================================================
// Settings' `providers` section is OWNER-ONLY, so a developer choosing either
// vendor gets a sentence naming who can do it rather than a button that
// navigates a window to a section the registry will not return. That is asked
// of the registry through `useAppReach` -- never restated here -- so the day
// the provider gates widen to owner-or-developer, this surface follows with
// no edit.

type DoorId = "fleet" | "anthropic" | "openai";

const DOORS: readonly (ChoiceOption & { value: DoorId })[] = [
  {
    value: "fleet",
    label: "A machine on your fleet",
    description: "Pair a computer you already own and let it serve the model. Nothing leaves your hardware, and there is no bill.",
  },
  {
    value: "anthropic",
    label: "Anthropic",
    description: "Federate this cluster with Anthropic, so no API key exists anywhere -- or seal a key instead.",
  },
  {
    value: "openai",
    label: "OpenAI",
    description: "Seal an API key. OpenAI publishes no federation mechanism yet.",
  },
];

export function InferenceStop({ role }: { role: string }) {
  const [door, setDoor] = useState<DoorId>("fleet");
  const fleet = useAppReach("fleet", role);
  const settings = useAppReach("settings", role);

  const canPair = fleet.sections.includes("machines") && fleet.canOpenWindows;
  const canFederate = settings.sections.includes("providers") && settings.canOpenWindows;

  return (
    <div className="os-setup-stop">
      <ChoiceStack
        name="setup-inference-door"
        label="How this cluster reaches a model"
        voice="prose"
        value={door}
        onChange={(next) => setDoor(next as DoorId)}
        options={DOORS}
      />
      <div className="os-setup-stop-act">{act()}</div>
    </div>
  );

  function act() {
    if (door === "fleet") {
      return canPair ? (
        <Button
          tone="primary"
          onClick={() => {
            // The Add machine panel, opened for them. Fleet's Machines
            // section has no role floor, so this is offered to a developer in
            // full -- which is the whole reason the fleet door comes first.
            fleet.open("machines", { addMachine: true });
          }}
        >
          Open Fleet
        </Button>
      ) : (
        <Caption>Pair a machine in Fleet, under Machines.</Caption>
      );
    }
    const vendor = door === "anthropic" ? "Anthropic" : "OpenAI";
    return canFederate ? (
      <Button tone="primary" onClick={() => settings.open("providers", { vendor: door })}>
        Open AI providers
      </Button>
    ) : (
      <Caption>An owner can set {vendor} up in Settings, under AI providers.</Caption>
    );
  }
}
