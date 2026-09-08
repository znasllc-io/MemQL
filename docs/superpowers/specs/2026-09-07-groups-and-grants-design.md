# Groups and grants -- Design (sub-project A of the access program)

- **Date:** 2026-09-07
- **Status:** approved in the 2026-09-07 brainstorm; the program record
  (`2026-09-07-access-program.md`) carries the cross-cutting decisions P1 to P9, cited
  below by number.
- **Scope:** the engine half of groups. The reshaped `v1:identity:group`, the new
  `v1:identity:groupMembership`, the `account="<field>"` argument of the owned tier and
  its per-request resolution, the group and membership builtins, the default group per
  account, the standing staff rule, the account's domain walk (ownership, domain join, the
  reserved MemQL name), the arrival seams that materialize memberships, the MyAccess
  groups, the routing rules, the audit lines, and the declaration of the argument on every
  tied concept.
- **Not here:** roles (record B), every screen (record C), serving anything under the
  reserved name (record D).

## Why

A client's people are on the cluster to work on the client's things: the deployable a
developer stood up for them, the campaigns, the files, the knowledge. Today every one of
those rows is owner-tier, so a client-rank person sees only rows they made themselves,
and the account tie is a label that decides nothing (accounts D1). The owner's brief is
that every account has a group by default, the owner's staff are in every group because
they manage the relationship, and being in the group is what lets a person reach the
account's work. This record makes that true without inventing a second authorization
mechanism: the grant is one more argument of the tier every tied concept already declares.

## Locked decisions

| # | Decision | Choice (owner-approved) |
|---|---|---|
| D1 | Group power | Membership grants (program P1). Reads admit any active member of any active group tied to the account; writes admit a member whose role holds the verb |
| D2 | The rows | Group and membership rows are **unowned**: the deployment's rows, readable from admin rank through `unowned="admin"`, written only by Go builtins under internal origin. A membership keyed as owner-tier on `userId` would let a Member insert their own row into Acme, because an owned row admits its owner's inserts |
| D3 | Mechanism | `account="<field>"` on `@rowAuthz`, rendered as `AccountScopeExpression`, lowered per request (program P3). A list field is declared the same way and lowered as an array overlap |
| D4 | Writes under the grant | A member may write a tied row whatever the owner's rank, if their role holds the verb. Rank-strict (rank record D3) keeps governing principal rows and every concept with no account argument. This is the one place the program widens D3, and it is the purpose of the program |
| D5 | Default group | Creating an account creates its `account`-kind group by automation; boot backfills one for every active account that has none, the self account included. Archiving an account archives its account-kind group; archiving the group alone is refused while the account is active |
| D6 | Staff | Developer rank and above are standing members of every account-kind group (program P5). Resolved as a rule: their scope node lowers to "any tied row" rather than to a list |
| D7 | Membership writes | The `update` verb on `group` plus rank governance: the caller may add or remove a person ranked strictly below their own rank, and may remove themselves. Nobody adds themselves. A developer places people only through an invitation (`create` on `admission`) |
| D8 | Custom groups | Any number per account, and groups with no account at all. A group tied to an account grants that account; a group with no account grants nothing and exists to organize. The `kind` field says which one was made by the automation |
| D9 | Domain join | Legal only at `domainStatus == "verified"`. Applied when a user row is created (invitation accepted, first sign-in) for a **verified** email whose domain equals an active account's `domain` with `joinOnDomain` on. Never retroactive; never on an unverified email |
| D10 | Reserved name | `memqlDomain` defaults to `memql.` plus `domain`, editable, recorded as reserved (`memqlReservedAt`) by the reconciler once ownership is verified. Refused under the cluster's own domain or as a front-door host, the custom-domain guardrails |
| D11 | Identity | One `groupMembership` row per (group, user) at a derived id; re-adding writes a new version with `status: "active"`, removing one with `status: "removed"`. History is the versions |
| D12 | Where the writers live | A new self-registering plug-in `integrations/groups` (Go), builtins declared in `dsl/identity/builtins.memql` with `@executor("integration.groups.<verb>")`, writing through the engine under internal origin the way `integrations/customdomain/store.go` writes its cluster-owner-tier rows |

## A. What exists today (the ground this builds on)

- `concept group` at `dsl/identity/concepts.memql:709-718`: name, description, externalId,
  memberIds, agentIds, maxHumans, maxAgents, active. Every field's doc says no mutation is
  bound to it. `grep 'mutate group\|query group\|shape group' dsl/` returns nothing.
