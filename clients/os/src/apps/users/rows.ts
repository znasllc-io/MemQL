import { rowString, type Row } from "@znasllc-io/memql-sdk-core/client";

import { boolOr, flatten, stringsOf } from "../../kit/rows";

// The wire rows the Users app renders, projected into the shapes its surfaces
// read.
//
// PURE, and separate from every component, for the reason apps/fleet/rows.ts
// is: a projection asserted through render() is asserted through three layers
// that can each fail for unrelated reasons. Everything here is a function of a
// row and is unit-testable with no browser, no cluster and no React.
//
// `flatten` and `boolOr` moved to kit/rows.ts when the third app copied them.
// `boolOr` matters here more than anywhere: `user.active` and
// `invitation.active` both default to TRUE, and a folded CDC event carries
// only what the write touched -- so reading absent as false through the SDK's
// own `rowBool` makes everybody vanish from the list the first time anything
// about them changes.

// ---------------------------------------------------------------------------
// A person
// ---------------------------------------------------------------------------

/** `any` | `passkey_only`, or "" on a row written before the field existed. */
export type SignInPolicy = "" | "any" | "passkey_only";

export interface PersonRow {
  id: string;
  displayName: string;
  firstName: string;
  lastName: string;
  primaryEmail: string;
  /** The cluster-wide role: owner | admin | developer | writer | reader. */
  role: string;
  signInPolicy: SignInPolicy;
  /**
   * A mailbox several people read (memql#4477 territory). It is a quiet hint
   * rather than a column: it changes what a sign-in LINK means for this
   * address, which matters when reading the policy beside it, and it is true
   * of very few rows.
   */
  sharedMailbox: boolean;
  active: boolean;
  suspendedAt: string;
  suspendedReason: string;
  /**
   * Liveness. NEVER put this in a LiveList fingerprint: the engine churns it,
   * so naming it turns the whole list into a strobe on a timer -- the standing
   * badge the arrival cue exists not to be (clients/os/README.md).
   */
  lastSeenAt: string;
  createdAt: string;
}

export function personFromRow(raw: Row): PersonRow {
  const row = flatten(raw);
  const policy = rowString(row, "signInPolicy");
  return {
    id: rowString(row, "id"),
    displayName: rowString(row, "displayName"),
    firstName: rowString(row, "firstName"),
    lastName: rowString(row, "lastName"),
    primaryEmail: rowString(row, "primaryEmail"),
    role: rowString(row, "role"),
    signInPolicy: policy === "any" || policy === "passkey_only" ? policy : "",
    sharedMailbox: boolOr(row, "sharedMailbox", false),
    // ABSENT MEANS ACTIVE -- see boolOr.
    active: boolOr(row, "active", true),
    suspendedAt: rowString(row, "suspendedAt"),
    suspendedReason: rowString(row, "suspendedReason"),
    lastSeenAt: rowString(row, "lastSeenAt"),
    createdAt: rowString(row, "createdAt"),
  };
}

/**
 * What to call somebody.
 *
 * `displayName` when the directory has one, then the name parts, then the
 * email, and only then the id. NEVER blank: a nameless row is
 * indistinguishable from a row that failed to render, and every person on this
 * list has at least an address.
 */
export function personName(person: PersonRow): string {
  const display = person.displayName.trim();
  if (display !== "") return display;
  const parts = `${person.firstName} ${person.lastName}`.trim();
  if (parts !== "") return parts;
  if (person.primaryEmail.trim() !== "") return person.primaryEmail;
  return person.id;
}

/** Deactivated or suspended -- the two ways an account stops being current. */
export function personIsDim(person: PersonRow): boolean {
  return !person.active || person.suspendedAt !== "";
}

// ---------------------------------------------------------------------------
// An invitation
// ---------------------------------------------------------------------------

export type DeliveryState = "not_attempted" | "sent" | "failed";

