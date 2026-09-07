import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ReactNode } from "react";

// Settings -> AI providers (epic memql#4984; rebuilt by epic memql#5088).
//
// THE SECTION HAS NO KEY FIELD, and this suite's first job is to keep it that
// way. Federation is the only door for a cloud vendor and the fleet is the
// other one; the engine's key-sealing builtin is deleted, so a box that posted
// to it would be a control whose only outcome is a refusal.
//
// The planted key below is what the "no key anywhere" sweeps look for. It is
// long and distinctive so a sweep over the rendered tree is not vacuous, and
// nothing in the suite ever types it -- there is nowhere to type it.
const PLANTED_KEY = "sk-PLANTED-PROVIDER-KEY-DO-NOT-EMIT-000000";

/** The sentence `anthropicCredential` hands a half-configured provider entry. */
const HALF_SET_REASON =
  'provider "streamClaudeSonnet" is HALF-CONFIGURED for Anthropic workload identity ' +
  "federation: ruleId, organizationId set, serviceAccountId missing. Set all four " +
  "(or none). Runbook: docs/public/operate/auth/anthropic-federation.md";

const h = vi.hoisted(() => {
  const reply = (rows: unknown[]) => ({ rows: () => rows });
  const state = {
    providers: [] as unknown[],
    providerError: null as Error | null,
    federationCalls: [] as Record<string, string>[],
    inference: {} as Record<string, unknown>,
    inferenceError: null as Error | null,
    verifyReply: { verified: true, reason: "" } as Record<string, unknown>,
    reloadCalls: 0,
  };
  const connection = {
    nodeId: "bff-test",
    engineVersion: "v9.9.9",
    engineCommit: "abcdef123456",
    subscriptions: null,
    dispatcher: null,
    query: {
      providerAuthStatus: vi.fn(async () => {
        if (state.providerError) throw state.providerError;
        return reply(state.providers);
      }),
      inferenceStatus: vi.fn(async () => {
        if (state.inferenceError) throw state.inferenceError;
        return reply([state.inference]);
      }),
      providerFederationSet: vi.fn(async (args: Record<string, string>) => {
        state.federationCalls.push(args);
        return reply([{ message: "Federation ids stored." }]);
      }),
      providerVerify: vi.fn(async () => reply([state.verifyReply])),
      providersReload: vi.fn(async () => {
        state.reloadCalls += 1;
        return reply([{ availableOnThisNode: 1, registered: 2 }]);
      }),
    },
    onStatusChange: () => () => {},
  };
  return { connection, state };
});

vi.mock("../../src/live/connection", () => ({
  useOsConnection: () => h.connection,
  bridgePathFor: (base: string) => base + "_memql/ws",
  osBridgePath: "/_memql/ws",
}));

const { SessionProvider } = await import("../../src/chrome/access");
const { OsProvider } = await import("../../src/chrome/state");
const { OS_REGISTRY } = await import("../../src/apps/registry");
const { SettingsApp } = await import("../../src/apps/settings/SettingsApp");
const { LocalDesktopStore } = await import("../../src/system/store");
const { UNKNOWN_RUNTIME_CONFIG } = await import("../../src/cluster/config");
const { roleAdmits } = await import("../../src/system/roles");
const { PROVIDERS_SECTION_ROLE, DOOR_WORDS } = await import(
  "../../src/apps/settings/ProvidersSection"
);
const { doorFor, fleetDoorFrom, localityOf, missingFederationFields, summarize } = await import(
  "../../src/apps/settings/providerFacts"
);

function memStorage(): Pick<Storage, "getItem" | "setItem"> {
  const data = new Map<string, string>();
  return { getItem: (k) => data.get(k) ?? null, setItem: (k, v) => void data.set(k, v) };
}

