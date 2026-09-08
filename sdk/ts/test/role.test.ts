// A role crosses the wire as the SLUG the cluster wrote (epic memql#5166).
//
// THE BUG THIS FILE USED TO PIN, and why it cannot happen again. `UserRoleWire`
// was a hand-mirrored union of the proto's UserRole enum, and it stopped at
// READER while memql.proto defined USER_ROLE_DEVELOPER = 5 -- so
// `roleFromWire`'s `?? ""` fallback turned a developer into an indeterminate
// role (memql#3331). Nothing errored; the caller simply could not be told apart
// from an unauthenticated one, which left the VS Code deploy panel showing a
// hedge to the one role that surface exists to serve.
//
// Both the enum and the mapping are deleted. The set of roles is CLUSTER STATE
// -- an operator authors a role from the permissions that exist -- so a closed
// union could only ever name the five this repo shipped, and a custom role
// arrived as "", which is the value an unauthenticated caller gets. What this
// file pins now is the property that replaced the mapping: a slug passes
// through UNCHANGED, whatever it is, and an absent one stays absent.

import test from "node:test";
import assert from "node:assert/strict";

import { accessSummaryFromWire } from "../src/client/types.js";

test("a base role slug passes through unchanged", () => {
  for (const slug of ["owner", "developer", "admin", "user", "writer", "viewer", "reader"]) {
    const summary = accessSummaryFromWire({
      requestId: "req-1",
      userId: "u-1",
      primaryEmail: "a@example.com",
      role: slug,
    });
    assert.equal(summary?.role, slug, slug);
  }
});

test("a CUSTOM role slug passes through unchanged -- the memql#5166 point", () => {
  // Under the enum this arrived as "", indistinguishable from an
  // unauthenticated caller, so no client could act on a role its cluster had
  // authored for itself.
  const summary = accessSummaryFromWire({
    requestId: "req-1",
    userId: "u-1",
    primaryEmail: "a@example.com",
    role: "support-lead",
    roleName: "Support Lead",
    rank: 150,
  });
  assert.equal(summary?.role, "support-lead");
  assert.equal(summary?.roleName, "Support Lead");
  assert.equal(summary?.rank, 150);
});

test("an absent role is empty, and empty is UNKNOWN rather than least-privileged", () => {
  const summary = accessSummaryFromWire({
    requestId: "req-1",
    userId: "u-1",
    primaryEmail: "a@example.com",
  });
  assert.equal(summary?.role, "");
  // The name and the rank are absent too, and a client renders neither rather
  // than inventing a title or a rung for a role it could not resolve.
  assert.equal(summary?.roleName, "");
  assert.equal(summary?.rank, 0);
});

test("a name the catalog could not resolve is empty, not fabricated", () => {
  // The server sends the slug it has and leaves role_name empty when the slug
  // ranks nowhere -- a role deactivated under its holder, or a node whose
  // catalog has not loaded. The client renders the slug.
  const summary = accessSummaryFromWire({
    requestId: "req-1",
    userId: "u-1",
    primaryEmail: "a@example.com",
    role: "retired-lead",
  });
  assert.equal(summary?.role, "retired-lead");
  assert.equal(summary?.roleName, "");
  assert.equal(summary?.rank, 0);
});

test("accessSummaryFromWire is null for an absent payload", () => {
  assert.equal(accessSummaryFromWire(undefined), null);
});
