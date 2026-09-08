import { useMemo, useState } from "react";
import { ArrowLeft } from "lucide-react";

import {
  Button,
  Head,
  Input,
  Panel,
  Rail,
  RankMark,
  Select,
  roleRungOf,
  type Stop,
} from "../../kit";
import { ActionBar, type Act } from "../../kit/ActionBar";
import { AccountPicker } from "../accounts/AccountPicker";
import type { AccountRow } from "../accounts/rows";
import type { UsersActions } from "./actions";
import { Grid } from "./Grid";
import { heldPairs } from "./grid";
import { RefusalLine } from "./PersonPage";
import type { RoleRow } from "./rows";
import { ladderDescending, type RoleCatalog } from "./useRoles";

// A NEW ROLE, as a rail (design record, D3): Name, Start from, Permissions,
// Scope. The stops depend on each other -- what you start from prefills the
// grid and proposes the rank, and the rank decides whether the role is staff.

/** The slug a display name derives to: kebab-case, letters and digits only. */
export function slugFrom(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/**
 * The rank to propose for a role starting from `base`.
 *
 * THE BASE'S RANK PLUS 20, or the next free slot above it. The catalog's base
 * ranks are spaced 100 apart precisely so a custom role slots between two of
 * them, and proposing a taken rank would be proposing a value the engine
 * refuses (`rank-not-taken` is one of roleCreate's guards).
 */
export function proposeRank(base: RoleRow | null, ladder: readonly RoleRow[], callerRank: number): number {
  const taken = new Set(ladder.map((r) => r.rank));
  let proposed = (base?.rank ?? 0) + 20;
  while (taken.has(proposed) && proposed < callerRank) proposed += 1;
  return proposed;
}

/** Where a proposed rank lands on the ladder, in words. */
export function placementSentence(rank: number, ladder: readonly RoleRow[]): string {
  const below = [...ladder].filter((r) => r.rank < rank).sort((a, b) => b.rank - a.rank)[0];
  const above = [...ladder].filter((r) => r.rank > rank).sort((a, b) => a.rank - b.rank)[0];
  if (below && above) return `Placed just above ${below.name}, below ${above.name}.`;
  if (below) return `Placed just above ${below.name}.`;
  if (above) return `Placed below ${above.name}.`;
  return "";
}

export function NewRolePage({
  catalog,
  accounts,
  actions,
  viewerRole,
  onBack,
  onCreated,
}: {
  catalog: RoleCatalog;
  accounts: readonly AccountRow[];
  actions: UsersActions;
  viewerRole: string;
  onBack: () => void;
  onCreated: (slug: string) => void;
}) {
  const ladder = useMemo(() => ladderDescending(catalog.roles), [catalog.roles]);
  const callerRank = roleRungOf(viewerRole)?.rank ?? 0;
  const callerHolds = useMemo(() => heldPairs(catalog.grants, viewerRole), [catalog.grants, viewerRole]);

  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [baseSlug, setBaseSlug] = useState("");
  const [rank, setRank] = useState(0);
  const [grants, setGrants] = useState<string[]>([]);
  const [accountId, setAccountId] = useState("");

  const base = ladder.find((r) => r.slug === baseSlug) ?? null;
  const derived = slug === "" ? slugFrom(name) : slug;
  const developerRank = catalog.roles.find((r) => r.slug === "developer")?.rank ?? Number.MAX_SAFE_INTEGER;

  const rankTaken = ladder.some((r) => r.rank === rank);
  const rankTooHigh = rank >= callerRank;

  function chooseBase(next: string) {
    setBaseSlug(next);
    const chosen = ladder.find((r) => r.slug === next) ?? null;
    setGrants(chosen === null ? [] : heldPairs(catalog.grants, chosen.slug));
    setRank(proposeRank(chosen, ladder, callerRank));
  }

  const stops: Stop[] = [
    {
      id: "name",
      name: "Name",
      state: name.trim() === "" ? "open" : "done",
      sentence: "What people will call this role.",
      answer: name.trim() === "" ? "" : `${name.trim()} (${derived})`,
      body: (
        <>
          <Input id="role-name" label="Name" value={name} onChange={setName} placeholder="Field engineer" />
          <p className="os-caption">
            The slug is <span className="os-role-slug">{derived || "..."}</span>, and it is what the
            cluster stores. It never changes once the role exists.
          </p>
          <Input id="role-slug" label="Slug" value={derived} onChange={setSlug} />
        </>
      ),
    },
    {
      id: "base",
      name: "Start from",
      state: baseSlug === "" ? (name.trim() === "" ? "pending" : "open") : "done",
      sentence: "A role to copy, and where the new one sits beside it.",
      answer: base === null ? "" : `${base.name}, rank ${rank}`,
      body: (
        <>
          <Select id="role-base" label="Start from" value={baseSlug} onChange={chooseBase}>
            <option value="">Nothing -- start empty</option>
            {ladder.map((rung) => (
              <option key={rung.slug} value={rung.slug}>
                {rung.name}
              </option>
            ))}
          </Select>
          <ul className="os-role-ladder" aria-label="Where this role would sit">
            {[...ladder, { ...(base ?? emptyRole()), slug: derived || "new", name: name.trim() || "This role", rank }]
              .sort((a, b) => b.rank - a.rank)
              .map((rung) => (
                <li
                  key={`${rung.slug}:${rung.rank}`}
                  className="os-role-rung"
                  data-current={rung.slug === (derived || "new") ? "" : undefined}
                >
                  <span className="os-role-rung-line" data-static="true">
                    <RankMark actorRole={viewerRole} ownerRole={rung.slug} />
                    <span className="os-role-rung-name">{rung.name}</span>
                    <span className="os-role-slug">{rung.rank}</span>
                  </span>
                </li>
              ))}
          </ul>
          <Input
            id="role-rank"
            label="Rank"
            value={String(rank)}
            onChange={(next) => setRank(Number(next.replace(/[^0-9]/g, "")) || 0)}
          />
          <p className="os-caption">{placementSentence(rank, ladder)}</p>
          {rank >= developerRank ? (
            <p className="os-caption">
              A role at this rank is staff: in every account's group, standing.
            </p>
          ) : null}
          {rankTaken ? (
            <p className="os-caption">
              Rank {rank} is taken. Two rungs at one rank have no order between them, so the cluster
              refuses it.
            </p>
          ) : null}
          {rankTooHigh ? (
            <p className="os-caption">
              Rank {rank} is at or above your own. A role you could not be given is one you cannot
              make.
            </p>
          ) : null}
        </>
      ),
    },
    {
      id: "permissions",
      name: "Permissions",
      state: grants.length > 0 ? "done" : baseSlug === "" && name.trim() === "" ? "pending" : "waiting",
      sentence: "What somebody holding it can do.",
      answer: grants.length === 0 ? "" : `${grants.length} permissions`,
      body: (
        <Grid
          held={grants}
          callerHolds={callerHolds}
          editable
          label="What this role will hold"
          onToggle={(pair, next) =>
            setGrants((held) => (next ? [...held, pair] : held.filter((p) => p !== pair)))
          }
        />
      ),
    },
    {
      id: "scope",
      name: "Scope",
      state: "waiting",
      sentence: "Everywhere, or one client.",
      answer: accountId === "" ? "Everywhere" : accountId,
      body: (
        <>
          <AccountPicker
            value={accountId}
            onChange={setAccountId}
            accounts={[...accounts]}
            id="role-scope"
            label="The client this role is confined to"
          />
          <p className="os-caption">
            A scoped role is holdable only by somebody already in that client's group.
          </p>
        </>
      ),
    },
  ];

  const answered = name.trim() !== "" && derived !== "" && rank > 0 && !rankTaken && !rankTooHigh;
  const acts: Act[] = answered
    ? [
        {
          label: "Create role",
          tone: "primary",
          busy: actions.busyKey === `role:new:${derived}`,
          onAct: () =>
            void actions
              .roleCreate({
                slug: derived,
                name: name.trim(),
                rank,
                description: "",
                grants,
                accountId,
              })
              .then((created) => {
                if (created !== "") onCreated(created);
              }),
        },
      ]
    : [];

  return (
    <div className="os-app-stack">
      <Head title="New role">
        <Button tone="quiet" onClick={onBack} ariaLabel="Back to Roles">
          <ArrowLeft size={13} aria-hidden /> Roles
        </Button>
      </Head>

      <Panel label="The new role">
        {/* NO `openStop`: the compose reading, where the rail IS the form.
            See kit/Rail.tsx -- a rail with no open stop renders every body,
            and collapsing the stop being typed into would take the field away
            at the first keystroke. */}
        <Rail stops={stops} label="What this role is" />
        <RefusalLine actions={actions} />
      </Panel>

      <ActionBar
        state={answered ? "Ready to create" : "Not answered yet"}
        detail={answered ? undefined : "A name and a free rank below your own are what a role needs."}
        tone={answered ? "live" : "none"}
        acts={acts}
      />
    </div>
  );
}

function emptyRole(): RoleRow {
  return {
    id: "",
    slug: "",
    name: "",
    rank: 0,
    description: "",
    predefined: false,
    active: true,
    aliases: [],
    accountId: "",
  };
}
