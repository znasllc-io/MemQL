# Roles as data, enforced -- Design (sub-project B of the access program)

- **Date:** 2026-09-07
- **Epic:** memql#5166 (tasks #5177-#5181; the cockpit follow-up memql-cockpit#403), filed 2026-09-08
- **Status:** approved in the 2026-09-07 brainstorm; the program record
  (`2026-09-07-access-program.md`) carries the cross-cutting decisions P1 to P9, cited
  below by number.
- **Scope:** the engine half of roles. A runtime capability catalog read from the role
  and capability rows, role validation against the catalog, the three role builtins with
  their guards, account-scoped roles, the `@requiresCapability` annotation and the
  retirement of the slug-comparing specs, the MyAccess and delegation wire, the audit
  lines, and the tests that pin the base roles.
- **Not here:** groups (record A), the Roles section and the New role rail (record C).

## Why

Epic memql#2062 made roles data: a catalog with spaced ranks, capabilities as verb and
resource rows, a client-reachable `createRole` guarded rank-below-creator, and the rank
record's D5 said a custom role slots into the same ladder. What never followed is the
half that makes a custom role DO anything: every runtime gate reads a compiled map keyed
by five legacy slugs, the user row's role is a five-value enum, and the wire carries an
enum too. So today a custom role can be created, ranks correctly, cannot be assigned, and
would hold no permission if it could be. The owner's brief is that people create roles
from the permissions that exist. This record makes the rows the truth.

## Locked decisions

| # | Decision | Choice (owner-approved) |
|---|---|---|
| D1 | Catalog at runtime | Program P4. One resolver installed into `component/auth` by the engine at boot; every `Capable` and every Can adapter reads it; refreshed on `v1:rbac:role` and `v1:rbac:capability` events. The compiled `capabilitySets` map is the seed's mirror, consulted only until the rows are readable, and a parity test fails the build when they disagree |
| D2 | Fail closed | An unknown slug holds no capability and rank 0 (the `rankOf` rule at `component/memql/rowauthz_rank.go:357`), so it admits nothing at the data gate and nothing at the row gate |
| D3 | One role per person | Program P2. `user.role` is a catalog slug or alias; `inviteeRole` and `delegation.roleCeiling` are slugs; ranks compare through the ladder |
| D4 | Assignment governance | Setting a role needs `update` on `principal`, the caller's rank strictly above the target's current rank AND above the new rank (the `GovernPrincipal` predicate plus the rank-bound rule), and, for a scoped role, the target's membership in that account's groups |
| D5 | Rank not taken | A custom role's rank is strictly below its creator's and equal to no existing rung. Ties would make two roles peers under D2 and D3 of the rank record with nothing to say which is which |
| D6 | Grants a subset of the caller's | You cannot hand out what you do not hold. Checked in Go against the caller's resolved set at create and at update |
| D7 | Predefined roles are immutable | Kept from memql#2062 (`rbac_role_immutable_validation.go`): name, rank, grants and aliases of a predefined role refuse every change |
| D8 | Deactivate, never delete | A role held by any active user or named by any pending invitation refuses deactivation (`role_held`). A deactivated role stays as history and cannot be assigned |
| D9 | Allow only | `effect: "deny"` rows stay honoured by the resolver (deny wins) and are not written by any v1 path |
| D10 | Scoped roles | `role.accountId` (optional). A scoped role is offered to and holdable by members of that account's groups only; a global role has no `accountId` |
| D11 | The annotation | `@requiresCapability("<verb>", "<resource>")` on a query, mutation or logic, validated at load against the vocabulary, enforced at execution beside `@requiresRank`. The slug-comparing specs are deleted |
| D12 | Wire | MyAccess drops the `UserRole` enum and carries `role` (slug), `role_name`, `rank`; a frontend and cockpit contract change |

## A. What exists today (the ground this builds on)

- `v1:rbac:role` (`dsl/rbac/concepts.memql:69-77`): slug, name, rank, description,
  predefined, active, aliases. Seeds at `dsl/rbac/seeds.memql:53-97`: owner 400, developer
  300, admin 200, user 100 (alias writer), viewer 50 (alias reader).
- `v1:rbac:capability` (`concepts.memql:43`): roleSlug, verb (read, create, update,
  delete, execute), resourceType (open string), effect (allow, deny), predefined, active.
  Seeded pairs, `seeds.memql:104-244`: principal (all four verbs), construct (read,
  create, update, delete, execute), data (four), deployment (execute), agent (read,
  create), group (read, create), role (read, create, update, delete), admission (create).
