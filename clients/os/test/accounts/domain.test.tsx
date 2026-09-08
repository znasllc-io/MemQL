import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({ connection: null as unknown }));

vi.mock("../../src/live/connection", () => ({
  useOsConnection: () => h.connection,
  bridgePathFor: (base: string) => base + "_memql/ws",
  osBridgePath: "/_memql/ws",
}));

const { AccountsApp } = await import("../../src/apps/accounts/AccountsApp");
const { LocalAccountsSettingsStore } = await import("../../src/apps/accounts/settings");
const { accountFingerprint, accountFromRow } = await import("../../src/apps/accounts/rows");
const { accountRow, fakeConnection, withSession } = await import("./harness");

type Conn = ReturnType<typeof fakeConnection>;

function memoryStore() {
  const bag = new Map<string, string>();
  return new LocalAccountsSettingsStore({
    getItem: (k: string) => bag.get(k) ?? null,
    setItem: (k: string, v: string) => void bag.set(k, v),
  });
}

async function openDetail(connection: Conn): Promise<HTMLElement> {
  h.connection = connection;
  render(
    withSession(
      <AccountsApp sectionId="accounts" navigate={vi.fn()} askContext={() => {}} store={memoryStore()} />,
    ),
  );
  const row = await screen.findByText("Acme Consulting");
  await act(async () => {
    fireEvent.click(row.closest("button") as HTMLElement);
  });
  return await screen.findByRole("region", { name: /The domain of/ });
}

/** One stop of the rail, by the name a person reads. */
function stop(rail: HTMLElement, name: string): HTMLElement {
  const label = within(rail).getByText(name);
  const li = label.closest("li");
  if (li === null) throw new Error(`no stop called ${name}`);
  return li as HTMLElement;
}

/**
 * Open a stop and hand back its `<li>`.
 *
 * ONE STOP IS OPEN AT A TIME on this rail -- it is a record, and four open
 * bodies would be four panels with a line down them -- so a test that asks
 * about a closed stop's body is asking about a node the browser has not been
 * asked to render. The same reasoning `test/selectControl.ts` states for the
 * kit's listbox.
 */
async function openStop(rail: HTMLElement, name: string): Promise<HTMLElement> {
  const li = stop(rail, name);
  // The stop's OWN disclosure line, not whatever buttons its body holds: an
  // open stop's body carries three Copy controls, and `queryByRole("button")`
  // would find four.
  const line = li.querySelector(".os-rail-line") as HTMLElement | null;
  if (line !== null && line.getAttribute("aria-expanded") !== "true") {
    await act(async () => {
      fireEvent.click(line);
    });
  }
  return stop(rail, name);
}

// ===========================================================================
// FOUR STOPS, FROM FOUR ROW STATES
// ===========================================================================
// The rail is a rail because the stops depend on each other: there is a
// domain, ownership of it is proven, joining can THEN be turned on, and a
// MemQL name can then be reserved. Four panels would draw the same fields and
// say nothing about that order.