export interface InvitationRow {
  id: string;
  kind: string;
  status: string;
  active: boolean;
  inviteeEmail: string;
  inviteeName: string;
  inviteeRole: string;
  inviterName: string;
  expiresAt: string;
  respondedAt: string;
  /**
   * Read WITH `deliveryError`, never alone (memql#4587). `not_attempted` with
   * no error is a CONFIGURATION statement -- no mail is wired on this cluster,
   * so the link is the only delivery mechanism -- while `failed` with an error
   * is an incident. Rendering both as "not sent" is what let an invitation
   * look delivered when nothing had been sent at all.
   */
  deliveryState: DeliveryState;
  deliveryError: string;
  /**
   * The client this invitation is on behalf of (epic memql#4800, D5), or ""
   * for an invitation nobody tied.
   *
   * Written by the GUEST path, which went with the conversational product
   * (epic memql#4988), so nothing writes one today and every row this app's
   * list holds is `kind=="user"`. It is projected and rendered anyway rather
   * than left out: guest rows already written still read back through
   * `invitationAdminSummary`, the field is on the concept, and a surface that
   * displayed it only after somebody remembered to add it is a surface that
   * would have quietly shown nothing.
   */
  accountId: string;
  /**
   * The groups this person joins the moment they accept (epic memql#5165,
   * section G). Empty is the ordinary case and joins nobody.
   *
   * It is on the ROW rather than derived, which is what lets an invited person
   * appear on a group's member list before they exist: there is no membership
   * row to read until they arrive.
   */
  groupIds: string[];
  createdAt: string;
}

export function invitationFromRow(raw: Row): InvitationRow {
  const row = flatten(raw);
  const delivery = rowString(row, "deliveryState");
  return {
    id: rowString(row, "id"),
    // ABSENT MEANS "user" HERE AND ONLY HERE. The concept defaults `kind` to
    // "guest", but every row this app sees came through
    // pendingUserInvitations, whose filter is `kind=="user"`, and a folded
    // event that omits the field must not read as a guest invitation and get
    // dropped by our own scope check.
    kind: rowString(row, "kind") || "user",
    // Same reasoning: the concept defaults `status` to "pending".
    status: rowString(row, "status") || "pending",
    active: boolOr(row, "active", true),
    inviteeEmail: rowString(row, "inviteeEmail"),
    inviteeName: rowString(row, "inviteeName"),
    inviteeRole: rowString(row, "inviteeRole"),
    inviterName: rowString(row, "inviterName"),
    expiresAt: rowString(row, "expiresAt"),
    respondedAt: rowString(row, "respondedAt"),
    deliveryState:
      delivery === "sent" || delivery === "failed" ? delivery : "not_attempted",
    deliveryError: rowString(row, "deliveryError"),
    accountId: rowString(row, "accountId"),
    groupIds: stringsOf(row, "groupIds"),
    createdAt: rowString(row, "createdAt"),
  };
}

/**
 * Still outstanding.
 *
 * The READ says this server-side (`pendingUserInvitations` filters on
 * `statusIsPending && isActiveRecord`); this says the same thing about an
 * ARRIVING event, which is what stops an acceptance folding straight back in
 * as an update. The two have to agree, or a row the read excludes reappears
 * live and then vanishes on the next reseed.
 */
export function invitationIsPending(invite: InvitationRow): boolean {
  return invite.kind === "user" && invite.active && invite.status === "pending";
}

/** Past its expiry against the supplied clock. Blank expiry never expires. */
export function invitationHasExpired(invite: InvitationRow, now: Date): boolean {
  if (invite.expiresAt === "") return false;
  const at = Date.parse(invite.expiresAt);
  return Number.isFinite(at) && at <= now.getTime();
}

// ---------------------------------------------------------------------------
// A group, and one person's place in it
// ---------------------------------------------------------------------------

export type GroupKind = "account" | "custom";
export type GroupStatus = "active" | "archived";

export interface GroupRow {
  id: string;
  name: string;
  description: string;
  kind: GroupKind;
  /** The client this group grants. "" for a group that grants nothing. */
  accountId: string;
  status: GroupStatus;
  archivedAt: string;
  createdAt: string;
}

export function groupFromRow(raw: Row): GroupRow {
  const row = flatten(raw);
  const kind = rowString(row, "kind");
  const status = rowString(row, "status");
  return {
    id: rowString(row, "id"),
    name: rowString(row, "name"),
    description: rowString(row, "description"),
    // ABSENT MEANS "custom", which is the value a caller can make. The
    // account-kind group is written by the engine and always carries its kind;
    // a folded event that omitted the field must not turn a custom group into
    // one this app then refuses to archive.
    kind: kind === "account" ? "account" : "custom",
    accountId: rowString(row, "accountId"),
    // ABSENT MEANS ACTIVE, the boolOr reasoning in a two-value enum: a folded
    // CDC event carries only what the write touched, and reading an absent
    // status as archived would empty the list the first time anybody was added
    // to a group.
    status: status === "archived" ? "archived" : "active",
    archivedAt: rowString(row, "archivedAt"),
    createdAt: rowString(row, "createdAt"),
  };
}