function wrap(children: ReactNode, role: string, domain: string) {
  return (
    <SessionProvider
      value={{
        access: { userId: "u-1", primaryEmail: "owner@example.com", clusterRole: role },
        config: {
          ...UNKNOWN_RUNTIME_CONFIG,
          domain,
          identityUrl: domain === "" ? "" : `https://identity.${domain}`,
        },
      }}
    >
      <OsProvider
        registry={OS_REGISTRY}
        actorRole={role}
        grid={{ cols: 12, rows: 8 }}
        store={new LocalDesktopStore(memStorage())}
      >
        {children}
      </OsProvider>
    </SessionProvider>
  );
}

async function renderProviders(role = "owner", domain = "example.com") {
  const view = render(
    wrap(
      <SettingsApp sectionId="providers" navigate={vi.fn()} askContext={vi.fn()} />,
      role,
      domain,
    ),
  );
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
  return view;
}

/** The state word one vendor panel's door line carries. */
function doorWordIn(regionName: string): string {
  const panel = screen.getByRole("region", { name: regionName });
  return panel.querySelector(".os-door-state")?.textContent ?? "";
}

function doorStateIn(regionName: string): string {
  const panel = screen.getByRole("region", { name: regionName });
  return panel.querySelector(".os-door")?.getAttribute("data-os-door") ?? "";
}

beforeEach(() => {
  // PER TEST, because vitest isolates modules per FILE and not per test: the
  // spies below are module state, so a click in one case is still recorded
  // when the next one asks whether anything was called. `clearAllMocks` clears
  // the call log and keeps the implementations `vi.fn(impl)` installed.
  vi.clearAllMocks();
  h.state.providers = [
    {
      name: "chat54Mini",
      vendor: "openai",
      model: "gpt-5.4-mini",
      available: true,
      authSource: "federation",
      reason: "",
    },
    {
      name: "streamClaudeSonnet",
      vendor: "anthropic",
      model: "claude-sonnet-5",
      available: false,
      authSource: "unresolved",
      reason: "no credential configured",
    },
  ];
  h.state.providerError = null;
  h.state.federationCalls = [];
  h.state.inference = {
    eligible: true,
    localEligible: true,
    localModelCount: 3,
    eligibleModelIds: ["qwen3-coder", "llama4"],
    minimumContextWindow: 32000,
    fleetInferenceInstalled: true,
  };
  h.state.inferenceError = null;
  h.state.verifyReply = { verified: true, reason: "" };
  h.state.reloadCalls = 0;
});

describe("the provider summary", () => {
  // Pure, so the three states are pinned without rendering. The keyless case
  // is the one that matters: it is how a correctly-installed cluster starts.
  it("calls an empty registry the install state, not a fault", () => {
    expect(summarize([]).tone).toBe("unconfigured");
    expect(summarize([]).headline).toMatch(/how a cluster is installed/);
  });

  it("reports a partial registry as partial rather than ready", () => {
    const rows = [
      { name: "a", vendor: "openai", model: "m", available: true, authSource: "env", reason: "" },
      {
        name: "b",
        vendor: "anthropic",
        model: "m",
        available: false,
        authSource: "unresolved",
        reason: "",
      },
    ];
    expect(summarize(rows)).toEqual({
      tone: "partial",
      headline: "1 of 2 providers can be called.",
    });
  });

  it("calls a registry with no callable provider unconfigured, however many are registered", () => {
    // The trap this pins: `total > 0` is not the same question as "can this
    // cluster call a model". Two registered providers that both fail to
    // resolve is the keyless state wearing a count.
    const rows = [
      {
        name: "a",
        vendor: "openai",
        model: "m",
        available: false,
        authSource: "unresolved",
        reason: "",
      },
      {
        name: "b",
        vendor: "anthropic",
        model: "m",
        available: false,
        authSource: "unresolved",
        reason: "",
      },
    ];
    expect(summarize(rows).tone).toBe("unconfigured");
  });
});

