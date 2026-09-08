# The Users app and the account ties -- Design (sub-project C of the access program)

- **Date:** 2026-09-07
- **Status:** approved in the 2026-09-07 brainstorm, on four rendered mockups (the app's
  shape, the New role rail, the person page and the Invite rail, the account page); the
  program record (`2026-09-07-access-program.md`) carries the cross-cutting decisions P1 to
  P9, cited below by number.
- **Scope:** the OS half. The Users app rebuilt as People, Groups, Roles, Logs, Settings;
  the person, group and role pages; the Invite and New role rails; the Accounts page's
  People band and Domain rail; the one sentence the Deployables stop gains; the tests and
  screenshots that accept it.
- **Not here:** the engine (records A and B). Nothing in this record changes which rows
  come back; the engine decides that.

## Why

The Users app today is a roster, a separate invitations list, and three admin actions in
a panel appended beneath the list. The owner's brief: roles and groups live inside the
Users app, people create roles from the permissions that exist, every account has a
group, and nobody connects a person to a group or a role by switching sections. The
Deployables recomposition is the layout the owner calls cleanest, and its rail is the
guided form the owner wants kept where it is honest. This record is that app.

## Locked decisions

| # | Decision | Choice (owner-approved) |
|---|---|---|
| D1 | Sections | People, Groups, Roles, Logs, Settings (program P7). Invites is no longer a section: an invitation is a person who has not arrived |
| D2 | List and page | Every section is one list; opening a row replaces the list with a page, back in the Head (DESIGN.md rule 11, the `DeployablesSection` sibling-views form). The person detail appended beneath the list goes |
| D3 | Rails and pages | Composing is a rail, a record is a page (program P8). Rails: Invite (Who, Role, Groups) and New role (Name, Start from, Permissions, Scope). Pages: person, group, role. New group is a short form on the Groups Head, not a rail: two fields have no order |
| D4 | The grid | The Permissions stop and the role page draw the verb and resource grid: five verbs across, the seeded resource kinds down, a dash for a pair nothing gates, a pair the caller does not hold not offered |
| D5 | Feeds | Four retained at the app root, one per concept: users, invitations, groups, memberships. Roles and capabilities are on-demand reads of `activeRoles` and `activeCapabilities`, re-read on the two concepts' broadcast |
| D6 | The user row's silence | `v1:identity:user` broadcasts creates only; a role change produces no event. The person page re-reads on open and every write hands its accepted value back (the standing Users rule). Memberships broadcast, so the groups panel is live |
| D7 | Acts | One `ActionBar` per page: the state in words, then the acts legal from it, primary last, an illegal act absent (rule 12) |
| D8 | Refusals | In surface, the server's sentence beside the control, copy keyed by code in `apps/users/refusals.ts` with a test that reads the Go codes (the `test/deployables/refusals.test.ts` pattern) |
| D9 | The Accounts page | The People band first in the ledger, opening Users on the group by intent; the Domain rail beneath the profile with four stops |
| D10 | Pickers | A people picker and a group picker are built in `apps/users/` and exported, the `AccountPicker` precedent; the kit gains nothing domain-shaped |

## A. What exists today (the ground this builds on)

- `clients/os/src/apps/users/`: `UsersApp.tsx` (section switch, default-section
  preference), `settings.ts:20` (`USERS_SECTIONS`: people, invites with `wants: ["email"]`,
  logs at admin, settings; the settings document `{version, defaultSection, showDeactivated}`),
  `PeopleSection.tsx:34,117` (`openId`, `PersonDetail` as a sibling of `LiveList`),
  `PersonDetail.tsx` (facts, role `Select` built from `roleLadder().map(roleGrantSlug)`
  with each option admitted by `roleAdmits(ownerRole, { min: role })`, sign-in policy
  reset, enrolment link), `InvitesSection.tsx:98` (issue form, pending list, resend and
  revoke; `AccountChip` on rows), `actions.ts` (seven `IdentityAdminClient` calls),
  `usePeople.ts` (`searchUsers`), `useInvites.ts` (`pendingUserInvitations`),
  `useSessions.ts` (`sessionsForSubjectAdmin`), `rows.ts`.
- Manifest at `clients/os/src/apps/registry.tsx:276-285`: `roles: { min: "admin" }`,
  `settingsSection`, `logsSection`. The contract tests at
  `clients/os/test/system/settingsContract.test.ts` and `test/logs/logsContract.test.ts`
  gate the section list.
- The Deployables form to copy: `DeployablesSection.tsx:53-88` (the view union, one at a
  time), `page/DeployablePage.tsx:282-291` (own scroller, back in the Head),
  `page/rail.ts` and `page/Rail.tsx` (stop states), `page/head.ts` (the Head's one action
  as a state table), `page/acts.ts` (legal acts), `page/ComposePage.tsx` (the compose
  mode), `packages/refusals.ts`, `page/stops/Domains.tsx` (records as Type, Name, Value),
  `page/stops/WhereItLives.tsx:52-58` (the account picker on a stop).
