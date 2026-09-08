# Roles as data, enforced -- Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a custom role hold real permissions and be assignable -- a runtime
capability catalog read from `v1:rbac:role` / `v1:rbac:capability` rows becomes the one
resolver every `Capable` call reads, the user row's role becomes a catalog slug,
`@requiresCapability` joins `@requiresRank`, three builtins create/update/deactivate a
role, and MyAccess carries slug, name and rank.

**Architecture:** `component/auth` gains a `CapabilityCatalog` interface and a package
holder installed by the engine at boot; the compiled `capabilitySets` map is demoted to
the seed's mirror, consulted only until the rows are readable and pinned by a parity
test. `component/memql/rbac_catalog.go` loads both concepts with the `DISTINCT ON (id)`
collapse, keeps them in memory, reloads on their graph events, installs itself into
`component/auth`, and is what `rankLadder(ctx)` reads -- so the row gate and the data
gate cannot disagree about a rank. Assignment governance composes the existing pure
predicates (`GovernPrincipal`, `CanCreatePrincipal`, `GrantsPrincipalAuthorityBeyond`)
into one `MayAssignRole`. Everything fails closed: an unknown slug holds nothing and
ranks 0.

**Tech Stack:** Go 1.26, MemQL DSL, protobuf/gRPC, TypeScript (MemQL OS + TS SDK).

**Spec:** `docs/superpowers/specs/2026-09-07-roles-as-data-design.md`
(program record: `docs/superpowers/specs/2026-09-07-access-program.md`)

**Issues:** epic memql#5166; tasks memql#5177, #5178, #5179, #5180, #5181.
**Branch:** `epic/roles-as-data`. **Ships in ONE PR.**

## Global Constraints

- **Fail closed, everywhere.** An unknown slug holds no capability (`Holds` false for
  every pair) and ranks 0. An unresolvable floor refuses rather than admitting.
- **Deny wins.** An `effect: "deny"` capability row overrides an `allow` for the same
  (role, verb, resource). No v1 path writes one.
- **The compiled map is the MIRROR, not the model.** It answers only before the rows are
  readable. `TestSeedMatchesCompiledMirror` fails the build in either direction.
- **Pre-release: no shims.** No compat adapters, no deprecation windows. When a contract
  changes, both sides change and the old spelling is deleted.
- **`TestNoGoFileShipsARoleOrdering`** (`component/auth/role_ladder_client_parity_test.go`)
  fails any Go file outside `component/auth` that maps two or more role names to numbers.
  New code must never restate the ladder.
- **Test command is `make test`**, never `go test ./...` -- the bare pattern misses
  `component/memql`, `component/auth` and `component/language`.
- **Stage files by explicit path** (`git add <file>`). Never `git add -A`.
- **Commit format:** `Issue #<N>: <description>`.
- **No emojis** in any file, message or output.

---

## File Structure

| File | Responsibility |
|---|---|
| `component/auth/capability_catalog.go` **(new)** | The `CapabilityCatalog` interface, `VerbResource`, the package holder + `SetCapabilityCatalog`, and the catalog-or-mirror resolution helpers. |
| `component/auth/rbac_model.go` | `Capable`, `roleRank`, `RoleRank`, `GrantsPrincipalAuthorityBeyond` read the catalog when installed; `capabilitySets` reheaded as the mirror. |
| `component/auth/rbac.go` | `IsValidRole` through the catalog; `RoleAtMost` / `EffectiveRole` / `RoleLevel` compare ranks through it. |
| `component/auth/rbac_assignment.go` **(new)** | `MayAssignRole` + the typed refusal set. |
| `component/auth/seed_mirror_parity_test.go` **(new)** | `TestSeedMatchesCompiledMirror`, both directions. |
| `component/memql/rbac_catalog.go` **(new)** | The engine's catalog: load, collapse, in-memory snapshot, reload subscriber, install into `component/auth`, boot log line. |
| `component/memql/rowauthz_rank.go` | `rankLadder(ctx)` reads the catalog snapshot. |
| `component/memql/requires_capability.go` **(new)** | `refuseBelowRequiredCapability`, `validateRequiresCapabilitySlugs`, `refusePlanBelowRequiredCapability`. |
| `component/node/routing.go` | Broadcast rules for `v1:rbac:role` and `v1:rbac:capability`. |
| `dsl/rbac/concepts.memql` | `role.accountId` (+ `scopedTo` relationship), `role.createdBy`. |
| `dsl/rbac/shapes.memql` | `roleFull` projects both new fields. |
| `dsl/rbac/builtins.memql` | `roleCreate`, `roleUpdate`, `roleDeactivate`. |
| `dsl/rbac/mutations.memql` | `@serverOnly` on `createRole` and `createCapability`. |
| `integrations/rbac/roles.go` **(new)** | The three builtins' Go executors, their guards and audit lines. |
| `component/grpc/memql.proto` | `MyAccessResult` gains `role` / `role_name` / `rank`; `UserRole` deleted; four other role fields become strings. |
| `clients/os/src/modules/profile/access.ts` | `ProfileAccess` carries `role`, `roleName`, `rank`. |
| `clients/os/src/modules/profile/RoleIdentity.tsx` **(new)** | The one presentation of "who you are on this cluster". |

---

## Task 1: The catalog interface and the compiled mirror

**Issue:** memql#5177 (part 1). **Record:** section B.

**Files:**
- Create: `component/auth/capability_catalog.go`
- Create: `component/auth/seed_mirror_parity_test.go`
- Modify: `component/auth/rbac_model.go`
- Test: `component/auth/capability_catalog_test.go` (new)

**Interfaces:**
- Produces:
  ```go
  type VerbResource struct{ Verb, Resource string }
  type CapabilityCatalog interface {
      Rank(slug string) (rank int, ok bool)
      Holds(slug, verb, resource string) bool
      Grants(slug string) []VerbResource
      Scope(slug string) (accountId string)
      Active(slug string) bool
  }
  func SetCapabilityCatalog(c CapabilityCatalog)
  func InstalledCapabilityCatalog() CapabilityCatalog
  ```
  `Capable(role Role, verb, resource string) bool` and `RoleRank(role Role) int` keep
  their signatures and change behaviour.

- [ ] **Step 1: Write the failing test** -- `component/auth/capability_catalog_test.go`

```go
package auth

import "testing"

// fakeCatalog is a hand-built catalog for the read-through tests.
type fakeCatalog struct {
	ranks  map[string]int
	grants map[string]map[VerbResource]bool
	scopes map[string]string
	off    map[string]bool
}

func (f *fakeCatalog) Rank(slug string) (int, bool) { r, ok := f.ranks[slug]; return r, ok }
func (f *fakeCatalog) Holds(slug, verb, resource string) bool {
	return f.grants[slug][VerbResource{Verb: verb, Resource: resource}]
}
func (f *fakeCatalog) Grants(slug string) []VerbResource {
	out := []VerbResource{}
	for vr := range f.grants[slug] {
		out = append(out, vr)
	}
	return out
}
func (f *fakeCatalog) Scope(slug string) string { return f.scopes[slug] }
func (f *fakeCatalog) Active(slug string) bool  { return !f.off[slug] }

func TestCapableReadsTheCatalogWhenInstalled(t *testing.T) {
	t.Cleanup(func() { SetCapabilityCatalog(nil) })
	SetCapabilityCatalog(&fakeCatalog{
		ranks: map[string]int{"support-lead": 150},
		grants: map[string]map[VerbResource]bool{
			"support-lead": {{Verb: "read", Resource: "principal"}: true},
		},
	})
	if !Capable(Role("support-lead"), VerbRead, ResourcePrincipal) {
		t.Fatal("a custom role holding read-on-principal must be Capable of it")
	}
	if Capable(Role("support-lead"), VerbDelete, ResourcePrincipal) {
		t.Fatal("a custom role must hold only the pairs the catalog gives it")
	}
	if got := RoleRank(Role("support-lead")); got != 150 {
		t.Fatalf("RoleRank(support-lead) = %d, want 150", got)
	}
}

func TestUnknownSlugHoldsNothingAndRanksZero(t *testing.T) {
	t.Cleanup(func() { SetCapabilityCatalog(nil) })
	SetCapabilityCatalog(&fakeCatalog{ranks: map[string]int{"owner": 400}})
	for _, verb := range []string{VerbRead, VerbCreate, VerbUpdate, VerbDelete, VerbExecute} {
		for _, res := range []string{ResourcePrincipal, ResourceConstruct, ResourceData,
			ResourceDeployment, ResourceAdmission, ResourceAgent, ResourceGroup, ResourceRole} {
			if Capable(Role("ghost"), verb, res) {
				t.Fatalf("unknown slug held %s on %s", verb, res)
			}
		}
	}
	if got := RoleRank(Role("ghost")); got != 0 {
		t.Fatalf("RoleRank(unknown) = %d, want 0", got)
	}
}

func TestMirrorAnswersBeforeACatalogIsInstalled(t *testing.T) {
	SetCapabilityCatalog(nil)
	if !Capable(RoleOwner, VerbCreate, ResourcePrincipal) {
		t.Fatal("with no catalog installed the compiled mirror must answer for a base role")
	}
	if got := RoleRank(RoleDeveloper); got != rankDeveloper {
		t.Fatalf("RoleRank(developer) = %d, want %d", got, rankDeveloper)
	}
}

func TestDenyWinsOverAllow(t *testing.T) {
	t.Cleanup(func() { SetCapabilityCatalog(nil) })
	// The catalog implementation resolves deny itself; this asserts the
	// contract Capable relies on -- Holds already answers false.
	SetCapabilityCatalog(&fakeCatalog{
		grants: map[string]map[VerbResource]bool{"finance": {}},
	})
	if Capable(Role("finance"), VerbExecute, ResourceDeployment) {
		t.Fatal("a pair the catalog does not report held must not be Capable")
	}
}

func TestAnInactiveRoleHoldsNothing(t *testing.T) {
	t.Cleanup(func() { SetCapabilityCatalog(nil) })
	SetCapabilityCatalog(&fakeCatalog{
		ranks:  map[string]int{"retired": 120},
		grants: map[string]map[VerbResource]bool{"retired": {{Verb: "read", Resource: "data"}: true}},
		off:    map[string]bool{"retired": true},
	})
	if Capable(Role("retired"), VerbRead, ResourceData) {
		t.Fatal("a deactivated role must hold nothing")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && go test ./component/auth/ -run 'Catalog|UnknownSlug|Mirror|DenyWins|Inactive' -count=1`
