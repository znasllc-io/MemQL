import { act, render, screen, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const h = vi.hoisted(() => {
  const reply = (rows: unknown[]) => ({ rows: () => rows });
  return {
    connection: {
      nodeId: "bff-test",
      engineVersion: "v9.9.9",
      engineCommit: "abcdef",
      subscriptions: null,
      dispatcher: null,
      onStatusChange: () => () => {},
      query: {
        providerAuthStatus: vi.fn(async () => reply([])),
        integrationStatus: vi.fn(async () => reply([])),
        providersReload: vi.fn(async () => reply([])),
        // The section reads the FLEET door too since epic memql#5088 -- a
        // machine you own is one of the ways to reach a model, and on a local
        // cluster it is the only one. Nothing in this file asserts on it; it
        // is here so the section can render at all.
        inferenceStatus: vi.fn(async () => reply([{}])),
      },
    } as unknown,
  };
});

vi.mock("../../src/live/connection", () => ({
  useOsConnection: () => h.connection,
}));

import { SessionProvider } from "../../src/chrome/access";
import { OsProvider } from "../../src/chrome/state";
import { OS_REGISTRY } from "../../src/apps/registry";
import { UNKNOWN_RUNTIME_CONFIG } from "../../src/cluster/config";
import { SettingsApp } from "../../src/apps/settings/SettingsApp";

// ARRIVING AT A VENDOR (epic memql#5106). The first-run wizard hands Settings
// a `{ vendor }` intent; this is the receiving half of that contract.
//
// jsdom implements neither `scrollIntoView` nor layout, so what is asserted is
// the part that is real in every browser AND in a test: focus lands inside the
// named vendor's panel, and the intent is consumed by id exactly once.

afterEach(cleanup);

function wrap(children: ReactNode, role = "owner") {
  return (
    <SessionProvider
      value={{
        access: { userId: "u-1", primaryEmail: "owner@example.com", clusterRole: role },
        config: { ...UNKNOWN_RUNTIME_CONFIG, domain: "example.com" },
        ladderLoaded: true,
      }}
    >
      <OsProvider registry={OS_REGISTRY} actorRole={role} grid={{ cols: 12, rows: 8 }} layout="desktop">
        {children}
      </OsProvider>
    </SessionProvider>
  );
}

async function open(payload: Record<string, unknown> | null) {
  const consume = vi.fn();
  const view = render(
    wrap(
      <SettingsApp
        sectionId="providers"
        navigate={vi.fn()}
        askContext={vi.fn()}
        intent={payload === null ? undefined : { id: "intent-7", payload }}
        consumeIntent={consume}
      />,
    ),
  );
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
  return { view, consume };
}

describe("Settings, opened at a vendor", () => {
  it("puts the cursor inside the Anthropic panel and consumes the intent by id", async () => {
    const { consume } = await open({ vendor: "anthropic" });
    const panel = document.querySelector('[data-os-vendor="anthropic"]');
    expect(panel).not.toBeNull();
    expect(panel?.contains(document.activeElement)).toBe(true);
    expect(consume).toHaveBeenCalledExactlyOnceWith("intent-7");
  });

  it("puts the cursor inside the OpenAI panel for the other door", async () => {
    await open({ vendor: "openai" });
    expect(document.querySelector('[data-os-vendor="openai"]')?.contains(document.activeElement)).toBe(true);
  });

  it("does nothing, and consumes nothing, when the intent names no vendor", async () => {
    const { consume } = await open({ somethingElse: true });
    expect(consume).not.toHaveBeenCalled();
    // A vendor nobody named must not be revealed on a guess.
    expect(document.querySelector('[data-os-vendor="anthropic"]')?.contains(document.activeElement)).toBe(false);
  });

  it("renders the section unchanged when the shell hands it no intent at all", async () => {
    const { consume } = await open(null);
    expect(consume).not.toHaveBeenCalled();
    expect(screen.getByRole("heading", { name: "AI providers" })).toBeTruthy();
  });

  it("ignores a vendor this build has no panel for, rather than throwing", async () => {
    // COMPARED rather than interpolated: a payload is a string another
    // surface wrote, and a selector built out of one could become a
    // different selector. This value would match the Anthropic panel if it
    // were pasted into a query.
    const { consume } = await open({ vendor: 'x"] , [data-os-vendor="anthropic' });
    expect(document.querySelector('[data-os-vendor="anthropic"]')?.contains(document.activeElement)).toBe(false);
    expect(consume).toHaveBeenCalledExactlyOnceWith("intent-7");
  });
});