describe("a vendor's door, as a pure reading", () => {
  const row = (over: Record<string, unknown>) =>
    ({
      name: "p",
      vendor: "openai",
      model: "m",
      available: false,
      authSource: "unresolved",
      reason: "",
      ...over,
    }) as Parameters<typeof doorFor>[1][number];

  it("is open when a provider of that vendor resolved through federation", () => {
    expect(doorFor("openai", [row({ authSource: "federation", available: true })]).state).toBe(
      "open",
    );
  });

  it("is unset when no id is anywhere -- which is how every cluster is installed", () => {
    expect(doorFor("openai", [row({})]).state).toBe("unset");
    // And a vendor with no registered provider at all is unset, not unknown:
    // there is one answer to "can this cluster call Anthropic" and it is no.
    expect(doorFor("anthropic", [row({})]).state).toBe("unset");
  });

  it("is half when the engine says half-configured, and keeps the engine's words", () => {
    const door = doorFor("anthropic", [row({ vendor: "anthropic", reason: HALF_SET_REASON })]);
    expect(door.state).toBe("half");
    expect(door.said).toBe(HALF_SET_REASON);
  });

  it("prefers half over open, so the dangerous state cannot hide behind the reassuring one", () => {
    const door = doorFor("openai", [
      row({ authSource: "federation", available: true }),
      row({ name: "q", reason: HALF_SET_REASON }),
    ]);
    expect(door.state).toBe("half");
  });

  it("matches a vendor's provider types by prefix, the way the engine does", () => {
    // One auth block registers `openai`, `openaistt`, `openaiwhisper` and
    // seven more. An equality match would report the door shut while every
    // one of them was federated.
    expect(
      doorFor("openai", [row({ vendor: "openaiwhisper", authSource: "federation" })]).state,
    ).toBe("open");
    // The negative control: a prefix match must not reach across vendors.
    expect(
      doorFor("anthropic", [row({ vendor: "openaiwhisper", authSource: "federation" })]).state,
    ).toBe("unset");
  });
});

describe("whether either vendor could reach this cluster's issuer", () => {
  // Mirrors `IsLocalDomain` in integrations/email/delivery.go, arm for arm.
  it("reads every local shape the engine reads", () => {
    for (const d of [
      "localhost",
      "127.0.0.1",
      "::1",
      "0.0.0.0",
      "memql.localhost",
      "os.memql.localhost",
      "MEMQL.LOCALHOST",
      "app.local.example.com",
    ]) {
      expect(localityOf(d), d).toBe("local");
    }
  });

  it("reads an ordinary domain as reachable", () => {
    for (const d of ["example.com", "memql.example.com", "local.example.com"]) {
      // Note the third: `local.example.com` has `local` as its FIRST label,
      // not its second, so it is an ordinary domain. The engine draws the line
      // in the same place, and getting it wrong here would withhold the form
      // from a real cluster.
      expect(localityOf(d), d).toBe("reachable");
    }
  });

  it("answers unknown for an unread domain rather than guessing local", () => {
    // The one place this deliberately differs from the Go predicate, which
    // reads empty as local. In a browser empty means the runtime config has
    // not landed, and flashing "federation is not available here" at a cloud
    // operator is a claim the surface could not take back.
    expect(localityOf("")).toBe("unknown");
    expect(localityOf("   ")).toBe("unknown");
  });
});