- `user.groupIds` at `dsl/identity/concepts.memql:603`, "reserved for future"; accepted by
  `createUser` (`dsl/identity/mutations.memql:1199`) and `createUserOnFirstLogin` (`:1254`),
  projected at `dsl/identity/shapes.memql:467`, filtered by `activeUsers(groupId:)`
  (`dsl/identity/queries.memql:261`, `@serverOnly`, one caller in
  `component/identity/store.go` first-run setup).
- `v1:rbac:capability` seeds `create` on `group` for owner and admin and `read` on `group`
  for user and viewer (`dsl/rbac/seeds.memql`); today nothing consults either.
- The tier machinery: `RowAuthzDecl` in `component/language/parser/rowauthz_binding.go`
  (`RankVisible` at `:147`, `Unowned` at `:183`); `orRankScope` in
  `component/memql/rowauthz_enforce.go:132`; `RankScopeExpression` at
  `component/memql/rowauthz_rank.go:73`, `resolveRankScope` at `:261`, `principalRoles` at
  `:482`, the lowering at `:566-616`; the write side in
  `component/memql/rowauthz_write_guard.go`; subscriptions through `AdmitSubscriptionRow`
  (`component/memql/rowauthz_subscription.go:144`).
- The account: `dsl/accounts/concepts.memql:94`, tier at `:85`, `domain` at `:115`; the self
  singleton seeded create-if-absent by `seedSelfAccount`
  (`dsl/accounts/automations.memql:28`); mutations `createClientAccount` (`:78`),
  `updateClientAccount` (`:117`, stamps `configuredAt`), `archiveClientAccount` (`:150`),
  all `@requiresRank("admin")`.
- The domain checker: `integrations/customdomain/dns.go` (`VerifyRecordName`,
  `CheckOwnership`, `CheckPointing`), the reconciler (`reconcile.go:100`, `Run`, `verify`),
  driven by `reconcileCustomDomains` (`dsl/platform/automations.memql:61`) through the
  `customDomainReconcile` builtin.
