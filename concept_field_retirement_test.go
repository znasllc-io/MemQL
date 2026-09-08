package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/component/conceptfields"
)

// TestRetiredConceptFieldsAreMigratedSafely -- memql#5199.
//
// ===========================================================================
// REMOVING A FIELD FROM A CONCEPT BRICKS EVERY EXISTING ROW THAT CARRIES IT
// ===========================================================================
// Every concept builds its JSON schema with `additionalProperties: false`
// (concept_parser.go's parsedConcept defaults noAdditional to true, and no DSL
// annotation turns it off), and a mutation's read-merge validates the MERGED
// payload -- stored keys included. So deleting a field does not merely stop new
// writes carrying it: the next write to any row that has it is refused with
// `additionalProperties '<field>' not allowed`.
//
// The remedy is a migration that strips the key, and the trap in WRITING one is
// the reason this gate exists rather than a checklist. It has two halves and
// they fail in opposite directions:
//
//	TOO NARROW -- no migration at all. Silent until an upgrade, and invisible to
//	CI by construction: the db-tests lane runs against a FRESH database where
//	the boot seed writes clean rows, so only a database with history fails.
//
//	TOO WIDE -- a migration that strips by KEY NAME with no concept predicate.
//	This one DELETES LIVE DATA. `status` is retired on v1:cluster:cluster and
//	live on v1:cluster:releaseCut (3,120 versions measured) and
//	v1:cluster:deployment (1,559); `groupIds` is retired on v1:identity:user
//	and live on v1:agents:agent. A name-scoped UPDATE takes both.
//
// THIS GATE COVERS THE SECOND HALF, WHICH IS THE DESTRUCTIVE ONE. For every key
// an up-migration strips it requires a concept predicate in the same statement,
// and requires the key to be genuinely undeclared on each concept named. It
// cannot see the first half -- a retirement that shipped no migration leaves
// nothing here to read -- and the review-time check that would, comparing the
// declared field set per concept against a committed snapshot, is memql#5209.
// Saying so is the point: a gate that hides what it cannot examine is worse
// than no gate, because an auditor seeing one stops looking.
//
// DOWN MIGRATIONS ARE DELIBERATELY OUT OF SCOPE. A down migration restores an
// older shape, so it legitimately strips a key the CURRENT tree declares --
// 20260603000000_attachment_rename_gcsurl_to_bloburl.down.sql strips `blobUrl`,
// which is the live name. Checking them would fail on correct code.
func TestRetiredConceptFieldsAreMigratedSafely(t *testing.T) {
	declared, conceptFiles := declaredConceptFields(t)
	if len(declared) == 0 {
		t.Fatal("no concepts parsed out of dsl/: this gate would pass over everything")
	}

	ups, err := filepath.Glob("component/database/memory-nodes/migrations/*.up.sql")
	if err != nil {
		t.Fatalf("globbing migrations: %v", err)
	}
	if len(ups) == 0 {
		t.Fatal("no up-migrations found: this gate would pass over everything")
	}

	var failures []string
	checked := 0

	for _, path := range ups {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		for _, stmt := range sqlStatements(string(raw)) {
			f, n := checkStrip(filepath.Base(path), stmt, declared, conceptFiles)
			failures = append(failures, f...)
			checked += n
		}
	}

	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("migrations that strip a payload key unsafely:\n\n  %s\n",
			strings.Join(failures, "\n  "))
	}
	if checked == 0 {
		t.Fatal("no `payload - '<key>'` statement was examined at all. " +
			"Either the strip form changed or the scanner stopped matching it -- " +
			"a gate over zero statements passes for the wrong reason.")
	}
	t.Logf("examined %d (concept, key) strips across %d up-migrations", checked, len(ups))
}

// checkStrip is the rule, as a pure function over one statement, so it can be
// falsified by planting a violation rather than only observed passing over a
// corpus that has none. It returns the failures and the number of (concept, key)
// pairs it actually examined.
func checkStrip(label, stmt string, declared map[string]map[string]bool, conceptFiles map[string]string) ([]string, int) {
	keys := strippedPayloadKeys(stmt)
	if len(keys) == 0 {
		return nil, 0
	}
	concepts := scopedConcepts(stmt)
	if len(concepts) == 0 {
		return []string{fmt.Sprintf(
			"%s: strips %s with NO `concept = '...'` predicate in the same statement. "+
				"A payload key is not unique to one concept -- scoping by name alone deletes live data "+
				"from every other concept that declares it.",
			label, quoteAll(keys))}, 0
	}

	var failures []string
	checked := 0
	for _, concept := range concepts {
		fields, known := declared[concept]
		if !known {
			// Reported, never passed over in silence: a typo'd concept id looks
			// exactly like a retired concept, and both make the UPDATE match
			// nothing.
			failures = append(failures, fmt.Sprintf(
				"%s: strips %s scoped to %q, and no concept with that id is declared under dsl/. "+
					"Either the id is misspelled -- in which case the migration silently does nothing -- "+
					"or the concept itself was retired, which needs saying in the migration's own comment.",
				label, quoteAll(keys), concept))
			continue
		}
		for _, k := range keys {
			checked++
			if fields[k] {
				failures = append(failures, fmt.Sprintf(
					"%s: strips %q from %s, WHICH STILL DECLARES IT (%s). "+
						"This migration deletes live data. Scope it to the concept that retired the field.",
					label, k, concept, conceptFiles[concept]))
			}
		}
	}
	return failures, checked
}

