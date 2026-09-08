// What each section of the Users app is showing, as one closed union per
// section (design record, D2; DESIGN.md rule 11).
//
// ===========================================================================
// A LIST AND ITS DETAIL NEVER SHARE A SCROLL COLUMN
// ===========================================================================
// The app used to render `PersonDetail` as a SIBLING of the people list, so
// selecting a row appended a panel beneath the list it was selected from --
// two `Head`s in one scroller, which rule 11 names as the tell that neither
// form was adopted. Deployables was the violation the rule was written about
// and `DeployablesSection` is the form that fixed it: a union in state, ONE
// view rendered at a time, each with its own Head and a quiet back link to the
// list.
//
//     People --open a person--> Person
//        |
//        '--Invite-----------> Invite (a rail)
//
//     Groups --open a group---> Group
//        |
//        '--New group--------> a form in place of the list
//
//     Roles --open a role-----> Role
//        |
//        '--New role---------> New role (a rail)
//
// The unions live here rather than in the three section files because
// `test/users/layout.test.ts` walks them: a view added to a section without a
// Head, or a section that renders two, fails there rather than in review.

/** The People section: the roster, one person, or the Invite rail. */
export type PeopleView =
  | { kind: "list" }
  | { kind: "person"; userId: string }
  | { kind: "invited"; invitationId: string }
  | { kind: "invite" };

/** The Groups section: the list, one group, or the New group form. */
export type GroupsView =
  | { kind: "list" }
  | { kind: "group"; groupId: string }
  | { kind: "new" };

/** The Roles section: the ladder, one role, or the New role rail. */
export type RolesView =
  | { kind: "list" }
  | { kind: "role"; slug: string }
  | { kind: "new" };

/** Every view kind, per section, for the layout test to enumerate. */
export const USERS_VIEW_KINDS = {
  people: ["list", "person", "invited", "invite"],
  groups: ["list", "group", "new"],
  roles: ["list", "role", "new"],
} as const;
