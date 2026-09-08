// MyAccess data shape, data only. No portal chrome (memql#4706).
//
// ===========================================================================
// THE PARSER THAT USED TO LIVE HERE IS GONE (memql#4775)
// ===========================================================================
// `parseProfileAccess` returned null unless `userId`, `primaryEmail` AND
// `clusterRole` were all non-blank. That is one of the two ways the shell came
// to believe nobody had a role: a credential with no address -- a PAT, an
// operator key, a service account, all of which the SDK's own `AccessSummary`
// says exist -- had its perfectly good role thrown away with it.
//
// It also had no caller left. Boot used to fetch the facts over HTTP and run
// them through it; the facts now arrive from `query.getMyAccess()` on the
// cluster stream, already typed, and are narrowed by `accessFromSummary` in
// `useResolvedAccess.ts` -- which is lenient about the email and says why.
// A dead strict parser beside a live lenient one is an invitation to use the
// wrong one.
//
// What stays here is the TYPE, which is the shell's own vocabulary for a
// session and is deliberately narrower than the wire summary: no requestId, no
// sessionId, nothing chrome does not render or gate on.

export interface ProfileAccess {
  userId: string;
  primaryEmail: string;
  clusterRole: string;
  /**
   * The groups this person is in, as MyAccess reports them (epic memql#5165,
   * section H).
   *
   * IT IS HERE RATHER THAN READ, and that is the whole reason it exists: a
   * client-rank person cannot read `v1:identity:group` at all -- the query
   * carries `@requiresRank("admin")` -- so the only way they can know which
   * client they belong to is for the cluster to tell them alongside who they
   * are. The Deployables tie picker is the surface that needs it: without it,
   * a Member of Acme is offered no client to tie their work to and their
   * colleagues never see it.
   *
   * ABSENT OR EMPTY MEANS "NOT REPORTED" as well as "none", because a cluster
   * whose engine predates the field sends nothing. Every reader treats it as a
   * FALLBACK behind what it can read for itself, never as an authority --
   * which is also why it is OPTIONAL: a harness that constructs a session by
   * hand is not making a claim about anybody's groups.
   */
  groups?: AccessGroup[];
}

/** One group, and the client it grants -- named, so a member can read it. */
export interface AccessGroup {
  id: string;
  name: string;
  kind: string;
  accountId: string;
  accountName: string;
}