- `createRole` and `createCapability` (`dsl/rbac/mutations.memql:28,63`), client-reachable,
  no `@serverOnly`, no `@requiresRank`; guards in `component/memql/executor_mutation.go`
  (`validateRbacBaseRoleImmutable`, `validateRbacCustomRoleRankBound`). Queries
  `activeRoles`, `roleBySlug`, `activeCapabilities`, `capabilitiesForRole`,
  `capabilityGrant`, `capabilitiesForResourceType` (`dsl/rbac/queries.memql`), all
  `@public @cache(300)`. Governance builtins `rbacGovernPrincipal`,
  `rbacCanCreatePrincipal` (`dsl/rbac/builtins.memql:13,26` ->
  `integrations/rbac/capabilities.go` -> `component/auth/rbac_governance.go`).
- The compiled side: ranks at `component/auth/rbac_model.go:33-40`, `capabilitySets` at
  `:66-126`, `roleHasCapability` at `:140`, `Capable` at `:186`; the adapters
  `AtLeastAdmin` (`rbac.go:213`), `CanAdmitPeople` (`:239`), `AtLeastDeveloper` (`:251`),
  `CanWrite`, `CanAuthor`, `CanRunInline`; `IsValidRole` (`:428`); `RoleAtMost` (`:71`) and
  `EffectiveRole` (`:119`) for delegation; `GrantsPrincipalAuthorityBeyond`.
- The engine's ladder read: `rankLadder(ctx)` in `component/memql/rowauthz_rank.go` with a
  compiled fallback for the base rungs; `@requiresRank` enforcement in
  `component/memql/requires_rank.go` (`refuseBelowRequiredRank` at `:62`,
  `validateRequiresRankSlugs` at `:115`, `refusePlanBelowRequiredRank` at `:169`), stored
  on the function type and collected by `function_validator.go:490,600`.
- The gates that read a role: `component/grpc/data_capability_gate.go:121-135`
  (`Capable(role, read|create, data)`); `component/identity/adminops/adminops.go:338`
  (`authorize` -> `AtLeastAdmin`) and `:356` (`authorizeAdmission` -> `CanAdmitPeople`);
  `SetUserRole` at `:552`.
- The slug specs: `requiresAdmin`, `requiresOwnerOrAdmin`, `requiresDeveloperOrAbove`
  (`dsl/common/specs.memql:16,29,52`); uses at `dsl/identity/queries.memql:346,845,1359,
  1682,2473,2626` and `dsl/accounts/queries.memql:86,100,247`.
- The wire: `UserRole` (`component/grpc/memql.proto:443`), `MyAccessResult.cluster_role`
  (`:2061`), `roleToProto` in `component/grpc/my_access_handler.go:54`; the OS reads it at
  `clients/os/src/modules/profile/access.ts:26` and injects `actorRole` at
  `clients/os/src/chrome/Shell.tsx:217`.
- The client: `clients/os/src/system/roles.ts` holds no ordering; the ladder arrives from
  `activeRoles` (`clients/os/src/modules/profile/useRoleLadder.ts:61`); the fixture
  `clients/os/test/seededLadder.ts` is pinned to the seeds by
  `component/auth/role_ladder_client_parity_test.go`.

## B. The catalog

`component/auth` gains:

```go
type CapabilityCatalog interface {
    Rank(slug string) (rank int, ok bool)          // aliases resolve to their rung
    Holds(slug, verb, resource string) bool        // allow present and no deny; unknown slug false
    Grants(slug string) []VerbResource             // the resolved allow set, for the subset guard
    Scope(slug string) (accountId string)          // "" for a global role
    Active(slug string) bool
}
func SetCapabilityCatalog(c CapabilityCatalog)     // installed by the engine at boot
```

`Capable` becomes `catalog.Holds` when a catalog is installed, and the compiled mirror
before that (boot ordering: the identity node's gates run before the seed is readable,
the same reason `rankOf` keeps a compiled fallback). `roleRank` and `RoleRank` read the
catalog the same way. The compiled `capabilitySets` and the rank constants are kept, marked
as the mirror, and `TestSeedMatchesCompiledMirror` (`component/auth`) reads
`dsl/rbac/seeds.memql` the way the ladder parity test does and fails when a pair or a rank
differs in either direction.

The engine's implementation (`component/memql/rbac_catalog.go`) loads both concepts with
the `DISTINCT ON (id)` collapse, keeps them in memory, and subscribes to
`graph.node.created|updated` for both to reload; `rankLadder(ctx)` reads the same
structure so the row gate and the data gate can never disagree about a rank. The two
concepts gain broadcast routing rules so every replica reloads.

