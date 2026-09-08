package memql

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The MemQL OS role grid, and the seeds it is drawn from. Both paths are
// written out rather than derived so a failure names the files a reader has to
// go and open.
const (
	osRoleGridPath = "../../clients/os/src/apps/users/grid.ts"
	rbacSeedsPath  = "../../dsl/rbac/seeds.memql"
)

// osRoleGridPattern matches the grid's vocabulary literal and captures the
// string. It accepts exactly the ONE-LINE form
//
//	export const ROLE_GRID_VOCABULARY = "principal:read,create|...";
//
// and nothing computed: this gate compares NAMES, and a value assembled at
// runtime or broken across lines is one it cannot read. It says so rather than
// passing.
var osRoleGridPattern = regexp.MustCompile(
	`(?m)^\s*export\s+const\s+ROLE_GRID_VOCABULARY\s*=\s*"([^"]*)"\s*;?\s*$`,
)

// seededGrantPattern reads one `seed capability` line's verb and resourceType.
// Both appear on the same line in every seed the file holds, which is the form
// the flattened tree is authored in.
var seededGrantPattern = regexp.MustCompile(
	`verb:\s*"([a-z]+)"\s+resourceType:\s*"([a-z]+)"`,
)

// TestRoleGridVocabularyMatchesTheSeeds holds the pairs the OS's permission
// grid offers equal to the (verb, resourceType) pairs dsl/rbac/seeds.memql
// names, in both directions.
//
// THERE ARE DELIBERATELY TWO LISTS, exactly as TestSiteKindEnumMatchesOsOfferedKinds
// records for the site kinds. The seeds are what the cluster's own roles hold;
// the grid is what the OS draws a checkbox for, and the OS cannot ask the
// engine per keystroke which pairs are meaningful. So the risk is not
// duplication, it is DRIFT -- and drift here is invisible in the worst way:
//
//   - a pair the OS offers that no seed names is a checkbox nothing can store.
//     The write succeeds, the row lands, and no resolver ever asks that
//     question, so the person sees a permission that does nothing.
//   - a pair the seeds grow and the grid does not offer is a permission nobody
//     can grant from the shell, on a screen whose whole job is granting them.
//
// Both sides keep working, which is why this is a gate rather than a review
// note.
//
// A MISSING FILE IS A FAILURE, NOT A SKIP. A skip would make this gate
// silently vacuous the moment either file moved, which is exactly when the two
// lists are most likely to have diverged.
func TestRoleGridVocabularyMatchesTheSeeds(t *testing.T) {
	seeded := seededGrantPairs(t)
	offered := osGridPairs(t)

	seededSet := map[string]bool{}
	for _, p := range seeded {
		seededSet[p] = true
	}
	offeredSet := map[string]bool{}
	for _, p := range offered {
		offeredSet[p] = true
	}

	var missing, extra []string
	for _, p := range seeded {
		if !offeredSet[p] {
			missing = append(missing, p)
		}
	}
	for _, p := range offered {
		if !seededSet[p] {
			extra = append(extra, p)
		}
	}

	if len(missing) > 0 {
		t.Errorf("dsl/rbac/seeds.memql grants %v, and the OS grid offers no cell for it.\n"+
			"Add the pair to ROLE_GRID_VOCABULARY in %s -- a permission the seeds hold and the\n"+
			"grid does not draw is one nobody can grant from the shell.",
			missing, osRoleGridPath)
	}
	if len(extra) > 0 {
		t.Errorf("%s offers %v, and no seeded role holds it.\n"+
			"Either seed the grant in dsl/rbac/seeds.memql or drop the pair: a checkbox for a pair\n"+
			"nothing gates writes a row no resolver reads, so the person is shown a permission that\n"+
			"does nothing.",
			osRoleGridPath, extra)
	}
}

// seededGrantPairs reads every (resourceType, verb) pair the seeds grant.
func seededGrantPairs(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(rbacSeedsPath)
	if err != nil {
		t.Fatalf("the RBAC seeds are unreadable at %s: %v\n"+
			"This gate is the only thing keeping the OS's permission grid and the seeded grants in\n"+
			"step. If the file moved, update rbacSeedsPath; it is not a skip.", rbacSeedsPath, err)
	}
	seen := map[string]bool{}
	for _, m := range seededGrantPattern.FindAllStringSubmatch(string(raw), -1) {
		seen[m[2]+":"+m[1]] = true
	}
	if len(seen) == 0 {
		t.Fatalf("%s named no `verb: ... resourceType: ...` pair.\n"+
			"Every assertion in this test is of the form \"nothing was found missing\", so a scan\n"+
			"that read nothing would pass while covering nothing.", rbacSeedsPath)
	}
	return sortedPairs(seen)
}

// osGridPairs reads the pairs the OS grid offers out of its one-line literal.
func osGridPairs(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(osRoleGridPath)
	if err != nil {
		t.Fatalf("the OS role grid is unreadable at %s: %v\n"+
			"If the file moved, update osRoleGridPath; if it was deleted, the OS is drawing\n"+
			"permissions some other way and that needs a decision, not a skip.", osRoleGridPath, err)
	}
	m := osRoleGridPattern.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s does not export ROLE_GRID_VOCABULARY as a one-line string literal.\n"+
			"It must, and as a literal rather than a computed expression: this gate compares the\n"+
			"NAMES, and a value assembled at runtime is one it cannot read.", osRoleGridPath)
	}
	seen := map[string]bool{}
	for _, entry := range strings.Split(string(m[1]), "|") {
		resource, verbs, ok := strings.Cut(entry, ":")
		if !ok || resource == "" {
			t.Fatalf("ROLE_GRID_VOCABULARY entry %q is not `<resource>:<verb>,<verb>`", entry)
		}
		for _, verb := range strings.Split(verbs, ",") {
			if verb == "" {
				t.Fatalf("ROLE_GRID_VOCABULARY entry %q names an empty verb", entry)
			}
			seen[resource+":"+verb] = true
		}
	}
	return sortedPairs(seen)
}

func sortedPairs(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
