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
  held,
  clusterDomain,
  doors,
  canRead,
}: {
  accountId: string;
  /** `account.memqlDomain` -- the name, whether or not it is held. */
  reservedName: string;
  /** `account.memqlReservationReason` -- why it is not held, when it is not. */
  reservationReason: string;
  /**
   * `account.memqlReservedAt !== ""` -- whether the cluster has agreed to
   * serve this name.
   *
   * IT IS NOT DERIVABLE FROM THE DOOR, and the first version tried. With no
   * door row the stop said "Not held", which is a lie about a name that IS
   * held and whose door the sweep has simply not opened yet -- up to two
   * minutes after an operator sets it. That is precisely the conflation the
   * reservation reason exists to end, reintroduced one layer up.
   */
  held: boolean;
  /** This cluster's own domain, for the pointing target. */
  clusterDomain: string;
  doors: FrontDoorRow[];
  /**
   * Whether this viewer can read front doors at all.
   *
   * WITHOUT THIS THE STOP LIES. v1:platform:accountFrontDoor is clusterOwner
   * tier, so an admin gets zero rows AND NO ERROR -- and `currentDoor` then
   * returns null, which reads as "Not held" about a door that is serving. A
   * review caught it: an empty feed because there is nothing and an empty feed
   * because you may not look are different answers, and only one of them is
   * the account's.
   */
  canRead: boolean;
}) {
  const now = useNow(30_000);
  const door = currentDoor(doors, accountId);

  // NOT READABLE. Said plainly rather than rendered as an absence: the floor
  // here is a mirror of the server's own tier, and a person who cannot see
  // this should learn that rather than a fact about the account.
  if (!canRead) {
    return (
      <div className="os-frontdoor">
        {reservedName ? <StateLine tone="muted" state="Reserved" /> : null}
        <Caption>
          Only a cluster owner can see whether this name is being served.
        </Caption>
      </div>
    );
  }

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

  // NO DOOR ROW YET, which is TWO states and not one.
  //
  // HELD: the cluster has agreed to serve the name and the sweep has not
  // opened its door yet -- up to one pass, so up to two minutes after an
  // operator sets it. Saying "not held" there is a lie, and it is the same
  // conflation the reservation reason exists to end.
  //
  // NOT HELD: the typed reason says why, so the stop no longer has to infer it
  // from the ownership stop beside it.
  if (!door) {
    if (held) {
      return (
        <div className="os-frontdoor">
          <StateLine tone="muted" state="Reserved" />
          <Caption>
            Held for this client. The three records to create appear here when the
            cluster next looks, within two minutes.
          </Caption>
        </div>
      );
    }
    const why = reservationReasonSentence(reservationReason);
    return (
      <div className="os-frontdoor">
        <StateLine tone="muted" state="Not held" />
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
    <div className="os-frontdoor">
      <StateLine tone={doorTone(door)} state={serving ? "Serving" : "Not served"} />
      <Caption>{doorSentence(door)}</Caption>

      {serving ? (
        <ServingHosts hosts={hosts} />
      ) : (
        <PointingGuidance hosts={hosts} target={pointingTarget(clusterDomain)} />
      )}

      {/* THE DETAIL ONLY WHERE IT IS THE ONLY CONTENT. cert-manager's own
          condition message ("Issuing certificate as Secret does not exist")
          says something no sentence here could; a typed failure's detail
          restates the sentence directly above it in slightly different words,
          which reads as the surface saying the same thing twice and hedging. */}
      {door.failureDetail && !serving && door.failureReason === "" ? (
        <p className="os-frontdoor-detail">{door.failureDetail}</p>
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

/**
 * The one word that says whether the name answers.
 *
 * IT DOES NOT REPEAT THE NAME. The first version drew the reserved name here
 * as well, and a test caught it: the rail already renders it as the stop's own
 * answer, so the page said it twice -- DESIGN.md rule 7, which the rail's whole
 * answer/body split exists to keep. The name is the stop's subject; this line
 * is its state.
 */
function StateLine({ tone, state }: { tone: string; state: string }) {
  return (
    <div className="os-frontdoor-name" data-tone={tone}>
      <span className="os-frontdoor-state">{state}</span>
    </div>
  );
}

/**
 * What to create, once: the record type and the target. Then the three names,
 * each with what we saw at it.
 */
function PointingGuidance({ hosts, target }: { hosts: DoorHost[]; target: string }) {
  // ONCE ALL THREE POINT HERE, THE INSTRUCTION IS FINISHED WORK. The record to
  // create is guidance, and guidance for a thing already done is noise sitting
  // between a person and the one line that has changed -- what the certificate
  // is waiting on. The three names stay, because they are now the EVIDENCE that
  // the records are right rather than a list of records to make.
  const stillWrong = hosts.some((h) => h.state !== "ok");

  return (
    <div className="os-frontdoor-records">
      {stillWrong ? (
        <div className="os-frontdoor-shared">
          {/* TYPE IS NOT COPYABLE, the Domains panel's reasoning: every
              registrar offers it as a dropdown, so nobody pastes "CNAME". A
              copy button there is an affordance for something nobody does. */}
          <div className="os-frontdoor-part">
            <span className="os-frontdoor-label">Type</span>
            <span className="os-frontdoor-kind">CNAME</span>
          </div>
          <CopyablePart label="Value" value={target} grow />
        </div>
      ) : null}

      <ul className="os-frontdoor-hosts">
        {hosts.map((h) => (
          <li key={h.role} data-state={h.state}>
            <CopyablePart label="Name" value={h.host} grow hideLabel />
            <span className="os-frontdoor-saw">
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
    <ul className="os-frontdoor-hosts" data-serving="true">
      {hosts.map((h) => (
        <li key={h.role} data-state="ok">
          <a href={`https://${h.host}`} target="_blank" rel="noreferrer noopener">
            <code>{h.host}</code>
          </a>
          <span className="os-frontdoor-saw">{h.purpose}</span>
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
    <div className="os-frontdoor-part" data-grow={grow}>
      {hideLabel ? null : <span className="os-frontdoor-label">{label}</span>}
      <button
        type="button"
        className="os-frontdoor-value"
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