Expected: FAIL -- `undefined: SetCapabilityCatalog`, `undefined: VerbResource`.

- [ ] **Step 3: Write `component/auth/capability_catalog.go`**

The file holds: the `VerbResource` struct, the `CapabilityCatalog` interface, an
`atomic.Pointer`-guarded package holder (a `SetCapabilityCatalog(nil)` clears it), and
two resolution helpers the model calls -- `catalogHolds(slug, verb, resource) (bool, bool)`
and `catalogRank(slug) (int, bool)`, each returning `(answer, answered)` so "the catalog
has no opinion" is distinguishable from "the catalog says no". The header must argue:

- why the catalog is a package-level holder rather than a parameter (every `Can*`
  adapter is a free function called from ~40 sites across four modules; threading a
  resolver through all of them is a refactor this epic does not need and would leave
  half the sites reading the mirror);
- why `Active` is separate from `Holds` (a deactivated role must hold nothing AND still
  rank, so a holder's rows stay attributed rather than becoming unowned);
- that an installed catalog is AUTHORITATIVE: it is not consulted-then-fallen-back-to,
  because a fallback to the mirror for a slug the catalog does not know is exactly how an
  unknown role would acquire the base grants of a same-named legacy slug.

- [ ] **Step 4: Re-head `capabilitySets` and route the model through the catalog**

In `rbac_model.go`, change three functions and nothing else:

```go
func roleRank(r Role) int {
	if rank, ok := catalogRank(string(r)); ok {
		return rank
	}
	if answered := catalogInstalled(); answered {
		// An installed catalog that does not know this slug is a
		// STATEMENT, not a gap: the cluster's roles are its rows.
		return rankUnknown
	}
	switch r { /* ...unchanged compiled mirror... */ }
}

func roleHasCapability(r Role, verb, resource string) bool {
	if held, answered := catalogHolds(string(r), verb, resource); answered {
		return held
	}
	set, ok := capabilitySets[r]
	if !ok {
		return false
	}
	return set[verbResource{verb: verb, resource: resource}]
}
```

`GrantsPrincipalAuthorityBeyond` walks `capabilitySets[target]` directly; it must walk
the catalog's `Grants(target)` when one is installed, filtered to `principal`, and the
compiled set otherwise. Add `grantsFor(r Role) []verbResource` beside it and have both
readers use it.

Rewrite the `capabilitySets` block comment: it is the seed's MIRROR now, consulted only
until the rows are readable (the identity node's gates run before the seed is readable --
the same reason `rankOf` kept a compiled fallback), and `TestSeedMatchesCompiledMirror`
fails the build when it and `dsl/rbac/seeds.memql` disagree in either direction.

- [ ] **Step 5: Run the tests -- they pass**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && go test ./component/auth/ -count=1`
Expected: PASS, including the pre-existing `rbac_migration_conformance_test.go` and
`rbac_model_test.go` (no catalog installed in those, so the mirror answers exactly as
before).

- [ ] **Step 6: Write `TestSeedMatchesCompiledMirror`**

`component/auth/seed_mirror_parity_test.go`, reading `../../dsl/rbac/seeds.memql` the
way `seededRungs` in `role_ladder_client_parity_test.go` already does. Reuse
`readClientFile` and `seededRungs` from that file (same package). Add a
`seededGrants(t) map[string]map[verbResource]bool` parsed from
`seed capability <name> { roleSlug: "x" verb: "y" resourceType: "z" ... }` with a regexp
per field, and assert BOTH directions across the legacy-slug mapping
(`owner->owner, developer->developer, admin->admin, writer->user, reader->viewer`):

```go
func TestSeedMatchesCompiledMirror(t *testing.T) {
	seeded := seededGrants(t)
	if len(seeded) == 0 {
		t.Fatalf("parsed no capability seeds out of %s -- this gate would pass by "+
			"comparing two empty sets", rbacSeedPath)
	}
	for role, slug := range mirrorSlugs {
		want, ok := seeded[slug]
		if !ok {
			t.Fatalf("%s no longer seeds role %q, which the compiled mirror carries as %q",
				rbacSeedPath, slug, role)
		}
		got := capabilitySets[role]
		for vr := range want {
			if !got[vr] {
				t.Fatalf("%s seeds %s on %s for %q and the compiled mirror does not hold it. "+
					"The mirror is what answers before the rows are readable; a pair missing "+
					"from it is a permission that appears seconds after boot and not before.",
					rbacSeedPath, vr.verb, vr.resource, slug)
			}
		}
		for vr := range got {
			if !want[vr] {
				t.Fatalf("the compiled mirror holds %s on %s for %q and %s seeds no such "+
					"capability. The mirror is not a superset it is allowed to keep: a pair "+
					"only it holds is a permission that exists at boot and vanishes.",
					vr.verb, vr.resource, role, rbacSeedPath)
			}
		}
	}
}
```

Add the reachable positive (`len(seeded) == 0` above) AND a count assertion: the parsed
seed set must name all five slugs, or a renamed seed block would shrink the comparison
silently.

- [ ] **Step 7: Run it -- it must PASS against the tree as it stands**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && go test ./component/auth/ -run TestSeedMatchesCompiledMirror -count=1 -v`
Expected: PASS. If it fails, the mirror and the seeds already disagree -- report the pair
and fix the MIRROR (the seeds are the authored catalog).

- [ ] **Step 8: Run the negative control on the gate itself**

Temporarily delete one `vr("read", "role")` line from `RoleAdmin` in `rbac_model.go`,
re-run: it must FAIL naming `read on role for admin`. Restore. Then temporarily change a
seed's `verb:` and re-run: it must FAIL the other way. Restore. A parity gate that has
not been watched to fail in both directions is a gate nobody has tested.

- [ ] **Step 9: Commit**

```bash
cd /home/znas/memql-projects/epic-roles-as-data
git add component/auth/capability_catalog.go component/auth/capability_catalog_test.go \
        component/auth/seed_mirror_parity_test.go component/auth/rbac_model.go
git commit -m "$(cat <<'EOF'
Issue #5177: the capability catalog interface, with the compiled map demoted to the seed's mirror

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 2: The engine loader, the reload, and the ladder

**Issue:** memql#5177 (part 2). **Record:** section B.

**Files:**
- Create: `component/memql/rbac_catalog.go`
- Create: `component/memql/rbac_catalog_test.go`
- Modify: `component/memql/rowauthz_rank.go` (`rankLadder` reads the catalog)
- Modify: `component/memql/engine.go` (start the subscriber beside its siblings, ~:1886)
- Modify: `component/node/routing.go` (broadcast rules)
- Test: `component/node/routing_test.go` (rule presence)

**Interfaces:**
- Consumes: `auth.SetCapabilityCatalog`, `auth.CapabilityCatalog`, `auth.VerbResource`
  (Task 1).
- Produces:
  ```go
  func (e *MemQLEngine) StartCapabilityCatalog(ctx context.Context)
  func (e *MemQLEngine) ReloadCapabilityCatalog(ctx context.Context) error
  func (e *MemQLEngine) CapabilityCatalogSnapshot() *rbacCatalog   // nil until loaded
  ```
  `rbacCatalog` implements `auth.CapabilityCatalog` and additionally exposes
  `ladder() roleLadder` so `rankLadder` reads the same structure.

- [ ] **Step 1: Write the failing test** -- `component/memql/rbac_catalog_test.go`

A pure test over the COLLAPSE and the RESOLUTION, built from `memorynodes.MemoryNode`
values rather than a database, so it runs in `make test` with no DSN:

```go
func TestCatalogCollapsesToTheNewestVersionOfEachRow(t *testing.T) {
	cat := buildRbacCatalog(
		[]roleRow{
			{id: "support-lead", slug: "support-lead", rank: 150, active: true},
			// An older version of the same row, which must not win.
			{id: "support-lead", slug: "support-lead", rank: 350, active: true, older: true},
		},
		[]capabilityRow{{roleSlug: "support-lead", verb: "read", resource: "principal", effect: "allow", active: true}},
	)
	if rank, ok := cat.Rank("support-lead"); !ok || rank != 150 {
		t.Fatalf("Rank(support-lead) = %d,%v -- the collapse must take the newest version", rank, ok)
	}
}

func TestDenyRowWins(t *testing.T) {
	cat := buildRbacCatalog(
		[]roleRow{{id: "finance", slug: "finance", rank: 150, active: true}},
		[]capabilityRow{
			{roleSlug: "finance", verb: "execute", resource: "deployment", effect: "allow", active: true},
			{roleSlug: "finance", verb: "execute", resource: "deployment", effect: "deny", active: true},
		},
	)
	if cat.Holds("finance", "execute", "deployment") {
		t.Fatal("a deny row must win over an allow for the same triple")
	}
}

func TestAliasesResolveToTheirRung(t *testing.T) { /* writer -> user's 100 */ }
func TestASlugAlwaysWinsOverAnAlias(t *testing.T) { /* a custom role aliasing `reader` cannot re-point it */ }
func TestInactiveRoleAndInactiveGrantAreIgnored(t *testing.T) { /* both */ }
func TestScopeIsEmptyForAGlobalRole(t *testing.T) { /* accountId "" */ }
```

`roleRow` / `capabilityRow` / `buildRbacCatalog` are test helpers in the same file that
marshal payloads and call the same unexported builder the DB path calls, so the test
exercises production code rather than a parallel implementation.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && go test ./component/memql/ -run 'TestCatalog|TestDenyRow|TestAliases|TestASlug|TestInactive|TestScopeIs' -count=1`
Expected: FAIL -- `undefined: buildRbacCatalog`.

- [ ] **Step 3: Write `component/memql/rbac_catalog.go`**

Structure:

```go
const conceptRbacCapability = "v1:rbac:capability"   // conceptRbacRole already exists

// rbacCatalog is one immutable resolution of the two catalog concepts.
type rbacCatalog struct {
	ranks   map[string]int                       // slug AND alias -> rank
	scopes  map[string]string                    // slug -> accountId
	active  map[string]bool                      // slug -> active
	grants  map[string]map[auth.VerbResource]bool // slug -> resolved ALLOW set (denies removed)
	slugs   []string                             // sorted, for diagnostics
}

func (c *rbacCatalog) Rank(slug string) (int, bool)
func (c *rbacCatalog) Holds(slug, verb, resource string) bool
func (c *rbacCatalog) Grants(slug string) []auth.VerbResource
func (c *rbacCatalog) Scope(slug string) string
func (c *rbacCatalog) Active(slug string) bool
func (c *rbacCatalog) ladder() roleLadder
```

Load is `loadRbacCatalog(ctx, db)`: two `DISTINCT ON (id) ... ORDER BY id, "createdAt" DESC`
selects, then `buildRbacCatalog(roles, capabilities)`. The alias pass is DEFERRED exactly
as `rankLadder` already defers it, and the comment must carry that reason forward: a slug
always wins over an alias, so a custom role cannot re-point `reader`.

The engine holds `catalog atomic.Pointer[rbacCatalog]`. `StartCapabilityCatalog(ctx)`:

1. loads once synchronously, installs via `auth.SetCapabilityCatalog(cat)`, and logs
   ONE line saying which mode is in force:
   `"rbac catalog installed" roles=<n> capabilities=<n> mode="rows"` on success, or
   `"rbac catalog unreadable -- base roles answer from the compiled mirror; custom roles hold nothing until the rows load" mode="mirror"`
   on failure. The line is the operator's only way to tell the two apart.
2. subscribes to four topics on `e.eventBus` --
   `graph.node.created.v1:rbac:role`, `graph.node.updated.v1:rbac:role`,
   `graph.node.created.v1:rbac:capability`, `graph.node.updated.v1:rbac:capability` --
   each handler calling `ReloadCapabilityCatalog(ctx)`. Debounce is NOT needed: the seed
   materializer writes ~5 roles and ~50 capabilities once per boot, and a reload is two
   indexed selects.
3. tears the subscriptions down on `ctx.Done()`, the shape
   `StartCacheInvalidationSubscriber` uses.

**A failed reload keeps the last good snapshot.** Replacing a working catalog with an
empty one because the database blinked would take every custom role's permissions away
mid-request; the log line says the reload failed and the pointer is left alone.

- [ ] **Step 4: Point `rankLadder` at the catalog**

In `rowauthz_rank.go`, `rankLadder(ctx)` becomes:

```go
func (e *MemQLEngine) rankLadder(ctx context.Context) roleLadder {
	if cat := e.catalog.Load(); cat != nil {
		return cat.ladder()
	}
	return e.loadRankLadderFromDatabase(ctx)   // the existing body, renamed
}
```

The existing body stays under the new name and keeps its whole comment -- it is what
answers at boot, before `StartCapabilityCatalog` has run, which is exactly when
`validateRequiresRankSlugs` calls it. Add a sentence saying so: the two readers are one
implementation of the collapse in different lifecycles, not two ladders.

- [ ] **Step 5: Start it in `engine.go`**

Beside `e.StartCacheInvalidationSubscriber(ctx)` (~line 1886), with a comment naming why
this one matters more than its siblings: without the reload a role created on replica A
holds nothing on replica B until B restarts, and the person who created it sees it work
on one page and refuse on the next.

- [ ] **Step 6: Add the routing rules**

In `defaultRoutingRules()`, beside the Fleet block:

```go
// THE ROLE CATALOG (epic memql#5166). A role is written on whichever
// replica served the builtin and READ by every gate on every replica --
// the data gate, the row gate, @requiresRank and @requiresCapability all
// resolve through the catalog. Without these rules default-deny keeps the
// event on the writer: the new role works on one replica and holds nothing
// on the others, which presents as a permission that applies to half the
// requests.
//
// SAFE TO BROADCAST, checked rather than assumed: no automation in the
// tree triggers on v1:rbac:* node events.
{Pattern: "graph.node.created.v1:rbac:role", TargetType: ""},
{Pattern: "graph.node.updated.v1:rbac:role", TargetType: ""},
{Pattern: "graph.node.created.v1:rbac:capability", TargetType: ""},
{Pattern: "graph.node.updated.v1:rbac:capability", TargetType: ""},
```

Verify the "no automation triggers on v1:rbac" claim before writing it:
`grep -rn 'v1:rbac' dsl/*/automations.memql`.

- [ ] **Step 7: Write the routing-rule test**

In `component/node/routing_test.go`, assert all four patterns forward and that a
`v1:rbac:capability` event is not blocked. If the file has an existing table of expected
forwards, extend it rather than adding a parallel test.

- [ ] **Step 8: Run the tests**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && go test ./component/memql/ ./component/node/ -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add component/memql/rbac_catalog.go component/memql/rbac_catalog_test.go \
        component/memql/rowauthz_rank.go component/memql/engine.go \
        component/node/routing.go component/node/routing_test.go
git commit -m "$(cat <<'EOF'
Issue #5177: the engine's catalog, the reload, and one ladder for both gates

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 3: `IsValidRole` and `MayAssignRole`

**Issue:** memql#5178 (part 1). **Record:** section C, decision D4.

**Files:**
- Create: `component/auth/rbac_assignment.go`
- Create: `component/auth/rbac_assignment_test.go`
- Modify: `component/auth/rbac.go` (`IsValidRole`, `RoleLevel`, `RoleAtMost`, `EffectiveRole`)

**Interfaces:**
- Consumes: `catalogRank`, `catalogInstalled`, `Capable`, `GovernPrincipal`,
  `CanCreatePrincipal`, `GrantsPrincipalAuthorityBeyond` (Tasks 1 and existing).
- Produces:
  ```go
  type AssignRefusal string
  const (
      AssignAllowed         AssignRefusal = ""
      AssignUnknownRole     AssignRefusal = "unknown_role"
      AssignNotAUserManager AssignRefusal = "role_cannot_manage_principals"
      AssignTargetOutranks  AssignRefusal = "target_outranks_caller"
      AssignAboveCaller     AssignRefusal = "role_above_caller"
      AssignAuthorityBeyond AssignRefusal = "role_grants_authority_beyond_caller"
      AssignNotAMember      AssignRefusal = "target_not_a_member_of_the_scope"
  )
  func MayAssignRole(actor UserContext, targetUserId, targetCurrentSlug, newSlug string,
      targetIsMember func(accountId string) bool) AssignRefusal
  ```

- [ ] **Step 1: Write the failing matrix test**

Table-driven over (caller rank, caller slug, target current rank, new rank, scope,
membership), one row per asserted cell. Install a fake catalog with owner 400,
developer 300, admin 200, user 100, viewer 50 plus `support-lead` 150 scoped to
`acct-1`. Assert at minimum:

| caller | target now | new | expect |
|---|---|---|---|
| owner | admin | developer | allowed |
| owner | owner (other) | admin | allowed (owner manages everyone) |
| owner | user | owner | allowed (the owner carve-out; see step 3) |
| admin | user | developer | `role_above_caller` (300 >= 200) |
| admin | user | admin | `role_above_caller` (a peer is not below) |
| admin | developer | user | `target_outranks_caller` |
| admin | user | viewer | allowed |
| developer | user | admin | `role_grants_authority_beyond_caller` |
| developer | user | viewer | `role_cannot_manage_principals` (developer holds no update-on-principal) |
| user | user | viewer | `role_cannot_manage_principals` |
| admin | user | ghost | `unknown_role` |
| admin | user | support-lead (scoped, member) | allowed |
| admin | user | support-lead (scoped, NOT member) | `target_not_a_member_of_the_scope` |
| admin | self | viewer | allowed only if the new rank is below the caller's -- assert `role_above_caller` for admin->admin self |

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./component/auth/ -run TestMayAssignRole -count=1`
Expected: FAIL -- `undefined: MayAssignRole`.

- [ ] **Step 3: Write `component/auth/rbac_assignment.go`**

```go
func MayAssignRole(actor UserContext, targetUserId, targetCurrentSlug, newSlug string,
	targetIsMember func(accountId string) bool) AssignRefusal {

	newSlug = strings.ToLower(strings.TrimSpace(newSlug))
	newRank, known := resolveAssignableRank(newSlug)
	if !known {
		return AssignUnknownRole
	}
	if !Capable(actor.Role, VerbUpdate, ResourcePrincipal) {
		return AssignNotAUserManager
	}
	actorP := Principal{UserId: actor.ID, Rank: roleRank(actor.Role), IsOwner: actor.Role == RoleOwner}
	targetP := Principal{
		UserId:  targetUserId,
		Rank:    roleRank(Role(targetCurrentSlug)),
		IsOwner: isOwnerSlug(targetCurrentSlug),
	}
	if !GovernPrincipal(actorP, targetP, GovernUpdate) {
		return AssignTargetOutranks
	}
	// THE OWNER CARVE-OUT, and it is the one place this composition departs
	// from CanCreatePrincipal's bare arithmetic. `newRank < actorRank` refuses
	// owner -> owner (400 < 400 is false), which would make a second owner
	// unmakeable through every path in the product -- a cluster with one owner
	// and no way to name another. GovernPrincipal already carries the same
	// carve-out for the target half ("an owner manages everyone") for the same
	// reason: two owners share a rank, so a strict rule cannot express them.
	if !actorP.IsOwner && !CanCreatePrincipal(actorP, newRank) {
		return AssignAboveCaller
	}
	// RANK IS NOT AUTHORITY. developer (300) outranks admin (200) and holds
	// strictly fewer principal verbs, so the rank test alone lets a developer
	// mint an admin -- who then holds the user management the developer does
	// not. This is the clause invitation.go already carries; it moves here so
	// the two seams cannot drift.
	if GrantsPrincipalAuthorityBeyond(actor.Role, Role(newSlug)) {
		return AssignAuthorityBeyond
	}
	if scope := roleScope(newSlug); scope != "" {
		if targetIsMember == nil || !targetIsMember(scope) {
			return AssignNotAMember
		}
	}
	return AssignAllowed
}
```

`resolveAssignableRank` reads the catalog (slug or alias, and `Active` must be true), and
falls back to the compiled mirror only when no catalog is installed. `roleScope` reads
`Scope` off the installed catalog and answers `""` otherwise -- a cluster with no catalog
has no scoped roles, so an absent catalog cannot silently drop the membership check.

- [ ] **Step 4: Rewrite `IsValidRole`**

```go
// IsValidRole reports whether a slug names a role this cluster can assign:
// an ACTIVE catalog slug or one of its aliases.
//
// A DSL enum cannot name a row, which is why this is Go. The compiled
// five answer when no catalog is installed -- a first boot, a node whose
// database is unreachable -- so sign-in and the identity gates keep
// working on a cluster whose catalog has not seeded yet.
func IsValidRole(role Role) bool {
	slug := strings.ToLower(strings.TrimSpace(string(role)))
	if slug == "" {
		return false
	}
	if cat := installedCatalog(); cat != nil {
		if _, ok := cat.Rank(slug); ok {
			return cat.Active(canonicalSlugFor(cat, slug))
		}
		return false
	}
	switch Role(slug) {
	case RoleOwner, RoleAdmin, RoleDeveloper, RoleWriter, RoleReader:
		return true
	}
	return false
}
```

`Active` is asked of the CANONICAL slug, not the alias: `Rank` resolves aliases and
`Active` is keyed by slug, so asking `Active("writer")` of a catalog keyed on `user`
would answer false for every ordinary member. Add `canonicalSlugFor` to
`capability_catalog.go` and a test for exactly that (`IsValidRole("writer")` is true).

`RoleLevel` currently switches on `roleRank(r)` against the five compiled constants,
which answers 3 ("least privileged") for every custom rank. Replace its body with a
rank comparison so a custom role caps a delegation correctly:

```go
// RoleLevel is the legacy INVERTED scale (lower == more privileged) kept
// only for RoleAtMost's comparison. Derived from the rank so a custom
// rung orders correctly; the absolute numbers no longer mean anything and
// nothing but RoleAtMost reads them.
func RoleLevel(r Role) int { return -roleRank(r) }
```

Check every caller of `RoleLevel` before changing it
(`grep -rn 'RoleLevel' --include='*.go' .`) -- if anything compares it against a literal
0/1/2/3, that call site changes too or the function keeps its shape.

- [ ] **Step 5: Run the tests**

Run: `go test ./component/auth/ -count=1`
Expected: PASS, `TestMayAssignRole` included.

- [ ] **Step 6: Commit**

```bash
git add component/auth/rbac_assignment.go component/auth/rbac_assignment_test.go \
        component/auth/rbac.go component/auth/capability_catalog.go
git commit -m "$(cat <<'EOF'
Issue #5178: IsValidRole against the catalog, and one MayAssignRole for both assignment seams

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 4: The seams that write a role

**Issue:** memql#5178 (part 2). **Record:** section C.

**Files:**
- Modify: `dsl/identity/concepts.memql` (`user.role`, `delegation.roleCeiling`,
  `invitation.inviteeRole`, `clusterSettings.internalDefaultRole`: enum -> string)
- Modify: `dsl/identity/mutations.memql` (`createUser` :1204, `createUserOnFirstLogin`
  :1265 role args: enum -> string)
- Modify: `dsl/rbac/mutations.memql` (`@serverOnly` on both)
- Modify: `component/identity/adminops/adminops.go` (`SetUserRole`)
- Modify: `component/identity/adminops/invitation.go` (the inviter gate)
- Test: `component/identity/adminops/*_test.go`

- [ ] **Step 1: Turn the four DSL enums into strings**

Each field keeps its `@description` and gains a sentence: the value is a
`v1:rbac:role` slug or one of its aliases, validated in Go at the seam that writes it,
because a DSL enum cannot name a row. Do NOT add `@enum` to the mutation args either --
same reason.

- [ ] **Step 2: `@serverOnly` on `createRole` and `createCapability`**

Both gain the annotation plus a line saying why: the client-reachable path is the three
`role*` builtins, which apply the guards; a client reaching the raw mutation would write
a role with no rank bound and no subset check. The seed materializer keeps writing
through them because it stamps internal origin.

Verify the materializer stamps it:
`grep -rn 'ContextWithInternalOrigin' component/memql/seed_materializer.go`.
If it does not, this step's real work is making it do so -- and there is a test for it:
a boot on a fresh database must still materialize the five base roles.

- [ ] **Step 3: Write the failing tests for `SetUserRole`**

In `component/identity/adminops`, with a fake catalog installed:

```go
func TestSetUserRoleRefusesARoleAtOrAboveTheCaller(t *testing.T)  // admin -> developer
func TestSetUserRoleRefusesATargetTheCallerDoesNotOutrank(t *testing.T)
func TestSetUserRoleAcceptsACustomSlug(t *testing.T)               // support-lead lands and reads back
func TestSetUserRoleRefusesAnUnknownSlug(t *testing.T)
func TestSetUserRoleAcceptsAnAlias(t *testing.T)                   // "writer" resolves
```

Each asserts the audit line as well: `user_role_changed`, outcome `blocked` on a
refusal, with `detail.oldRole` and `detail.newRole` carrying SLUGS.

- [ ] **Step 4: Run them and watch them fail**

Run: `go test ./component/identity/... -run TestSetUserRole -count=1`
Expected: FAIL -- the current `SetUserRole` applies no rank rule at all, so the
"refuses" cases pass through.

- [ ] **Step 5: Wire `MayAssignRole` into `SetUserRole`**

After the existing `s.authorize(...)` and the `userById` read (the target's current role
is needed, so the call moves BELOW the read):

```go
if refusal := auth.MayAssignRole(
	auth.UserContext{ID: act.userID, Role: act.role},
	userID, user.Role, newRole,
	func(accountId string) bool { return s.userIsMemberOfAccount(ctx, userID, accountId) },
); refusal != auth.AssignAllowed {
	return fail(CodePermissionDenied, s.emit(ctx, identity.AuditCategoryAdmin, "user_role_changed",
		act, userID, user.PrimaryEmail, detail, identity.AuditOutcomeBlocked, string(refusal)),
		"identity admin: "+assignRefusalSentence(refusal, newRole))
}
```

`userIsMemberOfAccount` is a small reader over the account's groups. Record A (#5165)
owns groups; until it lands there is no group membership to read, so this epic's
implementation reads `v1:accounts:account` membership if a readable relationship exists
and otherwise returns FALSE with a comment saying so. **False is the fail-closed
answer**: a scoped role that cannot be shown to fit refuses, rather than being granted to
somebody outside its account.

`assignRefusalSentence` renders each refusal for a person: it names the requirement and
the caller's own role, and never names who could do it.

- [ ] **Step 6: Replace the invitation gate with the same function**

`invitation.go`'s bespoke `rank > inviterRank || GrantsPrincipalAuthorityBeyond(...)`
becomes `auth.MayAssignRole(..., targetCurrentSlug: "", ...)`. Keep the audit reason
`role_above_inviter` for the two refusals that meant that before (`AssignAboveCaller`,
`AssignAuthorityBeyond`) so the trail does not change meaning, and add
`role_not_assignable_here` for the scope refusal. Replace the long comment block with a
short one pointing at `MayAssignRole` -- the reasoning now lives beside the rule.

- [ ] **Step 7: Run the tests**

Run: `go test ./component/identity/... ./component/auth/ -count=1`
Expected: PASS. Existing invitation tests that asserted an admin may invite an admin will
FAIL -- that is the D4 tightening. Update each with a comment naming D4, and keep one
test asserting an OWNER may still invite an owner.

- [ ] **Step 8: Run the DSL conformance and lint gates**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && go run ./cmd/memqllint ./dsl && go test ./test/dslconformance/ -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add dsl/identity/concepts.memql dsl/identity/mutations.memql dsl/rbac/mutations.memql \
        component/identity/adminops/adminops.go component/identity/adminops/invitation.go \
        component/identity/adminops/*_test.go
git commit -m "$(cat <<'EOF'
Issue #5178: the user row's role is a catalog slug, and one rule governs both assignment seams

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 5: The role concept, the three builtins, and the audit lines

**Issue:** memql#5179. **Record:** sections D, F, H.

**Files:**
- Modify: `dsl/rbac/concepts.memql` (`accountId` + `scopedTo` relationship, `createdBy`)
- Modify: `dsl/rbac/shapes.memql` (`roleFull`)
- Modify: `dsl/rbac/builtins.memql` (three builtins)
- Create: `integrations/rbac/roles.go`
- Create: `integrations/rbac/roles_test.go`
- Modify: `integrations/rbac/capabilities.go` (register the three)
- Modify: `integrations/rbac/plugin.go` (the factory now needs `PluginContext.Engine`)
- Modify: `dsl/identity/concepts.memql` + `dsl/identity/mutations.memql`
  (`targetType` gains `"role"`)

**Interfaces:**
- Produces: builtins `roleCreate`, `roleUpdate`, `roleDeactivate`, each returning one
  synthetic node whose payload carries `{ok: bool, slug: string, code: string}`.
  Refusal codes: `role_slug_taken`, `role_rank_not_below_caller`, `role_rank_taken`,
  `role_grant_not_held`, `role_predefined_immutable`, `role_held_above_caller`,
  `role_held`.

- [ ] **Step 1: Add `accountId` and `createdBy` to the concept**

```memql
@relationship(type="references", as="scopedTo", field="accountId", target=account, direction="outgoing")
concept role {
  ...
  accountId    string  @description("Optional ACCOUNT this role is scoped to (D10). A scoped role is offered to and holdable by members of that account's groups only; a global role leaves this empty. Scope governs WHO MAY HOLD the role, never who may SEE that it exists -- a scoped role appears in the ladder like any other, because every client draws ranks on the whole ladder and a rung missing from one client's copy is a rank that resolves differently there.")
  createdBy    string  @description("v1:identity:user.id of the person who authored this role. Empty on the predefined seeds, which nobody authored. Kept because the rank-below-creator and grants-a-subset-of-the-creator's guards are decided against the CALLER at write time, so the row carries no record of them otherwise -- and 'who could have made this' is the first question asked of a role nobody recognises.")
}
```

The `use` line at the top must import the account concept for the relationship target;
check how another cross-domain relationship in `dsl/` spells it and follow that exactly.
If `dsl/rbac` importing `dsl/accounts` crosses a namespace boundary the loader refuses,
declare `field="accountId"` with `target=account` resolved through the file-top import,
and if that is refused, fall back to naming the target by its canonical concept id --
verify with `go run ./cmd/memqllint ./dsl` before moving on.

Add both to `roleFull` in `shapes.memql`.

- [ ] **Step 2: Add `"role"` to the two audit enums**

`dsl/identity/concepts.memql` `auditEvent.targetType` and `dsl/identity/mutations.memql`
`createAuditEvent`'s `targetType` arg both gain `"role"`, with a sentence in the concept's
description: it names a `v1:rbac:role` row -- a definition of authority rather than a
person who holds it, which is why it is not `identity` and not `config`.

- [ ] **Step 3: Declare the three builtins**

`dsl/rbac/builtins.memql`, each `@sdk`, `@executor("integration.rbac.roleCreate")` etc.
`grants` is a list of objects, so it rides `@args(grants="array")` -- check how another
builtin declares a list-of-object arg (`dsl/workbench/builtins.memql`'s `environment` is
the object precedent) and follow it.

- [ ] **Step 4: Write the failing guard tests** -- `integrations/rbac/roles_test.go`

One test per guard, each asserting the CODE rather than the message:

```go
func TestRoleCreateRefusesATakenSlug(t *testing.T)          // role_slug_taken, incl. an ALIAS
func TestRoleCreateRefusesARankAtOrAboveTheCaller(t *testing.T) // role_rank_not_below_caller
func TestRoleCreateRefusesATakenRank(t *testing.T)          // role_rank_taken
func TestRoleCreateRefusesAGrantTheCallerDoesNotHold(t *testing.T) // role_grant_not_held, message NAMES the pair
func TestRoleCreateAcceptsARank150RoleWithCreateOnPrincipal(t *testing.T) // the admin case from the record
func TestRoleUpdateRefusesAPredefinedRole(t *testing.T)     // role_predefined_immutable
func TestRoleUpdateRefusesARankChangeAboveAHolder(t *testing.T) // role_held_above_caller
func TestRoleDeactivateRefusesAHeldRole(t *testing.T)       // role_held, message carries the COUNT
func TestRoleCreateWritesTheRoleAndItsGrantsInOneWrite(t *testing.T)
func TestRoleUpdateWritesARemovedGrantInactive(t *testing.T)
```

The integration is given a fake engine recording the MemQL text it is handed, so the
"one internal-origin write" and "removed grant written inactive" assertions are about
what is EXECUTED. **Assert against the rendered text, not against a mock's method
names** -- a suite that records call names never notices a query that would not parse.

- [ ] **Step 5: Run them and watch them fail**

Run: `go test ./integrations/rbac/ -count=1`
Expected: FAIL -- `undefined: handleRoleCreate`.

- [ ] **Step 6: Write `integrations/rbac/roles.go`**

The guards, in the record's order, each returning its code. Facts the handlers need:

- the caller: `auth.AccessFromContext(ctx)` -- userId, role slug, primaryEmail;
- the catalog: `auth.InstalledCapabilityCatalog()` for the taken slugs, the taken ranks,
  the caller's rank and the caller's resolved grant set. **A nil catalog REFUSES every
  create** with a message saying the catalog has not loaded -- authoring a role against
  the compiled mirror would let a slug the rows already hold be minted again;
- the write: one `engine.Execute` per row, rendered with `langparser.QuoteString`, under
  `auth.ContextWithInternalOrigin` because `createRole` is now `@serverOnly`.

The audit line goes out through `createAuditEvent` in the shape
`integrations/identity/ownership_transfer.go` uses: category `"authorization"`, action
`role_created` / `role_updated` / `role_deactivated`, `targetType: "role"`,
`targetId: <slug>`, and for an update a `detail.changed` list naming the fields that
moved. Emit on SUCCESS and on a BLOCKED refusal, so a refused escalation attempt is in
the trail.

- [ ] **Step 7: Register them and widen the plugin factory**

`Capabilities()` gains three entries with full `ArgsSchema` maps. `plugin.go`'s factory
takes the engine off the `PluginContext` and stores it on the `Integration`; the header
comment's "the factory needs nothing from the plugin context" line is now false and must
be rewritten rather than left.

- [ ] **Step 8: Run the tests and the DSL gates**

Run: `go test ./integrations/rbac/ -count=1 && go run ./cmd/memqllint ./dsl && go test ./test/dslconformance/ -count=1`
Expected: PASS. A new `@sdk` builtin regenerates both SDKs -- run
`make sdk-gen` (or the target `make help` names) and commit the generated files in this
task's commit.

- [ ] **Step 9: Commit**

```bash
git add dsl/rbac/concepts.memql dsl/rbac/shapes.memql dsl/rbac/builtins.memql \
        dsl/identity/concepts.memql dsl/identity/mutations.memql \
        integrations/rbac/roles.go integrations/rbac/roles_test.go \
        integrations/rbac/capabilities.go integrations/rbac/plugin.go \
        sdk/go/client/generated_builtins.go sdk/ts/src/client/generated/builtins.ts
git commit -m "$(cat <<'EOF'
Issue #5179: scope and creator on the role row, the three builtins with their guards, and the audit lines

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 6: `@requiresCapability` -- parsed, validated, enforced

**Issue:** memql#5180 (part 1). **Record:** section E.

**Files:**
- Modify: `component/language/annotations/registry.go` (accept + document)
- Modify: `component/memql/function_types.go` (`RequiresCapability [2]string` or a
  named struct), `function_loader.go`, `function_validator.go`
- Create: `component/memql/requires_capability.go`
- Modify: `component/memql/engine_bootstrap.go` (the load-time check beside its sibling)
- Test: `component/memql/requires_capability_test.go`

**Interfaces:**
- Produces:
  ```go
  type CapabilityRequirement struct{ Verb, Resource string }
  // on Function: RequiresCapability CapabilityRequirement (zero value = none)
  func (e *MemQLEngine) refuseBelowRequiredCapability(ctx, fn *Function, name string) error
  func (e *MemQLEngine) validateRequiresCapabilitySlugs(ctx, fns *FunctionRegistry) []error
  func (e *MemQLEngine) refusePlanBelowRequiredCapability(ctx, plan *QueryPlan) error
  // on QueryPlan: RequiredCapabilities map[string]CapabilityRequirement
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestRequiresCapabilityRefusesAMemberAndAdmitsADeveloper(t *testing.T)
func TestRequiresCapabilityRefusesADeveloperOnUpdatePrincipal(t *testing.T)
func TestMisspelledResourceRefusesLoad(t *testing.T)     // names the seeds' list
func TestMisspelledVerbRefusesLoad(t *testing.T)         // names the five verbs
func TestBothAnnotationsTogetherRequireBoth(t *testing.T)
func TestInternalOriginPasses(t *testing.T)
func TestNoCallerIdentityIsRefused(t *testing.T)
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./component/memql/ -run 'RequiresCapability|MisspelledResource|MisspelledVerb|BothAnnotations' -count=1`
Expected: FAIL -- `undefined: CapabilityRequirement`.

- [ ] **Step 3: Accept the annotation**

`annotations/registry.go`: add `"requiresCapability"` to `Query`, `Mutation` and `Logic`
(the same three that carry `requiresRank`) and a `Docs` entry. The generic
`parseAttribute` already stores `@requiresCapability("read", "principal")` as
`Value = []string{"read","principal"}`; no parser change. The Docs line must say what
distinguishes it from `@requiresRank`: a rank is a FLOOR on the ladder and a capability
is a GRANT, and a cluster can hold one without the other -- which is the whole reason the
slug-comparing specs were wrong (`role == "admin"` is neither).

- [ ] **Step 4: Read it onto the function type**

`function_loader.go` beside `RequiresRank: stringAttributeValue(...)`:

```go
RequiresCapability: capabilityAttributeValue(funcDef.Attributes, "requiresCapability"),
```

`capabilityAttributeValue` takes the `[]string` and returns the zero
`CapabilityRequirement` unless there are EXACTLY two entries -- a one-argument or
three-argument form is a load problem, reported by the validator rather than silently
half-read.

`function_validator.go` collects it into `plan.RequiredCapabilities` at both the sites
that collect `RequiresRank` (`:490` and `:600`).

- [ ] **Step 5: Write `component/memql/requires_capability.go`**

`refuseBelowRequiredCapability` mirrors `refuseBelowRequiredRank` line for line:
internal origin passes; no caller identity is refused; the check is
`auth.Capable(auth.Role(ac.Role), req.Verb, req.Resource)`. The refusal names the
requirement and the caller's own role and never names who could do it.

`validateRequiresCapabilitySlugs` reads the vocabulary from the SEEDS, not from a Go
list: the five verbs are the concept's closed enum (name them as the `auth.Verb*`
constants) and the resource kinds are the distinct `resourceType` values the catalog
holds -- read through the catalog when one is installed and from
`auth.ResourceRole` and its siblings otherwise. The error message prints the list, sorted,
the way `knownSlugs()` does.

Wire the plan-level check wherever `refusePlanBelowRequiredRank` is called
(`grep -rn refusePlanBelowRequiredRank component/memql/`) and the per-function check
wherever `refuseBelowRequiredRank` is called. **Both, not one:** a floor enforced only on
the direct call is bypassed by a query that expands the floored construct.

- [ ] **Step 6: Add the boot check**

`engine_bootstrap.go`, beside the `validateRequiresRankSlugs` loop, same
`report.AddSkip` shape with `Keyword: "requiresCapability"`.

- [ ] **Step 7: Run the tests**

Run: `go test ./component/memql/ ./component/language/... -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add component/language/annotations/registry.go component/memql/function_types.go \
        component/memql/function_loader.go component/memql/function_validator.go \
        component/memql/requires_capability.go component/memql/requires_capability_test.go \
        component/memql/engine_bootstrap.go
git commit -m "$(cat <<'EOF'
Issue #5180: @requiresCapability, parsed beside @requiresRank and refused at boot on a typo

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 7: Migrate the nine spec uses and delete the three specs

**Issue:** memql#5180 (part 2). **Record:** section E's table.

**Files:**
- Modify: `dsl/common/specs.memql` (delete all three)
- Modify: `dsl/identity/queries.memql` (six uses + the `use` line)
- Modify: `dsl/accounts/queries.memql` (three uses + the `use` line)
- Modify: `dsl/deployment/specs.memql`, `dsl/common/traits.memql`,
  `dsl/_reference/_spec.memql`, `dsl/_reference/_trait.memql`, `dsl/_reference/_shape.memql`
  (prose referring to the deleted specs)
- Test: `test/dslconformance/`, `component/memql/`

The exact table, verified in the tree on 2026-09-07:

| Construct | File:line | Today | Becomes |
|---|---|---|---|
| `searchUsers` | `dsl/identity/queries.memql:346` | `requiresDeveloperOrAbove` conjunct | `@requiresRank("developer")` |
| `sessionsForSubjectAdmin` | `:845` | `requiresOwnerOrAdmin` conjunct | `@requiresCapability("update", "principal")` |
| `patIdentitiesForUser` | `:1359` | `requiresOwnerOrAdmin` conjunct | `@requiresCapability("update", "principal")` |
| `userById` | `:1682` | `requiresOwnerOrAdmin` conjunct | `@requiresCapability("read", "principal")` |
| `nodeTokenIdentitiesAdmin` | `:2473` | `requiresOwnerOrAdmin` conjunct | `@requiresCapability("update", "principal")` |
| `pendingUserInvitations` | `:2626` | `requiresDeveloperOrAbove` conjunct | `@requiresRank("developer")` |
| `clientAccountsAll` | `dsl/accounts/queries.memql:86` | OR-branch inside the owner check | the whole owner-check conjunct is DROPPED |
| `clientAccountById` | `:100` | same | same |
| `invitationsForAccount` | `:247` | `requiresDeveloperOrAbove` conjunct | `@requiresRank("developer")` |

- [ ] **Step 1: Write the failing tests FIRST, one per migrated construct**

Each asserts the rung the construct used to refuse is still refused and the rung it used
to admit is still admitted. For the two account reads, assert something stronger: the
same ROWS come back for owner, developer and admin as before the change.

- [ ] **Step 2: Run them against the unchanged tree**

Expected: PASS (they describe today's behaviour). This is the regression baseline; keep
it green through the migration.

- [ ] **Step 3: Migrate the six identity uses**

Remove the conjunct from the filter, add the annotation above the signature, and rewrite
the doc comment. Each comment must say what the construct MEANS rather than which spec it
used to name -- `userById` is "reading a person", which developers already do through
`searchUsers`; the three credential-adjacent reads are "editing a person's credentials",
which developer holds no `update` on `principal` for, so today's exclusion is preserved.

- [ ] **Step 4: Migrate the three account uses**

`clientAccountsAll` and `clientAccountById` lose the WHOLE
`(ownerUserId==actor.userId || actor.isClusterOwner==true || requiresDeveloperOrAbove)`
conjunct. The comment must carry the argument: the concept's composite tier is enforced
BESIDE the filter, so the OR-branch admitted nothing the tier refused; and accounts are
created at admin rank, so no Member-owned row exists for the dropped owner check to have
narrowed. Confirm the tier before writing that:
`grep -n 'rowAuthz' dsl/accounts/concepts.memql`.

- [ ] **Step 5: Delete the three specs and sweep the prose**

`dsl/common/specs.memql` loses `requiresAdmin`, `requiresOwnerOrAdmin` and
`requiresDeveloperOrAbove`. The file keeps its header, which must now say what a
caller-context spec is FOR now that the role-comparing three are gone: a spec compares
projected actor fields, and a ROLE comparison is not one of them any more -- that is
`@requiresRank` or `@requiresCapability`.

Then sweep every reference: `grep -rn 'requiresOwnerOrAdmin\|requiresDeveloperOrAbove\|requiresAdmin' --include='*.memql' --include='*.go' --include='*.ts' --include='*.tsx' --include='*.md' . | grep -v node_modules`.
Comments in `component/identity`, `clients/os`, `CLAUDE.md` and `clients/os/README.md`
name these specs as live gates; each is re-pointed at the annotation that replaced it.
`retired_spec_call_form_test.go` uses the names as string FIXTURES -- those stay, and the
fixture's live-form example is updated to a spec that still exists.

`dsl/_reference/_spec.memql` and `_trait.memql` use `requiresAdmin` as a teaching
example; rename the example to something not deleted (`isClusterAdmin` as an illustrative
name) so the reference does not teach a construct the tree refuses.

- [ ] **Step 6: Run everything**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && go run ./cmd/memqllint ./dsl && make test 2>&1 | tail -40`
Expected: PASS. `test/dslconformance/conformance_test.go` hard-fails on an unclassified
construct -- a query whose only authz was the deleted conjunct now classifies by its
concept tier plus the annotation, and if the classifier does not recognise
`@requiresCapability` as a classification input, TEACH IT rather than adding an
exemption.

- [ ] **Step 7: Commit**

```bash
git add dsl/common/specs.memql dsl/common/traits.memql dsl/identity/queries.memql \
        dsl/accounts/queries.memql dsl/deployment/specs.memql dsl/_reference/ \
        test/dslconformance/ component/memql/ clients/os/README.md CLAUDE.md
git commit -m "$(cat <<'EOF'
Issue #5180: the nine spec uses migrated to the annotations, and the slug-comparing specs deleted

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 8: The wire

**Issue:** memql#5181 (part 1). **Record:** section G.

**Files:**
- Modify: `component/grpc/memql.proto`
- Modify: `component/grpc/my_access_handler.go`
- Delete: `component/server/roles.go`, `component/grpc/server.go:2650-2668`
- Delete: `scripts/ci/user_role_wire_parity_test.go`
- Modify: `sdk/go/client/types.go`, `sdk/ts/src/client/{wire,types}.ts`
- Regenerate: `component/grpc/gen/`

- [ ] **Step 1: Edit the proto**

```proto
message MyAccessResult {
  ...
  reserved 4;
  reserved "cluster_role";
  ...
  // The caller's cluster role, as the SLUG on their v1:identity:user row
  // (epic memql#5166). A catalog slug or one of its aliases -- NOT a closed
  // enum, because the set of roles is cluster state now and an enum can only
  // name the five this repo happened to ship.
  string role = 11;
  // The role's DISPLAY NAME off the catalog row ("Owner", "Support Lead").
  // Empty when the slug resolves to no active rung, which is the honest
  // answer: a client renders the slug it already has rather than inventing
  // a title for a role the cluster does not recognise.
  string role_name = 12;
  // The role's RANK, higher == more privileged. Zero when the slug resolves
  // to no rung -- and zero is a real answer here rather than "unknown",
  // because an unrankable role admits nothing, which is what rank 0 means.
  int32 rank = 13;
}
```

8, 9 and 10 are LEFT FREE for record A (#5165), which edits this message too; the design
record says so and taking them would force a renumber in whichever epic lands second.

Delete the `UserRole` enum and turn its four other uses into strings:
`IdentityCreateMsg.role`, `IdentityInfo.role`, `DelegationCreateMsg.role_ceiling`,
`DelegationInfo.role_ceiling`. Same field numbers -- an enum and a string are wire-
incompatible either way, and this is a pre-release contract change with both sides in
this PR.

- [ ] **Step 2: Regenerate**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && scripts/dev/proto-gen.sh`
Expected: `component/grpc/gen/` updates; `go build ./component/grpc/` then names every
consumer that must change.

- [ ] **Step 3: Rewrite the handler**

`roleToProto` is deleted. `handleMyAccess` fills the three fields from the installed
catalog:

```go
slug := strings.ToLower(strings.TrimSpace(string(ac.Role)))
result := &memqlv1.MyAccessResult{
	...
	Role: slug,
}
if cat := auth.InstalledCapabilityCatalog(); cat != nil {
	if rank, ok := cat.Rank(slug); ok {
		result.Rank = int32(rank)
		result.RoleName = cat.Name(slug)
	}
}
```

This needs `Name(slug) string` on the catalog interface -- add it in Task 1's file if it
is not there yet, and add the fake-catalog method to the Task 1 test. (Self-review note:
`Name` is NOT in the record's five-method sketch; it is required by section G's
`role_name`, and adding it to the interface is better than a second read of the same rows.)

- [ ] **Step 4: Delete the two dead admin-role mappers**

`component/server/roles.go` (whole file: `hasAdminRole`, `HasAdminRole`,
`roleFromString`) and `component/grpc/server.go`'s `isAtLeastAdmin` /
`userRoleFromString`. Confirm they are dead first:
`grep -rn 'HasAdminRole\|hasAdminRole\|isAtLeastAdmin' --include='*.go' . | grep -v '/gen/'`
-- if any live caller appears, it changes to `auth.AtLeastAdmin` instead of the file being
deleted.

- [ ] **Step 5: Delete `scripts/ci/user_role_wire_parity_test.go`**

Its whole subject is the proto enum and the TS union mirroring it. With the enum gone the
gate compares two absences -- which is a gate that passes while measuring nothing. Delete
the file rather than emptying it.

- [ ] **Step 6: Update both SDKs**

Go (`sdk/go/client/types.go`): `AccessSummary` gains `Role string`, `RoleName string`,
`Rank int` and loses `ClusterRole`; `roleFromProto` and the `Role` union constants go if
nothing else uses them (check first). TypeScript: `MyAccessResultPayload` gains
`role` / `roleName` / `rank` and loses `clusterRole`; `UserRoleWire`, `userRoleFromWire`
and `roleFromWire` are deleted; `AccessSummary` in `types.ts` matches the Go shape.
`sdk/ts/test/role.test.ts` pinned the deleted mapping -- rewrite it to pin that a slug
passes through unchanged and an empty one stays empty.

- [ ] **Step 7: Build and test**

Run: `go build ./... && make test 2>&1 | tail -40 && cd sdk/ts && npm run build && npm test`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add component/grpc/memql.proto component/grpc/gen component/grpc/my_access_handler.go \
        component/grpc/server.go sdk/go/client/types.go sdk/ts/src sdk/ts/test
git rm component/server/roles.go scripts/ci/user_role_wire_parity_test.go
git commit -m "$(cat <<'EOF'
Issue #5181: MyAccess carries slug, name and rank; the UserRole enum is deleted

FRONTEND + COCKPIT CONTRACT CHANGE. MyAccessResult.cluster_role (the UserRole
enum) is removed and replaced by role (the slug), role_name and rank.
IdentityCreateMsg.role, IdentityInfo.role and both delegation role_ceiling
fields become strings. memql-cockpit's My Access reading adopts the slug in
memql-cockpit#403.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 9: The OS access reading, and the role identity it can finally draw

**Issue:** memql#5181 (part 2). **Record:** sections G and K, plus `clients/os/DESIGN.md`.

**Files:**
- Modify: `clients/os/src/modules/profile/access.ts`, `useResolvedAccess.ts`
- Create: `clients/os/src/modules/profile/RoleIdentity.tsx`
- Modify: `clients/os/src/apps/settings/SettingsApp.tsx` (About),
  `DiagnosticsSection.tsx` (Permissions), `buildDiagnosticsReport.ts`
- Modify: every `access?.clusterRole` reader (the rename)
- Modify: `clients/os/src/system/roles.ts` (`roleGrantSlug`'s rationale)
- Test: `clients/os/test/**`

- [ ] **Step 1: Widen `ProfileAccess` and rename the field**

```ts
export interface ProfileAccess {
  userId: string;
  primaryEmail: string;
  /** The cluster role's SLUG, as the user row carries it. */
  role: string;
  /** The role's display name off the catalog. Empty when the slug ranks nowhere. */
  roleName: string;
  /** HIGHER is more privileged. 0 when the slug ranks nowhere. */
  rank: number;
}
```

`clusterRole` is renamed rather than kept: it was the camelCase of a wire field that no
longer exists, and it named an enum. Sweep with
`grep -rln 'clusterRole' clients/os/src clients/os/test` and change each; `tsc` catches
anything missed.

`accessFromSummary` stays LENIENT for the reason its comment already gives, and gains
one rule: a blank `roleName` with a non-blank `role` is normal, not a failure.

- [ ] **Step 2: Write the failing OS tests**

```ts
it("reads a custom slug and resolves it by rank", () => {
  setRoleLadder([...seededLadder, { slug: "support-lead", name: "Support Lead", rank: 150, aliases: [] }]);
  const access = accessFromSummary({ userId: "u1", primaryEmail: "", role: "support-lead", roleName: "Support Lead", rank: 150, ... });
  expect(access?.role).toBe("support-lead");
  expect(roleAdmits(access!.role, { min: "user" })).toBe(true);
  expect(roleAdmits(access!.role, { min: "admin" })).toBe(false);
});
it("renders the role NAME, with the slug as the machine-readable second line", ...);
it("falls back to the slug when the cluster reports no name", ...);
```

Run them from inside `clients/os` (`cd clients/os && npx vitest run test/...`), never from
the repo root -- the root config runs the OS tests without the OS setup.

- [ ] **Step 3: Build `RoleIdentity.tsx`**

ONE presentation of "who you are on this cluster", used by About and by Diagnostics'
Permissions line, so the fact is said once (DESIGN.md rule 7) in one container language
(rule 8). It renders the NAME as the thing a person reads, the slug quietly beside it as
the machine-readable value an operator pastes into a support thread, and the rank as the
rung -- because with a catalog the interesting question stopped being "which of five" and
became "where on the ladder".

Shape, at the kit's one control height and existing tokens (no new CSS variables):

```
Support Lead                       <- --os-text-base, currentColor
support-lead . rank 150            <- --os-caption, .os-mono on the slug only
```

Rules to honour: no emoji; nothing invents a third field size (rule 5); an unknown role
renders `Unknown` with the caption `this cluster does not recognise your role`, because
the empty state is the one an operator most needs explained. Export a small
`describeRole(access)` beside it for the plain-text refusal sentences
(`The cluster declined this read for Support Lead.`) so those stop printing a slug at a
person.

- [ ] **Step 4: Use it in the two places that showed the slug**

About's `<dt>Cluster role</dt>` renders `<RoleIdentity />` instead of a mono slug.
Diagnostics' Permissions sentence becomes `You are <RoleIdentity inline />` and the
hidden-surface list's `requires {h.requires}; you are {slug}` uses `describeRole`.
`buildDiagnosticsReport` keeps a MACHINE-readable line and gains the rank:
`Cluster role:     Support Lead (support-lead, rank 150)`.

- [ ] **Step 5: Correct `roleGrantSlug`'s rationale**

Its comment says offering `user` or `viewer` is a write the engine rejects. After Task 3
`IsValidRole` accepts a catalog slug OR an alias, so both spellings are now legal. The
function STAYS -- it keeps a base role granting under the spelling existing rows already
carry, which is what keeps a picker's current-value matching -- and the comment says that
instead.

- [ ] **Step 6: Run the OS lanes**

Run:
```bash
cd /home/znas/memql-projects/epic-roles-as-data/clients/os && npx vitest run && npx tsc -b && cd .. && cd .. && make os-build
```
Expected: PASS. `make os-build` is the only lane that parses the stylesheet.

- [ ] **Step 7: Look at it**

Screenshot About and Diagnostics in BOTH themes, empty and populated, per DESIGN.md's
"Applying them": the acceptance for a surface change is rendered screenshots, not the
diff. Use the QA harness (`clients/os` Vite) rather than jsdom -- jsdom cannot see CSS
tokens.

- [ ] **Step 8: Commit**

```bash
git add clients/os/src clients/os/test
git commit -m "$(cat <<'EOF'
Issue #5181: the OS reads the slug, and draws the role it names

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Task 10: The docs, and the sweep

