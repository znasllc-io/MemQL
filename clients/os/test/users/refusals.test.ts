import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { SERVER_SENTENCE_ONLY, copyFor, knownCodes, refusalFrom } from "../../src/apps/users/refusals";

// Every refusal code the groups plug-in and the role builtins can raise has a
// home in this build: copy in the table, or an explicit listing as "the
// server's sentence is the whole copy". Neither home is guessed at render
// time, so the ONLY way a code arrives unnamed is that somebody added it
// engine-side and nobody told the OS. This reads the engine to find out.
//
// It is the `test/deployables/refusals.test.ts` pattern, pointed at two
// packages instead of one, and it reads the same two shapes: the `CodeX =
// "..."` constants, and any code spelled inline at a raise site.

const here = dirname(fileURLToPath(import.meta.url));
const integrations = join(here, "../../../../integrations");
const GO_PACKAGES = ["groups", "rbac"];

/** Every code the named packages declare or spell inline. */
function engineCodes(): string[] {
  const out = new Set<string>();
  for (const pkg of GO_PACKAGES) {
    const dir = join(integrations, pkg);
    if (!existsSync(dir)) continue;
    for (const name of readdirSync(dir)) {
      if (!name.endsWith(".go") || name.endsWith("_test.go")) continue;
      const source = readFileSync(join(dir, name), "utf8");
      // The declared constants, in BOTH spellings. integrations/groups exports
      // them (`CodeSelfAddRefused = "group_self_add_refused"`) and
      // integrations/rbac keeps them package-private (`codeRankTaken =
      // "role_rank_taken"`) -- and a scan that matched only the exported
      // spelling found NOTHING in rbac and passed, which is exactly the shape
      // of vacuous gate this file exists to be.
      for (const m of source.matchAll(/^\s*[Cc]ode\w+\s*=\s*"([a-z][a-z0-9_]*)"/gm)) out.add(m[1]!);
      // A code spelled at the raise site: `refusal("group_not_found", ...)`.
      for (const m of source.matchAll(/\brefusal\(\s*"([a-z][a-z0-9_]*)"/g)) out.add(m[1]!);
    }
  }
  return [...out].sort();
}

/**
 * Every code the packages declare, paired with whether the declaration is
 * actually USED at a raise site.
 *
 * THE OTHER DIRECTION, and it is the one a coverage test cannot see: a
 * constant nobody returns is a contract this app has copy for and can never
 * show. The check is on the IDENTIFIER rather than the string, because that is
 * how both packages raise -- `refuse(slug, codeRankTaken, ...)`,
 * `refusal(CodeGroupNotFound, ...)` -- so a declaration with no second mention
 * anywhere in its package is a code nothing can raise.
 */
function declaredButNeverRaised(): string[] {
  const orphans: string[] = [];
  for (const pkg of GO_PACKAGES) {
    const dir = join(integrations, pkg);
    if (!existsSync(dir)) continue;
    const files = readdirSync(dir).filter((n) => n.endsWith(".go") && !n.endsWith("_test.go"));
    const sources = files.map((n) => readFileSync(join(dir, n), "utf8"));
    const whole = sources.join("\n");
    for (const source of sources) {
      for (const m of source.matchAll(/^\s*([Cc]ode\w+)\s*=\s*"([a-z][a-z0-9_]*)"/gm)) {
        const identifier = m[1]!;
        const code = m[2]!;
        // The declaration itself is one mention; a raise site is a second.
        const mentions = whole.split(new RegExp(`\\b${identifier}\\b`)).length - 1;
        if (mentions < 2) orphans.push(code);
      }
    }
  }
  return orphans.sort();
}

function covered(code: string): boolean {
  return knownCodes().includes(code) || SERVER_SENTENCE_ONLY.includes(code);
}

describe("the refusal copy table", () => {
  it("read real codes out of the engine", () => {
    // THE POSITIVE THIS SUITE RESTS ON. Every assertion below is of the form
    // "nothing was found uncovered", and a scan that read no files at all
    // satisfies every one of them -- a moved directory, a renamed constant
    // prefix or a wrong relative path would turn this file green while
    // covering nothing. So the scan has to find something first.
    const codes = engineCodes();
    expect(codes.length).toBeGreaterThan(5);
    expect(codes).toContain("group_member_rank_not_below_caller");
    // ONE PER PACKAGE, because a scan that reads one package and misses the
    // other satisfies every "nothing was found uncovered" assertion below
    // while covering half the app. rbac's codes are package-private, which is
    // how they went unseen the first time.
    expect(codes).toContain("role_rank_taken");
  });

  it("names every code the engine can raise", () => {
    const missing = engineCodes().filter((code) => !covered(code));
    expect(missing).toEqual([]);
  });

  it("has no copy for a code the engine cannot raise", () => {
    // The other direction, and it is the one that rots quietly: copy for a
    // retired code is a sentence nobody will ever see, and it makes the
    // table's coverage look better than it is.
    const engine = engineCodes();
    expect(knownCodes().filter((code) => !engine.includes(code))).toEqual([]);
  });

  it("has no copy for a code no raise site can produce", () => {
    // Declared-and-never-raised is invisible to the coverage test above: the
    // scan finds the constant, the table has copy for it, and both directions
    // agree about a sentence nobody will ever read. Checking the identifier's
    // second mention is what tells a live code from a retired one.
    expect(declaredButNeverRaised()).toEqual([]);
  });

  it("gives every entry a headline", () => {
    for (const code of knownCodes()) {
      expect(copyFor(code)?.title, code).toBeTruthy();
    }
  });
});

describe("reading a thrown refusal", () => {
  it("splits the wire form into the code and the server's sentence", () => {
    const refusal = refusalFrom(new Error("group_self_add_refused: nobody adds themselves"));
    expect(refusal.code).toBe("group_self_add_refused");
    expect(refusal.detail).toBe("nobody adds themselves");
    expect(refusal.title).toBe("Nobody adds themselves to a group");
  });

  it("keeps an unknown code's own sentence and invents no headline", () => {
    const refusal = refusalFrom(new Error("group_something_new: the cluster said this"));
    expect(refusal.code).toBe("group_something_new");
    expect(refusal.detail).toBe("the cluster said this");
    expect(refusal.title).toBe("");
  });

  it("reads a refusal through the SDK's own call-name frame", () => {
    // What a surface actually catches: executeNamed re-throws as
    // `"<callName>: <code>: <sentence>"`, so a reader that matched the code at
    // the start of the string alone recognised none of them.
    const refusal = refusalFrom(
      new Error("groupMemberAdd: group_member_rank_not_below_caller: Ada ranks at or above you"),
    );
    expect(refusal.code).toBe("group_member_rank_not_below_caller");
    expect(refusal.detail).toBe("Ada ranks at or above you");
    expect(refusal.title).toBe("That person does not rank below you");
  });

  it("does not read an ordinary error's colon as a refusal code", () => {
    // "read timeout: 30s" is a failure, not a refusal, and giving it somebody
    // else's headline is how a fault reads as a mistake.
    const refusal = refusalFrom(new Error("Read timeout: 30s"));
    expect(refusal.code).toBe("");
    expect(refusal.detail).toBe("Read timeout: 30s");
  });
});
