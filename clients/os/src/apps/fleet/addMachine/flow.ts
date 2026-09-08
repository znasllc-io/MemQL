import type { StopState } from "../../../kit/Rail";
import { formatFreshness } from "../../../kit/format";
import { isWorkerOnline, ONLINE_WINDOW_SECONDS } from "../online";
import { hasRoundTrip, isRevoked, machineName, type MachineRow } from "../rows";
import { machineModelsFrom, type ModelPull } from "../machines/models";
import {
  INFERENCE_SETUP_COMMAND,
  INSTALL_PLATFORM_LABEL,
  PERMISSIONS_SETUP_COMMAND,
  type InstallPlatform,
} from "./install";

// WHAT THE GUIDED INSTALL KNOWS, as functions over values (design record
// 2026-09-08-cockpit-install-wizard, D2 to D6).
//
// ===========================================================================
// PURE, BECAUSE THE CLAIMS ARE ABOUT THE READING AND NOT THE PICTURE
// ===========================================================================
// "The machine that arrived is the one we minted for", "two heartbeats past
// the registration is steady", "a missing permission is a repair the person
// makes and the feed settles", "Cancel after a mint asks which of two things"
// are statements about this file. Asserting them through a rendered rail
// would make each one an assertion about the rail as well, and the rail has
// its own tests. No React here.
//
// ===========================================================================
// THE ORDER IS A LAW HERE, AND THE RAIL MOVES BY WHAT HAPPENS
// ===========================================================================
// The first-run rail is a set of doors that may be opened in any order. This
// one is a sequence: no command without a token, no connection without the
// command having run, no check without a registration. So the unreached stops
// draw `ahead`, and the rail moves on a mint, a registration, a heartbeat --
// never on a click. There is still no Next: the mint is the bar's forward
// act, and everything after it is answered by the cluster.

export type Phase = "describe" | "minting" | "waiting" | "connected";

export interface Draft {
  name: string;
  platform: InstallPlatform;
  computerUse: boolean;
  inference: boolean;
}

export const EMPTY_DRAFT: Draft = { name: "", platform: "mac", computerUse: false, inference: false };

/** What the mint returned: the plain token, shown once, and the identity the
 *  machine will authenticate as -- which is how its registration is matched. */
export interface Mint {
  token: string;
  identityId: string;
}

export interface FlowFacts {
  draft: Draft;
  /** A live connection to the cluster. Nothing can be minted without one. */
  connected: boolean;
  minting: boolean;
  /** The server's refusal, verbatim, or "". */
  mintError: string;
  mint: Mint | null;
  /** When the mint landed, for the "this is taking a while" reading. */
  mintedAt: Date | null;
  /** The registration matched to `mint.identityId`, or null while waiting. */
  machine: MachineRow | null;
  /** Heartbeats heard since the registration first appeared. */
  beats: number;
  /** Whether Cancel has been pressed while a token exists and the person has
   *  not yet answered which of two things to do. */
  cancelAsked: boolean;
  /** The revoke's refusal, verbatim, or "". */
  revokeError: string;
  revoking: boolean;
  now: Date;
}

export function phaseOf(facts: Pick<FlowFacts, "minting" | "mint" | "machine">): Phase {
  if (facts.minting) return "minting";
  if (facts.mint === null) return "describe";
  if (facts.machine === null) return "waiting";
  return "connected";
}

// ---------------------------------------------------------------------------
// Matching the registration
// ---------------------------------------------------------------------------

/**
 * Whether two ids name the same row across the bare/canonical seam.
 *
 * The mint reply and a bare-ified graph row are two egress seams, and the
 * client contract says the engine owns both directions -- so this compares
 * the trailing segment, the way the presence map in live/machines.tsx already
 * reads a registration id, rather than composing or parsing anything. Two
 * empty ids are not the same id: an unmatched registration carries "" and a
 * flow with no mint carries "", and equating them would match every machine.
 */
export function sameId(a: string, b: string): boolean {
  const left = bare(a);
  const right = bare(b);
  return left !== "" && left === right;
}

function bare(id: string): string {
  const trimmed = id.trim();
  const at = trimmed.lastIndexOf(":");
  return at === -1 ? trimmed : trimmed.slice(at + 1);
}

