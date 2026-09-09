import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";

import { LocalUsersSettingsStore, type UsersSettings } from "../../src/apps/users/settings";

// The Users suites' one mount, so five files do not spell the connection mock
// five ways. The mock replaces the module-level context READ, which is what
// lets the real LiveCollection, the real retain path and the real projections
// run under jsdom -- see the note in the deployables harness.

export function memoryStore(seed?: Partial<UsersSettings>) {
  const bag = new Map<string, string>();
  const store = new LocalUsersSettingsStore({
    getItem: (k: string) => bag.get(k) ?? null,
    setItem: (k: string, v: string) => void bag.set(k, v),
  });
  if (seed) store.save({ ...store.load(), ...seed });
  return store;
}

export function renderApp(element: ReactElement) {
  return render(element);
}

/** A click that flushes React's effects, the shape every suite here uses. */
export async function click(target: Element | null): Promise<void> {
  const { act, fireEvent } = await import("@testing-library/react");
  if (target === null) throw new Error("click: nothing to click");
  await act(async () => {
    fireEvent.click(target);
  });
}

export const noop: (...args: never[]) => void = vi.fn();
