import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({ connection: null as unknown }));

vi.mock("../../src/live/connection", () => ({
  useOsConnection: () => h.connection,
  bridgePathFor: (base: string) => base + "_memql/ws",
  osBridgePath: "/_memql/ws",
}));

const { useAccountOptions } = await import("../../src/apps/accounts/tie");
const { accountRow, fakeConnection, withSession } = await import("./harness");

// ===========================================================================
// THE TIE PICKER'S FALLBACK (epic memql#5167, section D)
// ===========================================================================
// A client-rank person cannot read `v1:accounts:account` at all -- the row's
// composite owner tier admits its owner and a cluster owner, and they are
// neither -- so this read answers EMPTY for them. A picker with no options
// would let a Member of Acme tie their campaign to nobody, and their work
// would land where their colleagues cannot see it: the opposite of what the
// tie is for.

type Groups = NonNullable<Parameters<typeof withSession>[1]>["groups"];

function options(connection: unknown, groups?: Groups) {
  h.connection = connection;
  return renderHook(() => useAccountOptions(), {
    wrapper: ({ children }) => withSession(children, groups === undefined ? {} : { groups }),
  });
}

describe("the tie picker's options", () => {
  it("offers the caller's own clients from their MyAccess groups when the read is empty", async () => {
    const { result } = options(fakeConnection({ clientAccountsAll: [] }), [
      { id: "g1", name: "Acme", kind: "account", accountId: "acct-acme", accountName: "Acme" },
      { id: "g2", name: "Acme leads", kind: "custom", accountId: "acct-acme", accountName: "Acme" },
      { id: "g3", name: "Borden", kind: "account", accountId: "acct-b", accountName: "Borden Ltd" },
    ]);

    await waitFor(() => expect(result.current.length).toBe(2));
    // ONE OPTION PER CLIENT, not per group: two groups of the same client are
    // one client to tie work to.
    expect(result.current.map((a) => a.id).sort()).toEqual(["acct-acme", "acct-b"]);
    expect(result.current.find((a) => a.id === "acct-b")?.name).toBe("Borden Ltd");
  });

  it("prefers the rows when the caller can read any", async () => {
    // A caller who can read accounts gets the ROWS -- richer, current, and
    // including clients they are not a member of. It is a fallback, not a
    // merge.
    const { result } = options(
      fakeConnection({
        clientAccountsAll: [accountRow({ id: "acct-real", name: "From the registry" })],
      }),
      [{ id: "g1", name: "Acme", kind: "account", accountId: "acct-acme", accountName: "Acme" }],
    );

    await waitFor(() => expect(result.current.length).toBe(1));
    expect(result.current[0]?.id).toBe("acct-real");
  });

  it("offers nothing when the cluster reports no groups", async () => {
    // ABSENT means "not reported" as well as "none" -- a cluster whose engine
    // predates the field sends nothing -- and neither is a licence to invent
    // an option.
    const { result } = options(fakeConnection({ clientAccountsAll: [] }));
    await waitFor(() => expect(result.current).toEqual([]));
  });
});