## C. Validation and assignment

- `IsValidRole(slug)`: an active catalog slug or alias. The user row's `role` and the
  `role` arguments of `createUser`, `createUserOnFirstLogin` and the invitation's
  `inviteeRole` become `string` with a Go validation against the catalog at the seams that
  write them (`SetUserRole`, invitation issue, first sign-in), because a DSL enum cannot
  name a row.
- `SetUserRole` (`adminops.go:552`) applies D4 through one function,
  `auth.MayAssignRole(actor, targetCurrentSlug, newSlug, targetIsMember func(accountId) bool)`,
  which record C's picker mirrors: rungs at or above the caller's are not offered, and a
  scoped role is offered only when the person is a member.
- Invitation issue applies the same function with the invitee's current rung as "none".
- `RoleAtMost` and `EffectiveRole` compare ranks through the catalog; `delegation.roleCeiling`
  becomes a string validated the same way.
- The seed materializer keeps writing base roles through `createRole` and
  `createCapability`; both gain `@serverOnly`, so no client reaches them.

## D. The three builtins

Declared in `dsl/rbac/builtins.memql`, `@sdk`, executed by `integrations/rbac`:

| Builtin | Arguments | Guards, in order | Writes |
|---|---|---|---|
| `roleCreate` | slug, name, rank, description, accountId, grants (list of `{verb, resource}`) | `create` on `role`; slug matches `^[a-z][a-z0-9-]{1,39}$` and is no slug or alias in the catalog (`role_slug_taken`); rank strictly below the caller's (`role_rank_not_below_caller`) and equal to no rung (`role_rank_taken`); every grant a known pair the caller holds (`role_grant_not_held`, naming the pair); accountId, if set, an active account | the role row (`predefined: false`, `active: true`) and one active capability row per grant, in one internal-origin write |
| `roleUpdate` | slug, name, description, rank, grants, accountId | `update` on `role`; the role is not predefined (`role_predefined_immutable`); rank and grant rules as above; a rank change also requires the caller to outrank every current holder (`role_held_above_caller`) | a new role version; removed grants written `active: false`, new ones inserted |
| `roleDeactivate` | slug | `update` on `role`; not predefined; held by no active user and named by no pending invitation (`role_held`, with the count) | `active: false` |

The vocabulary the editor offers is the set of pairs seeded on any base role
(`activeCapabilities` collapsed to distinct pairs); a pair no role holds is not a
permission this cluster has. Record C draws the grid from that set.

## E. `@requiresCapability`

- Parsed beside `@requiresRank` in `component/language` and stored on the function type
  (`function_types.go`), collected by `function_validator.go` the way `RequiresRank` is.
- Validated at load by `validateRequiresCapabilitySlugs` in
  `component/memql/requires_rank.go`: the verb is one of the five, the resource is one
  the seeds name; a misspelling refuses boot with the seed's list in the message.
- Enforced at execution by `refuseBelowRequiredCapability` beside
  `refuseBelowRequiredRank`, through the catalog, with the same refusal code family and
  the same plan-level check for expanded queries.
- Composes with `@requiresRank`: both must pass.

The migration of the nine spec uses, read in the tree on 2026-09-07, each to what the
construct means:

| Construct | Today | Becomes |
|---|---|---|
| `userById` (`dsl/identity/queries.memql:1679`) | `requiresOwnerOrAdmin` conjunct | `@requiresCapability("read", "principal")` -- developers read the user list already (`searchUsers`) and reach the Users app |
| `sessionsForSubjectAdmin` (`:842`), `patIdentitiesForUser` (`:1356`), `nodeTokenIdentitiesAdmin` (`:2473`) | `requiresOwnerOrAdmin` conjunct | `@requiresCapability("update", "principal")` -- credential-adjacent admin reads; developer holds no `update` on `principal`, so today's exclusion is preserved |
| `searchUsers` (`:343`), `pendingUserInvitations` (`:2626`), `invitationsForAccount` (`dsl/accounts/queries.memql:244`) | `requiresDeveloperOrAbove` as a floor conjunct | `@requiresRank("developer")`, which is what a floor is |
| `clientAccountsAll` (`dsl/accounts/queries.memql:83`), `clientAccountById` (`:97`) | `requiresDeveloperOrAbove` as an **OR-branch** inside the filter's own owner check | The whole owner-check conjunct is dropped and the concept's composite tier decides. The tier is enforced beside the filter, so the branch admitted nothing the tier refused; accounts are created at admin rank, so no Member-owned row exists for the dropped owner check to have narrowed |
| `requiresAdmin` | no uses | deleted |