describe("the fleet door, as a pure reading", () => {
  it("is open when a machine offers a model that clears the floor", () => {
    const door = fleetDoorFrom({
      localEligible: true,
      localModelCount: 3,
      eligibleModelIds: ["a", "b"],
      minimumContextWindow: 32000,
      fleetInferenceInstalled: true,
    });
    expect(door.state).toBe("open");
    expect(door.said).toMatch(/2 of 3 models on your machines meet the/);
  });

  it("agrees with itself about number", () => {
    // "2 of 3 models ... meets" and "offer 1 model, and none of them meets"
    // both shipped past a green suite, because no assertion read the sentence.
    expect(
      fleetDoorFrom({
        localEligible: true,
        localModelCount: 1,
        eligibleModelIds: ["a"],
        minimumContextWindow: 32000,
        fleetInferenceInstalled: true,
      }).said,
    ).toMatch(/1 of 1 model on your machines meets the/);
    expect(
      fleetDoorFrom({
        localEligible: false,
        fleetInferenceInstalled: true,
        localModelCount: 1,
        minimumContextWindow: 32000,
      }).said,
    ).toMatch(/offer one model, and it does not meet the/);
  });

  it("tells a node that cannot place fleet calls apart from a fleet with nothing on it", () => {
    // They look identical on a page and have entirely different fixes, which
    // is the whole reason the engine reports them apart.
    expect(
      fleetDoorFrom({ localEligible: false, fleetInferenceInstalled: false, localModelCount: 0 })
        .said,
    ).toMatch(/cannot place fleet model calls at all/);
    expect(
      fleetDoorFrom({ localEligible: false, fleetInferenceInstalled: true, localModelCount: 0 })
        .said,
    ).toMatch(/No machine you own is offering a model/);
  });

  it("names the floor when machines are there but none clears it", () => {
    const door = fleetDoorFrom({
      localEligible: false,
      fleetInferenceInstalled: true,
      localModelCount: 2,
      minimumContextWindow: 32000,
    });
    expect(door.state).toBe("unset");
    expect(door.said).toMatch(/offer 2 models/);
    expect(door.said).toMatch(/32,000-token floor/);
  });

  it("calls no reading unknown rather than closed", () => {
    expect(fleetDoorFrom(null).state).toBe("unknown");
  });
});

describe("which ids a draft is still missing", () => {
  it("names them for each vendor, and never counts the optional workspace", () => {
    expect(missingFederationFields("anthropic", {}).map((f) => f.key)).toEqual([
      "ruleId",
      "organizationId",
      "serviceAccountId",
    ]);
    expect(
      missingFederationFields("anthropic", {
        ruleId: "fdrl_1",
        organizationId: "org-1",
        serviceAccountId: "sa-1",
      }),
    ).toEqual([]);
    expect(
      missingFederationFields("openai", { identityProviderId: "idp_1" }).map((f) => f.key),
    ).toEqual(["serviceAccountId"]);
  });

  it("treats whitespace as missing, because the engine trims before it counts", () => {
    expect(
      missingFederationFields("openai", { identityProviderId: "  ", serviceAccountId: "x" }),
    ).toHaveLength(1);
  });
});

describe("Settings -> AI providers: there is no key", () => {
  it("offers no field, button or label that would take one", async () => {
    const { container } = await renderProviders();
    expect(screen.queryByLabelText(/api key/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /seal/i })).toBeNull();
    expect(container.innerHTML).not.toContain(PLANTED_KEY);
    // No input on the surface is a credential box: every one of them takes a
    // federation id, which is public by construction.
    for (const input of container.querySelectorAll("input")) {
      expect(input.getAttribute("type")).not.toBe("password");
    }
    // The negative control. Without it this asserts only that the sweep ran.
    expect(container.innerHTML).toContain("chat54Mini");
  });

  it("never offers a key as a route in the section's own copy", async () => {
    const { container } = await renderProviders();
    // The retired claim, verbatim. The 2026-08-22 record said OpenAI
    // published no federation; that was false when written -- OpenAI's has
    // been generally available since 2025-05-26.
    expect(container.textContent).not.toMatch(/publishes no federation/i);
    expect(container.textContent).not.toMatch(/paste[^.]{0,20}key/i);
    expect(container.textContent).toMatch(/federation is the only door/i);
  });
});