export function groupIsActive(group: GroupRow): boolean {
  return group.status === "active";
}

export type MembershipOrigin = "added" | "invitation" | "domain";

export interface MembershipRow {
  id: string;
  groupId: string;
  userId: string;
  origin: MembershipOrigin;
  /** The acting person, or "" when the engine placed them. */
  addedBy: string;
  status: "active" | "removed";
  removedAt: string;
  removedBy: string;
  createdAt: string;
}

export function membershipFromRow(raw: Row): MembershipRow {
  const row = flatten(raw);
  const origin = rowString(row, "origin");
  const status = rowString(row, "status");
  return {
    id: rowString(row, "id"),
    groupId: rowString(row, "groupId"),
    userId: rowString(row, "userId"),
    origin: origin === "invitation" || origin === "domain" ? origin : "added",
    addedBy: rowString(row, "addedBy"),
    status: status === "removed" ? "removed" : "active",
    removedAt: rowString(row, "removedAt"),
    removedBy: rowString(row, "removedBy"),
    createdAt: rowString(row, "createdAt"),
  };
}

export function membershipIsActive(membership: MembershipRow): boolean {
  return membership.status === "active";
}

/**
 * Why this person is in this group, as a sentence.
 *
 * The three origins answer different questions when somebody asks why a person
 * can see a client's work, so they are said in different words rather than
 * folded into one "member since" line. `domain` names the domain because that
 * is the rule that placed them; the other two name the person, because a
 * person is who somebody would go and ask.
 */
export function originSentence(
  membership: MembershipRow,
  names: { addedByName?: string; domain?: string } = {},
): string {
  switch (membership.origin) {
    case "domain":
      return names.domain ? `joined on @${names.domain}` : "joined on this client's domain";
    case "invitation":
      return names.addedByName ? `invited by ${names.addedByName}` : "invited";
    default:
      return names.addedByName ? `added by ${names.addedByName}` : "added";
  }
}

// ---------------------------------------------------------------------------
// A role, and a grant
// ---------------------------------------------------------------------------

export interface RoleRow {
  id: string;
  slug: string;
  name: string;
  rank: number;
  description: string;
  predefined: boolean;
  active: boolean;
  aliases: string[];
  /** The account an account-scoped role is confined to. "" is everywhere. */
  accountId: string;
}

export function roleFromRow(raw: Row): RoleRow {
  const row = flatten(raw);
  const rank = row["rank"];
  return {
    id: rowString(row, "id"),
    slug: rowString(row, "slug"),
    name: rowString(row, "name"),
    // A rank crosses the wire as a number OR as its own decimal string
    // depending on the envelope; `Number` reads both and NaN is repaired to 0,
    // which sorts a rankless row to the bottom of the ladder rather than
    // throwing it out of the list.
    rank: Number.isFinite(Number(rank)) ? Number(rank) : 0,
    description: rowString(row, "description"),
    predefined: boolOr(row, "predefined", false),
    active: boolOr(row, "active", true),
    aliases: stringsOf(row, "aliases"),
    accountId: rowString(row, "accountId"),
  };
}

export interface GrantRow {
  id: string;
  roleSlug: string;
  verb: string;
  resourceType: string;
  effect: "allow" | "deny";
  predefined: boolean;
  active: boolean;
}

export function grantFromRow(raw: Row): GrantRow {
  const row = flatten(raw);
  return {
    id: rowString(row, "id"),
    roleSlug: rowString(row, "roleSlug"),
    verb: rowString(row, "verb"),
    resourceType: rowString(row, "resourceType"),
    // ALLOW IS THE DEFAULT and a deny always wins at resolution, so an absent
    // effect must read as the permissive value the concept defaults to rather
    // than as a hole somebody deliberately carved.
    effect: rowString(row, "effect") === "deny" ? "deny" : "allow",
    predefined: boolOr(row, "predefined", false),
    active: boolOr(row, "active", true),
  };
}

/** Whether a role holds a verb on a resource: an allow with no deny. */
export function roleHolds(grants: readonly GrantRow[], slug: string, verb: string, resource: string): boolean {
  let allowed = false;
  for (const grant of grants) {
    if (!grant.active || grant.roleSlug !== slug || grant.verb !== verb || grant.resourceType !== resource) {
      continue;
    }
    if (grant.effect === "deny") return false;
    allowed = true;
  }
  return allowed;
}
