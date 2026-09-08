# The Users app and the account ties -- Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use
> checkbox (`- [ ]`) syntax. This plan is DELETED in the epic's merge commit.

**Goal:** rebuild MemQL OS's Users app as People, Groups, Roles, Logs and Settings with a
page per record and a rail per composition, and give the Accounts page its People band and
Domain rail, so nobody connects a person to a group or a role by switching sections.

**Architecture:** every section is a view union held in the section component, one `Head`
per view (the `DeployablesSection` sibling-views form). Four feeds retained at the app root
(users, invitations, groups, accounts-for-domain-notes); memberships are read per opened
page, never as a cluster-wide feed. Roles and capabilities are on-demand reads re-read on
their broadcast. Every write hands its accepted value back, because `v1:identity:user`
broadcasts creates only.

**Tech Stack:** React 19 + TypeScript, the OS kit (`src/kit`), `LiveList` +
`useLiveCollection` + `useTwoFeedView`, vitest + @testing-library/react, one Go parity test.

**Spec:** `docs/superpowers/specs/2026-09-07-users-app-and-account-ties-design.md`
(program record: `docs/superpowers/specs/2026-09-07-access-program.md`).

**Depends on:** epic memql#5165 (groups and grants) and memql#5166 (roles as data), both of
which must merge to main before this PR lands. The branch is developed on a local merge of
both epic branches and rebased onto main once they land.

## Global Constraints

- `clients/os/DESIGN.md`'s twelve rules are the acceptance, and rule 11 (a list and its
  detail never share a scroll column, two Heads in one scroller is the tell) and rule 12
  (one ActionBar, an illegal act ABSENT never disabled) are the two this epic is about.
- Presentation gating only: the engine's row admission and the Go guards are the authority.
  Hide a control only for what ALWAYS fails; otherwise render it and show the refusal.