**Issue:** memql#5181 (part 3) + the epic's section K.

**Files:**
- Modify: `docs/public/operate/auth/access-model.md` (the role spectrum -> "the catalog")
- Modify: `docs/public/language/authoring-rules.md` (`@requiresCapability`)
- Modify: `dsl/_reference/_concept.memql` (the `scopedTo` relationship as an example)
- Modify: `CLAUDE.md` (the authorization section; the DSL dependency tree)
- Modify: `docs/superpowers/specs/2026-09-07-roles-as-data-design.md` (status)

- [ ] **Step 1: Rewrite the access model's role spectrum**

It describes five fixed roles. It becomes the CATALOG: rows with ranks and grants, the
five seeded ones as the shipped set rather than the possible set, `@requiresRank` versus
`@requiresCapability` (a floor versus a grant), the three builtins and their guards, and
the failure mode the record's section I names -- a role row deactivated under a holder by
a cluster owner's write escape leaves the resolver treating that holder as unknown:
nothing, everywhere, until re-roled.

- [ ] **Step 2: Document the annotation in authoring-rules.md**

Beside `@requiresRank`, with the same shape: what it declares, that it is validated at
load against the seeds' vocabulary, that both together require both, and that it gates
who may CALL while `@rowAuthz` decides which rows come back.