- The arrival seams: `AcceptInvitationFunc`
  (`component/identity/web/invitation.go:193-200`, "provisions the user row, marks the
  invitation accepted, mints the session"), wired at `component/identity/web/server.go:301`;
  `Store.CreateUserOnFirstLogin` (`component/identity/store.go`) building the
  `createUserOnFirstLogin` mutation.
- The wire: `MyAccessResult` (`component/grpc/memql.proto:2057`), built by
  `component/grpc/my_access_handler.go:20`; `IssueUserInvitationRequest`
  (`memql.proto:3241`: email, role, ttl_seconds).
- Routing: per-concept broadcast rules for `v1:identity:invitation`
  (`component/node/routing.go:536-537`); `v1:identity:user` broadcasts created only.

## B. The rows

### `v1:identity:group` (reshaped)

```memql
@rowAuthz(owner="ownerUserId", rankVisible, unowned="admin", clusterOwner)
@displayCard(primary="name", secondary="description", status="status")
concept group {
  ownerUserId  string   // always empty: the deployment's row (D2). Present so the tier reads.
  name         string!
  description  string
  kind         enum("account", "custom")!
  accountId    string   // the account this group grants; empty on a custom group with no account
  status       enum("active", "archived")!
  archivedAt   datetime
  @relationship(type="references", as="forAccount", field="accountId", target=account, direction="outgoing")
}
```

Id: `v1:identity:group:<shortId>`; the account-kind group's short id is derived from the
account's (`acct-<accountShortId>`), so the automation is idempotent and a re-run is a
version, not a duplicate -- the `v1:accounts:account:self` reasoning (accounts D3).

The member list, agent list, capacities and externalId are deleted. Agents as group
members had no reader and is out of scope (program record).

### `v1:identity:groupMembership` (new)

```memql
@rowAuthz(owner="ownerUserId", rankVisible, unowned="admin", clusterOwner)
concept groupMembership {
  ownerUserId  string   // always empty (D2)
  groupId      string!
  userId       string!
  origin       enum("added", "invitation", "domain")!
  addedBy      string   // the acting user's id, or empty for the engine (domain join, backfill)
  status       enum("active", "removed")!
  removedAt    datetime
  removedBy    string
  @relationship(type="references", as="memberOf", field="groupId", target=group, direction="outgoing")
  @relationship(type="references", as="member", field="userId", target=user, direction="outgoing")
}
```

Id: `v1:identity:groupMembership:<groupShortId>-<userShortId>` (D11).

### `v1:identity:user`

`groupIds` is deleted, with its argument on `createUser` and `createUserOnFirstLogin`,
its projection in `shapes.memql`, and the `groupId` argument of `activeUsers`. Record B
changes `role`.

### `v1:identity:invitation`

Gains `groupIds []string` (the groups to join on acceptance) and keeps `accountId`,
stamped by the issue handler from the first account-kind group among `groupIds` so the
Accounts ledger's `invitationsForAccount` keeps reading. Record B changes `inviteeRole`.

### `v1:accounts:account`

Gains the domain walk, flat like `customDomain`:

| Field | Type | Meaning |
|---|---|---|
| `domainToken` | string | The ownership token published as `TXT _memql-verify.<domain>`. Minted by the reconciler the first time it sees a domain with no token. Plaintext for the reason `customDomain.token` gives |
| `domainStatus` | enum(`unverified`, `verifying`, `verified`) | `unverified`: no check has run. `verifying`: checked and failing, read the reason. `verified`: the TXT record carried the token at least once |
| `domainFailureReason` | string | typed: `dns_token_missing` today; not an enum, for `customDomain.failureReason`'s reason |
| `domainFailureDetail` | string | what the lookup saw, verbatim |
| `domainLastCheckedAt` | datetime | written on every pass; never in an arrival fingerprint |
| `domainVerifiedAt` | datetime | first pass; never cleared |
| `joinOnDomain` | bool | D9 |
| `memqlDomain` | string | D10; default `memql.<domain>` filled by `updateClientAccount` when empty and `domain` is set |
| `memqlReservedAt` | datetime | stamped by the reconciler when `domainStatus` becomes `verified` and `memqlDomain` passes the guardrails |

The self account seeds `domainStatus: "verified"`, `domainVerifiedAt: now`,
`memqlDomain: <MEMQL_DOMAIN>` and `memqlReservedAt: now`: the cluster serves that domain, so
ownership is a fact of the deployment. Changing the self account's `domain` afterwards
puts it through the same walk as any other account.

A Go pre-insert validation (the `validateRbacCustomRoleRankBound` pattern in
`component/memql/executor_mutation.go`) refuses `joinOnDomain: true` while
`domainStatus != "verified"` (`domain_not_verified`), and refuses a `memqlDomain` under
the cluster's own domain or equal to a front-door host (`domain_under_cluster_domain`,
`domain_is_front_door_host`), reusing `component/memql/platform_custom_domain_policy.go`.

## C. The account argument

### Declaration

`@rowAuthz(owner="ownerUserId", clusterOwner, account="accountId")` on a concept whose
payload carries a string account id; `account="accountIds"` where the field is a list.
`RowAuthzDecl` gains `Account string`; the parser checks the field is declared on the
concept, is a string or a string list, and is not the owner field; a missing field
refuses boot the way `unowned` with an unknown slug does
(`validateRowAuthzUnownedSlugs`, `component/memql/requires_rank.go:194`). Legal only with
`owner=` (it is an argument of the owned tier, program P3), and it composes with
`rankVisible`, `rankStrict`, `unowned` and `clusterOwner` as one more OR-ed branch on
reads.

### Rendering and lowering

`orAccountScope` mirrors `orRankScope` (`rowauthz_enforce.go:132`): a symbolic
`AccountScopeExpression{Field, IsList}` OR-ed onto the rendered predicate, cloned by
`cloneRowAuthzPredicate`, stamped by `stampRowAuthzConcept`, and lowered at execution by
`lowerAccountScope` beside `lowerRankScope` (`rowauthz_rank.go:566`) into one of three
comparisons:

| Actor's scope | Scalar field | List field |
|---|---|---|
| empty | `false` (matches nothing) | `false` |
| a set of account ids | `payload.<field> IN (…)` | `payload.<field> ?\| array[…]` (jsonb overlap) |
| every account (staff, D6) | `payload.<field> IS NOT NULL AND payload.<field> != ''` | `jsonb_array_length(payload.<field>) > 0` |

Both spellings of an account id are in the set, the `addOwnerSpellings` discipline.

### Resolution, per request

`resolveAccountScope(ctx)` beside `resolveRankScope`:

1. No actor: empty scope, fingerprint `noactor`.
2. Actor rank at or above developer (from the same ladder `resolveRankScope` reads):
   scope `everyAccount`.
3. Otherwise read the actor's active memberships (`DISTINCT ON (id)` over
   `v1:identity:groupMembership` where `payload.userId` spells the actor, the
   `principalRoles` collapse), then the active groups those name, and collect the
   non-empty `accountId` of each active `account`-kind or `custom` group. Archived groups
   and removed memberships grant nothing.
4. Fingerprint the sorted set plus the actor's id for the plan cache, as
   `fingerprintOwnerSet` does; the rank fingerprint and this one are both part of the plan
   key.

Two reads per request at most, both small. A per-user cache keyed on the membership feed
is an optimization the implementer may add behind the same function; it is not required
and it must invalidate on `graph.node.*` for both concepts.

### The row gate and subscriptions

`rowAuthzAdmits` (`rowauthz_enforce.go:330`) evaluates the same three cases against one
row's payload, so `AdmitSubscriptionRow` admits a member's live feed with no further work
and a `granted`-style id-only delivery never arises here. The resolution is done once per
stream actor per event batch, not per row.

### Writes

`rowAuthzAdmitsWrite` (`rowauthz_enforce.go:347`) gains the account branch (D4): a write
to a tied row is admitted when the actor's scope covers the row's account. The verb is
decided upstream by the data-plane gate and by the mutation's own `@requiresCapability`
(record B), never here; the row gate answers "which rows", not "may this actor write at
all". `rankStrict` is unaffected: it withdraws the cluster-owner escape, not the account
branch, and no principal concept declares an account.

### Declared on the tied concepts

| Concept | File | Argument |
|---|---|---|
| `v1:platform:site` | `dsl/platform/concepts.memql:204` | `account="accountId"` |
| `v1:campaigns:campaign` | `dsl/campaigns/concepts.memql:212` | `account="accountId"` |
| `v1:campaigns:audience` | `:104` | `account="accountId"` |
| `v1:campaigns:template` | `:142` | `account="accountId"` |
| `v1:campaigns:senderIdentity` | `:183` | `account="accountId"` |
| `v1:campaigns:emailRule` | `:546` | `account="accountId"` |
| `v1:campaigns:recipient`, `delivery`, `engagementEvent`, `consentEvent` | `:122`, `:270`, `:315`, `:495` | gain `accountId` (stamped, section J) and declare `account="accountId"` |
| `v1:library:artifact` | `dsl/library/concepts.memql:41` | `account="accountIds"` |
| `v1:work:goal` | `dsl/work/concepts.memql:28` | `account="accountIds"` |
| `v1:compose:composition`, `composeTemplate`, `recipe` | `dsl/compose/concepts.memql:47`, `:131`, `:171` | `account="accountIds"` |

Not declared, on purpose: `customDomain` (cluster-owner only, custom domains D1);
`knowledgeDomain` (public); `v1:work:run` and its children; the Library's backing rows.
`docs/public/operate/auth/per-row-authz-audit.md` gains the argument and this list.

## D. Builtins and queries

Builtins in `dsl/identity/builtins.memql`, `@sdk`, executed by `integrations/groups`:

| Builtin | Arguments | Guards, in order | Writes |
|---|---|---|---|
| `groupCreate` | name, description, accountId | `create` on `group`; the account exists and is active | one `group` row, `kind: "custom"` |
| `groupUpdate` | groupId, name, description | `update` on `group` | a new version |
| `groupArchive` | groupId | `update` on `group`; refused for an `account`-kind group while its account is active (`group_account_active`) | `status: "archived"` |
| `groupMemberAdd` | groupId, userId | `update` on `group`; target is not the caller (`group_self_add_refused`); target's rank strictly below the caller's (`group_member_rank_not_below_caller`); group active | the membership row, `origin: "added"`, `addedBy: caller` |
| `groupMemberRemove` | groupId, userId | `update` on `group`, or the target is the caller; otherwise the same rank rule | `status: "removed"`, `removedAt`, `removedBy` |

Every guard is checked in Go against the caller's `AccessContext` and the catalog
(record B), and the write runs under internal origin. Each refusal is a typed code the OS
keys copy on (record C).

Queries in `dsl/identity/queries.memql`, all `@requiresRank("admin")` (the Users app's
floor), shapes in `shapes.memql`:

