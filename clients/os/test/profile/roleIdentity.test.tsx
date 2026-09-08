import { describe, it, expect, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";

import { RoleIdentity, describeRole, placeOnLadder } from "../../src/modules/profile/RoleIdentity";
import { setRoleLadder } from "../../src/system/roles";
import { SEEDED_LADDER } from "../seededLadder";
import type { ProfileAccess } from "../../src/modules/profile/access";

// WHAT THE SHELL SAYS A ROLE IS (epic memql#5166).
//
// The surface printed one thing -- the slug, in mono -- because a role was an
// enum value out of five. A cluster authors its own roles now, and the fact
// worth reading is the NAME with its place on the ladder beside it. `rank 150`
// is a machine fact; "above Member" is what it means.

function access(over: Partial<ProfileAccess>): ProfileAccess {
  return {
    userId: "v1:identity:user:me",
    primaryEmail: "me@example.test",
    role: "owner",
    roleName: "Owner",
    rank: 400,
    ...over,
  };
}

const SUPPORT_LEAD = { slug: "support-lead", name: "Support Lead", rank: 150, aliases: [] };

beforeEach(() => {
  setRoleLadder([...SEEDED_LADDER, SUPPORT_LEAD]);
});

describe("the role a person reads", () => {
  it("names the role and places it against the rung below", () => {
    render(<RoleIdentity access={access({ role: "support-lead", roleName: "Support Lead", rank: 150 })} />);

    expect(screen.getByText("Support Lead")).toBeTruthy();
    // THE SLUG SURVIVES, because it is what somebody pastes into a support
    // thread, and the PLACE is what makes a role nobody has heard of legible.
    expect(screen.getByText("support-lead")).toBeTruthy();
    expect(screen.getByText(/above Member/)).toBeTruthy();
  });

  it("does not print the rank as a number", () => {
    render(<RoleIdentity access={access({ role: "support-lead", roleName: "Support Lead", rank: 150 })} />);

    // 150 means nothing on its own. Asserting its ABSENCE is the point: the
    // obvious implementation prints it, and the number reads as information
    // while telling a reader less than the neighbour's name does.
    expect(screen.queryByText(/150/)).toBeNull();
  });

  it("says so when the role is the top of the ladder", () => {
    render(<RoleIdentity access={access({})} />);

    expect(screen.getByText("Owner")).toBeTruthy();
    expect(screen.getByText(/the highest role/)).toBeTruthy();
  });

  it("prints an unnamed role's slug ONCE, not twice", () => {
    // A role deactivated under its holder, or a node whose catalog has not
    // loaded: the engine sends the slug and leaves the name empty. Rendering
    // the slug is honest; inventing a title for a role the engine will refuse
    // this person everything for is not.
    //
    // ONCE IS THE ASSERTION, and it came from LOOKING at the surface rather
    // than from reasoning about it. `describeRole` falls back to the slug, so
    // the obvious two-line form rendered `retired-lead` above `retired-lead`,
    // which reads as a rendering fault. Every string on the screen was correct
    // on its own, so no assertion about which strings are PRESENT would have
    // caught it -- only the count does.
    render(<RoleIdentity access={access({ role: "retired-lead", roleName: "", rank: 0 })} />);

    expect(screen.getAllByText("retired-lead")).toHaveLength(1);
    expect(screen.getByText("This cluster has no role by that name.")).toBeTruthy();
  });

  it("points at the repair when there is no role at all", () => {
    render(<RoleIdentity access={access({ role: "", roleName: "", rank: 0 })} />);

    expect(screen.getByText("Unknown")).toBeTruthy();
    // An empty state is a moment for direction. The fix is somebody else's to
    // make, which is worth saying rather than leaving the reader to guess.
    expect(screen.getByText(/An operator can set it again/)).toBeTruthy();
  });

  it("renders the name alone inline, for a sentence", () => {
    render(
      <p>
        You are <RoleIdentity access={access({ role: "support-lead", roleName: "Support Lead", rank: 150 })} inline />
      </p>,
    );

    expect(screen.getByText(/You are/)).toBeTruthy();
    expect(screen.queryByText("support-lead")).toBeNull();
  });
});

describe("describeRole", () => {
  it("prefers the name, falls through to the slug, and says unrecognised for neither", () => {
    expect(describeRole(access({ roleName: "Support Lead" }))).toBe("Support Lead");
    expect(describeRole(access({ role: "support-lead", roleName: "" }))).toBe("support-lead");
    expect(describeRole(access({ role: "", roleName: "" }))).toBe("an unrecognised role");
    expect(describeRole(null)).toBe("an unrecognised role");
  });
});

describe("placeOnLadder", () => {
  it("reads the LIVE ladder rather than the rank the wire sent", () => {
    // The interesting comparison is against the roles this cluster has. With
    // support-lead removed, the same access resolves to no rung at all -- which
    // is the honest answer, not "above Member" from a stale number.
    setRoleLadder(SEEDED_LADDER);
    expect(placeOnLadder(access({ role: "support-lead", roleName: "Support Lead", rank: 150 }))).toBe("");
  });

  it("is empty before the ladder loads", () => {
    setRoleLadder([]);
    expect(placeOnLadder(access({}))).toBe("");
  });

  it("names the rung below on the seeded ladder", () => {
    setRoleLadder(SEEDED_LADDER);
    expect(placeOnLadder(access({ role: "admin", roleName: "Admin", rank: 200 }))).toBe("above Member");
    expect(placeOnLadder(access({ role: "owner", roleName: "Owner", rank: 400 }))).toBe("the highest role");
  });
});