/**
 * The registration this flow minted for: the unrevoked row whose
 * `identityId` is the mint's. MATCHED, never counted (D3).
 *
 * Counting the population lied in both directions. Two people adding
 * machines at once, or any other machine of yours reconnecting, grew the
 * count with the wrong machine; a machine of yours revoked elsewhere shrank
 * it and hid a real arrival. A revoked row never matches: a token revoked
 * from the cancel question and then pasted anyway must not read as success.
 */
export function matchRegistration(rows: readonly MachineRow[], identityId: string): MachineRow | null {
  if (identityId.trim() === "") return null;
  return rows.find((row) => !isRevoked(row) && sameId(row.identityId, identityId)) ?? null;
}

// ---------------------------------------------------------------------------
// Heartbeats
// ---------------------------------------------------------------------------

/** How many heartbeats past the registration count as "staying". Two, because
 *  that is the online window (30 s at the 15 s cadence): a machine heard from
 *  across one whole window is one that the service kept alive, not one whose
 *  installer happened to still be running. */
export const STEADY_BEATS = 2;

export function isSteady(machine: MachineRow | null, beats: number, now: Date): boolean {
  return machine !== null && beats >= STEADY_BEATS && isWorkerOnline(machine, now);
}

// ---------------------------------------------------------------------------
// The checks (D4, D5)
// ---------------------------------------------------------------------------

/** The rail's own vocabulary, so a check draws with the rail's marks:
 *  `done` settled, `current` the machine is moving, `open` waiting on the
 *  person, `skipped` with a reason, `stopped` something went wrong, `unknown`
 *  the cockpit did not say, `ahead` not reachable until another check is. */
export type CheckState = Extract<StopState, "done" | "current" | "open" | "skipped" | "stopped" | "unknown" | "ahead">;

export interface Check {
  id: "connection" | "build" | "permissions" | "display" | "runtime" | "models";
  name: string;
  state: CheckState;
  /** The state in words: the answer once settled, what is being waited on
   *  otherwise. */
  answer: string;
  /** What the PERSON does, when the check is `open`. */
  repair?: string;
  /** The exact command the repair takes, when it takes one. */
  command?: string;
  /** An act the OS can take on the person's behalf, offered beside the
   *  repair: pull the recommended models onto the machine (D13), or ask the
   *  served model something to prove the whole path (D14). */
  act?: "pullRecommended" | "askIt";
}

const WORKER_LOG = "~/.memql/state/worker.log";

/** What the flow knows about pulls onto the matched machine: the one still
 *  running, if any. Read from the `modelPullsForWorker` feed by the hook. */
export interface PullFacts {
  live: ModelPull | null;
}

const NO_PULLS: PullFacts = { live: null };

export function checksFor(
  draft: Draft,
  machine: MachineRow,
  beats: number,
  now: Date,
  pulls: PullFacts = NO_PULLS,
): Check[] {
  const checks: Check[] = [connectionCheck(machine, beats, now), buildCheck(draft, machine)];
  if (draft.computerUse && draft.platform === "mac") checks.push(permissionsCheck(machine));
  if (draft.computerUse && draft.platform === "linux") checks.push(displayCheck(machine));
  if (draft.inference) checks.push(...inferenceChecks(machine, pulls));
  return checks;
}

/** The cluster's own round trip, in words, or "" when never measured. */
export function roundTripSentence(machine: MachineRow, now: Date): string {
  if (!hasRoundTrip(machine)) return "";
  return `round trip ${machine.rttMs} ms, checked ${formatFreshness(machine.rttAt, now)}`;
}

function connectionCheck(machine: MachineRow, beats: number, now: Date): Check {
  const online = isWorkerOnline(machine, now);
  const last = formatFreshness(machine.lastSeenAt, now);
  if (!online) {
    return {
      id: "connection",
      name: "Connection",
      state: "stopped",
      answer: `Nothing heard for more than ${ONLINE_WINDOW_SECONDS} s -- last heartbeat ${last}.`,
      repair: `On the machine, read the worker log: ${WORKER_LOG}. A worker that cannot reach the cluster says why there.`,
    };
  }
  const rtt = roundTripSentence(machine, now);
  if (beats >= STEADY_BEATS) {
    return {
      id: "connection",
      name: "Connection",
      state: "done",
      answer: `Online and steady -- heartbeats every 15 s, last ${last}${rtt === "" ? "" : `; ${rtt}`}.`,
    };
  }
  return {
    id: "connection",
    name: "Connection",
    state: "current",
    answer:
      beats === 0
        ? "Connected. Listening for its first heartbeat."
        : `Heartbeat ${beats} of ${STEADY_BEATS} heard, last ${last}.`,
  };
}

