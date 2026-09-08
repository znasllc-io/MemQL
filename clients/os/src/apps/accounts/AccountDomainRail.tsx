import { useState } from "react";

import {
  Check,
  Chip,
  CopyValue,
  Notice,
  Panel,
  Rail,
  Subhead,
  formatMoment,
  nextOpen,
  type Stop,
} from "../../kit";
import type { UpdateAccountState } from "./actions";
import { accountIsSelf, accountName, domainIsVerified, type AccountRow } from "./rows";

// THE DOMAIN RAIL: what a client's own domain buys them, in four stops.
//
// ===========================================================================
// A RAIL BECAUSE THE STOPS DEPEND ON EACH OTHER
// ===========================================================================
// There is a domain, ownership of it is proven, joining can then be turned on,
// and a MemQL name can then be reserved. Each stop is legal only once the one
// above it is answered, which is exactly what a rail says and what four panels
// would not.
//
// NOTHING HERE HAS A CHECK BUTTON, and that is the point of the footer
// sentence. The reconciler walks every unverified domain on its own schedule;
// a "Check now" control would either lie (it cannot make DNS propagate) or
// invite somebody to press it repeatedly while a TTL expires.

export function AccountDomainRail({
  account,
  update,
}: {
  account: AccountRow;
  update: UpdateAccountState;
}) {
  const [openStop, setOpenStop] = useState("");
  const self = accountIsSelf(account);
  const verified = domainIsVerified(account) || self;

  if (account.domain.trim() === "" && !self) {
    return (
      <Panel label="This client's domain">
        <Subhead>Domain</Subhead>
        <p className="os-caption">
          No domain recorded. Edit the profile above to add one; proving it is what lets this
          client's people reach their work by arriving on it.
        </p>
      </Panel>
    );
  }

  const stops: Stop[] = [
    {
      id: "domain",
      name: "Domain",
      state: account.domain.trim() === "" ? "waiting" : "done",
      sentence: "The client's own domain, as they use it.",
      answer: account.domain,
      body: (
        <p className="os-caption">
          Recorded on the profile above. Changing it clears the proof: proof of one name is not
          proof of another.
        </p>
      ),
    },
    {
      id: "ownership",
      name: "Ownership",
      state: verified ? "done" : account.domainStatus === "verifying" ? "current" : "open",
      sentence: verified
        ? self
          ? "Verified by this cluster."
          : "Proven, and not checked again while the domain stands."
        : "Publish this record, and the cluster will see it.",
      answer: verified
        ? self
          ? "verified by this cluster"
          : `proven ${formatMoment(account.domainVerifiedAt)}`
        : account.domainStatus === "verifying"
          ? "not seen yet"
          : "not checked yet",
      body: self ? (
        <p className="os-caption">
          This is the cluster's own company, so nothing has to be proven to anybody.
        </p>
      ) : (
        <>
          {/* THE RECORD AS THREE PARTS, each copyable on its own: a person is
              pasting them into three different fields of a registrar's form,
              and one blob of text is one they have to split by hand. The
              Deployables domains stop set this shape. */}
          <dl className="os-dns-record">
            <div>
              <dt>Type</dt>
              <dd>
                <CopyValue value="TXT" label="Type" />
              </dd>
            </div>
            <div>
              <dt>Name</dt>
              <dd>
                <CopyValue value={`_memql-verify.${account.domain}`} label="Name" />
              </dd>
            </div>
            <div>
              <dt>Value</dt>
              <dd>
                <CopyValue value={account.domainToken} label="Value" />
              </dd>
            </div>
          </dl>
          {account.domainFailureReason === "" ? null : (
            <Notice
              tone="warn"
              sentence={reasonSentence(account.domainFailureReason)}
              detail={account.domainFailureDetail}
            />
          )}
          <p className="os-caption">
            {account.domainLastCheckedAt === ""
              ? "No check has run yet."
              : `Last looked ${formatMoment(account.domainLastCheckedAt)}.`}{" "}
            Checked every two minutes on its own; there is nothing to press.
          </p>
        </>
      ),
    },
    {
      id: "joining",
      name: "Joining",
      // `waiting`, never `pending`, before the proof: `pending` means NOT
      // REACHABLE, which dims the stop and gives it no disclosure at all -- so
      // the one sentence a person needs ("prove ownership first") would be
      // behind a stop they cannot open. Reachable-and-not-done is the truth.
      state: account.joinOnDomain ? "done" : verified ? "open" : "waiting",
      sentence: "Whether somebody arriving on this domain joins this client's group.",
      answer: account.joinOnDomain ? `on for @${account.domain}` : verified ? "off" : "",
      body: verified ? (
        <>
          <Check
            checked={account.joinOnDomain}
            onChange={(next) => void update.setJoining(account.id, next)}
            disabled={update.busy}
          >
            People with a verified address on @{account.domain} join {accountName(account)}'s group
          </Check>
          <p className="os-caption">
            It applies at arrival and to nobody already here. Somebody already on this cluster is
            placed by hand, in Users.
          </p>
        </>
      ) : (
        // NO CONTROL AT ALL before the proof, rather than a disabled one: the
        // engine refuses `domain_not_verified`, and a checkbox that can only
        // fail is one somebody has to read past to learn it is not for them.
        <p className="os-caption">Prove ownership first.</p>
      ),
    },
    {
      id: "memql",
      name: "MemQL address",
      state: account.memqlReservedAt === "" ? (verified ? "open" : "waiting") : "done",
      sentence: "The name this client is reserved under on this cluster.",
      answer: account.memqlDomain,
      body: (
        <>
          <p className="os-caption">
            {self
              ? "This cluster's own."
              : account.memqlReservedAt === ""
                ? "Recorded once ownership is proven."
                : `Reserved ${formatMoment(account.memqlReservedAt)}.`}
          </p>
          {account.memqlDomain === "" ? null : (
            <p className="os-account-hosts">
              {["app", "api", "id"].map((host) => (
                <Chip key={host}>{`${host}.${account.memqlDomain}`}</Chip>
              ))}
            </p>
          )}
          <p className="os-caption">
            Recorded now; served when the per-account front door lands.
          </p>
        </>
      ),
    },
  ];

  return (
    <Panel label={`The domain of ${accountName(account)}`}>
      <Subhead>Domain</Subhead>
      {/* COLLAPSED, ONE OPEN, and the one it opens by default is the first
          stop still to do -- which on an unproven domain is Ownership, the
          only stop with something for a person to act on. This is the RECORD
          reading of the control (the Deployables page's), not the compose one:
          every stop here has an answer to show, and four open bodies would be
          four panels with a line drawn down them. */}
      <Rail
        stops={stops}
        label="What this client's domain buys them"
        openStop={openStop === "" ? nextOpen(stops) : openStop}
        onOpenStop={setOpenStop}
      />
    </Panel>
  );
}

/**
 * A typed failure reason, in words.
 *
 * NOT an enum on the row, deliberately -- a resolver reports things a closed
 * set written today does not anticipate -- so an unrecognised value keeps its
 * own name rather than being dressed as one of these.
 */
function reasonSentence(reason: string): string {
  switch (reason) {
    case "dns_token_missing":
      return "The record is not there yet.";
    default:
      return `The last check reported ${reason}.`;
  }
}
