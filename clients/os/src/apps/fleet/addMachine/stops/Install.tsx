import { Caption, CopyField, Notice, Subhead } from "../../../../kit";
import { installSteps, type Draft } from "../flow";
import {
  CLUSTER_URL_PLACEHOLDER,
  INFERENCE_SETUP_COMMAND,
  INSTALL_PLATFORM_LABEL,
  installCommand,
  workerClusterUrl,
} from "../install";

// INSTALL: one line, on the machine itself -- and what happens when it is
// pasted, in the order it happens (design record D5, D13, D15).
//
// ===========================================================================
// THE TOKEN IS SHOWN ONCE AND THEN IT IS GONE
// ===========================================================================
// CreateWorkerTokenMsg returns the plain `mql_wkr_...` bearer in the reply
// and nothing keeps it: only its SHA-256 hash lands on the identity row. So
// this stop is the only place it exists, and the copy says so rather than
// leaving a person to discover it by closing the window. It is deliberately
// NOT written to localStorage, sessionStorage or a URL -- see
// useAddMachineFlow.ts, where the reason and the consequence live.
//
// ===========================================================================
// THE STEPS ARE NUMBERED BECAUSE THEY ARE A SEQUENCE
// ===========================================================================
// A terminal, a paste, a password prompt, a permission dialog, a download:
// each happens after the one before, on the machine, and the person reading
// this is about to walk away from this screen to do them. Numbers are right
// here and nowhere else on the page.
//
// ===========================================================================
// LOCAL MODELS ARE A SECOND COMMAND, SAID UP FRONT (D13)
// ===========================================================================
// The one-liner runs without a terminal to ask on, so it cannot approve a
// runtime install; on a fresh machine `worker setup --inference` is always
// run by hand afterwards. Saying so here, beside the line, is what stops it
// reading as a failure when the installer prints it.

export function InstallStop({
  draft,
  token,
  domain,
}: {
  draft: Draft;
  token: string;
  /** The cluster's published domain, or "". */
  domain: string;
}) {
  const clusterUrl = workerClusterUrl(domain);
  const command = installCommand({
    platform: draft.platform,
    clusterUrl,
    token,
    computerUse: draft.computerUse,
    inference: draft.inference,
  });

  return (
    <div className="os-stop-body os-fleet-addstop">
      <Notice tone="warn">
        <p className="os-notice-line" role="alert">
          The token below lives only in this window. It is not shown again -- the cluster keeps
          only its hash, so there is nowhere to look it up. If it is lost, mint another one and
          revoke this machine.
        </p>
      </Notice>

      <Subhead>Token</Subhead>
      <CopyField value={token} label="the worker token" id="fleet-add-token" />

      <Subhead>Run this on {INSTALL_PLATFORM_LABEL[draft.platform]}</Subhead>
      <CopyField value={command} label="the install command" id="fleet-add-command" />

      {clusterUrl === "" ? (
        <Caption>
          This deployment publishes no domain, so {CLUSTER_URL_PLACEHOLDER} is a placeholder --
          substitute the address you reach this cluster's API at, with the scheme. A value with no
          scheme is dialled in the clear whatever its port.
        </Caption>
      ) : null}

      <ol className="os-fleet-steps" aria-label="What happens on the machine">
        {installSteps(draft).map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>

      {draft.inference ? (
        <>
          <Subhead>Then, for local models</Subhead>
          <CopyField value={INFERENCE_SETUP_COMMAND} label="the local models setup command" id="fleet-add-inference" />
          <Caption>
            Run it in the same terminal once the installer prints SUCCESS. It checks the hardware,
            installs a model runtime after showing you the commands, pulls a starting model and tells
            the worker. The Checks stop notices on its own when the models appear.
          </Caption>
        </>
      ) : null}

      <Caption>
        The installer and the worker ship from the memql-cockpit repository -- the worker is a run
        mode of the memql command that repo builds. The full walkthrough is in the workers runbook.
      </Caption>
    </div>
  );
}
