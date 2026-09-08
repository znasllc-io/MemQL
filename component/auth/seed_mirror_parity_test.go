package auth

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// THE MIRROR IS PINNED TO THE SEEDS IT MIRRORS (epic memql#5166, D1).
//
// component/auth's `capabilitySets` used to BE the authorization model: every
// runtime gate resolved through it, so a role authored as data in
// dsl/rbac/seeds.memql held whatever this map said and nothing else. memql#5166
// demoted it to the seed's mirror -- consulted only until the catalog rows are
// readable -- and a mirror is exactly as good as the guarantee that it matches.
//
// WHY IT HAS TO BE PINNED IN BOTH DIRECTIONS, which is not symmetric flourish:
//
//   - a pair the SEEDS hold and the mirror does not is a permission that
//     appears seconds after boot and not before. The window is short and the
//     symptom is a gate refusing exactly one class of request during startup,
//     which is the shape of bug that gets closed as "could not reproduce".
//   - a pair the MIRROR holds and the seeds do not is worse: a permission that
//     exists at boot and then vanishes, and on a node whose database never
//     comes up, one that never vanishes at all.
//
// So the mirror is not allowed to be a superset "for safety" in either
// direction, and this gate refuses both.
//
// It reads the DSL as TEXT for the reason TestClientLadderFixtureMatchesTheSeeds
// does: component/auth is a leaf module that must not import the engine, and
// parsing the seeds the same way both gates do keeps the comparison about the
// two files rather than about whether anything had been generated.

// mirrorSlugs maps each compiled mirror key to the catalog slug it stands for.
//
// The two vocabularies are not the same set, and that is the fact this table
// carries: a user row spells the member tier `writer` and the viewer tier
// `reader`, while the catalog seeds them as `user` and `viewer` and records the
// legacy spelling in the role row's `aliases`. The mirror is keyed by the
// USER-ROW spelling because that is what arrives on a request.
var mirrorSlugs = map[Role]string{
	RoleOwner:     "owner",
	RoleDeveloper: "developer",
	RoleAdmin:     "admin",
	RoleWriter:    "user",
	RoleReader:    "viewer",
}

func TestSeedMatchesCompiledMirror(t *testing.T) {
	// The holder is package state and every other test in this package may
	// have installed one. The mirror is what is under test here, so make sure
	// nothing is answering ahead of it.
	SetCapabilityCatalog(nil)

	seeded := seededGrants(t)

	// THE REACHABLE POSITIVE. Without it a renamed seed block, a changed
	// keyword or a moved file would leave this gate comparing two empty sets
	// and reporting success -- the exact failure mode it exists to prevent,
	// one level up.
	if len(seeded) == 0 {
		t.Fatalf("parsed no capability seeds out of %s. This gate would then pass by "+
			"comparing two empty sets, which is worse than the drift it refuses.", rbacSeedPath)
	}
	for role, slug := range mirrorSlugs {
		if _, ok := seeded[slug]; !ok {
			t.Fatalf("%s seeds no capabilities for %q, which the compiled mirror carries as %q. "+
				"Either the seed was renamed (update mirrorSlugs) or a base role lost its "+
				"grants, and this gate cannot tell which -- both need a person.",
				rbacSeedPath, slug, role)
		}
	}

	for role, slug := range mirrorSlugs {
		want := seeded[slug]
		got := capabilitySets[role]

		for vr := range want {
			if !got[vr] {
				t.Fatalf("%s seeds %s on %s for %q and the compiled mirror (auth.%s) does not "+
					"hold it.\n\nThe mirror is what answers before the catalog rows are readable, "+
					"so a pair missing from it is a permission that appears seconds after boot "+
					"and not before.",
					rbacSeedPath, vr.verb, vr.resource, slug, role)
			}
		}
		for vr := range got {
			if !want[vr] {
				t.Fatalf("the compiled mirror (auth.%s) holds %s on %s and %s seeds no such "+
					"capability for %q.\n\nThe mirror is not allowed to be a superset: a pair "+
					"only it holds is a permission that exists at boot and then vanishes -- and "+
					"on a node whose database never comes up, one that never vanishes at all.",
					role, vr.verb, vr.resource, rbacSeedPath, slug)
			}
		}
	}
}