- [ ] **Step 3: Update CLAUDE.md**

The "Authorization model" section gains one paragraph on the catalog and the annotation;
the "DSL dependency tree" diagram's rules line gains the annotation. Keep it SHORT -- the
file is already long and gate-scanned.

- [ ] **Step 4: Verify the docs gates**

Run: `cd /home/znas/memql-projects/epic-roles-as-data && go test -count=1 . && go test ./docs/... -count=1 2>/dev/null; go test ./test/... -count=1`
Expected: PASS. Editing root `CLAUDE.md` is gate-scanned by tests in the root package;
a new doc file trips the docs front-matter gate.

- [ ] **Step 5: The final sweep -- the acceptance criterion, checked**

```bash
cd /home/znas/memql-projects/epic-roles-as-data
grep -rn 'UserRole\|cluster_role\|clusterRole' --include='*.go' --include='*.ts' \
  --include='*.tsx' --include='*.proto' --include='*.memql' --include='*.md' . \
  | grep -v node_modules | grep -v '/gen/' | grep -v '\.pb\.go'
```
Expected: only `SetUserRole` (a different name), `setUserRole`, `testUserRole` (a test
constant), `priorUserRole` (a mutation-meta field) and history. Anything else is a miss.

- [ ] **Step 6: Full verification**

