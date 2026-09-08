import type { ProfileAccess } from "./access";
import { roleLadder, roleRungOf } from "../../system/roles";

// WHO YOU ARE ON THIS CLUSTER, said once (epic memql#5166).
//
// ===========================================================================
// WHY THE SLUG STOPPED BEING THE ANSWER
// ===========================================================================
//
// This surface printed one thing -- the role slug, in mono -- and mono was
// honest about what it was: an enum value out of five, a machine word. A
// cluster now authors its own roles, and a role arrives as three facts rather
// than one:
//
//   the NAME    what you are. "Support Lead".
//   the SLUG    what you paste into a support thread or a policy file.
//   the RANK    where you sit among the other rungs.
//
// Only the first is what a person came here to read, so it is the value. The
// other two go on one quiet line beneath it, and the RANK IS NOT PRINTED AS A
// NUMBER: 150 means nothing on its own. What it means is "above Member", and
// that sentence is the whole reason a custom role is legible at all -- the
// spacing between the seeded rungs exists so a role can slot between two you
// already know, and naming the neighbour is what says which two.
//
// NO LADDER GRAPHIC, deliberately. A row of rung ticks was the tempting
// version and the words say it completely; a diagram inside a definition list
// in Settings is decoration wearing information's clothes.
//
// The visual language is not chosen here. clients/os/DESIGN.md is twelve
// written rules and a theme pack carries colour values only -- so this uses the
// shell's own tokens and its own caption grammar, and the design decision left
// to make was what the three facts SAY.

/**
 * The neighbouring rung a rank sits above, or "" when there is none below it.
 *
 * Read from the LIVE ladder rather than from the rank the wire sent, because
 * the interesting comparison is against the roles this cluster actually has --
 * "above Member" is only true if Member is the next rung down HERE.
 */
function rungBelow(rank: number, slug: string): string {
  const rungs = roleLadder();
  let best: { name: string; rank: number } | null = null;
  for (const rung of rungs) {
    if (rung.slug === slug) continue;
    if (rung.rank >= rank) continue;
    if (best === null || rung.rank > best.rank) best = { name: rung.name, rank: rung.rank };
  }
  return best?.name ?? "";
}

/**
 * The role's place, in words: "above Member", "the highest role", or "" when
 * the ladder cannot say.
 *
 * EMPTY IS AN ANSWER. Before the ladder read lands, and on a cluster whose
 * catalog has not loaded, there is no neighbour to name -- and a placeholder
 * like "rank 150" would be the machine fact this function exists to avoid.
 */
export function placeOnLadder(access: ProfileAccess | null): string {
  if (access === null) return "";
  const rung = roleRungOf(access.role);
  if (rung === null) return "";
  const rungs = roleLadder();
  const highest = rungs.length > 0 ? rungs[rungs.length - 1] : undefined;
  if (highest !== undefined && highest.slug === rung.slug) return "the highest role";
  const below = rungBelow(rung.rank, rung.slug);
  return below === "" ? "" : `above ${below}`;
}

/**
 * The role as a person would say it: its name, or its slug when the cluster
 * reports no name, or a plain statement that it is unrecognised.
 *
 * For the sentences that name a role inside prose -- a refusal, a hidden-surface
 * list. Those printed the raw slug at people, which is the machine's word for a
 * thing the person knows by name.
 */
export function describeRole(access: ProfileAccess | null): string {
  if (access === null) return "an unrecognised role";
  const name = access.roleName.trim();
  if (name !== "") return name;
  const slug = access.role.trim();
  return slug === "" ? "an unrecognised role" : slug;
}

interface RoleIdentityProps {
  access: ProfileAccess | null;
  /**
   * Render on one line, for a sentence rather than a definition-list value.
   * The name only; the slug and the place stay behind on the block form, where
   * there is a line for them.
   */
  inline?: boolean;
}

/**
 * The one presentation of a cluster role.
 *
 * Used by Settings -> About and by Diagnostics -> Permissions, which is DESIGN.md
 * rule 7 applied to a fact rather than to a scope: the role is named in one
 * place, and the two surfaces that show it show the same thing.
 */
export function RoleIdentity({ access, inline = false }: RoleIdentityProps) {
  const name = describeRole(access);
  const slug = access?.role.trim() ?? "";

  if (inline) return <>{name}</>;

  if (slug === "") {
    return (
      <>
        <div>Unknown</div>
        {/* AN EMPTY STATE POINTS AT THE REPAIR. This is what a person sees when
            their role was retired under them, or when the cluster's catalog has
            not loaded on the node serving them -- and in the first case the fix
            is somebody else's to make, which is worth saying rather than
            leaving them to guess. */}
        <div className="os-caption">
          This cluster does not recognise your role. An operator can set it again.
        </div>
      </>
    );
  }

  const place = placeOnLadder(access);
  const named = name !== slug;

  // A ROLE THE CATALOG CANNOT NAME PRINTS ITS SLUG ONCE. `name` falls back to
  // the slug, so the obvious two-line form renders `retired-lead` above
  // `retired-lead` -- which reads as a rendering fault rather than as the state
  // it is. Caught by looking at it; no assertion about the strings present
  // would have noticed, because both lines were correct on their own.
  if (!named) {
    return (
      <>
        <div className="os-mono">{slug}</div>
        <div className="os-caption">
          {place === "" ? "This cluster has no role by that name." : place}
        </div>
      </>
    );
  }

  return (
    <>
      <div>{name}</div>
      <div className="os-caption">
        <span className="os-mono">{slug}</span>
        {place === "" ? null : <>, {place}</>}
      </div>
    </>
  );
}
