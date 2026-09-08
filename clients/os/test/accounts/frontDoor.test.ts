import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  currentDoor,
  DOOR_ROLES,
  doorSentence,
  doorTone,
  frontDoorFingerprint,
  frontDoorFromRow,
  hostsFor,
  isKnownReservationReason,
  pointingTarget,
  reservationReasonSentence,
  type FrontDoorRow,
} from "../../src/apps/accounts/frontDoor";

function door(over: Partial<FrontDoorRow> = {}): FrontDoorRow {
  return {
    id: "door-1",
    accountId: "acct-1",
    reservedName: "memql.acme.com",
    status: "pending_dns",
    hostChecks: {},
    failureReason: "",
    failureDetail: "",
    lastCheckedAt: "",
    verifiedAt: "",
    issuedAt: "",
    removedAt: "",
    createdAt: "2026-09-08T00:00:00Z",
    ...over,
  };
}

describe("the three hosts", () => {
  it("composes one host per role under the reserved name", () => {
    const hosts = hostsFor(door());
    expect(hosts.map((h) => h.host)).toEqual([
      "app.memql.acme.com",
      "api.memql.acme.com",
      "id.memql.acme.com",
    ]);
  });

  // A role with NO recorded check reads as pending, not as a failure. Absent
  // is "nobody has looked yet", which is what a door looks like in the seconds
  // between being opened and the sweep's first pass -- a different statement
  // from "we looked and it is wrong".
  it("reads an unchecked host as pending rather than wrong", () => {
    expect(hostsFor(door()).map((h) => h.state)).toEqual(["pending", "pending", "pending"]);
  });

  it("marks only the hosts that failed, and carries what was seen at them", () => {
    const hosts = hostsFor(
      door({
        hostChecks: {
          app: { ok: true, reason: "", detail: "" },
          api: { ok: true, reason: "", detail: "" },
          id: { ok: false, reason: "dns_not_pointing", detail: "resolves to 198.51.100.7" },
        },
      }),
    );
    expect(hosts.map((h) => h.state)).toEqual(["ok", "ok", "wrong"]);
    expect(hosts.find((h) => h.role === "id")?.observed).toBe("resolves to 198.51.100.7");
    // A host that is FINE carries no observation: repeating the target three
    // times says nothing, and the stop prints "points here" instead.
    expect(hosts.find((h) => h.role === "app")?.observed).toBe("");
  });

  // ONE TARGET FOR ALL THREE, which is the fact the whole stop layout rests
  // on: one ingress controller terminates them all and routes by Host.
  it("points every host at one target", () => {
    expect(pointingTarget("memql.localhost")).toBe("os.memql.localhost");
  });
});

// THE MIRROR. component/frontdoor.AccountRoles() is the ONE place these labels
// are spelled in the engine, and this array is the OS's copy. They are read
// out of each other's source rather than restated, so a change on either side
// that the other does not follow fails here -- and the failure it prevents is
// a client's employee typing a host nothing serves.
describe("the labels the engine spells", () => {
  it("matches component/frontdoor.AccountRoles", () => {
    const go = readFileSync(
      resolve(__dirname, "../../../../component/frontdoor/account_hosts.go"),
      "utf8",
    );

    // THE STRING VALUES, NOT THE GO IDENTIFIERS. The first version of this
    // test matched /AccountRole(App|API|ID)/ and passed happily while the
    // engine's `"id"` was renamed to `"identity"` -- it was asserting that Go
    // still spells its own constant names the same way, which nothing was
    // ever going to change. What reaches a client's employee is the VALUE.
    const values = new Map(
      [...go.matchAll(/\b(AccountRole\w+)\s+AccountRole\s*=\s*"([a-z]+)"/g)].map(
        (m) => [m[1], m[2]] as const,
      ),
    );
    expect(values.size).toBeGreaterThan(0);

    // And the ORDER, from AccountRoles() itself -- the OS renders them in that
    // order, and a person reads the shell's host first.
    const body = go.slice(go.indexOf("func AccountRoles()"));
    const ordered = [...body.slice(0, body.indexOf("}")).matchAll(/\bAccountRole\w+\b/g)]
      .map((m) => values.get(m[0]))
      .filter((v): v is string => Boolean(v));

    expect(ordered).toEqual(DOOR_ROLES.map((r) => r.role));
  });
});

