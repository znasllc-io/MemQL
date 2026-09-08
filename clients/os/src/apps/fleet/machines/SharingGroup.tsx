import { useState } from "react";

import { useSession } from "../../../chrome/access";
import { Button, Notice, Subhead } from "../../../kit";
import { formatMoment } from "../../../kit/format";
import { machineName, type MachineRow } from "../rows";
import type { MachineWrites } from "./useMachineWrites";

// Whether this machine serves the cluster (epic memql#5146, D6).
//
// ===========================================================================
// TWO CONSENTS, RENDERED SEPARATELY, ALWAYS
// ===========================================================================
// A machine serves somebody other than its owner only when BOTH say cluster:
// the owner, from this page, and the cockpit, from that machine's own
// policy.yaml. It would be shorter to render one derived "shared / not shared"
// line, and it would be wrong, because THE TWO REPAIRS ARE IN DIFFERENT
// PLACES: one is an act here, the other is a line in a file on a disk this
// page cannot reach. A single "not shared" sends half the operators to the
// wrong machine, and the laptop's owner goes looking on a web page for a
// setting that is not there.
//
// So there are two lines, always both, each saying its own state and carrying
// its own repair when it is not given.
//
// ===========================================================================
// THE LEDGER IS COUNTS, AND SAYING SO IS PART OF THE OFFER
// ===========================================================================
// Somebody deciding whether to lend their Mac Studio to the team is entitled
// to know what they will and will not see. The sentence saying the owner sees
// counts and never content sits WITH THE ACT rather than in a help page,
// because that is the moment the question is being asked.

export function SharingGroup({
  machine,
  writes,
  ledger,
}: {
  machine: MachineRow;
  writes: MachineWrites;
  /** The week's counts, already folded by the engine: calls, people, and
   *  nothing else. Null while it has not been read. */
  ledger: { sentence: string } | null;
}) {
  const { access } = useSession();
  const isOwner =
    access !== null && access.userId !== "" && sameSubject(access.userId, machine.ownerUserId);

  const ownerShared = machine.sharingMode === "cluster";
  const cockpitWilling = machine.inferenceServe === "cluster";
  const serving = ownerShared && cockpitWilling;

  return (
    <div className="os-fleet-sharing">
      <Subhead>Sharing</Subhead>

      <p className="os-fleet-sharing-state" data-serving={serving || undefined}>
        {serving
          ? "This machine serves the whole cluster."
          : "This machine serves its owner's calls only."}
      </p>

      <ul className="os-fleet-consents">
        <Consent
          given={ownerShared}
          given_text={
            machine.sharedAt
              ? `Shared by its owner ${formatMoment(machine.sharedAt)}.`
              : "Shared by its owner."
          }
          missing_text="Its owner has not shared it. That is the act below."
        />
        <Consent
          given={cockpitWilling}
          given_text="Its cockpit is willing to serve the cluster."
          missing_text={
            "Its cockpit is not willing to serve the cluster. Set inference.serve to cluster in " +
            "that machine's policy.yaml -- it is a decision about where the machine is, and only " +
            "the machine can make it."
          }
        />
      </ul>

      {serving && ledger ? <p className="os-caption">{ledger.sentence}</p> : null}

      {isOwner ? (
        <ShareControl machine={machine} shared={ownerShared} writes={writes} />
      ) : (
        <p className="os-caption">Only this machine&apos;s owner can share it with the cluster.</p>
      )}
    </div>
  );
}

/**
 * One consent, given or not.
 *
 * BOTH LINES ARE ALWAYS PRESENT, including the one that is given. Rendering
 * only the missing half would make a fully shared machine show nothing at all,
 * and a person who had just shared theirs would have no confirmation that
 * anything had happened.
 */
function Consent({
  given,
  given_text,
  missing_text,
}: {
  given: boolean;
  given_text: string;
  missing_text: string;
}) {
  return (
    <li className="os-fleet-consent" data-given={given || undefined}>
      <span className="os-fleet-consent-mark" aria-hidden="true" />
      <span>{given ? given_text : missing_text}</span>
    </li>
  );
}

/**
 * The act.
 *
 * TURNING IT ON IS ONE PRESS; TURNING IT OFF IS ALSO ONE PRESS, and neither
 * asks for confirmation. Sharing is reversible on the next call and takes
 * nothing away that cannot be given back, so a confirmation would spend a
 * person's attention on a decision that costs nothing to change.
 *
 * The sentence beneath is the OFFER'S TERMS, and it is here rather than in a
 * help page because here is where the question is being asked.
 */
function ShareControl({
  machine,
  shared,
  writes,
}: {
  machine: MachineRow;
  shared: boolean;
  writes: MachineWrites;
}) {
  const [failed, setFailed] = useState("");
  const busy = writes.busyId === machine.id;
  const label = machineName(machine);

  return (
    <div className="os-fleet-sharing-act">
      <Button
        tone={shared ? undefined : "primary"}
        busy={busy}
        busyLabel={shared ? "Stopping..." : "Sharing..."}
        onClick={() => {
          setFailed("");
          void writes.setSharing(machine.id, shared ? "owner" : "cluster").then((ok) => {
            if (!ok) setFailed(writes.actionError);
          });
        }}
      >
        {shared ? "Stop sharing with the cluster" : "Share with the cluster"}
      </Button>

      <p className="os-caption">
        {shared
          ? `Stopping takes effect on the next call. One already running on ${label} finishes -- stopping it mid-answer would lose work somebody is waiting for and change nothing about the prompt already sent.`
          : "You see how many calls ran and for how many people. You never see what anybody asked or what the model answered."}
      </p>

      {failed ? (
        <Notice
          tone="error"
          sentence="That change was not saved."
          next="This machine is sharing exactly as it was."
          detail={failed}
        />
      ) : null}
    </div>
  );
}

/**
 * Compare two user ids that may differ only in canonicalisation.
 *
 * The engine bare-ifies ids on egress and the shell never composes them, so a
 * row's `ownerUserId` and the session's `userId` are routinely the same subject
 * in two spellings. Comparing them naively would hide the act from the person
 * whose machine it is.
 */
function sameSubject(a: string, b: string): boolean {
  const bare = (id: string) => {
    const trimmed = id.trim();
    const at = trimmed.lastIndexOf(":");
    return at >= 0 ? trimmed.slice(at + 1) : trimmed;
  };
  return a.trim() === b.trim() || (bare(a) !== "" && bare(a) === bare(b));
}