function buildCheck(draft: Draft, machine: MachineRow): Check {
  const version = machine.version.trim() === "" ? "" : ` (cockpit ${machine.version.trim()})`;
  const tag = machine.buildTag.trim();
  const hasComputerUse = machine.capabilities.includes("COMPUTERUSE") || tag === "computeruse";
  if (draft.computerUse) {
    if (tag === "headless") {
      return {
        id: "build",
        name: "Build",
        state: "stopped",
        answer: `The headless build registered${version}.`,
        repair: "The line was run without --computeruse. Run it again with the flag; the installer replaces the binary in place.",
      };
    }
    if (tag === "computeruse" || hasComputerUse) {
      return { id: "build", name: "Build", state: "done", answer: `Computer-use build${version}.` };
    }
    return {
      id: "build",
      name: "Build",
      state: "unknown",
      answer: `The cockpit did not say which build it is${version}.`,
    };
  }
  if (hasComputerUse) {
    // Not what was asked, and not wrong: a machine that can do more than the
    // line asked for is a fact, never a failure.
    return { id: "build", name: "Build", state: "done", answer: `Computer-use build${version}.` };
  }
  return { id: "build", name: "Build", state: "done", answer: `Headless build${version}.` };
}

function permissionsCheck(machine: MachineRow): Check {
  const p = machine.permissions;
  if (!p.present) {
    return {
      id: "permissions",
      name: "macOS permissions",
      state: "unknown",
      answer: "Not reported -- this cockpit predates the permissions report. Nothing is wrong.",
    };
  }
  const missing = [
    ...(p.accessibility ? [] : ["Accessibility"]),
    ...(p.screenRecording ? [] : ["Screen Recording"]),
  ];
  if (missing.length === 0) {
    return {
      id: "permissions",
      name: "macOS permissions",
      state: "done",
      answer: "Accessibility and Screen Recording granted.",
    };
  }
  return {
    id: "permissions",
    name: "macOS permissions",
    state: "open",
    answer: `${missing.join(" and ")} not granted yet.`,
    repair: `On the machine, allow memql under System Settings -> Privacy & Security -> ${missing.join(" and ")}, then run this in a terminal so the worker re-checks and re-registers. This waits for it.`,
    command: PERMISSIONS_SETUP_COMMAND,
  };
}

function displayCheck(machine: MachineRow): Check {
  const server = machine.displayServer.trim();
  const x11 = machine.permissions.present ? machine.permissions.x11Display : null;
  if (server === "wayland" || (x11 === false && server !== "x11")) {
    return {
      id: "display",
      name: "Display",
      state: "skipped",
      answer:
        "Wayland session: the machine registered without mouse and keyboard. Computer use needs an X11 session; everything else works.",
    };
  }
  if (server === "x11" || x11 === true || machine.capabilities.includes("COMPUTERUSE")) {
    return { id: "display", name: "Display", state: "done", answer: "X11 display -- computer use available." };
  }
  return {
    id: "display",
    name: "Display",
    state: "unknown",
    answer: "Not reported -- this cockpit predates the display report. Nothing is wrong.",
  };
}

