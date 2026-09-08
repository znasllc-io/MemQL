# Users, groups, roles and accounts -- the access program

- **Date:** 2026-09-07
- **Status:** agreed with the owner in the 2026-09-07 brainstorm. Every fork below was
  put to the owner and answered, in the terminal or on a rendered mockup; the per-record
  reasoning says what each choice rejected.
- **What it is:** the index for a program of four sub-projects that turn the Users app
  into the place where people, groups and roles are managed, and make an account's
  group the thing that lets a client's people reach the account's work. A group grants
  (1), a role is data the engine enforces (2), the Users app and the account page carry
  both without a person switching sections to connect anything (3), and an account's own
  domain becomes a way in, first as a membership rule and a reserved name and later as a
  front door of its own (4).
- **Predecessors:** the Accounts app design (`2026-09-01-accounts-app-design.md`), whose
  D1 ("an account is a record, never a visibility scope") this program keeps for the tie
  and reverses for the group; the rank visibility and surface authorization design
  (`2026-09-01-rank-visibility-and-surface-authorization-design.md`), whose D5 said a
  custom role is expressible and whose section G said the authoring surface is its own
  epic -- this is that epic; and the custom domains design
  (`2026-09-01-custom-domains-design.md`), whose ownership check the account's domain walk
  reuses.
- **Repositories:** all four sub-projects live in `memql`. `memql-cockpit` consumes the
  MyAccess wire (Cockpit Settings -> My Access reads the cluster role) and must adopt the
  slug-plus-rank shape when sub-project 2 lands; that is a one-task follow-up on the
  cockpit repo, named in sub-project 2's record, with no separate design.

---

## The four sub-projects, in shipping order

| # | Sub-project | Record | Depends on |
|---|---|---|---|
| A | Groups and grants (engine) | `2026-09-07-groups-and-grants-design.md` | nothing |
| B | Roles as data, enforced (engine) | `2026-09-07-roles-as-data-design.md` | nothing (A is convenient: both touch MyAccess) |
| C | The Users app and the account ties (OS) | `2026-09-07-users-app-and-account-ties-design.md` | A and B for its first PR; A for its second |
| D | The per-account front door | `2026-09-08-per-account-front-door-design.md` (the scope record `2026-09-07-per-account-front-door-scope.md` is what it was asked to answer) | A; designed after A ships |

A and B run in parallel. C's Users app PR needs both engines because the person page
edits a role and a group on one screen; C's Accounts PR needs only A. D is deliberately
not designed here: it changes the one-domain derivation that the front door, the identity
issuer and CORS all read, and that deserves its own brainstorm with the groups engine
already in hand.

Plans are not pre-written. Each record is implementation-ready (decisions, the change by
path, failure modes, testing, delivery by PR), and the session that picks a sub-project
up writes the plan from it with `superpowers:writing-plans` before the first task, then
deletes the plan in the merge (docs/CLAUDE.md).

## Issues

Filed 2026-09-08 from these records, one epic per sub-project, every task a GitHub
sub-issue of its epic, all `claude`-labeled and carrying the epic's `epic:<slug>` label so
another session can pick the work up from the label alone. Every epic body names its PR
grouping, its record and its branch; every task body opens with its epic, its PR number
and its record section, then its deliverable, acceptance and files.

| Sub-project | Repository | Epic issue | Task issues | PR grouping |
|---|---|---|---|---|
| A Groups and grants | memql | #5165 | #5169-#5176 | 1 engine PR |
| B Roles as data | memql | #5166 | #5177-#5181 | 1 engine PR |
| B Roles as data, the cockpit's My Access reading | memql-cockpit | (engine epic #5166) | #403 | lands after the engine PR |
| C The Users app and the account ties | memql | #5167 | #5182-#5188, #5190 | 2 OS PRs |
| D The per-account front door | memql | #5168 | #5191 (the design session), #5200-#5207 (filed from its record 2026-09-08) | 1 PR |

memql#5189 is not part of the program; it was filed by another session between two of
these.

## The decisions that cut across sub-projects

Each record carries its own locked-decision table. These are the ones more than one
record depends on, so they are stated once here and cited from there.