- Kit: `kit/Rail.tsx` (`Stop`, `nextOpen`, `stopIsOpen`), `kit/ActionBar.tsx` (`Act`),
  `kit/RankMark.tsx` (`RankMark`, `RoleTag`), `kit/RankStates.tsx` (`SurfaceRefused`,
  `PeerRowReadOnly`), `kit/controls.tsx` (`Head`, `Refine`, `SortControl`, `Row`, `Panel`,
  `Subhead`, `Field`, `Select`, `Check`, `Chip`, `Notice`, `Facts`, `CopyValue`),
  `live/LiveList.tsx`, `live/useLiveCollection.ts`, `live/mergedView.ts`.
- Accounts: `apps/accounts/AccountDetail.tsx:275-365` (the ledger bands, one-shot reads,
  "Not yours to read" on refusal), `AccountPicker.tsx`, `tie.tsx:46` (`useAccountOptions`),
  `useAccounts.ts:152` (`invitationsForAccount`).
- Navigation between apps: `openApp(state, appId, sectionId, intent)`
  (`clients/os/src/system/desks.ts:140`); an app consumes an intent by id
  (`apps/concepts/ConceptsApp.tsx:35-57`).
- Tests: `clients/os/test/users/{harness.tsx,contract.test.ts,people.test.tsx,
  invites.test.tsx,actions.test.tsx}`, connection-shaped doubles;
  `test/setup.ts` seeds the ladder; `test/seededLadder.ts` pinned to the seeds by a Go
  test; `component/memql/site_kind_os_parity_test.go` is the pattern for pinning a client
  literal to an engine enum.

## B. The Users app

### Manifest and feeds

`USERS_SECTIONS`: `people` ("People"), `groups` ("Groups"), `roles` ("Roles"), `logs`
(admin), `settings`. No section carries `wants`; the Invite action does: the People Head
offers Invite only when the email module is ready, and otherwise renders the gate's
sentence in its place (`kit/ReadinessStates`), because an app that hides Invite with no
account of itself reads as a missing feature. The manifest floor stays `{ min: "admin" }`.

`UsersApp.tsx` retains four collections for the life of the window and passes them
down: `searchUsers` (users), `pendingUserInvitations` (invitations), `groupsAll` (groups),
and memberships through `membersOfGroup` per opened group plus `groupsForUser` per opened
person -- two reads, not a cluster-wide membership feed, because a membership feed is
every membership in the cluster to render one page, the Deployables timeline argument. The
person page and the group page each key their `LiveList` on the row they show. Roles and
capabilities come from `activeRoles` and `activeCapabilities`, re-read when the two
concepts broadcast.

`settings.ts` gains `showArchivedGroups` and `sort`; the sanitizer repairs each field on
its own.

### People

The list is one `LiveList` over a two-feed view (`useTwoFeedView`) of users and
invitations: a person row carries name, email, `RankMark` with the role name, and group
chips; an invited row carries the address, "Invited", the role it grants and the days
left. Refine: search, role, group, state (active, invited, deactivated). Deactivated
people are hidden unless the setting says otherwise, and the empty state points at the
setting when hiding is why it is empty (rule 4). Sort by name or by last seen; last seen
is displayed and never in the arrival fingerprint. The fingerprint: `displayName |
role | status | groupIds | signInPolicy`.

The Head: "People", the count as meta, and one action, Invite.

**The person page** (`PersonPage.tsx`), replacing the list:

- Head: back to People, the name, the email as meta, Ask quiet beside it.
- Role panel: the ladder drawn as `RankMark` rungs, the current one marked; the offered
  rungs are those `auth.MayAssignRole` would admit (record B, D4), mirrored client-side
  as: strictly below the caller's rank, and a scoped role only when the person is a member
  of that account. The unoffered rungs draw dashed, with the sentence "Only rungs below
  your own are offered". Changing the rung calls `setRole`; the panel shows the accepted
  value and the server's sentence on refusal.
- Groups panel: one row per active membership from `groupsForUser`: the group name, the
  account chip, the origin as a sentence ("joined on @acme.com", "added by Ken Adachi",
  "invited by Ken Adachi"), and Remove where `groupMemberRemove` would admit. One Add
  control, a group picker over the groups the caller may add to (record A, D7). Staff
  (developer and above) get no Groups panel rows and one sentence instead: "In every
  account's group, standing".
- Sign-in panel: the facts (signs in with, last seen, joined, invited by) and the
  sessions list with End per row, from `useSessions`.
