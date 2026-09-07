// The "Before it runs" list for a rebuild (memql#4246).
//
// PURE: the panel gathers the facts, this states them. Every item is a sentence
// the operator can check, and the one that carries the epic is "Image source".
// A rebuild moves a cluster off released images and onto ones built from a
// working tree, and nothing else on the machine announces that -- afterwards
// the Deployments row simply stops naming a version and starts naming a commit.
// So the crossing is STATED HERE, before it happens, and its mirror is stated
// in preflight.ts before an install, upgrade or repair crosses back. Neither
// direction is silent.
//
// It shares `PreflightItem` with the install checklist rather than defining a
// second shape, so both render through one `renderPreflight` and a warning
// cannot come to look like two different things in one extension.
//
// Deliberately free of `vscode` imports (cmd/memql-lsp/vscodeimportrule_test.go).
//
// Refs: #4246 #4195

import type { CheckoutState } from "../install/checkoutState.js";
import { normalizeNodeList } from "../install/nodeList.js";
import type { ImageSource } from "../install/receipt.js";
import { checkoutSkew } from "../version/checkoutSkew.js";
import { releasedImages } from "./imageLane.js";
import type { PreflightItem } from "./preflight.js";

export interface RebuildPreflightInputs {
  dockerReachable: boolean;
  checkoutDir: string;
  /** Both files `k3d.dev` itself gates on: a Dockerfile and the local overlay. */
  checkoutIsMemql: boolean;
  /** Absent when git could not read the checkout -- which is its own line. */
  state?: CheckoutState;
  /** Comma-separated node types, or "" for all app nodes. */
  nodes: string;
  /** Which lane set the images last. "" is no evidence, never "released". */
  imageSource: ImageSource | "";
  /** The release the cluster would return to. Empty renders as "release". */
  releasedTag: string;
  /**
   * The commit THIS EXTENSION was packaged from, and whether that build carried
   * uncommitted edits (memql#5076). Both absent for an unpackaged extension.
   */
  extensionCommit?: string;
  extensionDirty?: boolean;
}

export function rebuildPreflightItems(i: RebuildPreflightInputs): PreflightItem[] {
  const items: PreflightItem[] = [];

  // Docker first, because it is the one prerequisite whose absence makes every
  // other line moot -- the build is the first thing the step does.
  items.push(
    i.dockerReachable
      ? { label: "Docker", state: "ok", detail: "The Docker daemon answers." }
      : {
          label: "Docker",
          state: "attention",
          detail:
            "Docker is not answering. Start Docker Desktop (or the daemon) before running; " +
            "the build is the first thing this does.",
        },
  );

  items.push(
    i.checkoutIsMemql
      ? {
          label: "Checkout",
          state: "ok",
          detail: `${i.checkoutDir} -- the checkout the install recorded.`,
        }
      : {
          label: "Checkout",
          state: "attention",
          detail:
            `${i.checkoutDir} is not a MemQL checkout (no Dockerfile or local overlay). ` +
            "Repair the install to clone one.",
        },
  );

  // WHAT WOULD BE BUILT. A line that could not be read says so rather than
  // reporting a clean tree: "clean at 0000000" in front of a build whose
  // provenance nothing recorded is worse than saying nothing at all.
  if (i.state === undefined) {
    items.push({
      label: "Git state",
      state: "attention",
      detail: "git could not read the checkout; what gets built will not be recorded.",
    });
  } else {
    const ref =
      i.state.ref.kind === "detached" ? "detached HEAD" : `${i.state.ref.kind} ${i.state.ref.name}`;
    const dirty =
      i.state.dirtyCount === 0
        ? "clean"
        : `${i.state.dirtyCount} uncommitted file${i.state.dirtyCount === 1 ? "" : "s"}`;
    // deploy/ IS THE ONE PATH WORTH NAMING. A rebuild builds and imports IMAGES;
    // ArgoCD reconciles the manifests from the checkout's target revision, so an
    // uncommitted manifest edit is exactly the change a developer would expect
    // this button to apply and the one it will not.
    const deploy = i.state.deployDirty
      ? " deploy/ has edits -- manifests do not ride a rebuild, only images do."
      : "";
    items.push({
      label: "Git state",
      state: i.state.deployDirty ? "attention" : "ok",
      detail: `${ref} at ${i.state.commit.slice(0, 7)}, ${dirty}.${deploy}`,
    });
  }

  // WORDED FROM WHAT THE RUN WILL BE SENT, not from the raw typing. The two used
  // to be different rules -- this one tidied "bff, agent" for the sentence while
  // the plan forwarded it verbatim -- so the checklist blessed a list the script
  // then refused with exit 2 (install/nodeList.ts).
  const nodes = normalizeNodeList(i.nodes);
  items.push({
    label: "Nodes",
    state: "ok",
    detail:
      nodes === ""
        ? "all app nodes (the script's default)."
        : nodes.split(",").join(", ") + ".",
  });

  // THE LANE CROSSING. Stated when it is a crossing and not when it is not: a
  // line that appeared on every rebuild would be noise, and noise is what makes
  // the one that matters unreadable.
  items.push(
    i.imageSource === "checkout"
      ? {
          label: "Image source",
          state: "ok",
          detail: "local already runs checkout-built images; this rebuilds them.",
        }
      : {
          label: "Image source",
          state: "attention",
          detail:
            "This switches local to images built from your checkout. An install, upgrade or " +
            `repair returns it to ${releasedImages(i.releasedTag)}.`,
        },
  );

  // THE SKEW, WHERE THE CONSEQUENCE LANDS (memql#5076). This is the second of
  // the issue's two tiers -- the first is a quiet fact on the instance page --
  // and it is here rather than only there because HERE is where an operator is
  // about to act on the difference.
  //
  // The difference is real and it is not a bug: since memql#5056 and
  // memql#5064 a build from the checkout runs the CHECKOUT's scripts, so the
  // recipe matches the tree, while the graph documents, the plan functions,
  // this checklist and the panel are whatever the extension was packaged with.
  // Both of those issues presented as the product being broken, and both cost
  // hours, because nothing said the two halves came from two commits.
  //
  // IT DOES NOT BLOCK. A checkout ahead of the extension is the normal state of
  // a from-source install; refusing on it would refuse the ordinary case. The
  // third tier the issue sketches -- refusing a genuinely incompatible pair --
  // needs a compatibility contract to check against, and there is not one.
  items.push(buildProvenanceItem(i));

  items.push({
    label: "Duration",
    state: "ok",
    detail: "A first build takes minutes; later builds reuse Docker's cache.",
  });

  return items;
}

/**
 * The "which code is driving this" line, shared by the rebuild and the update.
 *
 * Exported so updatePreflight.ts states it identically rather than composing a
 * second wording -- the same reason version/checkoutSkew.ts owns the sentence.
 */
export function buildProvenanceItem(i: RebuildPreflightInputs): PreflightItem {
  const skew = checkoutSkew({
    extensionCommit: i.extensionCommit,
    extensionDirty: i.extensionDirty,
    // The checkout's own HEAD as git reads it NOW, which is the commit the
    // build will actually use -- not the one the receipt recorded at install
    // time, which is what the instance page compares and is exactly as stale
    // as the last install.
    checkoutCommit: i.state?.commit,
  });
  return {
    label: "Extension",
    // `unknown` is `attention` for Git state's reason one block up: "cannot
    // tell you" is something to look at, and reporting it as fine would be the
    // clean-tree-over-an-unreadable-repo answer that file already refuses.
    state: skew.state === "same" ? "ok" : "attention",
    detail: skew.sentence,
  };
}
