import { useRef, useState } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PasskeyStop } from "../../src/apps/setup/PasskeyStop";
import { SetupReturnDispatcher } from "../../src/apps/setup/SetupReturnDispatcher";
import {
  clearSetupReturn,
  devicesUrl,
  leaveForPasskey,
  parkSetupReturn,
  takeSetupReturn,
} from "../../src/apps/setup/returns";
import { OS_REGISTRY } from "../../src/apps/registry";
import { OsProvider, useOs } from "../../src/chrome/state";

// LEAVING AND COMING BACK. One act in this wizard replaces the document --
// the passkey ceremony lives on the identity origin -- so the marker is
// written by the load that leaves and spent by the one that comes back.

afterEach(() => {
  cleanup();
  clearSetupReturn();
});

describe("the marker", () => {
  it("is taken exactly once", () => {
    parkSetupReturn("passkey");
    expect(takeSetupReturn()).toEqual({ stopId: "passkey" });
    // A value that survived would move somebody's desk on the next remount.
    expect(takeSetupReturn()).toBeNull();
  });

  it("is null when nothing was parked, which is not a failure", () => {
    expect(takeSetupReturn()).toBeNull();
  });
});

describe("the destination", () => {
  it("is the identity service's Devices page, with no double slash", () => {
    expect(devicesUrl("https://identity.example.test")).toBe("https://identity.example.test/me/devices");
    expect(devicesUrl("https://identity.example.test/")).toBe("https://identity.example.test/me/devices");
  });

  it("is EMPTY when the shell does not know its identity origin", () => {
    // Never a URL composed from the current host: that would send somebody to
    // a page that does not exist, which is worse than saying where to look.
    expect(devicesUrl("")).toBe("");
  });
});

describe("leaving", () => {
  it("parks the marker and hands the browser over, in that order", () => {
    const assign = vi.fn();
    const ok = leaveForPasskey("https://identity.example.test", {
      location: { assign },
    } as unknown as Window);
    expect(ok).toBe(true);
    expect(assign).toHaveBeenCalledWith("https://identity.example.test/me/devices");
    expect(takeSetupReturn()).toEqual({ stopId: "passkey" });
  });

  it("refuses, and parks nothing, when there is nowhere to send them", () => {
    const assign = vi.fn();
    expect(leaveForPasskey("", { location: { assign } } as unknown as Window)).toBe(false);
    expect(assign).not.toHaveBeenCalled();
    expect(takeSetupReturn()).toBeNull();
  });
});

describe("the passkey stop's own act", () => {
  it("leaves through that one path", () => {
    const assign = vi.fn();
    render(
      <PasskeyStop
        done={false}
        identityUrl="https://identity.example.test"
        win={{ location: { assign } } as unknown as Window}
      />,
    );
    screen.getByRole("button", { name: "Register a passkey" }).click();
    expect(assign).toHaveBeenCalledWith("https://identity.example.test/me/devices");
  });

  it("says where to look, with no control at all, when the origin is unknown", () => {
    render(<PasskeyStop done={false} identityUrl="" />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("Register one on the identity service, under Devices.")).toBeTruthy();
  });

  it("offers a way to manage them once one is held, and no act", () => {
    render(<PasskeyStop done identityUrl="https://identity.example.test" />);
    expect(screen.queryByRole("button")).toBeNull();
    const link = screen.getByRole("link", { name: "Manage your passkeys under Devices" });
    expect(link.getAttribute("href")).toBe("https://identity.example.test/me/devices");
  });
});

describe("the dispatcher", () => {
  /**
   * Records the desk the dispatcher switched to, through the REAL provider.
   *
   * WRAPPED EXACTLY ONCE. The actions object is stable across renders, so a
   * component that re-wraps on every render wraps its own wrapper -- and one
   * call then records two, which reads as a dispatcher that fired twice.
   */
  function useSwitchSpy(onSwitch: (id: string) => void) {
    const os = useOs();
    const wrapped = useRef(false);
    if (!wrapped.current) {
      wrapped.current = true;
      const original = os.actions.switchDesk;
      os.actions.switchDesk = ((id: string) => {
        onSwitch(id);
        return original(id);
      }) as typeof original;
    }
    return os;
  }

  function Spy({ onSwitch }: { onSwitch: (id: string) => void }) {
    useSwitchSpy(onSwitch);
    return null;
  }

  function mount() {
    const switched: string[] = [];
    render(
      <OsProvider registry={OS_REGISTRY} actorRole="owner" grid={{ cols: 12, rows: 8 }} layout="desktop">
        <Spy onSwitch={(id) => switched.push(id)} />
        <SetupReturnDispatcher />
      </OsProvider>,
    );
    return switched;
  }

  beforeEach(() => {
    clearSetupReturn();
  });

  it("does nothing at all when nothing was parked", () => {
    expect(mount()).toEqual([]);
  });

  it("stays put when the desk holding the widget is already the one on screen", () => {
    parkSetupReturn("passkey");
    expect(mount()).toEqual([]);
    expect(takeSetupReturn()).toBeNull();
  });

  it("switches back to the desk holding the widget when somebody left from another", () => {
    parkSetupReturn("passkey");
    const switched: string[] = [];
    let seeded = "";
    function Harness() {
      const os = useSwitchSpy((id) => switched.push(id));
      const [mounted, setMounted] = useState(false);
      seeded = os.state.shell.desks[0]?.id ?? "";
      return (
        <>
          <button type="button" onClick={() => os.actions.addDesk()}>
            add desk
          </button>
          <button type="button" onClick={() => setMounted(true)}>
            come back
          </button>
          {mounted ? <SetupReturnDispatcher /> : null}
        </>
      );
    }
    render(
      <OsProvider registry={OS_REGISTRY} actorRole="owner" grid={{ cols: 12, rows: 8 }} layout="desktop">
        <Harness />
      </OsProvider>,
    );
    // A second desk, which is where they were when they followed the link.
    fireEvent.click(screen.getByRole("button", { name: "add desk" }));
    switched.length = 0;
    fireEvent.click(screen.getByRole("button", { name: "come back" }));
    expect(switched).toEqual([seeded]);
  });

  it("spends the marker even when there is no widget to return to", () => {
    // The design's own failure mode: they registered the passkey, came back,
    // and the core had finished configuring while they were away -- or they
    // are a role whose desk never carried the wizard. Landing somebody on a
    // desk to look at a widget that is not there would be a worse answer than
    // leaving them where they are.
    parkSetupReturn("passkey");
    const switched: string[] = [];
    render(
      <OsProvider registry={OS_REGISTRY} actorRole="reader" grid={{ cols: 12, rows: 8 }} layout="desktop">
        <Spy onSwitch={(id) => switched.push(id)} />
        <SetupReturnDispatcher />
      </OsProvider>,
    );
    expect(switched).toEqual([]);
    expect(takeSetupReturn()).toBeNull();
  });
});