function inferenceChecks(machine: MachineRow, pulls: PullFacts): Check[] {
  const models = machineModelsFrom(machine.reportedLabels);
  const runtimes = machine.hardware.runtimes.map((r) => r.name).filter((n) => n !== "");
  const runtimeNames = runtimes.join(", ");
  const runtime: Check =
    models.length > 0 || runtimes.length > 0
      ? {
          id: "runtime",
          name: "Model runtime",
          state: "done",
          answer: runtimes.length > 0 ? `Found: ${runtimeNames}.` : "A runtime is serving models.",
        }
      : {
          id: "runtime",
          name: "Model runtime",
          state: "open",
          answer: "No runtime reported.",
          repair:
            "The one-liner cannot install a runtime on its own -- it needs a person to approve the install commands. Run this in the same terminal on the machine; it sets up the runtime, pulls a starting model and tells the worker. This waits for it.",
          command: INFERENCE_SETUP_COMMAND,
        };
  let modelsCheck: Check;
  if (models.length > 0) {
    modelsCheck = {
      id: "models",
      name: "Models",
      state: "done",
      answer: `Serving ${models.length === 1 ? "one model" : `${models.length} models`}: ${models.map((m) => m.modelId).join(", ")}.`,
      act: "askIt",
    };
  } else if (runtime.state !== "done") {
    modelsCheck = { id: "models", name: "Models", state: "ahead", answer: "After the runtime." };
  } else if (pulls.live !== null) {
    // A PULL IN FLIGHT IS THE MACHINE MOVING. The line is the runtime's own
    // status, verbatim, because that is what a person reads when a pull
    // stalls -- and the model re-advertises when it is done, so this settles
    // from the feed rather than from the pull's end.
    const line = pulls.live.statusLine.trim();
    modelsCheck = {
      id: "models",
      name: "Models",
      state: "current",
      answer: `Pulling ${pulls.live.model}${line === "" ? "" : ` -- ${line}`}. The machine re-advertises when it is done.`,
    };
  } else {
    // THE RUNTIME IS THERE AND NOTHING IS ON IT. Two ways forward, and both
    // are offered: the OS can ask the machine to pull the recommended set
    // (D13), or the person runs the setup command on the machine, which pulls
    // a starting model itself.
    modelsCheck = {
      id: "models",
      name: "Models",
      state: "open",
      answer: "No models yet.",
      repair:
        "Pull the set the catalog recommends for this machine from here, or run the setup command on the machine, which pulls a starting model itself.",
      command: INFERENCE_SETUP_COMMAND,
      act: "pullRecommended",
    };
  }
  return [runtime, modelsCheck];
}

/** A check that asks nothing more of anybody. `unknown` is settled: "the
 *  cockpit did not say" is an answer, and a wizard that waited on an older
 *  cockpit to grow a field would wait forever. */
export function checkIsSettled(check: Check): boolean {
  return check.state === "done" || check.state === "skipped" || check.state === "unknown";
}

export function checksSettled(checks: readonly Check[]): boolean {
  return checks.length > 0 && checks.every(checkIsSettled);
}

// ---------------------------------------------------------------------------
// The stops (D2, D3)
// ---------------------------------------------------------------------------

export type StopId = "machine" | "install" | "connect" | "checks";

export interface FlowStop {
  id: StopId;
  name: string;
  state: StopState;
  /** What the stop is for, under its name while open. */
  sentence: string;
  /** The state in words, on the collapsed line. */
  answer: string;
}

export function draftSummary(draft: Draft): string {
  const parts = [INSTALL_PLATFORM_LABEL[draft.platform]];
  if (draft.computerUse) parts.push("computer-use build");
  if (draft.inference) parts.push("local models");
  const name = draft.name.trim();
  return name === "" ? parts.join(", ") : `${name} -- ${parts.join(", ")}`;
}

export function stopsFor(facts: FlowFacts, checks: readonly Check[]): FlowStop[] {
  const phase = phaseOf(facts);
  const machine = facts.machine;
  const host = machine === null ? "" : machine.hostname.trim() || machine.name.trim();
  const settled = checksSettled(checks);

  const machineStop: FlowStop = {
    id: "machine",
    name: "This machine",
    sentence: "What it is called, and what it will run.",
    state: phase === "describe" ? "open" : phase === "minting" ? "current" : "done",
    answer: phase === "describe" ? (facts.mintError === "" ? "" : "The token was not minted") : draftSummary(facts.draft),
  };

  const installStop: FlowStop = {
    id: "install",
    name: "Install",
    sentence: "One line, on the machine itself.",
    state: phase === "describe" || phase === "minting" ? "ahead" : phase === "waiting" ? "open" : "done",
    answer:
      phase === "waiting"
        ? "Run the command on the machine"
        : phase === "connected"
          ? host === ""
            ? "Installed"
            : `Installed on ${host}`
          : "",
  };

  const connectStop: FlowStop = {
    id: "connect",
    name: "Connect",
    sentence: "The cluster listens. Nothing to reload.",
    state: phase === "describe" || phase === "minting" ? "ahead" : phase === "waiting" ? "current" : "done",
    answer:
      phase === "waiting"
        ? "Listening for the machine"
        : phase === "connected" && machine !== null
          ? connectedAnswer(machine)
          : "",
  };

  // THE CHECKS STOP'S MARK IS THE CHECKS' OWN VERDICT: moving while the machine
  // is (a heartbeat, a pull), held while only the person's hand is missing (a
  // permission, a command), lit when nothing more is asked of anybody.
  const anyCurrent = checks.some((c) => c.state === "current");
  const anyOpen = checks.some((c) => c.state === "open");
  const anyStopped = checks.some((c) => c.state === "stopped");
  const checksStop: FlowStop = {
    id: "checks",
    name: "Checks",
    sentence: "Online, steady, and what you asked for.",
    state:
      phase !== "connected"
        ? "ahead"
        : settled
          ? "done"
          : anyStopped && !anyCurrent
            ? "stopped"
            : anyOpen && !anyCurrent
              ? "open"
              : "current",
    answer: phase !== "connected" ? "" : checksAnswer(checks),
  };

  return [machineStop, installStop, connectStop, checksStop];
}