// TestSeedMatchesCompiledMirrorCoversEverySeededRole is the OTHER coverage
// question, and the one the loop above cannot ask: mirrorSlugs is a hand-kept
// table, so a base role added to the seeds and not to it would be compared
// against nothing at all while every assertion above still passed.
func TestSeedMatchesCompiledMirrorCoversEverySeededRole(t *testing.T) {
	seeded := seededGrants(t)
	covered := map[string]bool{}
	for _, slug := range mirrorSlugs {
		covered[slug] = true
	}
	var uncovered []string
	for slug := range seeded {
		if !covered[slug] {
			uncovered = append(uncovered, slug)
		}
	}
	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		t.Fatalf("%s seeds predefined capabilities for %s, which mirrorSlugs does not name.\n\n"+
			"A seeded role missing from that table is compared against nothing, so the parity "+
			"gate passes while measuring a shrinking subset of the catalog. Either add the role "+
			"to the compiled mirror and to mirrorSlugs, or -- if it is deliberately not "+
			"mirrored -- say so here with the reason.",
			rbacSeedPath, strings.Join(uncovered, ", "))
	}
}

// ---------------------------------------------------------------------
// parsing
// ---------------------------------------------------------------------

// seedCapabilityPattern reads one `seed capability <name> { ... }` body.
// Anchored on the keyword pair so a capability DISCUSSED in prose -- and this
// file's neighbours discuss them at length -- is not parsed as a declaration.
var seedCapabilityPattern = regexp.MustCompile(`seed\s+capability\s+[\w-]+\s*\{([^}]*)\}`)

var (
	seedRoleSlugPattern     = regexp.MustCompile(`roleSlug:\s*"([^"]+)"`)
	seedVerbPattern         = regexp.MustCompile(`verb:\s*"([^"]+)"`)
	seedResourceTypePattern = regexp.MustCompile(`resourceType:\s*"([^"]+)"`)
	seedEffectPattern       = regexp.MustCompile(`effect:\s*"([^"]+)"`)
)

// seededGrants parses dsl/rbac/seeds.memql into slug -> grant set.
//
// DENY ROWS ARE EXCLUDED, and the mirror is a set of ALLOWS: `capabilitySets`
// has no way to express a deny (its value is a bool), so a seeded deny would be
// unrepresentable there and comparing it in would fail this gate for a reason
// that is about the mirror's shape rather than about drift. No seed writes one
// today -- D9 says no v1 path does -- and if one ever appears, this gate should
// fail loudly rather than silently ignore it, which is what the count below is.
func seededGrants(t *testing.T) map[string]map[verbResource]bool {
	t.Helper()
	src := stripComments(readClientFile(t, rbacSeedPath))

	out := map[string]map[verbResource]bool{}
	denies := 0
	for _, block := range seedCapabilityPattern.FindAllStringSubmatch(src, -1) {
		body := block[1]
		slug := seedRoleSlugPattern.FindStringSubmatch(body)
		verb := seedVerbPattern.FindStringSubmatch(body)
		resource := seedResourceTypePattern.FindStringSubmatch(body)
		if slug == nil || verb == nil || resource == nil {
			t.Fatalf("a `seed capability` block in %s carries no roleSlug/verb/resourceType "+
				"triple:\n%s\n\nThis gate parses text; a shape it cannot read is a grant it "+
				"cannot compare, so it fails rather than skipping.", rbacSeedPath, body)
		}
		if effect := seedEffectPattern.FindStringSubmatch(body); effect != nil && effect[1] == "deny" {
			denies++
			continue
		}
		if out[slug[1]] == nil {
			out[slug[1]] = map[verbResource]bool{}
		}
		out[slug[1]][verbResource{verb: verb[1], resource: resource[1]}] = true
	}
	if denies > 0 {
		t.Fatalf("%s seeds %d capability rows with effect=\"deny\". The compiled mirror's value "+
			"is a bool and cannot express one, so the two can no longer be compared pair for "+
			"pair. D9 says no v1 path writes a deny; if that changed, this gate and "+
			"capabilitySets both need a shape that can hold it.", rbacSeedPath, denies)
	}
	return out
}