describe("the domain rail", () => {
  it("draws the ownership record as three copyable parts", async () => {
    // A person is pasting these into three different fields of a registrar's
    // form; one blob of text is one they have to split by hand.
    const rail = await openDetail(
      fakeConnection({ clientAccountsAll: [accountRow({ id: "a1", domain: "acme.com" })] }),
    );
    const ownership = await openStop(rail, "Ownership");
    expect(within(ownership).getByText("TXT")).toBeTruthy();
    expect(within(ownership).getByText("_memql-verify.acme.com")).toBeTruthy();
    expect(within(ownership).getByText("memql-verify-abc123")).toBeTruthy();
    expect(within(ownership).getByRole("button", { name: /Copy Value/ })).toBeTruthy();
  });

  it("says there is nothing to press, because the walk is on its own schedule", async () => {
    const rail = await openDetail(
      fakeConnection({ clientAccountsAll: [accountRow({ id: "a1", domain: "acme.com" })] }),
    );
    expect(
      within(rail).getByText(/Checked every two minutes on its own; there is nothing to press/),
    ).toBeTruthy();
    expect(within(rail).queryByRole("button", { name: /Check/ })).toBeNull();
  });

  it("reads the typed failure reason and what the lookup actually saw", async () => {
    const rail = await openDetail(
      fakeConnection({
        clientAccountsAll: [
          accountRow({
            id: "a1",
            domain: "acme.com",
            domainStatus: "verifying",
            domainFailureReason: "dns_token_missing",
            domainFailureDetail: "the zone answered with no TXT records",
            domainLastCheckedAt: "2026-09-07T10:00:00Z",
          }),
        ],
      }),
    );
    const ownership = await openStop(rail, "Ownership");
    expect(within(ownership).getByText("The record is not there yet.")).toBeTruthy();
    expect(within(ownership).getByText("the zone answered with no TXT records")).toBeTruthy();
  });

  it("offers NO joining control before ownership is proven", async () => {
    // The engine refuses `domain_not_verified`, and a checkbox that can only
    // fail is one somebody has to read past to learn it is not for them.
    const rail = await openDetail(
      fakeConnection({ clientAccountsAll: [accountRow({ id: "a1", domain: "acme.com" })] }),
    );
    const joining = await openStop(rail, "Joining");
    expect(within(joining).getByText("Prove ownership first.")).toBeTruthy();
    expect(within(joining).queryByRole("checkbox")).toBeNull();
  });

  it("offers it once ownership is proven, and writes through updateClientAccount", async () => {
    const conn = fakeConnection({
      clientAccountsAll: [
        accountRow({
          id: "a1",
          domain: "acme.com",
          domainStatus: "verified",
          domainVerifiedAt: "2026-09-05T00:00:00Z",
        }),
      ],
    });
    const rail = await openDetail(conn);
    const joining = await openStop(rail, "Joining");
    const check = within(joining).getByRole("checkbox");
    await act(async () => {
      fireEvent.click(check);
    });
    await waitFor(() =>
      expect(conn.query.updateClientAccount).toHaveBeenCalledWith({
        accountId: "a1",
        joinOnDomain: true,
      }),
    );
  });

  it("names the domain_not_verified refusal at the Joining stop", async () => {
    // The engine's validation reads the MERGED payload, so flipping the flag
    // on a row whose STORED status is not verified is refused by code -- which
    // is what happens when somebody changes the domain and turns joining on in
    // the same sitting. A generic failure would leave them with a checkbox
    // that will not stay on and nothing to act on.
    const conn = fakeConnection({
      clientAccountsAll: [
        accountRow({ id: "a1", domain: "acme.com", domainStatus: "verified" }),
      ],
    });
    conn.query.updateClientAccount.mockRejectedValue(
      new Error("domain_not_verified: joinOnDomain may only be set once domainStatus is verified"),
    );
    const rail = await openDetail(conn);
    const joining = await openStop(rail, "Joining");
    await act(async () => {
      fireEvent.click(within(joining).getByRole("checkbox"));
    });
    expect(await screen.findByText("This domain is not proven yet.")).toBeTruthy();
    expect(
      screen.getByText(/joinOnDomain may only be set once domainStatus is verified/),
    ).toBeTruthy();
  });

  it("reads the self account's stops as the cluster's own", async () => {
    const rail = await openDetail(
      fakeConnection({
        clientAccountsAll: [
          accountRow({ id: "self", name: "Acme Consulting", domain: "acme.com", memqlDomain: "memql.acme.com" }),
        ],
      }),
    );
    expect(within(await openStop(rail, "Ownership")).getByText(/nothing has to be proven/)).toBeTruthy();
    expect(
      within(await openStop(rail, "MemQL address")).getByText("This cluster's own."),
    ).toBeTruthy();
  });

  it("says the MemQL name is recorded and served by nothing yet", async () => {
    const rail = await openDetail(
      fakeConnection({
        clientAccountsAll: [
          accountRow({
            id: "a1",
            domain: "acme.com",
            domainStatus: "verified",
            memqlDomain: "memql.acme.com",
            memqlReservedAt: "2026-09-06T00:00:00Z",
          }),
        ],
      }),
    );
    const memql = await openStop(rail, "MemQL address");
    expect(within(memql).getByText("app.memql.acme.com")).toBeTruthy();
    expect(
      within(memql).getByText(/Recorded now; served when the per-account front door lands/),
    ).toBeTruthy();
  });
});

// ===========================================================================
// THE FIELD THAT MOVES ON A TIMER
// ===========================================================================

describe("the arrival fingerprint", () => {
  it("excludes domainLastCheckedAt", () => {
    // The walk writes it on EVERY pass, so a digest built over it changes
    // every two minutes -- and the account list would ring on a timer, which
    // is the standing badge the cue exists not to be.
    // Projected through the real `accountFromRow`, so the fingerprint is taken
    // over the same shape the list holds rather than over a literal a test
    // wrote.
    const before = accountFingerprint(
      accountFromRow(accountRow({ id: "a1", domainLastCheckedAt: "2026-09-07T10:00:00Z" })),
    );
    const after = accountFingerprint(
      accountFromRow(accountRow({ id: "a1", domainLastCheckedAt: "2026-09-07T10:02:00Z" })),
    );
    expect(before).toBe(after);
  });

  it("does fire on the walk's real answers", () => {
    // `domainStatus` and `joinOnDomain` are what a person would call a change:
    // a domain that became proven, and joining that was switched on.
    const unproven = accountFingerprint(
      accountFromRow(accountRow({ id: "a1", domainStatus: "unverified" })),
    );
    const proven = accountFingerprint(
      accountFromRow(accountRow({ id: "a1", domainStatus: "verified" })),
    );
    expect(unproven).not.toBe(proven);
  });
});