- `ActionBar`: state "Active" with Reset sign-in policy (only when `passkey_only`) and
  Deactivate; "Deactivated" with Reactivate; "Invited · N days left" with Resend and
  Revoke. A person the caller cannot govern (at or above their rank) gets the bar's state
  and no acts, plus `PeerRowReadOnly`.

**The Invite rail** (`InvitePage.tsx`, opened from the Head's Invite): three stops.
Who (the address; the domain is matched against the accounts feed's verified domains
with joining on, and a match answers the Groups stop with that account's group and says
so), Role (the ladder, offered by the same rule as the person page; a scoped role
appears once its account's group is chosen), Groups (a multi group picker, prefilled).
The Head's one action follows `nextOpen`: absent until the stops are answered, then
"Send invitation". On success the rail becomes the invited person's page, the link in a
`Notice` with `CopyValue` for the case where mail did not go out; on refusal the stop that
owns the refused value shows the sentence.

### Groups

The list: rows with the name, the account chip, the kind ("your company" for the self
account's group, the account name for another account's, "no account" for an untied
custom group), the member count, archived hidden by the setting. The Head: "Groups", the
count, one action New group, which opens a `Panel` form in place of the list: name,
description, an optional `AccountPicker` imported from `apps/accounts/`. Create calls
`groupCreate`; the row arrives on its own broadcast with the arrival cue, nothing is
inserted locally.

**The group page** (`GroupPage.tsx`):

- Head: back to Groups, the name, the account chip, one action Add people, a people
  picker over non-members the caller may add.
- Members: one `LiveList` over `membersOfGroup`, keyed on the group id; rows with the
  person, `RankMark`, the origin sentence, Remove. Invited people who will join on
  acceptance appear from the invitations feed as "Invited" rows.
- "Managed by" band: the standing staff, read from the users feed at developer and
  above, with the sentence "Everyone at developer and above, standing"; no controls.
- Domain note, for an account-kind group: "Joins on @acme.com" when the account has
  joining on, or "Joining is off" with a link opening Accounts on the account. Read from
  the accounts feed; the group holds no domain state.
- `ActionBar`: "Active" with Rename (an inline edit of name and description through
  `groupUpdate`) and Archive; Archive absent for an account-kind group while its account
  is active, with the sentence "Archive the account to archive this group". "Archived"
  with nothing.

### Roles

The list is the ladder, rank descending: `RankMark`, the name, "predefined" or the scope
chip, the holder count (from the users feed). The Head: "Roles", one action New role,
offered when the caller holds `create` on `role`.

**The role page** (`RolePage.tsx`): Head with back and the name; the ladder with this
rung marked; the grid, read-only for a predefined role and editable for a custom one
when the caller holds `update` on `role`; Scope; Holders as rows opening the person page.
`ActionBar`: "Active" with Deactivate, absent while anyone holds the role, with "Held by
3 people; move them first"; "Deactivated" with nothing. Edits call `roleUpdate` with the
whole grant set; a refusal names the pair.

**The New role rail** (`NewRolePage.tsx`): Name (the display name; the slug derives from
it, shown quiet, editable), Start from (a base or custom role; choosing one prefills the
grid and proposes the rank as the base's plus 20, or the next free slot above it, shown
on the ladder with the sentence "placed just above Member"; the rank is a number field
the person may change, refused above the caller's own rung or onto a taken one), Permissions
(the grid), Scope (Everywhere, or one account through the `AccountPicker`). The Head's one
action becomes Create role. The rank stop says one more thing when the proposed rank is
at or above developer: "A role at this rank is staff: in every account's group, standing"
(record A, D6).

**The grid** (`Grid.tsx`): rows are the resource kinds, columns the five verbs, in the
order read, create, update, delete, execute, labelled Read, Create, Edit, Delete, Run. A
cell is one of: a check the caller may toggle, a check the caller may not toggle (held by
the role being edited but not by the caller: shown and locked with a title saying why), a
dash (a pair no seeded role holds). The row and cell set is `ROLE_GRID_VOCABULARY`, a
single-line literal in `apps/users/grid.ts`, pinned to `dsl/rbac/seeds.memql` by a Go test
the way `OFFERED_KINDS` is pinned to the site enum: a pair the OS offers that no seed names
is a checkbox nothing can store.

### Settings and Logs

Settings: `SetupGroup` above the preferences, the default section radio built from
`USERS_SECTIONS`, show deactivated, show archived groups, default sort. Logs: the app's
subjects gain `v1:identity:group`, `v1:identity:groupMembership`, `v1:rbac:role` and
`v1:rbac:capability`.

## C. The Accounts app

- **The People band** (`AccountDetail.tsx`), first among the ledger bands, owner "Users":
  "4 in 2 groups", naming the groups, read on demand with the others (`groupsForAccount`,
  `membersOfGroup` summed) and printing the shared `readAt`. Opening it calls
  `openApp("users", "groups", { groupId })`; the Users app consumes the intent by id and
  opens the group page.
- **The Domain rail** (`AccountDomainRail.tsx`, using `kit/Rail`), beneath the profile,
  four stops read off the account row: Domain (from the profile; editing goes through
  Edit profile); Ownership (the record as Type, Name, Value with `CopyValue` each,
  `domainLastCheckedAt` displayed and never fingerprinted, the typed reason and detail
  beneath, the footer "Checked every two minutes on its own; there is nothing to
  press"); Joining (a `Check` inside a form row, legal once verified, calling
  `updateClientAccount` with `joinOnDomain`; before verification the stop reads "Prove
  ownership first" with no control); MemQL address (the reserved name, editable through
  the profile, the three hosts it will carry as chips, the sentence "Recorded now; served
  when the per-account front door lands"). The self account's stops read Domain,
  Ownership "verified by this cluster", Joining, MemQL address "this cluster's own".
- Creating an account is unchanged. The account row's `domainLastCheckedAt` joins the
  list of fields kept out of the arrival fingerprint.

## D. The other apps

Deployables' Where-it-lives stop gains one sentence under the account picker: "Visible
to Acme's 4 people", from `groupsForAccount` plus member counts, or "Visible to nobody
else yet" when the account has no members, or nothing when the deployable has no
account. Read on the stop's open, not live.

`useAccountOptions` (`apps/accounts/tie.tsx`) gains one fallback: when the account read
returns no rows, the options are the caller's own accounts from MyAccess groups (record
A, section H). A Member of Acme cannot read the account row, and a picker that offers
nothing would let them tie a campaign to no one; offering their own account is what
lets a member's work land where their colleagues can see it. No other app changes.

## E. Shared pieces

Built in `apps/users/` and exported: `PeoplePicker` (search over the users feed the
caller holds, excluding a given set), `GroupPicker` (single and multi), `RoleLadderPicker`
(the rungs with the offered rule applied). Imported by the Accounts app for nothing yet;
the export is the precedent. `apps/users/refusals.ts` keys copy on the codes records A
and B name.

## F. Copy

Plain verbs, sentence case, one job per line. Invite, Send invitation, Add people, New
group, New role, Create role, Rename, Archive, Deactivate, Reactivate, Reset sign-in
policy, End. The state words: Active, Invited, Deactivated, Archived. The origin
sentences are the three in section B. No placeholder says more than "Search".

## G. Testing

- `test/users/contract.test.ts`: the section list, the manifest floor, the settings
  document; the settings and logs contract suites pass for the new sections.
- `test/users/people.test.tsx`: the two-feed join; an accepted invitation moves the row
  live; the cue fires on a rename and not on last seen; the person page re-reads on open.
- `test/users/person.test.tsx`: the offered rungs for owner, developer, admin callers; a
  scoped role offered only to a member; the acts per state; `PeerRowReadOnly` at or above
  rank.
- `test/users/invite.test.tsx`: the domain match answers Groups; the Head action follows
  the stops; `group_ids` on the wire; refusal at the owning stop.
- `test/users/groups.test.tsx`: create arrives by broadcast; the group page's members
  are live; Archive absent on an account-kind group.
- `test/users/roles.test.tsx`: the ladder order; the grid states per caller; New role
  proposes the slot; a refusal names the pair; Deactivate absent while held.
- `test/users/refusals.test.ts`: reads the Go codes of `integrations/groups` and
  `integrations/rbac` and fails on an unnamed one.
- `test/users/layout.test.ts`: one `Head` per view, never two in one scroller (the
  Deployables geometry test).
- `test/accounts/domain.test.tsx`: the four stops from four row states; the record is
  three copyable parts; joining has no control before verification.
- The Go parity test for `ROLE_GRID_VOCABULARY`.
- Screenshots: both modes, empty and populated, for People, a person, Invite, Groups, a
  group, Roles, a role, New role, the account page; attached to the epic before it
  closes (DESIGN.md, "Applying them").

## H. Delivery

Two OS PRs. PR 1, the Users app, after records A and B have merged: the app, its
pickers, refusals, tests and screenshots, and the Users section of `clients/os/README.md`
rewritten for the five rules a fourth app gets wrong (the two-feed roster, the offered-rung
rule, the no-membership-feed rule, the grid pin, the silence of the user row). PR 2, the
Accounts page and the Deployables sentence, after record A: the band, the rail, the
intent, tests, screenshots, and the Accounts section of the README.

## I. Out of scope, and neighbors

Out of scope: a group rail inside People (rejected on the mockup); an Access section
mixing groups and roles (rejected); a live members band in the Accounts ledger; a
members picker in Campaigns or Files; theming any of it (packs carry colour only).

Neighbors: records A and B are the engine this app calls; the front-door record (D) is
what the MemQL address stop waits on.
