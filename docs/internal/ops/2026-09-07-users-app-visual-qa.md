---
title: "Visual QA: the Users app and the account ties"
audience: internal
status: stable
area: ops
sinceVersion: 0.9.38
owner: znas
---

# Visual QA: the Users app and the account ties

- **Date:** 2026-09-07
- **Epic:** memql#5167 (issues #5182-#5188, #5190)
- **Method:** a temporary Vite QA harness (`clients/os/qa.html` + `qa/main.tsx`
  + `qa/connection.tsx` + `qa/fake.ts` + `qa/shoot.mjs` + `vite.qa.config.ts`)
  mounting the REAL `UsersApp` and `AccountsApp` over a fake connection shaped
  like the one the suites drive, in headless Chrome at 1180x900 (2x), in both
  themes. Thirteen surfaces, twenty-six frames. Deleted after the sweep, as its
  predecessors were.

The whole app was rebuilt for this epic, and jsdom takes none of the
measurements that decide whether it works: it performs no layout and resolves
no custom properties, so the 84 green Users tests say nothing about how the
screens read. DESIGN.md's closing note makes rendered screenshots the
acceptance, and this is that pass.

## What the harness had to get right

Both traps the previous sweeps recorded fired again, which is the argument for
having written them down.

1. **The module swap is decided on the RESOLVED path.** `src/live/connection`
   is imported by five relative spellings, and a specifier alias cannot cover
   `./connection` from inside `src/live` without also catching
   `chrome/connection`, a different module.
2. **`setRoleLadder`.** The ladder starts EMPTY and the shell fills it from
   `v1:rbac:role`; without it every rung picker draws nothing and every gated
   control renders read-only. Correct product behaviour, silent to debug.

A third, specific to this app: **the session needs a READINESS feed**.
`gateFor` answers `unknown` for an absent one, and unknown is not ready -- so
the People Head renders the email gate's sentence in place of Invite, and every
frame of the Invite rail would have been a screenshot of the wrong state.

## What it found

Eight defects, every one of them invisible to the suite by construction.

1. **The role ladder drew every rung as REFUSED on the role page.** The dashed
   "not offered" style was written unscoped, and the same markup draws two
   different things: a ladder somebody is CHOOSING from, where an unoffered
   rung is a fact about them, and a ladder the role page draws as a RECORD,
   where nothing is being offered at all. Scoped to `[data-picker]`.
2. **The compose rails dimmed the stops somebody was meant to fill in.** Role
   and Groups were `pending` until the address was typed, and `pending` means
   NOT REACHABLE -- it dims the stop and gives it no disclosure. On a rail
   whose stops all render their bodies, that is a form that looks disabled.
   They are `waiting` now: reachable, not done. The same fix applies to the
   Domain rail's Joining and MemQL stops, where "Prove ownership first" was
   behind a stop nobody could open.
3. **"Acme Acme".** The group picker and the Groups list rendered the group's
   name and its client's name side by side, and an account-kind group is NAMED
   after its client. The client's name is rendered only when it says something
   the group's name does not.
4. **The New role rail proposed rank 0.** A rank is proposed on open now, from
   the ladder, rather than only when a base is chosen -- rank 0 drew the new
   rung below every existing one and left the Head's action refusing until
   somebody typed a number, which is a form that is answered and still will
   not go.
5. **"The slug is . . ."** rendered before there was a name, above an empty
   field for a value nothing derives. The slug line and its field appear
   together, once a name exists.
6. **Three full-width button bars.** "Add to a group", "Open in Accounts" and
   the People band's "Open in Users" each stretched their container, because a
   Panel and a band are flex COLUMNS -- a bare button in one reads as a banner
   rather than as an act.
7. **The absent act was explained under the Head instead of at the bar.**
   Archive is missing from a group's bar while its client is active, and the
   sentence that says why now rides the bar's own `detail` slot, where somebody
   looking for the control is looking.
8. **Filter chrome over no content** (rule 2). A cluster with nobody in it drew
   Refine and the sort line above its empty state. They are absent now unless
   a refinement is what is hiding everything, where they have to stay.

## What it confirmed

- **One Head per view, in every frame.** The list, the person page, the Invite
  rail, the group page, the role page and both compose rails each carry exactly
  one -- which is what `test/users/layout.test.tsx` asserts and what rule 11
  names as the tell.
- **The grid reads.** Eight resource rows by five verbs is forty cells and a
  role holds a handful; the held cell carries the accent ring and a soft
  ground, the unheld one a hairline, and the dash is visibly not an unchecked
  box. What a role holds is legible at a glance, in both themes.
- **The two-feed roster is one list.** An invited person sits among the people
  with the address, "Invited", the role and the days left; nothing about the
  row says it came from a different read.
- **The account page's order is right.** Profile, then the Domain rail, then
  the ledger with People first. The ownership record is three separately
  copyable parts, and the failure reason and what the lookup actually saw sit
  under it in the data voice.
- **The empty states point at the fix.** "Nobody active. Invite someone from
  the Head -- or turn on deactivated people in this app's settings if you are
  looking for an account that was retired."

## What it did not measure

The Groups list carries no member count, and that is a decision rather than a
gap: a count per row is one `membersOfGroup` read PER GROUP on every render,
which is the cluster-wide membership feed the design forbids, arrived at one
read at a time. The count lives where the members already are -- the group's
own page. The Accounts ledger's People band does fan out, and is bounded by the
groups of ONE client.

Group chips on a person's row are absent for the same reason. An invited row
DOES carry them, because an invitation names its groups on its own row.