| # | Decision | Choice (owner-approved) |
|---|---|---|
| P1 | What a group does | **Membership grants.** Being in a group tied to an account is what lets a person read, and with the right verbs act on, the rows tied to that account. The account tie stays a plain record (accounts D1); the group is what turns a tie into scope. The alternative, groups as a directory with authorization unchanged, was rejected because a client-rank member could never see the deployable a developer created for them, which is the whole reason a client is on the cluster |
| P2 | One role per person | A person holds one cluster role. The role says the verbs; the groups say whose rows. An account-scoped role is a catalog entry only members of that account's groups may hold. A role per membership was rejected: it makes the effective role depend on which row is being touched, a second resolution path for a case (write for one client, read for another) nobody has asked for |
| P3 | The grant mechanism | A new **argument of the owned tier**, `account="<field>"`, list fields included, rendered as a symbolic scope node OR-ed onto the concept's admission and resolved per request into the actor's account set -- the shape the rank scope already has. The granted tier with a membership spec per concept was rejected: a granted row cannot be decided against one row, so every live feed would arrive id-only, and twelve tied concepts would need twelve specs and a join |
| P4 | Capabilities at runtime | A **data-backed catalog**: every `Capable` call reads the role and capability rows, loaded at boot and refreshed on their events; the compiled Go map becomes the seed's mirror, pinned by a parity test; an unknown slug holds nothing. Cloning a base role's set (rank as the only customization) was rejected because the owner chose the permission grid |
| P5 | Staff | Everyone at developer rank and above is a **standing member of every account-kind group**: a rule the engine applies, not rows an automation writes. Rows drift (a developer hired later is in no group until something adds them); a rule does not. The group page shows it as "Managed by" |
| P6 | The account's domain | Three things, in this order of arrival: ownership proven by the TXT token the custom-domain checker already verifies; **domain join**, which places a person with a verified email on that domain into the account's group when they arrive; and a **reserved MemQL name** (`memql.<domain>` by default) recorded now and served by sub-project D. Nothing below ownership is legal before it |
| P7 | The Users app's shape | **People, Groups, Roles**, then Logs and Settings: three nouns, three lists, one verb per Head. Chosen on a rendered comparison against two entries (People and an Access list mixing groups and roles) and against a rail of groups inside People. A person's page carries their role and groups; a group's page carries its members; nothing is connected by switching sections |
| P8 | Composing is a rail; a record is a page | Inviting someone and creating a role are compose rails in the Deployables grammar (the next unanswered stop is open, the Head's one action follows the state). A person, a group and a role are pages with panels and one action bar. The permissions stop is the **verb and resource grid**, chosen over named sentences on a rendered comparison |
| P9 | Pre-release, no shims | The inert `v1:identity:group` concept is reshaped, `user.groupIds` is deleted, the five-value `UserRole` wire enum is replaced by slug plus rank, `inviteeRole` and the delegation ceiling become slugs, and the slug-comparing specs are deleted in favour of `@requiresCapability`. Every wire change is named in its commit body for the frontend and the cockpit |

## What the program does to every app

The owner asked for a matrix of every OS app against users, groups, roles and accounts,
and for every overlap to be connected. This is it. Presentation stays as it is in every
app but Users and Accounts; what changes is which rows a client member can reach, and
that is decided by the concept's own declaration, never by the app.

| App | Account tie today | Who reaches the rows today | This program | Sub-project |
|---|---|---|---|---|
| Users | `invitation.accountId`, rendered on invite rows and written by nothing | admin and above | The whole app: People, Groups, Roles; invitations carry groups; the person page edits role and groups in place | C (PR 1), A, B |
| Accounts | owns the concept; five-band ledger | admin and above (`unowned="admin"`) | People band first in the ledger; the Domain rail (ownership, joining, reserved name); creating an account creates its group | C (PR 2), A |
| Deployables | `site.accountId` picker on the Where-it-lives stop, list filter, chip; `customDomain.accountId` as provenance | the site's owner, the cluster owner | `site` declares `account="accountId"`; the stop says who can see the deployable. Custom domains stay cluster-owner only | A, C (PR 2) |
| Campaigns | `accountId` on campaign, audience, template, senderIdentity, emailRule, with a picker on each | the owner, the cluster owner | The five declare the argument. Recipient, delivery, engagementEvent and consentEvent rows carry no account today, so the engine stamps the parent's `accountId` down at write; rows written before that stay owner-only | A |
| Files | `artifact.accountIds` multi picker in the inspector; browse facet | the owner, the cluster owner | `artifact` declares `account="accountIds"`. The content route already re-resolves the index row under the caller, so a member's download admits through it; `file`, `fileVersion`, `fileChunk` and `folder` stay owner-tier and are reached through the index | A |
| Training | `knowledgeDomain.accountId` tag and filter | any identity (the concept is public) | No change | none |
| Nexus | `goal.accountIds` multi tag on New goal | the owner, the cluster owner | `goal` declares the argument. Runs, steps, model calls, approvals and observations stay owner-only until a member surface needs them | A |
| Materializer | `accountIds` on composition, composeTemplate, recipe | the owner, the cluster owner | The three declare the argument | A |
| Fleet | none; machines are per user | the owner | No change. The shared-machines epic (memql#5151) is where a group becomes a share target later | none |
| Bin, Cluster, Concepts, Logs, Stores, Settings | none or cluster-level | operator floors | No change. Bin follows each concept's tier by construction | none |

## What was verified before filing

The inventory below is what the records build on. Each fact was read in the tree on
2026-09-07, at the paths named; a record cites the path again where it matters.

- **Roles are already data.** `v1:rbac:role` (`dsl/rbac/concepts.memql`) seeds owner 400,
  developer 300, admin 200, user 100 (alias writer), viewer 50 (alias reader)
  (`dsl/rbac/seeds.memql:53-97`). `v1:rbac:capability` rows are verb x resource per role,
  seeded for eight resource kinds (`seeds.memql:104-244`). `createRole` and
  `createCapability` (`dsl/rbac/mutations.memql:28,63`) are client-reachable, guarded in Go
  by base-role immutability (`component/memql/rbac_role_immutable_validation.go`) and
  rank-strictly-below-creator (`component/memql/rbac_custom_role_rankbound.go`). Nothing
  updates or deactivates a role; no UI creates one.
- **The runtime does not read those rows.** Every `Capable` call resolves through the
  compiled `capabilitySets` map keyed by the five legacy slugs
  (`component/auth/rbac_model.go:66-140`), so a custom role would rank correctly and hold
  no permission at any gate. `IsValidRole` (`component/auth/rbac.go:428`) and the user row's
  `role` field (`dsl/identity/concepts.memql:602`) are the five-value enum, so a custom role
  cannot be assigned at all. MyAccess carries the role as the `UserRole` proto enum
  (`component/grpc/memql.proto:443,2057`).
- **Groups exist and do nothing.** `concept group` (`dsl/identity/concepts.memql:709-718`)
  carries member and agent id lists and capacities, bound to no mutation, query or shape;
  `user.groupIds` (`:603`) is reserved, accepted by `createUser` and
  `createUserOnFirstLogin`, and filtered by the `@serverOnly` `activeUsers`. No team,
  membership or group Go type exists.
- **Accounts tie twelve concepts, all optional, all record-only.** `v1:accounts:account`
  (`dsl/accounts/concepts.memql:94`) carries name, the client's own `domain` as free text,
  contacts, notes, status and `configuredAt`, on the tier
  `@rowAuthz(owner="ownerUserId", rankVisible, unowned="admin", clusterOwner)`. The ties
  are listed in the matrix above with their files in record A.
- **The rank scope is the shape to copy.** `RankScopeExpression` is a symbolic node OR-ed
  onto the tier predicate (`component/memql/rowauthz_enforce.go:120-150`), lowered per
  request from `resolveRankScope` (`component/memql/rowauthz_rank.go:261-300`), which reads
  principals with a `DISTINCT ON (id)` collapse (`:482`) and fingerprints the resolution for
  the plan cache. `AdmitSubscriptionRow` (`component/memql/rowauthz_subscription.go:144`)
  runs the same admission for a stream's actor.
- **Custom domains verify ownership with a TXT token** (`integrations/customdomain/dns.go`
  `CheckOwnership`, record name `_memql-verify.<hostname>`), on the schedule of
  `reconcileCustomDomains` (`dsl/platform/automations.memql:61`), with typed failure
  reasons the panel renders as Type, Name and Value
  (`clients/os/src/apps/deployables/page/stops/Domains.tsx`).
- **The OS conventions the app follows.** A section's list is replaced by its page, one
  Head each, back in the Head (`clients/os/src/apps/deployables/DeployablesSection.tsx:53-88`,
  DESIGN.md rule 11); acts follow the state in one `ActionBar` (rule 12); the compose rail
  is `kit/Rail.tsx` with `page/rail.ts` computing states; a domain's picker lives in the
  owning app, not the kit (`clients/os/src/apps/accounts/tie.tsx:7-18`); `RankMark`
  draws a role on the live ladder (`clients/os/src/kit/RankMark.tsx`). The Users app today
  appends its person detail beneath the list (`PeopleSection.tsx:117-125`), the pattern
  Deployables abandoned.

## How another session picks a sub-project up

1. Read this file, then the sub-project's record end to end, then the predecessors it
   names. Read `clients/os/README.md` and `clients/os/DESIGN.md` before touching the OS.
2. Claim the epic on GitHub, then work in a git worktree; the repo is shared by
   concurrent sessions.
3. Write the plan from the record with `superpowers:writing-plans`. The record's
   "Delivery" section is the PR grouping; do not split a sub-project into a PR per task.
4. A correction to a record goes on the epic as a comment and into the record in the
   epic's own PR, so the file stays the account of what shipped.
5. Verify with `make test` and the db-gated trees, never `go test ./...`; run the OS
   suite from `clients/os`; render screenshots in both modes, empty and populated, before
   closing an OS epic (DESIGN.md, "Applying them").
