import { Button, Caption, CopyField, Notice, Rail, type Stop } from "../../../../kit";
import { machineModelsFrom } from "../../machines/models";
import { machineName, type MachineRow } from "../../rows";
import { AskIt } from "../AskIt";
import type { Check } from "../flow";

// CHECKS: online, steady, and what you asked for (design record D4, D5, D13,
// D14).
//
// ===========================================================================
// A RAIL INSIDE A STOP, NOT A SECOND VOCABULARY
// ===========================================================================
// Each check is a mark, a name and its answer -- which is exactly what a rail
// stop is -- so the checks draw as a short rail of their own beneath the
// Checks stop, with the same marks, the same colours and the same states the
// page's rail uses. A list with its own dots would be a second way of saying
// "done", "moving", "yours", and a person reading down the page would have to
// learn it.
//
// No `openStop`: every check renders its body, because a check's body is its
// repair, and a repair folded behind a chevron is a repair somebody misses.
// Settled checks with nothing to offer have no body at all.

export function ChecksStop({
  checks,
  machine,
  pulling,
  pullError,
  onPullRecommended,
}: {
  checks: readonly Check[];
  machine: MachineRow;
  /** Whether the recommended pull has been asked for and not yet answered. */
  pulling: boolean;
  /** The cluster's refusal of the pull, verbatim, or "". */
  pullError: string;
  onPullRecommended: () => void;
}) {
  const label = machineName(machine);
  const firstModel = machineModelsFrom(machine.reportedLabels)[0]?.modelId ?? "";

  const stops: Stop[] = checks.map((check) => ({
    id: check.id,
    name: check.name,
    state: check.state,
    sentence: check.answer,
    body: bodyFor(check),
  }));

  return (
    <div className="os-fleet-checks">
      <Rail stops={stops} label="Checks on this machine" />
    </div>
  );

  function bodyFor(check: Check) {
    const hasRepair = check.repair !== undefined;
    const hasAct = check.act !== undefined;
    if (!hasRepair && !hasAct) return undefined;
    return (
      <div className="os-stop-body os-fleet-repair">
        {check.act === "pullRecommended" ? (
          <div className="os-fleet-repair-act">
            <Button tone="primary" busy={pulling} busyLabel="Asking the machine..." onClick={onPullRecommended}>
              Pull the recommended models
            </Button>
            <Caption>Onto {label}, in the order the catalog recommends for its class. Several gigabytes.</Caption>
          </div>
        ) : null}
        {check.act === "pullRecommended" && pullError !== "" ? (
          <Notice
            tone="error"
            sentence="The pull was not started."
            next="Nothing was written. The cluster's own reason is below; the setup command on the machine still works."
            detail={pullError}
          />
        ) : null}
        {check.repair === undefined ? null : <Caption>{check.repair}</Caption>}
        {check.command === undefined ? null : (
          <CopyField value={check.command} label={`the ${check.name.toLowerCase()} command`} />
        )}
        {check.act === "askIt" && firstModel !== "" ? <AskIt modelId={firstModel} machineLabel={label} /> : null}
      </div>
    );
  }
}