| Query | Reads |
|---|---|
| `groupsAll(includeArchived)` | every group |
| `groupById(groupId)` | one |
| `groupsForAccount(accountId)` | the account's groups, for the Accounts ledger's People band |
| `membersOfGroup(groupId, includeRemoved)` | membership rows of one group |
| `groupsForUser(userId)` | membership rows of one person |

The standing staff (D6) are not rows and no query returns them; the OS reads
`activeUsers`-shaped people at developer and above from the roster it already holds and
draws the "Managed by" band from that.

## E. Automations

- `ensureAccountGroup` in `dsl/accounts/automations.memql`, `@trigger(event="node.created",
  concept="v1:accounts:account", partition="*")`: calls `groupEnsureForAccount(accountId)`
  (a sixth `integrations/groups` builtin, no `@sdk`), which writes the account-kind group
  at the derived id if none is active. The seed materializer calls the same builtin for
  every active account at boot, self included, which is the backfill.
- `archiveAccountGroup`, `@trigger(event="node.updated", concept="v1:accounts:account")`,
  `@filter(status == "archived")`: archives the account-kind group and writes `removed`
  on its memberships. The custom groups tied to that account are archived too; nothing
  under an archived account grants anything (section C).
- `reconcileAccountDomains` in `dsl/accounts/automations.memql`, on the schedule
  `reconcileCustomDomains` uses: calls `accountDomainReconcile()`, a builtin added to
  `integrations/customdomain` (section F).

## F. The domain walk

`accountDomainReconcile` walks every active account whose `domain` is non-empty and
whose `domainStatus` is not `verified`:

1. No `domainToken`: mint one (the custom-domain minting), write it with
   `domainStatus: "verifying"`.
2. `CheckOwnership(ctx, resolver, domain, token)` at `_memql-verify.<domain>`. Failure:
   write `domainLastCheckedAt`, `domainFailureReason: "dns_token_missing"`, the detail.
   Success: `domainStatus: "verified"`, `domainVerifiedAt`, and `memqlReservedAt` if
   `memqlDomain` is set and passes the guardrails (D10).

Verified accounts cost nothing per tick. A changed `domain` resets `domainStatus` to
`unverified`, clears the token and the reservation, and switches `joinOnDomain` off, in
the `updateClientAccount` validation: proof of one name is not proof of another.

**Domain join** is `groups.ApplyDomainJoin(ctx, userId, email, emailVerified)` in
`integrations/groups`: no-op unless `emailVerified`; takes the address's domain, finds an
active account with that `domain`, `domainStatus == "verified"` and `joinOnDomain`, and
writes the membership into that account's account-kind group with `origin: "domain"`.
Idempotent. Called from both arrival seams (section G). The self account's domain join
puts the owner's own people into the self group.

## G. The arrival seams

- **Invitation issue.** `IssueUserInvitationRequest` gains `repeated string group_ids = 4`
  (wire-compatible: an omitting client behaves as today). The handler
  (`component/identity/adminops/invitation.go`) validates each id names an active group,
  applies the same rank rule as `groupMemberAdd` to the invitee's role, and stamps
  `invitation.accountId` from the first account-kind group.
- **Invitation acceptance.** The `AcceptInvitationFunc` implementation writes one
  membership per `groupIds` entry with `origin: "invitation"` and `addedBy: issuedBy`
  after provisioning the user row, then calls `ApplyDomainJoin` with the invitation's
  address as verified (the link was delivered to it).
- **First sign-in.** `Store.CreateUserOnFirstLogin` calls `ApplyDomainJoin` after the
  insert with the provider's verified flag: the OIDC federation rule (an unverified claim
  links nothing) applies to joining exactly as to linking. A magic-link first sign-in
  counts as verified for the same reason an invitation does.

## H. Wire

`MyAccessResult` gains:

```proto
message MyAccessGroup { string id = 1; string name = 2; string kind = 3; string account_id = 4; string account_name = 5; }
repeated MyAccessGroup groups = 8;
repeated string account_ids = 9;   // the resolved scope
bool every_account = 10;           // staff (D6): account_ids is empty and this is set
```

Field numbers are chosen by the implementer against the current message; the names are
the contract. Record B replaces `cluster_role` in the same message. Named in the commit
body as a frontend contract change; the SDK generators run.

## I. Events and audit

