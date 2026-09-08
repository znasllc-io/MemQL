import { useState } from "react";
import { Check, Copy } from "lucide-react";

import { Caption } from "../../kit";
import { formatFreshness } from "../../kit/format";
import { useNow } from "../../kit/useNow";
import {
  currentDoor,
  doorSentence,
  doorTone,
  hostsFor,
  isKnownReservationReason,
  pointingTarget,
  reservationReasonSentence,
  type DoorHost,
  type FrontDoorRow,
} from "./frontDoor";

// The MemQL address stop: an account's reserved name, and the three hosts this
// cluster serves beneath it (epic memql#5168, design I).
//
// ===========================================================================
// THE CONSTANT IS STATED ONCE; THE VARIABLE GETS THE RHYTHM
// ===========================================================================
// The Deployables Domains panel renders a Type / Name / Value strip per bound
// domain, and every one of those three fields genuinely differs between two
// domains. Here they do not: three hosts, one record type, ONE target -- because
// one ingress controller terminates all three and routes by Host.
//
// Copying that shape would give three near-identical strips differing in one of
// six fields, which buries the only thing that varies under five things that do
// not. So the record is inverted: Type and Value are stated once, and the three
// Names carry the vertical rhythm, each with what this cluster last saw at it.
//
// ===========================================================================
// THERE IS NO ACT HERE, AND THAT IS THE DESIGN
// ===========================================================================
// The decision is made one stop up: reserving the name is what asks for the
// door, and CLEARING the name is what stops it -- the reservation is withdrawn,
// and the reconciler tears the routes and the certificate down (design D9).
// An act here would be a second control for one choice, which is exactly what
// DESIGN.md rule 12 exists to prevent.
//
// The stop says so in the serving state rather than leaving somebody to infer
// it from the absence of a button.
//
// ===========================================================================
// NO RE-CHECK BUTTON, DELIBERATELY
// ===========================================================================
// The Domains panel's rule, for the same two endpoints: retries ride the
// sweep's own schedule, and a button would invite hammering a recursive
// resolver and an ACME endpoint -- making the fastest path to a certificate the
// one where somebody clicks fastest. Said in as many words, because an absent
// control with no explanation reads as an omission.

export function AccountFrontDoorStop({
  accountId,
  reservedName,
  reservationReason,
  clusterDomain,
  doors,
}: {
  accountId: string;
  /** `account.memqlDomain` -- the name, whether or not it is held. */
  reservedName: string;
  /** `account.memqlReservationReason` -- why it is not held, when it is not. */
  reservationReason: string;
  /** This cluster's own domain, for the pointing target. */
  clusterDomain: string;
  doors: FrontDoorRow[];
}) {
  const now = useNow(30_000);
  const door = currentDoor(doors, accountId);

  // NOTHING RESERVED AT ALL. The name is what the stop is about, so with no
  // name there is nothing to say beyond where to set one -- and the control
  // that sets it is directly above.
  if (!reservedName) {
    return (
      <Caption>
        No MemQL address yet. Set one above and this cluster will serve the
        client&rsquo;s people under their own domain.
      </Caption>
    );
  }

  // RESERVED BUT NOT HELD. The typed reason says which of the two it is, so the
  // stop no longer has to infer it from the ownership stop beside it.
  if (!door) {
    const why = reservationReasonSentence(reservationReason);
    return (
      <div className="os-door">
        <NameLine name={reservedName} tone="muted" state="Not held" />
        <Caption>
          {isKnownReservationReason(reservationReason)
            ? why
            : "This name is not being served yet."}
        </Caption>
      </div>
    );
  }

  const hosts = hostsFor(door);
  const serving = door.status === "live";

  return (
    <div className="os-door">
      <NameLine
        name={reservedName}
        tone={doorTone(door)}
        state={serving ? "Serving" : "Not served"}
      />
      <Caption>{doorSentence(door)}</Caption>

      {serving ? <ServingHosts hosts={hosts} /> : <PointingGuidance hosts={hosts} target={pointingTarget(clusterDomain)} />}

      {door.failureDetail && !serving ? (
        <p className="os-door-detail">{door.failureDetail}</p>
      ) : null}

      <Caption>
        {serving ? (
          <>Stop serving by clearing the MemQL address above.</>
        ) : (
          <>
            {door.lastCheckedAt
              ? `Checked ${formatFreshness(door.lastCheckedAt, now)}. `
              : ""}
            Rechecked every two minutes; there is no re-check button, because
            asking a resolver faster does not make DNS propagate faster.
          </>
        )}
      </Caption>
    </div>
  );
}

/** The reserved name itself, with the one word that says whether it answers. */
function NameLine({ name, tone, state }: { name: string; tone: string; state: string }) {
  return (
    <div className="os-door-name" data-tone={tone}>
      <code>{name}</code>
      <span className="os-door-state">{state}</span>
    </div>
  );
}

/**
 * What to create, once: the record type and the target. Then the three names,
 * each with what we saw at it.
 */
function PointingGuidance({ hosts, target }: { hosts: DoorHost[]; target: string }) {
  return (
    <div className="os-door-records">
      <div className="os-door-shared">
        {/* TYPE IS NOT COPYABLE, the Domains panel's reasoning: every registrar
            offers it as a dropdown, so nobody pastes "CNAME". A copy button
            there is an affordance for something nobody does. */}
        <div className="os-door-part">
          <span className="os-door-label">Type</span>
          <span className="os-door-kind">CNAME</span>
        </div>
        <CopyablePart label="Value" value={target} grow />
      </div>

      <ul className="os-door-hosts">
        {hosts.map((h) => (
          <li key={h.role} data-state={h.state}>
            <CopyablePart label="Name" value={h.host} grow hideLabel />
            <span className="os-door-saw">
              {h.state === "ok" ? "points here" : h.state === "pending" ? "not checked yet" : h.observed || "does not point here"}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Once it is serving, the hosts stop being records and become addresses. */
function ServingHosts({ hosts }: { hosts: DoorHost[] }) {
  return (
    <ul className="os-door-hosts" data-serving="true">
      {hosts.map((h) => (
        <li key={h.role} data-state="ok">
          <a href={`https://${h.host}`} target="_blank" rel="noreferrer noopener">
            <code>{h.host}</code>
          </a>
          <span className="os-door-saw">{h.purpose}</span>
        </li>
      ))}
    </ul>
  );
}

function CopyablePart({
  label,
  value,
  grow = false,
  hideLabel = false,
}: {
  label: string;
  value: string;
  grow?: boolean;
  hideLabel?: boolean;
}) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      // A CLIPBOARD REFUSAL IS NOT AN ERROR TO REPORT. The value is on screen
      // and selectable, so the fallback is one somebody already has; a notice
      // here would be a message about the browser rather than about the door.
      setCopied(false);
    }
  }

  return (
    <div className="os-door-part" data-grow={grow}>
      {hideLabel ? null : <span className="os-door-label">{label}</span>}
      <button
        type="button"
        className="os-door-value"
        onClick={() => void copy()}
        title={`Copy ${label.toLowerCase()}`}
        aria-label={`Copy ${label.toLowerCase()}: ${value}`}
      >
        <code>{value}</code>
        {copied ? <Check size={11} aria-hidden /> : <Copy size={11} aria-hidden />}
      </button>
    </div>
  );
}
