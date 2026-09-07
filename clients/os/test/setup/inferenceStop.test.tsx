import { useRef } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { OS_REGISTRY } from "../../src/apps/registry";
import { OsProvider, useOs } from "../../src/chrome/state";
import { InferenceStop } from "../../src/apps/setup/InferenceStop";

// THE THREE DOORS, and where each one lands. What is asserted is the TRIPLE
// each act opens -- app, section, payload -- because that is the contract
// between this stop and the two apps that consume it, and the only part of it
// a rendered button can get wrong silently.

afterEach(cleanup);

/**
 * Records what the shell was asked to open, through the REAL provider.
 *
 * WRAPPED EXACTLY ONCE. The actions object is stable across renders, so a
 * component that re-wraps on every render wraps its own wrapper, and one
 * click then records two.
 */
function Spy({ onOpen }: { onOpen: (call: unknown[]) => void }) {
  const os = useOs();
  const wrapped = useRef(false);
  if (!wrapped.current) {
    wrapped.current = true;
    os.actions.openApp = ((...args: unknown[]) => {
      onOpen(args);
      return { kind: "none" } as never;
    }) as typeof os.actions.openApp;
  }
  return null;
}

function mount(role: string) {
  const calls: unknown[][] = [];
  render(
    <OsProvider registry={OS_REGISTRY} actorRole={role} grid={{ cols: 12, rows: 8 }} layout="desktop">
      <Spy onOpen={(c) => calls.push(c)} />
      <InferenceStop role={role} />
    </OsProvider>,
  );
  return calls;
}

describe("the doors an owner is offered", () => {
  it("offers three, with the fleet one first and selected", () => {
    mount("owner");
    // THE ORDER IS THE RECOMMENDATION, and it is the only one this surface
    // makes: a machine somebody already owns costs nothing, and the two
    // vendors need an account.
    expect(screen.getAllByRole("radio").map((el) => el.textContent)).toEqual([
      "A machine on your fleet",
      "Anthropic",
      "OpenAI",
    ]);
    expect(screen.getAllByRole("radio")[0]?.getAttribute("aria-checked")).toBe("true");
  });

  it("says what the CHOSEN door costs, one sentence at a time", () => {
    // The doors carry no prose of their own: three names read at a glance,
    // and the sentence belongs to the one being considered. Three
    // descriptions at once put the act below the fold of a desk widget,
    // which the visual pass caught and no test would have.
    mount("owner");
    expect(screen.getByText("A computer you already own serves the model. Nothing leaves it, and there is no bill.")).toBeTruthy();

    fireEvent.click(screen.getByRole("radio", { name: "Anthropic" }));
    expect(screen.getByText("Anthropic serves the model, on their hardware and their bill.")).toBeTruthy();

    fireEvent.click(screen.getByRole("radio", { name: "OpenAI" }));
    expect(screen.getByText("OpenAI serves the model, on their hardware and their bill.")).toBeTruthy();
  });

  it("says nothing about how a vendor proves who this cluster is", () => {
    // THE PIN, and it is a durability claim rather than a copy one. A
    // sentence here naming a key or a federation is a sentence that goes
    // stale the next time either changes -- one of them was false the day it
    // was written -- and the credential belongs to the panel that sets it up.
    mount("owner");
    for (const vendor of ["Anthropic", "OpenAI"]) {
      fireEvent.click(screen.getByRole("radio", { name: vendor }));
      const said = document.body.textContent ?? "";
      expect(said).not.toMatch(/API key|federat/i);
    }
  });

  it("opens Fleet at Machines with the Add machine panel, for the fleet door", () => {
    const calls = mount("owner");
    fireEvent.click(screen.getByRole("button", { name: "Open Fleet" }));
    expect(calls).toEqual([["fleet", "machines", { addMachine: true }]]);
  });

  it("opens AI providers at the named vendor, for each federation door", () => {
    const calls = mount("owner");
    fireEvent.click(screen.getByRole("radio", { name: /Anthropic/ }));
    fireEvent.click(screen.getByRole("button", { name: "Open AI providers" }));
    fireEvent.click(screen.getByRole("radio", { name: /OpenAI/ }));
    fireEvent.click(screen.getByRole("button", { name: "Open AI providers" }));
    expect(calls).toEqual([
      ["settings", "providers", { vendor: "anthropic" }],
      ["settings", "providers", { vendor: "openai" }],
    ]);
  });
});

describe("the doors a developer is offered", () => {
  it("gets the fleet door in full: Fleet's Machines section has no role floor", () => {
    const calls = mount("developer");
    fireEvent.click(screen.getByRole("button", { name: "Open Fleet" }));
    expect(calls).toEqual([["fleet", "machines", { addMachine: true }]]);
  });

  it("gets WORDS for a federation door, never a button that would be refused", () => {
    mount("developer");
    fireEvent.click(screen.getByRole("radio", { name: /Anthropic/ }));
    expect(screen.queryByRole("button", { name: "Open AI providers" })).toBeNull();
    expect(screen.getByText("An owner can set Anthropic up in Settings, under AI providers.")).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: /OpenAI/ }));
    expect(screen.getByText("An owner can set OpenAI up in Settings, under AI providers.")).toBeTruthy();
  });

  it("asks the REGISTRY rather than restating who may federate", () => {
    // The providers section declares `roles: { min: "owner" }`. This is the
    // pin: widen that manifest and the developer gets the button, with no
    // edit to the stop -- which is the whole reason the check is a lookup.
    const settings = OS_REGISTRY.apps.find((a) => a.id === "settings");
    expect(settings?.sections?.find((s) => s.id === "providers")?.roles).toEqual({ min: "owner" });
  });
});

describe("with no shell to open into", () => {
  it("names both destinations in words", () => {
    render(<InferenceStop role="owner" />);
    expect(screen.getByText("Pair a machine in Fleet, under Machines.")).toBeTruthy();
    fireEvent.click(screen.getByRole("radio", { name: /Anthropic/ }));
    expect(screen.getByText("An owner can set Anthropic up in Settings, under AI providers.")).toBeTruthy();
  });
});