Routing rules in `component/node/routing.go`, per concept, created and updated:
`v1:identity:group`, `v1:identity:groupMembership`. Both are human actions, low volume,
and the OS's group page and person page are live on them. `v1:accounts:account` keeps
its existing rules; the domain walk writes `domainLastCheckedAt` every pass, so record C
keeps that field out of every arrival fingerprint.

Audit on `v1:identity:auditEvent` (`identity.AuditCategoryAdmin`, the `SetUserRole`
pattern at `component/identity/adminops/adminops.go:552`): `group_created`,
`group_updated`, `group_archived`, `group_member_added`, `group_member_removed` (with
origin and the acting user), `account_domain_verified`, `account_domain_join_changed`,
`account_memql_domain_reserved`.

## J. Stamping the account down in campaigns

`recipient`, `delivery`, `engagementEvent` and `consentEvent` carry no `accountId`, and a
member who can read Acme's campaign must be able to read who got it. Each concept gains
an optional `accountId`; the engine copies the parent's value at write -- the audience's
for a recipient, the campaign's for a delivery and an engagement event, the recipient's
for a consent event -- in the builtins and the drain worker under `component/campaigns`
that write them. Rows written before this land without the field and stay owner-only;
there is no backfill, and the record says so rather than pretending the history moved.

## K. Failure modes

- **A member of nothing.** Empty scope lowers to `false`; owner and cluster-owner
  branches still admit their rows. Nothing widens by accident.
- **A membership into an archived group, or a group under an archived account.** Grants
  nothing (section C step 3); the row is history.
- **A stale plan.** The account fingerprint is in the plan key with the rank one; a
  membership change on the next request resolves a new set and a new key.
- **A guard the mutation cannot express.** `joinOnDomain` before verification and a
  reserved name under the cluster's domain are refused in Go before the insert, with
  typed codes, never silently corrected.
- **Domain join on an unverified address.** No-op by construction; the flag is passed by
  the seam that knows.
- **Self-add.** Refused by name (`group_self_add_refused`); no owned tier exists on the
  membership row for a raw insert to slip through (D2).
- **A developer who is not "staff".** D6 is rank-based; a custom role ranked at or above
  developer is staff too. That is what the ladder means, and the role editor (record C)
  says so on the rank stop.

## L. Testing

- Parser: the argument is refused without `owner=`, with an undeclared field, with a
  non-string field; a list field lowers as overlap (`component/language/parser`).
- Row gate, db-gated (`component/memql/rowauthz_*_db_test.go` pattern): a Member in
  Acme's group reads Acme's site and not Beta's; the same with a list field; writes admit
  with the verb and refuse without; an archived group grants nothing; staff read every
  tied row and no untied peer row; the plan key changes with membership.
- Subscription: a member's stream receives Acme's campaign update and not Beta's, the
  memql#4309 harness.
- Builtins: every guard in section D has a refusing test and an admitting one; the
  derived id makes re-add a version.
- Automations: account created writes the group; boot backfill on a cluster with accounts
  and no groups; archive cascades.
- Domain walk: token minted, `dns_token_missing` written with detail, verified stamps
  reservation, changed domain resets; `ApplyDomainJoin` on verified and unverified
  addresses; both arrival seams call it (a wire test on invitation acceptance, a store
  test on first sign-in).
- Conformance: `test/dslconformance` classifies the two new concepts; the row-authz audit
  doc lists them and the argument.
- Wire: MyAccess carries groups and scope; `group_ids` omitted behaves as before.

## M. Delivery

One engine PR, on a branch named for the epic, in this order inside it: the parser and
the row gate with their tests; the concepts and the declarations; the builtins and
queries; the automations and backfill; the domain walk; the arrival seams; the wire; the
routing rules and audit; the docs (`access-model.md`, `per-row-authz-audit.md`,
`authoring-rules.md` for the argument, `dsl/_reference/_concept.memql`). Generated
artifacts regenerate in the same PR: proto and both SDKs, the architecture model, embed
counts, `memqllint`. The commit body names the two wire changes for the frontend and the
cockpit.

## N. Out of scope, and neighbors

Out of scope: a role per membership (program P2); agents as group members; retroactive
domain join; group-shared machines (memql#5151); nested accounts; a members band that is
live (the Accounts ledger stays on-demand, accounts D-record reasoning).

Neighbors: record B changes the same MyAccess message and the same user row; land whichever
first and rebase the other. Record C consumes everything here. Record D consumes
`memqlDomain` and `memqlReservedAt` and nothing else.
