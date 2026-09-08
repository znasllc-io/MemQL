import { accountIsArchived, domainIsVerified, type AccountRow } from "../accounts/rows";
import type { GroupRow } from "./rows";

// The Invite rail's three stops, as pure functions of the draft and the feeds.
//
// A RAIL, NOT A FORM, because inviting somebody has an ORDER: the address
// decides which client they are joining, which decides which groups are
// offered, which decides whether an account-scoped role can be named at all
// (program record, P8 -- composing is a rail, a record is a page).

export interface InviteDraft {
  email: string;
  role: string;
  groupIds: string[];
  /** Whether the person has touched the Groups stop, so a match can prefill it once. */
  groupsTouched: boolean;
}

export const EMPTY_INVITE: InviteDraft = { email: "", role: "", groupIds: [], groupsTouched: false };

/** The domain half of an address, lowercased. "" when there is not one. */
export function domainOf(email: string): string {
  const at = email.trim().toLowerCase().lastIndexOf("@");
  if (at < 0) return "";
  return email.trim().toLowerCase().slice(at + 1);
}

/**
 * The client whose VERIFIED domain matches this address, and who is taking
 * people on it.
 *
 * BOTH CONDITIONS, and neither is decoration. `joinOnDomain` off means the
 * client has a proven domain and has not asked for anyone arriving on it to be
 * placed, so prefilling their group would do by hand exactly what they turned
 * off. An UNVERIFIED domain proves nothing at all -- the engine refuses
 * `joinOnDomain` on one for that reason -- and matching on it would put a
 * stranger into a client's group on the strength of a name somebody typed.
 *
 * An archived client never matches: their group grants access to work nobody
 * is doing.
 */
export function domainMatch(email: string, accounts: readonly AccountRow[]): AccountRow | null {
  const domain = domainOf(email);
  if (domain === "") return null;
  return (
    accounts.find(
      (account) =>
        !accountIsArchived(account) &&
        account.joinOnDomain &&
        domainIsVerified(account) &&
        account.domain.trim().toLowerCase() === domain,
    ) ?? null
  );
}

/** The account-kind group of one client, which is what a domain match answers with. */
export function accountGroupOf(accountId: string, groups: readonly GroupRow[]): GroupRow | null {
  if (accountId === "") return null;
  return (
    groups.find((g) => g.accountId === accountId && g.kind === "account" && g.status === "active") ??
    null
  );
}

/**
 * Whether the rail has been answered enough to send.
 *
 * THE ADDRESS AND THE ROLE, AND NOT THE GROUPS. Inviting somebody into no
 * group is the ordinary case -- staff join every account's group by rule and
 * an ordinary colleague may belong to none -- so requiring one would make the
 * common invitation impossible.
 */
export function inviteIsAnswered(draft: InviteDraft): boolean {
  return draft.email.trim() !== "" && draft.email.includes("@") && draft.role.trim() !== "";
}

/**
 * Which stop owns a refusal, so the server's sentence lands where the value
 * that caused it was entered.
 *
 * A refusal rendered at the bottom of a three-stop rail makes somebody re-read
 * all three to find out which one to change.
 */
export function stopForRefusal(code: string, detail: string): "who" | "role" | "groups" {
  if (code.startsWith("group_")) return "groups";
  const text = `${code} ${detail}`.toLowerCase();
  if (text.includes("role")) return "role";
  return "who";
}