`dsl/common/specs.memql` loses all three specs, and
`docs/public/operate/auth/access-model.md` describes the annotation.

## F. The role concept

`v1:rbac:role` gains `accountId` (D10) with a `references` relationship `as="scopedTo"`
to `v1:accounts:account`, and `createdBy`. `roleFull` projects both. `activeRoles` stays
`@public`, because the ladder is what every client draws ranks on; `capabilitiesForRole`
stays `@public` for the same reason. A scoped role appears in the ladder like any other;
what is scoped is who may hold it, not who may see that it exists.

## G. Wire

`MyAccessResult`: `cluster_role` (the enum) is removed; gains `string role = 11`,
`string role_name = 12`, `int32 rank = 13` (numbers chosen against the message by the
implementer, beside record A's additions). `UserRole` is deleted from the proto together
with `roleToProto`. The OS's `access.ts` reads the slug, `roleRungOf` already resolves
it against the live ladder, and `seededLadder.ts` stays pinned. `memql-cockpit`'s My
Access reading adopts the slug in a one-task follow-up on that repo; the commit body
names both.

## H. Events and audit

Broadcast rules for `v1:rbac:role` and `v1:rbac:capability` (section B). Audit on
`auditEvent`: `role_created`, `role_updated` (with the changed fields), `role_deactivated`,
`user_role_changed` keeps its name and now records slugs.

## I. Failure modes

- **The catalog is empty or unreadable at boot.** The mirror answers for base roles;
  custom roles hold nothing until the rows load; a log line at boot says which mode is
  in force.
- **A role row is deactivated while someone holds it.** Refused (D8); a row edited
  directly by the cluster owner under the write escape is the one path around it, and the
  resolver then treats the holder as unknown: nothing, everywhere, until re-roled. Named
  in the access model as the consequence of that escape.
- **A subset guard against a caller whose own set is empty.** Refuses every grant, which
  is right: a role that can create roles but holds no other verb creates empty roles.
- **`{ any }` requirements in the OS.** An explicit rung set never names a custom role;
  those two surfaces stay closed to custom roles. Correct for owner-or-developer surfaces;
  recorded so nobody reads it as a bug.
- **A rank change that reorders the ladder under running requests.** The plan fingerprint
  includes the ladder (`fingerprintOwnerSet`), so the next request resolves fresh.

## J. Testing

- `TestSeedMatchesCompiledMirror`: both directions, every base role.
- Unknown slug: `Capable` false for every pair; `rankOf` 0; the data gate refuses; the row
  gate admits only the actor's own rows.
- A user on a custom role passes the data gate for exactly its verbs and no other; a deny
  row wins.
- `roleCreate`: each guard refuses by its code; a create by an admin of a rank-150 role
  with `create` on `principal` succeeds; with `create` on `role` it succeeds only because
  admin holds it; with `delete` on `role` it refuses (`role_grant_not_held`).
- `roleUpdate` and `roleDeactivate`: predefined refuses; held refuses with the count; a
  rank change above a holder refuses.
- `@requiresCapability`: misspelled resource refuses boot; a Member calling a
  `read`-on-`principal` construct is refused; a developer is admitted; both annotations
  together require both.
- `MayAssignRole`: the matrix of caller rank, target rank, new rank, scoped membership.
- Wire: MyAccess round-trips slug, name and rank; the OS `access.ts` test reads a custom
  slug and `roleAdmits` resolves it by rank.
- The migration: each migrated construct has a test refusing the rung it used to refuse.

## K. Delivery

One engine PR: the catalog and resolver with the parity test; validation and
assignment; the concept change and the builtins; the annotation and the spec migration;
the wire, with generated proto and SDKs; routing rules and audit; docs
(`access-model.md`'s role spectrum rewritten to "the catalog", `authoring-rules.md` for
the annotation, `dsl/_reference/`). The commit body names the MyAccess change for the
frontend and the cockpit.

## L. Out of scope, and neighbors

Out of scope: a per-membership role; deny grants in any UI; a resource kind a product
bundle introduces at runtime (the vocabulary is open by design and the editor draws what
the seeds name; a bundle's own pairs appear when a seed names them); retiring the
`writer` and `reader` aliases from the user row's spelling.

Neighbors: record A edits the same MyAccess message and the same user row; record C's
Roles section draws from `activeRoles` and `activeCapabilities` and calls the three
builtins; the rank record's `@requiresRank` stays as it is.