function connectedAnswer(machine: MachineRow): string {
  const parts = [`Connected as ${machineName(machine)}`];
  if (machine.platform.trim() !== "") parts.push(machine.platform);
  if (machine.version.trim() !== "") parts.push(`cockpit ${machine.version.trim()}`);
  return parts.join(" -- ");
}

/**
 * The Checks stop's collapsed line: a COUNT and the name of what is next, never
 * the next check's own sentence. That sentence is drawn one line below on the
 * checks' own rail the moment the stop is open, and the rail's rule is that a
 * line says something the line above does not.
 */
function checksAnswer(checks: readonly Check[]): string {
  const settled = checks.filter(checkIsSettled).length;
  if (settled === checks.length) return "All settled";
  const next = checks.find((c) => !checkIsSettled(c) && c.state !== "ahead");
  const count = `${settled} of ${checks.length} settled`;
  return next === undefined ? count : `${count} -- ${next.name.toLowerCase()}`;
}

/** The stop the rail opens when nobody has chosen one: the person's own
 *  stop, or the one the cluster is answering. */
export function openStopFor(stops: readonly FlowStop[]): StopId {
  const open = stops.find((s) => s.state === "open");
  if (open) return open.id;
  const current = stops.find((s) => s.state === "current" || s.state === "stopped");
  if (current) return current.id;
  return stops[stops.length - 1]?.id ?? "machine";
}

// ---------------------------------------------------------------------------
// The action bar (D6, interface rule 12)
// ---------------------------------------------------------------------------

export type ActId = "cancel" | "mint" | "keepWaiting" | "revokeAndLeave" | "leaveKeepToken" | "open" | "done";

export interface FlowAct {
  id: ActId;
  label: string;
  tone: "quiet" | "primary" | "danger";
  busy?: boolean;
}

export interface FlowBar {
  state: string;
  detail: string;
  tone: "live" | "paused" | "busy" | "none";
  /** Prose the bar carries in place of nothing: the cancel question. */
  question: string;
  acts: FlowAct[];
}

/** How long a wait is before the Connect stop names the usual causes. */
export const LONG_WAIT_MS = 10 * 60 * 1000;

export function waitedLong(facts: Pick<FlowFacts, "mintedAt" | "now">): boolean {
  return facts.mintedAt !== null && facts.now.getTime() - facts.mintedAt.getTime() >= LONG_WAIT_MS;
}

