import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({ connection: null as unknown, open: vi.fn() }));
vi.mock("../../src/live/connection", () => ({
  useOsConnection: () => h.connection,
  bridgePathFor: (base: string) => base + "_memql/ws",
  osBridgePath: "/_memql/ws",
}));
vi.mock("../../src/kit", async (original) => ({
  ...await original<typeof import("../../src/kit")>(),
  useAppReach: () => ({ canOpenWindows: true, open: h.open }),
}));
const { ReadinessSection } = await import("../../src/apps/cluster/readiness/ReadinessSection");
const { fakeConnection, withSession } = await import("./harness");
afterEach(cleanup);

function mount(fleetCatalogInstalled: boolean, fleetInferenceInstalled: boolean) {
  h.connection = fakeConnection({ inferenceStatus: [{
    eligible: false, localEligible: false, localModelCount: 0,
    fleetCatalogInstalled, fleetInferenceInstalled,
  }] });
  render(withSession(<ReadinessSection />));
}

describe("fleet readiness through a BFF", () => {
  it("offers pairing when the BFF reads the catalog without dispatching locally", async () => {
    mount(true, false);
    await screen.findByText(/No door to a model is open/);
    fireEvent.click(screen.getByRole("button", { name: "Add a machine for local models" }));
    expect(h.open).toHaveBeenCalledWith("machines", { addMachine: { inference: true } });
    expect(screen.getByText(/will run local models/)).toBeTruthy();
    expect(screen.queryByText(/cannot place fleet model calls/)).toBeNull();
  });

  it("reports missing inventory access without claiming the fleet is empty", async () => {
    mount(false, true);
    await screen.findByText(/fleet inventory cannot be read/);
    expect(screen.queryByText("Your fleet offers no models at all.")).toBeNull();
    expect(screen.queryByRole("button", { name: "Add a machine for local models" })).toBeNull();
  });
});
