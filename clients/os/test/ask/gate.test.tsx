import { cleanup, render, screen } from "@testing-library/react";
import { useEffect } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { coreAt, readiness, withOs } from "../setup/harness";
import { withSession } from "../cluster/harness";
import { AskProvider, useAsk } from "../../src/ask/AskProvider";
import { AskSheet } from "../../src/ask/AskSheet";
import { AskUnconfigured } from "../../src/ask/AskUnconfigured";
import type { AskTransport } from "../../src/ask/askController";
import { MODULE_DESCRIPTIONS } from "../../src/system/modules";
import type { Readiness } from "../../src/live/readiness";

afterEach(cleanup);

// THE ASK SHEET GATES LIKE THE ASK WIDGET (design record
// 2026-09-07-core-gate-and-honest-install, D7).
//
// The widget gated on the `ai` module and the sheet did not, so the keyboard
// shortcut sent a question on an unconfigured cluster and rendered the
// engine's own error back at the person: "no streaming provider available".

const SENTENCE = `${MODULE_DESCRIPTIONS.ai} An owner or developer can set it up in Settings.`;

/** A transport that records whether anything was ever sent through it. */
function spyTransport() {
  const sent: string[] = [];
  const transport = {
    send: vi.fn(async (text: string) => {
      sent.push(text);
      return { text: "an answer" };
    }),
  } as unknown as AskTransport;
  return { transport, sent };
}

/** Opens the sheet on mount, the way the keyboard shortcut does. */
function OpenOnMount() {
  const { openAsk } = useAsk();
  useEffect(() => openAsk(null), [openAsk]);
  return null;
}

function sheet(feed: Readiness, transport: AskTransport) {
  // THE REAL SHELL PROVIDER around it: AskSheet reads `useMakeGoal`, which
  // reads `useOs`, because handing a prompt off is a shell act -- one Ask,
  // three entry points. A sheet mounted without it is not a case this surface
  // has in production.
  return withSession(
    withOs(
      <AskProvider transport={transport} voice={null}>
        <OpenOnMount />
        <AskSheet />
      </AskProvider>,
      "owner",
    ),
    { readiness: feed },
  );
}

describe("the sentence an unconfigured Ask says", () => {
  it("is the engine's module description plus the ask-an-owner line", () => {
    render(<AskUnconfigured />);
    expect(screen.getByText(SENTENCE)).toBeTruthy();
  });
});

describe("the sheet on a cluster with no inference door", () => {
  it("says the same sentence the widget says, and offers no input", () => {
    const { transport, sent } = spyTransport();
    render(sheet(coreAt("unconfigured", "configured", "configured"), transport));
    expect(screen.getByRole("dialog", { name: "Ask" })).toBeTruthy();
    expect(screen.getByText(SENTENCE)).toBeTruthy();
    // NEVER SENDS. There is nothing to type into, so there is no path from
    // this sheet to the engine's "no streaming provider available".
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(sent).toHaveLength(0);
  });

  it("opens normally once a door is configured", () => {
    const { transport } = spyTransport();
    render(sheet(coreAt("configured", "configured", "configured"), transport));
    expect(screen.queryByText(SENTENCE)).toBeNull();
    expect(screen.getByRole("dialog", { name: "Ask" })).toBeTruthy();
  });

  it("opens normally while the feed is UNKNOWN", () => {
    // A shell that does not yet know what is configured must not refuse a
    // question it could answer -- gateFor answers "unknown" for an unloaded
    // feed, and only "unconfigured" gates.
    const { transport } = spyTransport();
    render(sheet(readiness(false, []), transport));
    expect(screen.queryByText(SENTENCE)).toBeNull();
  });

  it("says the sentence when ai is UNREPORTED, exactly as the widget does", () => {
    // THE SHEET AND THE CORE GATE READ THIS ONE DIFFERENTLY, and both are
    // right, because they are answering different questions.
    //
    // The GATE asks "should this person be held out of the whole OS", and a
    // cluster nobody reported for is not a cluster with no door -- holding
    // them would lock them out of the Cluster app they need in order to go and
    // find out why nothing is reporting. So it opens the desk.
    //
    // The SHEET asks "can this question be answered", and it cannot: there is
    // no evidence of a provider anywhere. It says so in the same words the Ask
    // WIDGET says them in, which is D7's actual requirement -- both read
    // `gateFor`, which folds `unreported` into `unconfigured`, so one Ask
    // cannot answer two ways depending on which key you pressed.
    const { transport } = spyTransport();
    render(sheet(coreAt("unreported", "configured", "configured"), transport));
    expect(screen.getByText(SENTENCE)).toBeTruthy();
  });
});