// THE MIGRATION READERS LIVE IN component/conceptfields NOW (memql#5209).
//
// This gate asks "is this strip safe"; conceptfields.VerifyLedger asks "does
// this strip have a record, and does this record have a strip". They are
// opposite directions over the SAME corpus, so two readers of `payload - 'x'`
// would be two definitions of what a strip IS -- and a migration could satisfy
// one gate's parse while falling outside the other's, with nothing saying so.
// One reader, two questions. The thin wrappers below keep this file's own
// prose and its planted-violation tests reading as they did.

func sqlStatements(sql string) []string        { return conceptfields.SQLStatements(sql) }
func strippedPayloadKeys(stmt string) []string { return conceptfields.StrippedPayloadKeys(stmt) }
func scopedConcepts(stmt string) []string      { return conceptfields.ScopedConcepts(stmt) }

// declaredConceptFields reads every concept's TOP-LEVEL field names, keyed by
// the full `v1:<namespace>:<name>` id the migrations spell.
//
// IT IS conceptfields.Scan, not a reader of its own (memql#5209). The regex
// that stood here globbed `dsl/*/concepts.memql`, which missed the 65
// per-file concepts under `dsl/shopify/generated/` entirely -- so a migration
// stripping a live shopify field would have been cleared by the
// unknown-concept branch below rather than caught by the still-declares-it
// branch, which is the exact inversion this gate exists to prevent. The
// shared scanner walks the tree the LOADER walks and builds each concept's
// real JSON schema, so "declared" here means what it means at runtime.
func declaredConceptFields(t *testing.T) (map[string]map[string]bool, map[string]string) {
	t.Helper()
	snap, err := conceptfields.Scan(conceptfields.DefaultDSLRoot)
	if err != nil {
		t.Fatalf("scanning %s: %v", conceptfields.DefaultDSLRoot, err)
	}
	fields := map[string]map[string]bool{}
	files := map[string]string{}
	for _, entry := range snap.Concepts {
		set := make(map[string]bool, len(entry.Fields))
		for _, f := range entry.Fields {
			set[f] = true
		}
		fields[entry.Concept] = set
		files[entry.Concept] = conceptfields.DefaultDSLRoot + "/" + entry.File
	}
	return fields, files
}

func quoteAll(keys []string) string {
	q := make([]string, 0, len(keys))
	for _, k := range keys {
		q = append(q, fmt.Sprintf("%q", k))
	}
	return strings.Join(q, ", ")
}

// ===========================================================================
// The instrument, proved to move
// ===========================================================================
// The corpus has two strips and both are correct, so the gate above passing is
// a null result -- it cannot tell a rule that runs and finds nothing from a
// rule that does not run. These plant each violation and one control.

func retirementFixture() (map[string]map[string]bool, map[string]string) {
	return map[string]map[string]bool{
			// `status` retired here...
			"v1:cluster:cluster": {"name": true, "region": true, "provider": true},
			// ...and live here. This pair is the whole reason for the rule.
			"v1:cluster:releaseCut": {"status": true, "version": true},
		}, map[string]string{
			"v1:cluster:cluster":    "dsl/cluster/concepts.memql",
			"v1:cluster:releaseCut": "dsl/cluster/concepts.memql",
		}
}