describe("the sentence at the top", () => {
  it("answers 'what now' in every state, including the settled ones", () => {
    for (const status of ["pending_dns", "verifying", "issuing", "live", "removing", "removed"]) {
      expect(doorSentence(door({ status }))).not.toBe("");
    }
  });

  // A cluster with no ACME issuer sits at `issuing` forever, and that is the
  // correct local answer rather than a stuck state -- so the sentence must say
  // what is actually true rather than "waiting for the certificate".
  it("says a cluster that issues nothing issues nothing", () => {
    const s = doorSentence(door({ status: "issuing", failureReason: "no_acme_issuer" }));
    expect(s).toContain("issues no certificates");
    expect(s).not.toContain("Waiting");
  });

  it("tones a serving door apart from a stuck one", () => {
    expect(doorTone(door({ status: "live" }))).toBe("ok");
    expect(doorTone(door({ status: "verifying" }))).toBe("warn");
    expect(doorTone(door({ status: "issuing", failureReason: "issuance_failed" }))).toBe("error");
    expect(doorTone(door({ status: "removed" }))).toBe("muted");
  });
});

// The ask this field answers: an absent memqlReservedAt used to mean two
// different things, and the rail inferred which from the ownership stop beside
// it.
describe("why a name is not held", () => {
  it("distinguishes unproven ownership from a refused name", () => {
    expect(reservationReasonSentence("ownership_unproven")).toContain("not verified");
    expect(reservationReasonSentence("domain_under_cluster_domain")).toContain(
      "under this cluster's own domain",
    );
    expect(reservationReasonSentence("ownership_unproven")).not.toBe(
      reservationReasonSentence("domain_under_cluster_domain"),
    );
  });

  it("says nothing rather than inventing a sentence for a code it does not know", () => {
    expect(reservationReasonSentence("something_new")).toBe("");
    expect(isKnownReservationReason("something_new")).toBe(false);
  });
});

// A HEARTBEAT IS NOT NEWS. lastCheckedAt moves every two minutes for every
// unsettled door, forever; fingerprinting it would turn the stop into a strobe.
describe("the arrival cue", () => {
  it("does not move when only the heartbeat did", () => {
    const before = door({ lastCheckedAt: "2026-09-08T12:00:00Z" });
    const after = door({ lastCheckedAt: "2026-09-08T12:02:00Z" });
    expect(frontDoorFingerprint(after)).toBe(frontDoorFingerprint(before));
  });

  it("does not move when only a resolver's wording did", () => {
    const before = door({ hostChecks: { id: { ok: false, reason: "dns_not_pointing", detail: "a" } } });
    const after = door({ hostChecks: { id: { ok: false, reason: "dns_not_pointing", detail: "b" } } });
    expect(frontDoorFingerprint(after)).toBe(frontDoorFingerprint(before));
  });

  it("moves when the status or the reason does", () => {
    expect(frontDoorFingerprint(door({ status: "live" }))).not.toBe(frontDoorFingerprint(door()));
    expect(frontDoorFingerprint(door({ failureReason: "no_acme_issuer" }))).not.toBe(
      frontDoorFingerprint(door()),
    );
  });
});

describe("picking the door to show", () => {
  it("prefers the one that is not settled", () => {
    const rows = [
      door({ id: "old", status: "removed", createdAt: "2026-09-01T00:00:00Z" }),
      door({ id: "now", status: "verifying", createdAt: "2026-09-08T00:00:00Z" }),
    ];
    expect(currentDoor(rows, "acct-1")?.id).toBe("now");
  });

  // "We served this and stopped" is a different answer from "we never did",
  // and the row surviving removal is what makes it sayable.
  it("falls back to the most recent removed one", () => {
    const rows = [
      door({ id: "older", status: "removed", createdAt: "2026-09-01T00:00:00Z" }),
      door({ id: "newer", status: "removed", createdAt: "2026-09-05T00:00:00Z" }),
    ];
    expect(currentDoor(rows, "acct-1")?.id).toBe("newer");
  });

  it("shows nothing for an account with no door", () => {
    expect(currentDoor([door()], "somebody-else")).toBeNull();
  });
});

describe("projection", () => {
  it("survives a row with no hostChecks at all", () => {
    const row = frontDoorFromRow({ id: "d", accountId: "a", reservedName: "memql.acme.com" } as never);
    expect(row.hostChecks).toEqual({});
    expect(hostsFor(row).every((h) => h.state === "pending")).toBe(true);
  });

  it("ignores a hostChecks entry of the wrong shape rather than throwing", () => {
    const row = frontDoorFromRow({
      id: "d",
      accountId: "a",
      reservedName: "memql.acme.com",
      hostChecks: { app: "not an object", api: { ok: true } },
    } as never);
    expect(row.hostChecks.app).toBeUndefined();
    expect(row.hostChecks.api).toEqual({ ok: true, reason: "", detail: "" });
  });
});