```bash
cd /home/znas/memql-projects/epic-roles-as-data
go build ./... && go vet ./component/... && make test 2>&1 | tail -40
go run ./cmd/memqllint ./dsl
MEMQL_REQUIRE_DB=1 MEMQL_DATABASE_DSN=postgres://memql:memql_dev@localhost:15434/memql \
  go test -count=1 ./component/memql/... 2>&1 | tail -20
cd clients/os && npx vitest run && npx tsc -b
```

- [ ] **Step 7: Commit and delete this plan**

The plan is deleted in the epic's merge, per every task issue.

```bash
git add docs/public CLAUDE.md dsl/_reference docs/superpowers/specs
git rm docs/superpowers/plans/2026-09-07-roles-as-data.md
git commit -m "$(cat <<'EOF'
Issue #5166: the docs for the catalog and the annotation, and the epic's plan retired

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_013XB6nKA4A7d1JKtJjXSgp9
EOF
)"
```

---

## Self-review against the spec

| Spec section | Task |
|---|---|
| B -- the catalog, the resolver, the mirror, the parity test | 1, 2 |
| B -- the engine loader, the collapse, the reload, routing rules | 2 |
| C -- `IsValidRole`, `MayAssignRole`, the four slug fields, `@serverOnly` | 3, 4 |
| D -- the three builtins and their guards | 5 |
| E -- the annotation, and the nine-use migration | 6, 7 |
| F -- `accountId` + `scopedTo`, `createdBy`, `roleFull` | 5 |
| G -- the wire, the SDKs, the OS reading | 8, 9 |
| H -- routing rules and the audit lines | 2, 5 |
| I -- failure modes | 2 (boot log, failed reload), 3 (unknown slug), 10 (documented) |
| J -- the test list | every task's step 1 |
| K -- delivery, one PR, docs, the commit body | 8, 10 |
| D1-D12 | 1 (D1, D2), 3 (D3, D4), 5 (D5-D9), 5 (D10), 6 (D11), 8 (D12) |

Deviations from the record, both deliberate and both argued in place:

1. **`Name(slug)` is a sixth catalog method** (Task 8, step 3). Section B's sketch has
   five and section G needs `role_name`; a second read of the same rows to answer it
   would be a second source for one fact.
2. **The owner carve-out in `MayAssignRole`** (Task 3, step 3). D4's literal composition
   refuses owner -> owner, which makes a second owner unmakeable; `GovernPrincipal`
   already carries the identical carve-out on the target half for the identical reason.