func TestAnUnscopedStripIsRefused(t *testing.T) {
	declared, files := retirementFixture()
	stmt := `UPDATE "MemoryNodes" SET payload = payload - 'status' WHERE payload ? 'status'`
	got, checked := checkStrip("planted.up.sql", stmt, declared, files)
	if len(got) != 1 {
		t.Fatalf("want 1 failure for a strip with no concept predicate, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "NO `concept = '...'` predicate") {
		t.Errorf("the failure must name the missing predicate:\n%s", got[0])
	}
	if checked != 0 {
		t.Errorf("checked = %d; an unscoped strip examines no (concept, key) pair", checked)
	}
}

func TestStrippingALiveFieldIsRefused(t *testing.T) {
	// The destructive direction, in its exact real shape: `status` is genuinely
	// retired on v1:cluster:cluster and genuinely live on v1:cluster:releaseCut,
	// so a migration aimed at the wrong one deletes 3,120 versions of real data.
	declared, files := retirementFixture()
	stmt := `UPDATE "MemoryNodes" SET payload = payload - 'status' WHERE concept = 'v1:cluster:releaseCut'`
	got, _ := checkStrip("planted.up.sql", stmt, declared, files)
	if len(got) != 1 {
		t.Fatalf("want 1 failure for stripping a declared field, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "WHICH STILL DECLARES IT") {
		t.Errorf("the failure must say the concept still declares the field:\n%s", got[0])
	}
}

func TestAnUnknownConceptIsReportedNotSkipped(t *testing.T) {
	// A misspelled id and a retired concept look identical, and both make the
	// UPDATE match nothing. Passing over it silently is how a migration that
	// does nothing reads as a migration that worked.
	declared, files := retirementFixture()
	stmt := `UPDATE "MemoryNodes" SET payload = payload - 'status' WHERE concept = 'v1:cluster:clustre'`
	got, _ := checkStrip("planted.up.sql", stmt, declared, files)
	if len(got) != 1 {
		t.Fatalf("want 1 failure for an unknown concept id, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "no concept with that id is declared") {
		t.Errorf("the failure must say the id resolves to nothing:\n%s", got[0])
	}
}

func TestACorrectlyScopedStripIsSilent(t *testing.T) {
	// The control. Without it, a rule that fails on every strip passes all three
	// tests above.
	declared, files := retirementFixture()
	stmt := `UPDATE "MemoryNodes" SET payload = payload - 'status' WHERE concept = 'v1:cluster:cluster' AND payload ? 'status'`
	got, checked := checkStrip("planted.up.sql", stmt, declared, files)
	if len(got) != 0 {
		t.Fatalf("the shipped migration's own shape was reported: %v", got)
	}
	if checked != 1 {
		t.Errorf("checked = %d, want 1 -- the pair must actually be examined, not skipped", checked)
	}
}

func TestEveryConceptInAnInListIsChecked(t *testing.T) {
	// An IN-list is one statement scoped to several concepts, and a reader that
	// took only the first would clear a migration that is wrong about the rest.
	declared, files := retirementFixture()
	stmt := `UPDATE "MemoryNodes" SET payload = payload - 'status' WHERE concept IN ('v1:cluster:cluster', 'v1:cluster:releaseCut')`
	got, checked := checkStrip("planted.up.sql", stmt, declared, files)
	if len(got) != 1 {
		t.Fatalf("want the releaseCut half reported, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "v1:cluster:releaseCut") {
		t.Errorf("the failure must name the concept that still declares it:\n%s", got[0])
	}
	if checked != 2 {
		t.Errorf("checked = %d, want 2 -- both concepts in the list must be examined", checked)
	}
}

func TestProseIsNotAStrip(t *testing.T) {
	// These migrations carry long comments that quote the very pattern this gate
	// matches. Comments are stripped FIRST, and this is what says so.
	sql := `-- UPDATE "MemoryNodes" SET payload = payload - 'status' WHERE concept = 'v1:cluster:releaseCut'
SELECT 1;`
	declared, files := retirementFixture()
	for _, stmt := range sqlStatements(sql) {
		if got, _ := checkStrip("planted.up.sql", stmt, declared, files); len(got) != 0 {
			t.Fatalf("a commented-out strip was read as real: %v", got)
		}
	}
}

func TestTheChainedStripFormIsRead(t *testing.T) {
	// `payload - 'a' - 'b'` is one statement retiring two fields, and a scanner
	// that took only the first would clear the second silently.
	declared, files := retirementFixture()
	declared["v1:cluster:releaseCut"]["extra"] = true
	stmt := `UPDATE "MemoryNodes" SET payload = payload - 'status' - 'extra' WHERE concept = 'v1:cluster:releaseCut'`
	got, checked := checkStrip("planted.up.sql", stmt, declared, files)
	if checked != 2 {
		t.Errorf("checked = %d, want 2 -- both keys in the chain must be examined", checked)
	}
	if len(got) != 2 {
		t.Fatalf("want both live keys reported, got %d: %v", len(got), got)
	}
}

func TestTheRealTreeParsesIntoConceptsAndFields(t *testing.T) {
	// The parser's own coverage. Everything above runs on fixtures, so a scanner
	// that read zero concepts out of dsl/ would leave the corpus gate passing
	// over nothing while every unit test stayed green.
	declared, _ := declaredConceptFields(t)
	if len(declared) < 100 {
		t.Fatalf("parsed %d concepts out of dsl/, expected well over 100 -- the scanner has stopped matching", len(declared))
	}
	cluster, ok := declared["v1:cluster:cluster"]
	if !ok {
		t.Fatal("v1:cluster:cluster did not parse")
	}
	if !cluster["seedNodeTypes"] || !cluster["provider"] {
		t.Errorf("v1:cluster:cluster is missing declared fields: %v", cluster)
	}
	if cluster["status"] {
		t.Error("v1:cluster:cluster reads as declaring `status`, which memql#4772 removed -- the scanner is reading the prose note that says so")
	}
	// A nested object's members are not top-level payload keys and must not
	// appear: `payload - 'x'` cannot reach one.
	if reg, ok := declared["v1:worker:registration"]; ok && reg["vramBytes"] {
		t.Error("a nested object's member leaked into the top-level field set")
	}
}
