package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/component/conceptfields"
)

// TestConceptFieldSnapshotIsNotStale -- memql#5209.
//
// ===========================================================================
// THE HALF memql#5199 COULD NOT COVER
// ===========================================================================
// TestRetiredConceptFieldsAreMigratedSafely covers the direction that DELETES
// LIVE DATA: for every key an up-migration strips, it requires a concept
// predicate in the same statement and requires the key to be genuinely
// undeclared on each concept named. It cannot cover the direction that BRICKS
// ROWS, because a retirement that shipped no migration leaves nothing for a
// migration-reader to read. That direction is the one that has actually bitten,
// three times -- memql#4766, memql#4772, and memql#5165, whose own migration
// comment records that it MISSED `active` until a per-concept field sweep found
// it. Every one was invisible to CI by construction: the db-tests lane runs
// against a fresh database where the boot seed writes clean rows, so only a
// database with history fails, and no CI lane has one.
//
// ===========================================================================
// WHY A COMMITTED SNAPSHOT AND NOT `git merge-base`
// ===========================================================================
// The check is "did a field disappear from a concept in this branch", which
// needs a BEFORE. The obvious source does not exist here: every
// `actions/checkout` in ci.yml is depth-1, so the main Go lanes have no history
// at all (only install-e2e.yml and gitleaks.yml set fetch-depth: 0). The
// committed snapshot IS the merge base by construction -- the file in the tree
// is what main has -- and it works in a depth-1 checkout, in `make test`, with
// no database and no network.
//
// ===========================================================================
// WHAT FAILING HERE MEANS
// ===========================================================================
// Two different things, and the message says which:
//
//	STALE -- the tree declares something the snapshot does not record, or the
//	other way round. Run `make concept-snapshot` and commit the result.
//
//	UNRECORDED -- the tree TAKES SOMETHING AWAY and the ledger does not say
//	what happened to the stored rows. `make concept-snapshot` REFUSES this one
//	rather than absorbing it, and prints the ledger line to add. That refusal
//	is the gate; a regeneration that quietly dropped the field would be the
//	same as not running.
//
// It is the SEVENTH regeneration gate (memqllint, sdk-gen, arch-model,
// frontdoor-hosts, frontdoor-paths, proto-gen) and the only one that refuses to
// regenerate.
func TestConceptFieldSnapshotIsNotStale(t *testing.T) {
	result, err := conceptfields.Verify(
		conceptfields.DefaultDSLRoot,
		conceptfields.DefaultSnapshotPath,
		conceptfields.DefaultMigrationsDir,
	)
	if err != nil {
		t.Fatalf("reading the tree: %v", err)
	}

	if problem := result.Problem(); problem != "" {
		t.Fatalf("\n\n%s\n", problem)
	}

	if string(result.Committed) != string(result.Wanted) {
		t.Fatalf("%s is stale.\n\nRun `make concept-snapshot` and commit the result.\n%s",
			conceptfields.DefaultSnapshotPath, firstDifference(string(result.Committed), string(result.Wanted)))
	}

	// A snapshot over nothing passes for the wrong reason, and the two ways
	// that happens are opposite: a scanner that stopped matching, and a
	// committed file somebody emptied.
	if len(result.Snapshot.Concepts) < 200 {
		t.Fatalf("the snapshot holds %d concepts, expected well over 200 -- "+
			"either the scanner has stopped matching the tree or the committed file was truncated",
			len(result.Snapshot.Concepts))
	}
	if len(result.Snapshot.Retired) == 0 {
		t.Fatal("the retirement ledger is empty. Three migrations in the corpus strip a retired " +
			"payload key, so at least their entries must be there -- an empty ledger means the " +
			"inverse check has nothing to compare against and would clear any future deletion")
	}
}

// firstDifference points at the first line that differs, so a failure over an
// 8,000-line generated file names a place to look rather than inviting a diff
// of the whole thing.
func firstDifference(committed, wanted string) string {
	a := strings.Split(committed, "\n")
	b := strings.Split(wanted, "\n")
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return "\nFirst difference at line " + strconv.Itoa(i+1) + ":\n  committed: " + a[i] + "\n  wanted:    " + b[i]
		}
	}
	if len(a) != len(b) {
		return "\nThe files agree for " + strconv.Itoa(min(len(a), len(b))) + " lines and then differ in length (" +
			strconv.Itoa(len(a)) + " committed, " + strconv.Itoa(len(b)) + " wanted)."
	}
	return ""
}
