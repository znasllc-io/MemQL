import { act, render, screen, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { FleetSettings, FleetSettingsStore } from "../../src/apps/fleet/settings";

const h = vi.hoisted(() => ({ connection: null as unknown }));

vi.mock("../../src/live/connection", () => ({
  useOsConnection: () => h.connection,
  bridgePathFor: (base: string) => base + "_memql/ws",
  osBridgePath: "/_memql/ws",
}));

const { MachinesProvider } = await import("../../src/live/machines");
const { FleetApp } = await import("../../src/apps/fleet/FleetApp");
const { DEFAULT_FLEET_SETTINGS } = await import("../../src/apps/fleet/settings");
const { fakeConnection, withSession } = await import("./harness");

// ARRIVING FROM THE WIZARD (epic memql#5106). The first-run wizard's fleet
// door opens Fleet at Machines with `{ addMachine: true }`, and this is the
// receiving half: the Add machine panel is already open when they land.
//
// LANDING ON THE LIST WITH THE BUTTON STILL TO FIND is what this prevents.
// The act a person took was "pair a machine that serves a model" -- delivering
// them to a directory of machines completes about half of it.

afterEach(cleanup);

function memoryStore(initial: FleetSettings): FleetSettingsStore {
  let held = initial;
  return { load: () => held, save: (next) => void (held = next) };
}

async function open(payload: Record<string, unknown> | null) {
  h.connection = fakeConnection({ myWorkersWithStatus: [] });
  const consume = vi.fn();
  render(
    withSession(
      <MachinesProvider>
        <FleetApp
          sectionId="machines"
          navigate={vi.fn()}
          askContext={vi.fn()}
          store={memoryStore(DEFAULT_FLEET_SETTINGS)}
          intent={payload === null ? undefined : { id: "intent-3", payload }}
          consumeIntent={consume}
        />
      </MachinesProvider>,
    ),
  );
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
  return { consume };
}

describe("Fleet, opened to add a machine", () => {
  it("opens the Add machine panel and consumes the intent by id", async () => {
    const { consume } = await open({ addMachine: true });
    expect(screen.getByRole("region", { name: "Add a machine" })).toBeTruthy();
    // The Head's control says Close, because the panel is already open.
    expect(screen.getByRole("button", { name: "Add a machine" }).textContent).toBe("Close");
    expect(consume).toHaveBeenCalledExactlyOnceWith("intent-3");
  });

  it("leaves the panel closed when the shell hands it no intent", async () => {
    const { consume } = await open(null);
    expect(screen.queryByRole("region", { name: "Add a machine" })).toBeNull();
    expect(consume).not.toHaveBeenCalled();
  });

  it("leaves it closed for an intent that says something else", async () => {
    // Fleet's Logs section takes intents of its own, and a truthiness test
    // here would open the pairing panel for one of those.
    const { consume } = await open({ logLineId: "v1:observability:logLine:x" });
    expect(screen.queryByRole("region", { name: "Add a machine" })).toBeNull();
    expect(consume).not.toHaveBeenCalled();
  });

  it("is not fooled by a value that is merely truthy", async () => {
    const { consume } = await open({ addMachine: "yes" });
    expect(screen.queryByRole("region", { name: "Add a machine" })).toBeNull();
    expect(consume).not.toHaveBeenCalled();
  });
});