describe("Settings -> AI providers: the three doors", () => {
  it("renders a federated vendor, an unset one and the fleet, each distinctly", async () => {
    await renderProviders();
    expect(doorWordIn("OpenAI")).toBe(DOOR_WORDS.open);
    expect(doorStateIn("OpenAI")).toBe("open");
    expect(doorWordIn("Anthropic")).toBe(DOOR_WORDS.unset);
    expect(doorStateIn("Anthropic")).toBe("unset");
    expect(doorStateIn("Your machines")).toBe("open");
    // Distinct is the whole requirement, and the word carries it in greyscale.
    expect(doorWordIn("OpenAI")).not.toBe(doorWordIn("Anthropic"));
  });

  it("styles an unset door as normal, not as a failure", async () => {
    h.state.providers = [];
    await renderProviders();
    const anthropic = screen.getByRole("region", { name: "Anthropic" });
    expect(anthropic.querySelector(".os-door")?.getAttribute("data-os-door")).toBe("unset");
    // Not an alert, not an error tone: a cluster with no federated vendor is
    // how every cluster is installed and the permanent state of every local
    // one. An operator who meets a red banner concludes the install failed.
    expect(within(anthropic).queryByRole("alert")).toBeNull();
    expect(anthropic.querySelector('[data-tone="error"]')).toBeNull();
    expect(anthropic.textContent).toMatch(/normal state of a new cluster/i);
    // And it does not open by listing what is blank. An untouched form has
    // failed nothing, and naming its empty fields on arrival is how the
    // normal state comes to read as a list of complaints.
    expect(anthropic.textContent).not.toMatch(/Still needed/);
  });

  it("says what saving would do to a door that is already open", async () => {
    // An empty form under "Open" reads as unfinished. Rotating onto a
    // different service account is a real act and must stay reachable -- it
    // is just not the act the panel is about, so the caption says so.
    await renderProviders();
    const openai = screen.getByRole("region", { name: "OpenAI" });
    expect(openai.textContent).toMatch(/already in use/i);
    expect(openai.textContent).toMatch(/replaces them at the next Apply/i);
  });

  it("names the missing ids once somebody is filling the form in, and not before", async () => {
    h.state.providers = [];
    await renderProviders();
    const openai = screen.getByRole("region", { name: "OpenAI" });
    expect(openai.textContent).not.toMatch(/Still needed/);
    fireEvent.change(within(openai).getByLabelText("Identity provider id"), {
      target: { value: "idp_1" },
    });
    expect(openai.textContent).toMatch(/Still needed: Service account id/);
  });

  it("tells a half-set vendor to re-enter every id, because the write stores the set whole", async () => {
    // The engine's sentence above names two of three ids as already SET. A
    // form that then demanded only the third would create a set nobody had
    // checked; one that silently demanded all three would look broken.
    h.state.providers = [
      {
        name: "streamClaudeSonnet",
        vendor: "anthropic",
        model: "claude-sonnet-5",
        available: false,
        authSource: "unresolved",
        reason: HALF_SET_REASON,
      },
    ];
    await renderProviders();
    const anthropic = screen.getByRole("region", { name: "Anthropic" });
    expect(anthropic.textContent).toMatch(/re-enter every id/i);
    // And it must not contradict the engine by calling those two ids missing
    // before anybody has touched the form.
    expect(anthropic.textContent).not.toMatch(/Still needed/);
  });

  it("renders a half-configured door as the actionable one, in the engine's own words", async () => {
    h.state.providers = [
      {
        name: "streamClaudeSonnet",
        vendor: "anthropic",
        model: "claude-sonnet-5",
        available: false,
        authSource: "unresolved",
        reason: HALF_SET_REASON,
      },
    ];
    await renderProviders();
    expect(doorStateIn("Anthropic")).toBe("half");
    expect(doorWordIn("Anthropic")).toBe(DOOR_WORDS.half);
    const anthropic = screen.getByRole("region", { name: "Anthropic" });
    // The consequence, said plainly -- this is the state that takes the fleet
    // down at its next restart, hours after the save that caused it.
    expect(anthropic.textContent).toMatch(/refuses to boot/i);
    // And which ids, verbatim: the engine already names both halves better
    // than a re-derivation here would.
    expect(within(anthropic).getByText(/serviceAccountId missing/)).toBeTruthy();
  });
});