export function barFor(facts: FlowFacts, checks: readonly Check[]): FlowBar {
  const phase = phaseOf(facts);
  const name = facts.draft.name.trim();
  const label = name === "" ? "the machine" : name;

  if (phase === "minting") {
    return { state: "Minting a token", detail: `for ${label}`, tone: "busy", question: "", acts: [] };
  }

  if (phase === "describe") {
    const canMint = facts.connected && name !== "";
    return {
      state: facts.mintError === "" ? "Describe the machine" : "The token was not minted",
      detail:
        facts.mintError !== ""
          ? "nothing was created; mint again"
          : !facts.connected
            ? "a token can only be minted over a live connection to the cluster"
            : name === ""
              ? "a name, and which operating system it runs"
              : "then mint the token the install command carries",
      tone: "none",
      question: "",
      acts: [
        { id: "cancel", label: "Cancel", tone: "quiet" },
        // ABSENT, NEVER DISABLED (rule 12): a mint with no name is refused by
        // the engine, so it is not offered until there is one.
        ...(canMint ? [{ id: "mint" as const, label: "Mint a token", tone: "primary" as const }] : []),
      ],
    };
  }

  if (phase === "waiting") {
    if (facts.cancelAsked) {
      return {
        state: "Leave?",
        detail: "",
        tone: "paused",
        question:
          facts.revokeError === ""
            ? "The token you copied still works: if the install finishes later, the machine appears in Machines on its own. Revoke it if it is not going to be used -- a credential nobody will use should not stay live."
            : `The token was not revoked: ${facts.revokeError}. It is still live; you can leave and keep it, or try again.`,
        acts: [
          { id: "keepWaiting", label: "Keep waiting", tone: "quiet" },
          { id: "revokeAndLeave", label: "Revoke the token and leave", tone: "danger", busy: facts.revoking },
          { id: "leaveKeepToken", label: "Leave, keep the token", tone: "primary" },
        ],
      };
    }
    return {
      state: `Waiting for ${label}`,
      detail: waitedLong(facts)
        ? "this is taking a while -- the Connect stop names what usually went wrong"
        : "run the command on the machine; this moves on by itself the moment it registers",
      tone: "busy",
      question: "",
      acts: [{ id: "cancel", label: "Cancel", tone: "quiet" }],
    };
  }

  // connected
  const machine = facts.machine;
  const shown = machine === null ? label : machineName(machine);
  const settled = checksSettled(checks);
  const pending = checks.find((c) => !checkIsSettled(c) && c.state !== "ahead");
  const stopped = checks.some((c) => c.state === "stopped");
  return {
    state: settled ? "Ready" : stopped ? "Connected, with a problem" : "Connected",
    detail: settled
      ? `${shown} is online and steady, and every check settled`
      : pending === undefined
        ? `${shown} is online`
        : `${pending.name.toLowerCase()}: ${pending.answer}`,
    tone: settled ? "live" : stopped ? "paused" : "busy",
    question: "",
    acts: [
      { id: "open", label: `Open ${shown}`, tone: "quiet" },
      // Done is legal the moment the machine has registered -- leaving with a
      // repair outstanding is the person's call, and the machine is theirs
      // either way -- but it is only PRIMARY once nothing is outstanding.
      { id: "done", label: "Done", tone: settled ? "primary" : "quiet" },
    ],
  };
}

// ---------------------------------------------------------------------------
// The manual steps (D5)
// ---------------------------------------------------------------------------

/** What the one-liner installs to keep the worker running, per platform --
 *  stated as a fact about the command, never as a check the OS cannot make. */
export function serviceSentence(platform: InstallPlatform): string {
  return platform === "mac"
    ? "The command installs a LaunchAgent (com.znasllc.memql-worker) that starts at login, so the machine reconnects after a restart."
    : "The command installs a user systemd unit (memql-worker.service) that starts at login and restarts on failure, so the machine reconnects after a restart.";
}

/** The manual steps, in the order they happen on the machine. */
export function installSteps(draft: Draft): string[] {
  const steps = [
    "Open a terminal on the machine you are adding.",
    "Paste the command and press Enter. It downloads the cockpit and asks for your account password, because it installs the memql command under /usr/local/bin. Add --user-local to the line to skip the password; the install is then only as safe as your account.",
  ];
  if (draft.computerUse && draft.platform === "mac") {
    steps.push(
      "The first time the worker runs, macOS asks for Accessibility and Screen Recording. Approve both; the checks below wait for it.",
    );
  }
  if (draft.computerUse && draft.platform === "linux") {
    steps.push(
      "On a Wayland session the worker registers without mouse and keyboard; computer use needs an X11 session. Everything else works either way.",
    );
  }
  if (draft.inference) {
    steps.push(
      "The installer then checks the hardware, sets up a model runtime and pulls a starting model in the same terminal -- several gigabytes, so it takes a while. If it cannot install the runtime on its own it prints the command to run yourself.",
    );
  }
  steps.push(`Leave the terminal open until it prints SUCCESS. ${serviceSentence(draft.platform)}`);
  return steps;
}
