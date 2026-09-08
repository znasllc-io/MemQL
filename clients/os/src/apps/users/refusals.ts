// What this cluster's group and role refusals mean, said once.
//
// ===========================================================================
// KEYED BY CODE, RENDERED AS A SENTENCE, AND NEVER INVENTED
// ===========================================================================
// `integrations/groups` and `integrations/rbac` refuse with a TYPED CODE and a
// sentence, joined as `"<code>: <sentence>"` on the error a builtin call
// throws (integrations/groups/guards.go, `refusal`). The codes are the
// contract -- guards.go says so in its own header: "a refusal that reworded
// would still be recognised and one that renamed would not".
//
// So this table supplies the HEADLINE and, where the server's sentence does
// not already say it, the next step. The server's sentence is rendered
// verbatim beneath, because it names the specific thing -- which group, whose
// rank, which account -- and a paraphrase would drop exactly that.
//
// A code with no entry here keeps its own message under a neutral heading.
// Inventing a friendly sentence for a refusal this build does not know is how
// a real fault gets mistaken for somebody's mistake. `test/users/refusals.test.ts`
// reads the two Go packages and fails when a code they can raise has no home.

export interface RefusalCopy {
  /** The headline: what happened, in the reader's terms. */
  title: string;
  /** What to do about it. Empty when the server's own sentence says. */
  next: string;
}

const COPY: Record<string, RefusalCopy> = {
  // ---- the groups plug-in -------------------------------------------------
  group_no_caller: {
    title: "This window is not acting as a person",
    next: "Sign in again. Placing somebody in a group is a person's act, and the cluster could not resolve one.",
  },
  group_capability_missing: {
    // NOT "you are not an admin". The catalog decides which roles hold
    // update-on-group, and an operator may have defined a rung that does --
    // naming a role here would be this build guessing at cluster state.
    title: "Your role does not carry that",
    next: "Groups are edited by a role holding update on group. An owner can grant it in Roles.",
  },
  group_self_add_refused: {
    title: "Nobody adds themselves to a group",
    next: "Ask somebody who ranks above you to place you. You can always take yourself out.",
  },
  group_member_rank_not_below_caller: {
    title: "That person does not rank below you",
    next: "You can place and remove people below your own rung, and nobody at or above it.",
  },
  group_account_active: {
    // The server names the account. This says what to do instead, which is
    // the half a person cannot work out from the refusal.
    title: "This group belongs to a client that is still active",
    next: "Archive the account in Accounts and this group goes with it. Archiving it alone would leave the client with no way for their people to reach their work.",
  },
  group_not_active: {
    title: "This group is archived",
    next: "An archived group grants nothing, and editing one would read as reviving it. Make a new group instead.",
  },
  group_not_found: { title: "No group by that name", next: "" },
  group_account_not_found: { title: "No such client", next: "" },
  group_account_not_active: {
    title: "That client is archived",
    next: "A group tied to an archived client would grant access to work nobody is doing.",
  },
  group_target_user_not_found: { title: "No such person", next: "" },
  group_account_kind_not_creatable: {
    title: "A client's own group is the cluster's to make",
    next: "Every account gets one automatically. A second would split the client's membership across two rows with no way to tell which one grants.",
  },
};

/**
 * Codes whose own sentence is the whole copy.
 *
 * Empty today and kept as the declared home for the case, exactly as the
 * Deployables table keeps it: a code belongs here when the server's sentence
 * needs no headline of ours, and the coverage test treats listing as covering.
 */
export const SERVER_SENTENCE_ONLY: readonly string[] = [];

/** A refusal, split into the parts a surface renders. */
export interface Refusal {
  /** The typed code, or "" when the failure carried none. */
  code: string;
  /** The server's own sentence, verbatim. */
  detail: string;
  /** The headline, or "" when this build does not know the code. */
  title: string;
  /** The next step, or "". */
  next: string;
}

/**
 * Read a thrown builtin error into its parts.
 *
 * TWO FRAMES, and the outer one is the SDK's. A refusal leaves the engine as
 * `"<code>: <sentence>"` (integrations/groups/guards.go, `refusal`), and
 * `QueryClient.executeNamed` re-throws it as `"<callName>: <that>"` -- so what
 * actually reaches a surface is `"groupMemberAdd: group_self_add_refused: ..."`.
 * Matching the code at the start of the string alone therefore recognised
 * nothing, and every refusal in this app would have rendered under the neutral
 * heading with its copy table never consulted.
 *
 * The code half is matched STRICTLY -- lower snake case, and the SDK's frame
 * is stripped only when it is a lowerCamelCase call name -- so an ordinary
 * error whose message happens to contain a colon ("Read timeout: 30s") is not
 * mistaken for a typed refusal and given somebody else's headline.
 */
export function refusalFrom(err: unknown): Refusal {
  const message = (err instanceof Error ? err.message : String(err ?? "")).trim();
  const direct = matchCode(message);
  if (direct !== null) return direct;
  // One SDK frame, and exactly one: `executeNamed` adds a single call name.
  const unframed = /^[a-z][A-Za-z0-9_]*:\s+(.+)$/s.exec(message);
  const inner = unframed === null ? null : matchCode((unframed[1] ?? "").trim());
  if (inner !== null) return inner;
  return { code: "", detail: message, title: "", next: "" };
}

function matchCode(message: string): Refusal | null {
  // At least one underscore: every code in both packages is snake_case, and
  // requiring the separator is what keeps a bare call name ("groupCreate" is
  // excluded by case; "publish" would not be) out of the code slot.
  const match = /^([a-z][a-z0-9]*(?:_[a-z0-9]+)+):\s*(.+)$/s.exec(message);
  if (match === null) return null;
  const code = match[1] ?? "";
  const copy = copyFor(code);
  return {
    code,
    detail: (match[2] ?? "").trim(),
    title: copy?.title ?? "",
    next: copy?.next ?? "",
  };
}

/**
 * copyFor returns the headline and next step for a code, or null when this
 * cluster has said something this build does not have a name for.
 *
 * A null result is the signal to render the server's message ALONE, under a
 * neutral heading -- never under a guessed one.
 */
export function copyFor(code: string): RefusalCopy | null {
  return COPY[code.trim()] ?? null;
}

/** Every code this build renders copy for. Exported for the coverage test. */
export function knownCodes(): string[] {
  return Object.keys(COPY).sort();
}
