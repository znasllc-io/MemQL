// What commit this extension was built from (memql#5076).
//
// WHY THE EXTENSION HAS TO KNOW ITS OWN COMMIT. It drives a checkout that moves
// independently of it, and neither side could tell the operator they had
// diverged. memql#5056 and memql#5064 were both that, and both presented as
// something else. The observed shape, twice:
//
//     buildinfo.commit=07f9747c1   <- the checkout had the fix
//     staged scripts               <- the extension did not
//
// An operator reading that log has no way to see the skew, so it reads as the
// product being broken. Both investigations took hours; a line naming the two
// commits would have ended them at a glance.
//
// IT IS A FACT, NOT A FAULT. A checkout WEEKS ahead of the extension driving it
// is the NORMAL state for a from-source install -- `install-main` clones main
// HEAD while the extension was packaged at whatever commit it was built from.
// So every surface that uses this states it and none of them blocks on it.
//
// Deliberately free of `vscode` imports (cmd/memql-lsp/vscodeimportrule_test.go)
// so it is unit-testable under plain `node --test`.

import * as fs from "node:fs";
import * as path from "node:path";

import { STAGED_ROOT_DIR } from "../install/root.js";

/** The file `scripts/vscode/package.sh` writes into the staged tree. */
export const BUILD_INFO_FILE = "buildinfo.json";

export interface BuildStamp {
  /** The commit the extension was packaged from, or "" when not recorded. */
  commit: string;
  /** Whether that commit had uncommitted edits on top of it at build time. */
  dirty: boolean;
  /** `package.json`'s version at build time. */
  version: string;
  builtAt: string;
}

/**
 * Reads the stamp a packaged extension carries, or undefined.
 *
 * UNDEFINED IS A REAL ANSWER and the surfaces must render it as one. An
 * extension running out of a checkout in the Extension Development Host was
 * never packaged, so there is no stamp and nothing to compare -- and an
 * unstamped build claiming to match the checkout would be exactly the
 * confident-and-wrong statement this whole module exists to replace.
 *
 * A stamp with an EMPTY commit is also undefined here rather than a stamp with
 * a blank field: packaging without git records "", and a caller that had to
 * remember to check both the presence of the object and the emptiness of the
 * string would eventually not.
 */
export function readBuildStamp(extensionPath: string | undefined): BuildStamp | undefined {
  // A BLANK PATH IS "NOT RECORDED", NOT A CRASH. `path.join(undefined, ...)`
  // throws ERR_INVALID_ARG_TYPE, and this is called during activation -- so a
  // host that hands over no extensionPath (the Extension Development Host on
  // some launch shapes, and every test double) would take the whole activation
  // down over a diagnostic field. The one thing this function must never do is
  // be load-bearing.
  const root = (extensionPath ?? "").trim();
  if (root === "") return undefined;
  const file = path.join(root, STAGED_ROOT_DIR, BUILD_INFO_FILE);
  let raw: string;
  try {
    raw = fs.readFileSync(file, "utf8");
  } catch {
    return undefined;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    // A corrupt stamp is not a crash. It is the same answer as no stamp:
    // nothing here is known.
    return undefined;
  }
  if (typeof parsed !== "object" || parsed === null) return undefined;
  const obj = parsed as Record<string, unknown>;
  const commit = typeof obj.commit === "string" ? obj.commit.trim() : "";
  if (commit === "") return undefined;
  return {
    commit,
    dirty: obj.dirty === true,
    version: typeof obj.version === "string" ? obj.version : "",
    builtAt: typeof obj.builtAt === "string" ? obj.builtAt : "",
  };
}
