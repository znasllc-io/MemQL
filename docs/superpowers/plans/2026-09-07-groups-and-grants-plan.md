# Groups and grants -- implementation plan (epic memql#5165, one PR)

Written from `docs/superpowers/specs/2026-09-07-groups-and-grants-design.md`.
DELETED in this epic's own merge (docs/CLAUDE.md: a plan is spent when it ships).

Branch `epic/groups-and-grants`; worktree `/home/znas/memql-projects/epic-groups-and-grants`.
Order is the record's section M. Eight GitHub issues, ONE PR.

## 0. Ground truth the record did not carry (found while mapping)

| Finding | Consequence |
|---|---|
| Nobody holds `update` on `group`. `capabilitySets` (`component/auth/rbac_model.go:65`) grants owner/admin only `create`; `dsl/rbac/seeds.memql` mirrors that. | Four of five builtins would refuse every caller. Seed `update` on `group` for owner + admin in BOTH places. |
| `auth.ContextWithInternalOrigin` has an ENFORCED package allowlist (`call_origin_conformance_test.go`). | `integrations/groups` needs an entry + a precondition test, the `integrations/work` pattern. |
| `adminops.emit()` hardcodes `TargetType = "user"` whenever targetId is set. | Group audit lines would be mislabelled. Widen with an explicit target type, and add `group` + `groupMembership` to the `targetType` enum in `dsl/identity/concepts.memql:81` (gated by `identity_audit_enum_contract_test.go`). |
| `concept group` today has NO `@rowAuthz` and no `ownerUserId`; `concept invitation` has none either. | The reshape ADDS a tier where there was none -- every existing read of `group` narrows. Nothing reads it (no bound construct exists), so the blast radius is zero, but say so in the audit doc. |
| `dsl/accounts/builtins.memql` does not exist. | New file. |
| `dsl/accounts/shapes.memql` header states it names EVERY field of the concept. | Nine new domain fields must all land in `clientAccountFull` or they are invisible. |
| accounts mutations use `accept{}/stamp{}`; identity mutations use bare `insert{}`. | Match the file being edited, not a single house style. |
| A no-arg builtin is `builtin foo { }` with NO `@args(profile="object")` (memql#4927 gate). | `groupEnsureForAccount` takes an arg; `accountDomainReconcile` does not. |
| The seed sweep must not reuse a UI list query (paginate 50, newest-first). | Add an `@unbounded` `accountsForGroupSweep`. |
| `langparser.QuoteString`, never `%q` (memql#3611). | Every rendered call. |

## 1. The account argument of the owned tier (#5169)

**1a. Parser** -- `component/language/parser/rowauthz_binding.go`. DONE:
`RowAuthzDecl.Account`, `rowAuthzArgAccount`, the `rowAuthzOwnedModifiers` entry (keyword, value `false`),
the per-modifier noun/placeholder split so a blank `account=` says "field name" not "role slug",
the account-is-not-the-owner-field refusal, `FormatRowAuthz`'s refusal table + canonical order
(after `unowned`, before `clusterOwner`). Tests in `rowauthz_account_binding_test.go`, negative control run.

**1b. Load-time field check** -- `component/database/memory-nodes/concept_parser.go`, a twin of
`validateRowAuthz`: the field is declared on the concept, and its type is `string` or `array of string`.
A missing field refuses boot the way an unknown `unowned` slug does.

**1c. The scope node** -- `component/memql/rowauthz_account.go` (new, beside `rowauthz_rank.go`):
`AccountScopeExpression{Field}`, `accountScope{accounts, everyAccount, fingerprint}`,
`resolveAccountScope`, `accountAdmitsRow`, `lowerAccountScope`, `treeHasAccountScope`, the memo.
`orAccountScope` in `rowauthz_enforce.go` beside `orRankScope`; the branch in `rowAuthzAdmitsMode`
beside `rankAdmitsRow`; `cloneRowAuthzPredicate` + `stampRowAuthzConcept` arms; the lowering call sites
(`executor.go`, `engine.go`, `executor_mutation.go`, `grpc/server.go`); the fingerprint into the plan key.

**Lowering, both shapes with ONE expression.** `payload->'<field>' ?| array[...]` answers for a jsonb
string AND a jsonb array (Postgres `?` tests a top-level key, an array element, OR a scalar string),
so the engine never has to know which the concept declared. Empty scope lowers to `false`;
`everyAccount` lowers to "the field is present and non-empty".

**1d. Writes** -- `rowAuthzAdmitsWrite` inherits it through `rowAuthzAdmitsMode`. `rankStrict` untouched.

**Tests:** parser (done); a unit fixture concept with a scalar and a list field; db-gated row gate,
write gate, plan-key change on membership change; subscription via the memql#4309 harness.

## 2. The rows (#5170)

`dsl/identity/concepts.memql`: reshape `group` (ownerUserId always empty, name, description,
kind, accountId + `forAccount`, status, archivedAt; delete memberIds/agentIds/maxHumans/maxAgents/
externalId/active), add `groupMembership`. Both on
`@rowAuthz(owner="ownerUserId", rankVisible, unowned="admin", clusterOwner)`.
Delete `user.groupIds` + its two mutation args + its projection + `activeUsers(groupId:)`.
Add `invitation.groupIds`.
`dsl/accounts/concepts.memql`: the nine domain fields; `dsl/accounts/shapes.memql` projects them.
`dsl/accounts/automations.memql`: the self account seeds verified.
Go: `component/memql/account_domain_validation.go` -- `joinOnDomain` before verified refuses
`domain_not_verified`; a `memqlDomain` under the cluster domain or equal to a front-door host refuses
`domain_under_cluster_domain` / `domain_is_front_door_host` (reusing `frontdoor.Apex` /
`DomainDerivationSuffix` / `Hosts`). Call site in `executor_mutation.go` beside the self-archive guard.

## 3. The groups plug-in (#5171)

`integrations/groups/`: `plugin.go`, `store.go`, `guards.go`, `capabilities.go`, `ensure.go`,
`domainjoin.go`. Six verbs; five `@sdk` in `dsl/identity/builtins.memql`, `groupEnsureForAccount` not.
Guards in Go: `auth.Capable(role, VerbUpdate, ResourceGroup)`; target strictly below caller
(`principalOf` + rank compare); nobody adds themselves (`group_self_add_refused`); a caller may remove
themselves; an account-kind group refuses archive while its account is active (`group_account_active`);
`group_not_active`; `group_member_rank_not_below_caller`.
Queries `groupsAll`, `groupById`, `groupsForAccount`, `membersOfGroup`, `groupsForUser`, all
`@requiresRank("admin")`, with shapes. Routing rules for both concepts (created + updated).
Audit: five actions, with a target type that is not hardcoded to `user`.
Capability seeds: `cap-owner-update-group`, `cap-admin-update-group` + the Go mirror.

## 4. Default groups (#5172)

`ensureAccountGroup` on `node.created` of `v1:accounts:account`; `archiveAccountGroup` on
`node.updated` with `@filter(status == "archived")`; the boot backfill in `seed_materializer.go`
over `accountsForGroupSweep` (`@unbounded`), self included. Derived id `acct-<accountShortId>`.

## 5. The domain walk (#5173)

`integrations/customdomain/account_domain.go` + the `accountDomainReconcile` capability;
`dsl/accounts/builtins.memql` (new) + `reconcileAccountDomains` on `reconcileCustomDomains`'s schedule.
`updateClientAccount` resets the walk on a changed domain and fills `memqlDomain`.
`integrations/groups/domainjoin.go`: `ApplyDomainJoin(ctx, userId, email, emailVerified)`.

## 6. Arrival seams and the wire (#5174)

`IssueUserInvitationRequest.group_ids = 4`; the issue handler validates, applies the rank rule, and
stamps `invitation.accountId` from the first account-kind group. `invitation.Accept` writes one
membership per group then calls `ApplyDomainJoin` (the address is verified: the link reached it).
`Store.CreateUserOnFirstLogin` gains a verified flag on `UserProfileSeed` and calls `ApplyDomainJoin`;
the OIDC caller passes `c.EmailVerified`, magic-link passes true, bootstrap-owner passes false.
`MyAccessResult`: `MyAccessGroup groups = 8`, `account_ids = 9`, `every_account = 10`, built from the
same resolution the row gate uses. Hand-mirror into `sdk/ts/src/client/wire.ts`.

## 7. The argument on every tied concept (#5175)

`account="accountId"` on site + the five campaigns concepts; `account="accountIds"` on artifact, goal,
composition, composeTemplate, recipe. The four campaigns children gain `accountId` and the declaration;
the builtins and the drain worker stamp it from the parent. No backfill.

## 8. Docs and generated artifacts (#5176)

`access-model.md` (groups, memberships, the standing staff rule, domain join), `authoring-rules.md`,
`dsl/_reference/_concept.memql`, `per-row-authz-audit.md`, CLAUDE.md's authorization paragraph.
Regenerate: `make proto-gen`, `make sdk-gen`, `make arch-model`, `make frontdoor-*-check` (unchanged).

## Verification

`make test`; `MEMQL_REQUIRE_DB=1 MEMQL_DATABASE_DSN=postgres://memql:memql_dev@localhost:15434/memql
go test -count=1 ./component/memql/...`; `go test ./test/dslconformance/...`;
`go run ./cmd/memqllint`; `make sdk-gen-check`; `make proto-gen-check`; `make arch-model-check`;
`go test -count=1 .` (root repo-walking gates); tagged builds per node type.