describe("Settings -> AI providers: applying federation", () => {
  it("saves Anthropic's ids under its own vendor, without the optional workspace", async () => {
    await renderProviders();
    const anthropic = screen.getByRole("region", { name: "Anthropic" });
    for (const [label, value] of [
      ["Federation rule id", "fdrl_1"],
      ["Organization id", "org-1"],
      ["Service account id", "sa-1"],
    ] as const) {
      fireEvent.change(within(anthropic).getByLabelText(label), { target: { value } });
    }
    fireEvent.click(within(anthropic).getByRole("button", { name: "Save Anthropic federation" }));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(h.state.federationCalls).toEqual([
      { vendor: "anthropic", ruleId: "fdrl_1", organizationId: "org-1", serviceAccountId: "sa-1" },
    ]);
    // The projected token path is a deployment fact, already base env beside
    // the volume it names. A box for it here could only disagree with the
    // mount, and a path that disagrees with the mount refuses boot.
    expect(h.state.federationCalls[0]?.["identityTokenFile"]).toBeUndefined();
  });

  it("saves OpenAI's two ids under its own vendor", async () => {
    // The other half of the vendor argument: one of the ids is spelled the
    // same for both vendors, so a write that did not say which vendor it was
    // could be read as either.
    h.state.providers = [];
    await renderProviders();
    const openai = screen.getByRole("region", { name: "OpenAI" });
    fireEvent.change(within(openai).getByLabelText("Identity provider id"), {
      target: { value: "idp_1" },
    });
    fireEvent.change(within(openai).getByLabelText("Service account id"), {
      target: { value: "svac_1" },
    });
    fireEvent.click(within(openai).getByRole("button", { name: "Save OpenAI federation" }));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(h.state.federationCalls).toEqual([
      { vendor: "openai", identityProviderId: "idp_1", serviceAccountId: "svac_1" },
    ]);
  });

  it("will not save a partial set, and names what is still missing", async () => {
    h.state.providers = [];
    await renderProviders();
    const openai = screen.getByRole("region", { name: "OpenAI" });
    fireEvent.change(within(openai).getByLabelText("Identity provider id"), {
      target: { value: "idp_1" },
    });
    // One of the two required ids is still blank. A partial set REFUSES BOOT,
    // so accepting one here would take the fleet down at its next restart.
    expect(
      within(openai)
        .getByRole("button", { name: "Save OpenAI federation" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(openai.textContent).toMatch(/Service account id/);
    expect(h.state.federationCalls).toEqual([]);
  });
});

describe("Settings -> AI providers: a local cluster cannot federate", () => {
  it("withholds both forms and says why, rather than offering ids that cannot work", async () => {
    h.state.providers = [];
    await renderProviders("owner", "memql.localhost");
    for (const vendor of ["Anthropic", "OpenAI"]) {
      const panel = screen.getByRole("region", { name: vendor });
      expect(panel.querySelector(".os-door")?.getAttribute("data-os-door")).toBe("unset");
      expect(panel.querySelector(".os-door-state")?.textContent).toBe(DOOR_WORDS.closed);
      expect(within(panel).queryByRole("button", { name: /Save .* federation/ })).toBeNull();
      expect(within(panel).queryByLabelText(/Service account id/)).toBeNull();
      expect(panel.textContent).toMatch(/local cluster/i);
    }
  });

  it("puts the fleet first, because on a local cluster it is the only door", async () => {
    // Read as a person reads it: the heading each panel shows, in order.
    const headings = (root: Element) =>
      [...root.querySelectorAll(".os-door-panel .os-subhead")].map((el) => el.textContent);
    const local = await renderProviders("owner", "memql.localhost");
    expect(headings(local.container)).toEqual(["Your machines", "Anthropic", "OpenAI"]);
    local.unmount();
    // The negative control: on a cloud cluster federation leads, because
    // there the order is the recommendation.
    const cloud = await renderProviders("owner", "example.com");
    expect(headings(cloud.container)).toEqual(["Anthropic", "OpenAI", "Your machines"]);
  });

  it("offers the form while the domain is still unread, rather than guessing local", async () => {
    h.state.providers = [];
    await renderProviders("owner", "");
    const openai = screen.getByRole("region", { name: "OpenAI" });
    expect(within(openai).getByLabelText("Identity provider id")).toBeTruthy();
    expect(openai.textContent).not.toMatch(/local cluster/i);
  });
});

describe("Settings -> AI providers: who may see it", () => {
  it("admits a developer and refuses a writer", async () => {
    // D7: a developer helps an owner through setup, so the four provider
    // builtins move to the owner-or-developer SET. It is a SET rather than a
    // floor because the ladder puts admin (200) BELOW developer (300) --
    // `{ min: "developer" }` would admit admin, whose concern is user
    // administration, and offer them a form the engine refuses field by field.
    expect(roleAdmits("developer", PROVIDERS_SECTION_ROLE)).toBe(true);
    expect(roleAdmits("owner", PROVIDERS_SECTION_ROLE)).toBe(true);
    expect(roleAdmits("admin", PROVIDERS_SECTION_ROLE)).toBe(false);
    expect(roleAdmits("writer", PROVIDERS_SECTION_ROLE)).toBe(false);
    expect(roleAdmits("reader", PROVIDERS_SECTION_ROLE)).toBe(false);
  });

  it("declares the same requirement in the manifest as in the section", () => {
    // Two copies of a gate is how they drift. This is the one that fails when
    // somebody widens one of them.
    const settings = OS_REGISTRY.apps.find((a) => a.id === "settings");
    expect(settings?.sections?.find((s) => s.id === "providers")?.roles).toEqual(
      PROVIDERS_SECTION_ROLE,
    );
  });

  it("renders the whole surface for a developer session", async () => {
    await renderProviders("developer");
    expect(screen.getByRole("region", { name: "Anthropic" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "OpenAI" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Apply" })).toBeTruthy();
  });
});

describe("Settings -> AI providers: the registry it can reach", () => {
  it("says what can be called, and names the credential source per provider", async () => {
    await renderProviders();
    const registry = screen.getByRole("region", { name: "What this node can call" });
    expect(within(registry).getByText(/1 of 2 providers can be called./)).toBeTruthy();
    expect(within(registry).getByText(/workload identity/)).toBeTruthy();
    expect(within(registry).getByText(/nothing configured/)).toBeTruthy();
  });

  it("renders a vendor's refusal as the vendor's answer, not as a fault of ours", async () => {
    h.state.verifyReply = { verified: false, reason: "invalid x-api-key" };
    await renderProviders();
    const registry = screen.getByRole("region", { name: "What this node can call" });
    fireEvent.click(
      within(registry).getByRole("button", { name: "Verify chat54Mini with the vendor" }),
    );
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(screen.getByText(/invalid x-api-key/)).toBeTruthy();
  });

  it("reaches no vendor until somebody presses Verify", async () => {
    await renderProviders();
    // Rendering must not spend a person's quota with a third party. The check
    // is a control, never something a panel does on open.
    expect(h.connection.query.providerVerify).not.toHaveBeenCalled();
  });

  it("renders a server refusal in surface, in the engine's own words", async () => {
    h.state.providerError = new Error("providerAuthStatus is owner-only");
    await renderProviders("admin");
    // The section is a role SET in the manifest, so an admin should never
    // reach it -- but presentation is not the authorization, and if they do,
    // the engine's own sentence is what they read.
    expect(screen.getByText(/declined this read for admin/)).toBeTruthy();
    expect(screen.getByText("providerAuthStatus is owner-only")).toBeTruthy();
  });

  it("keeps the fleet door standing when the provider read is refused", async () => {
    // Two readings, settling separately. A refusal of one must never decide
    // the state of the other -- they have different reasons to fail.
    h.state.providerError = new Error("providerAuthStatus is owner-only");
    await renderProviders("admin");
    expect(screen.getByRole("region", { name: "Your machines" })).toBeTruthy();
  });

  it("says the fleet reading failed rather than calling the door shut", async () => {
    h.state.inferenceError = new Error("inferenceStatus: not connected");
    await renderProviders();
    const fleet = screen.getByRole("region", { name: "Your machines" });
    expect(fleet.querySelector(".os-door")?.getAttribute("data-os-door")).toBe("unknown");
    expect(within(fleet).getByText("inferenceStatus: not connected")).toBeTruthy();
  });

  it("separates saving from applying, and says what Apply did", async () => {
    await renderProviders();
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(h.state.reloadCalls).toBe(1);
    expect(screen.getByText(/can call 1 of 2 providers/)).toBeTruthy();
  });
});
