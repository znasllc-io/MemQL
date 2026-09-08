import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({ connection: null as unknown }));

vi.mock("../../src/live/connection", () => ({
  useOsConnection: () => h.connection,
  bridgePathFor: (base: string) => base + "_memql/ws",
  osBridgePath: "/_memql/ws",
}));

const { UsersApp } = await import("../../src/apps/users/UsersApp");
const {
  accountRow,
  adminRefusal,
  fakeConnection,
  grantRow,
  groupRow,
  roleRow,
  withSession,
} = await import("./harness");
const { click, memoryStore, renderApp } = await import("./mount");
const { domainMatch } = await import("../../src/apps/users/invite");
const { accountFromRow } = await import("../../src/apps/accounts/rows");

type Conn = ReturnType<typeof fakeConnection>;

const LADDER = [
  roleRow({ slug: "viewer", name: "Viewer", rank: 50, aliases: ["reader"] }),
  roleRow({ slug: "user", name: "Member", rank: 100, aliases: ["writer"] }),
  roleRow({ slug: "admin", name: "Admin", rank: 200 }),
  roleRow({ slug: "owner", name: "Owner", rank: 400 }),
];

const GRANTS = [
  grantRow("owner", "read", "principal"),
  grantRow("owner", "create", "principal"),
  grantRow("owner", "update", "principal"),
  grantRow("owner", "create", "admission"),
];

const ACME = accountRow({
  id: "acct-acme",
  name: "Acme",
  domain: "acme.com",
  domainStatus: "verified",
  domainVerifiedAt: "2026-09-01T00:00:00Z",
  joinOnDomain: true,
});

function seed(extra: Parameters<typeof fakeConnection>[0] = {}) {
  return fakeConnection({
    activeRoles: LADDER,
    activeCapabilities: GRANTS,
    clientAccountsAll: [ACME],
    groupsAll: [groupRow({ id: "g-acme", name: "Acme", kind: "account", accountId: "acct-acme" })],
    ...extra,
  });
}

async function openInvite(connection: Conn, role = "owner") {
  h.connection = connection;
  const view = renderApp(
    withSession(
      <UsersApp sectionId="people" navigate={() => {}} askContext={() => {}} store={memoryStore()} />,
      { role },
    ),
  );
  await click(await screen.findByRole("button", { name: "Invite" }));
  await screen.findByText("Invite somebody");
  return view;
}

async function type(label: string, value: string): Promise<void> {
  const { act, fireEvent } = await import("@testing-library/react");
  const field = screen.getByLabelText(label);
  await act(async () => {
    fireEvent.change(field, { target: { value } });
  });
}

// ===========================================================================
// THE DOMAIN MATCH
// ===========================================================================
// BOTH CONDITIONS, and neither is decoration: an UNVERIFIED domain proves
// nothing (the engine refuses `joinOnDomain` on one), and joining switched off
// means the client has asked for arrivals NOT to be placed.

describe("matching an address to a client", () => {
  const accounts = [
    accountFromRow(ACME),
    accountFromRow(
      accountRow({ id: "acct-b", name: "Beta", domain: "beta.com", domainStatus: "verifying", joinOnDomain: true }),
    ),
    accountFromRow(
      accountRow({ id: "acct-c", name: "Gamma", domain: "gamma.com", domainStatus: "verified", joinOnDomain: false }),
    ),
  ];

  it("matches a verified domain that takes people on it", () => {
    expect(domainMatch("kit@acme.com", accounts)?.id).toBe("acct-acme");
  });

  it("does not match an unproven domain", () => {
    expect(domainMatch("kit@beta.com", accounts)).toBeNull();
  });

  it("does not match a client who has joining switched off", () => {
    expect(domainMatch("kit@gamma.com", accounts)).toBeNull();
  });
});

// ===========================================================================
// THE RAIL
// ===========================================================================

describe("the Invite rail", () => {
  it("answers the Groups stop from the address's domain, and says so", async () => {
    const view = await openInvite(seed());
    await type("Email address", "kit@acme.com");

    expect(await screen.findByText(/Acme's domain, and they take people on it/)).toBeTruthy();
    // The stop's own answer line names the group that was filled in.
    await waitFor(() => expect(screen.getByText(/Acme is filled in below/)).toBeTruthy());
    view.unmount();
  });

  it("keeps the Head's action absent until the stops are answered", async () => {
    const view = await openInvite(seed());
    const bar = () => screen.getByRole("group", { name: "What you can do with this" });
    expect(within(bar()).queryByRole("button", { name: "Send invitation" })).toBeNull();

    await type("Email address", "kit@example.com");
    expect(within(bar()).queryByRole("button", { name: "Send invitation" })).toBeNull();

    const ladder = screen.getByRole("list", { name: /The role this invitation grants/ });
    await click(within(ladder).getByRole("button", { name: /Member/ }));

    await waitFor(() =>
      expect(within(bar()).getByRole("button", { name: "Send invitation" })).toBeTruthy(),
    );
    view.unmount();
  });

  it("puts group_ids on the wire", async () => {
    const connection = seed();
    connection.dispatcher.sendAndWait.mockResolvedValue({
      identityAdminResult: {
        ok: true,
        errorCode: 0,
        auditEventId: "v1:identity:auditEvent:a1",
        invitationUrl: "https://identity.example.com/invite/abc",
        emailSent: true,
      },
    });
    const view = await openInvite(connection);
    await type("Email address", "kit@acme.com");
    const ladder = screen.getByRole("list", { name: /The role this invitation grants/ });
    await click(within(ladder).getByRole("button", { name: /Member/ }));
    await click(screen.getByRole("button", { name: "Send invitation" }));

    await waitFor(() =>
      expect(connection.dispatcher.sendAndWait).toHaveBeenCalledWith(
        expect.objectContaining({
          identityAdmin: expect.objectContaining({
            issueUserInvitation: expect.objectContaining({
              email: "kit@acme.com",
              role: "user",
              groupIds: ["g-acme"],
            }),
          }),
        }),
        undefined,
      ),
    );

    // The rail BECOMES the invited person's page, with the link shown once.
    expect(await screen.findByText(/shown once/)).toBeTruthy();
    view.unmount();
  });

  it("lands a refusal at the stop that owns the value", async () => {
    const connection = seed();
    connection.dispatcher.sendAndWait.mockResolvedValue(
      adminRefusal("role_above_inviter: an inviter cannot grant above their own role"),
    );
    const view = await openInvite(connection);
    await type("Email address", "kit@example.com");
    const ladder = screen.getByRole("list", { name: /The role this invitation grants/ });
    await click(within(ladder).getByRole("button", { name: /Member/ }));
    await click(screen.getByRole("button", { name: "Send invitation" }));

    // The Role stop is where the refused value was entered; a refusal at the
    // bottom of a three-stop rail makes somebody re-read all three.
    const roleStop = await screen.findByText(/an inviter cannot grant above their own role/);
    expect(roleStop.closest("li")?.textContent).toContain("Role");
    view.unmount();
  });

  it("renders the email gate's sentence where Invite would be", async () => {
    // An app that hides Invite with no account of itself reads as a missing
    // feature, and somebody looking for it has nowhere to find out why.
    h.connection = seed();
    const view = renderApp(
      withSession(
        <UsersApp sectionId="people" navigate={() => {}} askContext={() => {}} store={memoryStore()} />,
        { role: "owner", emailReady: false },
      ),
    );
    await screen.findByText("People");
    expect(screen.queryByRole("button", { name: "Invite" })).toBeNull();
    expect(screen.getByText(/needs a mailbox/)).toBeTruthy();
    view.unmount();
  });
});