- Refusal copy is keyed by the Go code; an unknown code keeps the server's own sentence.
- `lastSeenAt` and `domainLastCheckedAt` are never in an arrival fingerprint.
- No emoji anywhere. Sentence case. Plain verbs. Placeholders say no more than "Search".
- Screenshots in both modes are the acceptance for every surface (DESIGN.md, "Applying
  them").

---

### Task 1: The app skeleton (#5182)

**Files:**
- Modify: `clients/os/src/apps/users/settings.ts` (sections, `showArchivedGroups`, `sort`)
- Modify: `clients/os/src/apps/users/UsersApp.tsx` (four feeds, view intent, section switch)
- Create: `clients/os/src/apps/users/views.ts` (the view unions, one per section)
- Create: `clients/os/src/apps/users/refusals.ts` (copy keyed by Go code)
- Create: `clients/os/src/apps/users/useGroups.ts` (`groupsAll` feed + per-page reads)
- Delete: `clients/os/src/apps/users/PersonDetail.tsx`, `InvitesSection.tsx`
- Test: `clients/os/test/users/{contract,layout,refusals}.test.ts`

**Interfaces produced:** `USERS_SECTIONS` (people, groups, roles, logs, settings, no
`wants`); `UsersSettings {version, defaultSection, showDeactivated, showArchivedGroups,
sort}`; `copyFor(code)`, `knownCodes()`, `SERVER_SENTENCE_ONLY`; `useGroups()`,
`useMembersOfGroup(groupId)`, `useGroupsForUser(userId)`.

- [ ] Write `test/users/contract.test.ts` for the five sections and the sanitizer's
      per-field repair; run it red.
- [ ] Write `test/users/layout.test.ts`: render each section's list and page and assert
      exactly one `.os-head` per rendered view.
- [ ] Write `test/users/refusals.test.ts` reading `integrations/groups/guards.go` and
      `integrations/rbac/*.go` for `Code\w+ = "..."` constants; assert the scan found a
      non-empty set (a null scan must fail, not pass) and every code has copy.
- [ ] Implement settings, views, refusals, useGroups; delete the two dead components.
- [ ] Logs subjects gain the four concepts.
- [ ] Run `npx vitest run test/users` and commit.

### Task 2: People and the person page (#5183)

**Files:** `PeopleSection.tsx`, `PersonPage.tsx`, `rows.ts`, `usePeople.ts`,
`useMemberships.ts`, `useSessions.ts`, `actions.ts`; tests `people.test.tsx`,
`person.test.tsx`.

**Interfaces produced:** `personRowsView(users, invites, filter)`, `offeredRungs(caller,
person, memberships)`, `PersonPage` props `{person, onBack, ...}`.

- [ ] `rows.ts`: `MembershipRow`, `membershipFromRow`, `originSentence(membership, byName)`.
- [ ] `usePeople`/`useInvites` unchanged; add `useTwoFeedView` join in `PeopleSection`.
- [ ] Fingerprint is `displayName|role|status|groupIds|signInPolicy`; last seen displayed
      only.
- [ ] `offeredRungs` mirrors `auth.MayAssignRole`: strictly below the caller's rank, and a
      scoped role only when the person is a member of that account's group.
- [ ] `PersonPage`: Head (back, name, email, Ask), Role panel (ladder, dashed unoffered
      rungs, the sentence), Groups panel (live `groupsForUser`, origin sentences, Remove,
      one Add over `GroupPicker`, the staff sentence at developer and above), Sign-in panel
      (facts + sessions with End), `ActionBar` per state, `PeerRowReadOnly` at or above rank.
- [ ] Tests, then commit.

### Task 3: The Invite rail (#5184)

**Files:** `InvitePage.tsx`, `invite.ts`, `actions.ts`; test `invite.test.tsx`.

- [ ] `invite.ts`: `stopsFor(draft, accounts, groups)` pure, and `domainMatch(email,
      accounts)` returning the account whose verified domain matches with joining on.
- [ ] The Head's one action follows `nextOpen`; `issueInvitation(email, role, groupIds)`.
- [ ] Success becomes the invited person's page with the link in a `Notice` + `CopyValue`;
      a refusal lands at the owning stop.
- [ ] The Invite action is replaced by the email gate's sentence when the module is not
      ready.

### Task 4: Groups (#5185)

**Files:** `GroupsSection.tsx`, `GroupPage.tsx`, `NewGroupForm.tsx`, `pickers.tsx`,
`useGroups.ts`; test `groups.test.tsx`.

- [ ] The list, archived hidden by the setting; New group as a `Panel` form in place of the
      list; create by `groupCreate`, the row arrives by broadcast.
- [ ] `GroupPage`: members live on `membersOfGroup` keyed on the group, invited-to-join rows
      from the invitations feed, the "Managed by" band from the users feed at developer and
      above, the domain note from the accounts feed, the `ActionBar` with Archive absent for
      an account-kind group while its account is active.
- [ ] `PeoplePicker`, `GroupPicker`, `RoleLadderPicker` exported from `pickers.tsx`.

### Task 5: Roles and the grid (#5186)

**Files:** `RolesSection.tsx`, `RolePage.tsx`, `NewRolePage.tsx`, `Grid.tsx`, `grid.ts`,
`useRoles.ts`; `component/memql/role_grid_os_parity_test.go`; test `roles.test.tsx`.

- [ ] `grid.ts`: `ROLE_GRID_VOCABULARY` as a single-line literal, `cellState(...)`.
- [ ] The Go parity test reads `dsl/rbac/seeds.memql` and the literal and fails on
      disagreement, the `site_kind_os_parity_test.go` form.
- [ ] The ladder list, the role page, the New role rail with the rank proposal, the grid.

### Task 6: The account page (#5188)

**Files:** `apps/accounts/{AccountDetail,AccountDomainRail,useAccounts,rows}.tsx|ts`,
`apps/users/UsersApp.tsx` (intent); tests `test/accounts/{domain,people}.test.tsx`.

- [ ] The People band first in the ledger, `groupsForAccount` + `membersOfGroup` summed.
- [ ] `openApp("users", "groups", { groupId })`, consumed by the Users app by id.
- [ ] The Domain rail's four stops; `domainLastCheckedAt` out of the fingerprint.

### Task 7: Deployables and the tie fallback (#5190)

**Files:** `apps/deployables/page/stops/WhereItLives.tsx`, `apps/accounts/tie.tsx`,
`src/modules/profile/access.ts`; tests.

- [ ] The one sentence, read on the stop's open.
- [ ] `useAccountOptions` falls back to the caller's MyAccess groups when the read is empty.

### Task 8: The README and the screenshots (#5187)

- [ ] Rewrite the "Users, the second app" section for the five rules; add the Accounts
      band and rail to the Accounts section.
- [ ] Screenshots in both modes, empty and populated, for People, a person, Invite, Groups,
      a group, Roles, a role, New role and the account page, attached to the epic.

