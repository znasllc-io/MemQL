package conceptfields

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The ledger's own rules, each planted.
//
// These run against a throwaway migrations directory rather than the corpus,
// for the reason memql#5199's gate states about its own: the committed corpus
// is correct, so a check that passes over it cannot tell a rule that runs from
// a rule that does not.

func migrationsFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

const stripClusterStatus = `UPDATE "MemoryNodes" SET payload = payload - 'status'
  WHERE concept = 'v1:cluster:cluster' AND payload ? 'status';`

func ledgerProblems(t *testing.T, retired []Retirement, files map[string]string) []string {
	t.Helper()
	return VerifyLedger(Snapshot{Retired: retired}, migrationsFixture(t, files))
}

func wantOneProblemContaining(t *testing.T, got []string, want string) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("want exactly 1 problem, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], want) {
		t.Errorf("the problem must say %q:\n%s", want, got[0])
	}
}

func TestACorrectlyRecordedRetirementIsSilent(t *testing.T) {
	// The control. Without it, a rule that failed on every entry passes every
	// test below.
	got := ledgerProblems(t,
		[]Retirement{{
			Concept: "v1:cluster:cluster", Field: "status", Kind: KindField,
			Migration: "20260908010000_cluster_status_retired.up.sql",
		}},
		map[string]string{"20260908010000_cluster_status_retired.up.sql": stripClusterStatus})
	if len(got) != 0 {
		t.Fatalf("a correct entry was reported: %v", got)
	}
}

func TestAStripWithNoLedgerEntryIsReported(t *testing.T) {
	// The INVERSE direction, and it is what gives the ledger's append-only
	// discipline something behind it: deleting an entry whose migration is
	// still committed fails the build.
	got := ledgerProblems(t, nil,
		map[string]string{"20260908010000_cluster_status_retired.up.sql": stripClusterStatus})
	wantOneProblemContaining(t, got, "the ledger has no entry for it")
}

func TestAnEntryNamingAMissingMigrationIsReported(t *testing.T) {
	got := ledgerProblems(t,
		[]Retirement{{
			Concept: "v1:cluster:cluster", Field: "status", Kind: KindField,
			Migration: "20260101000000_not_here.up.sql",
		}},
		// A directory with SOME up-migration, so the "no up-migrations at all"
		// refusal is not what answers here.
		map[string]string{"20260101000000_unrelated.up.sql": "SELECT 1;"})
	wantOneProblemContaining(t, got, "which is not in")
}

func TestAnEntryWhoseMigrationStripsADifferentConceptIsReported(t *testing.T) {
	// The failure this exists for: an entry that reads as repaired while the
	// migration is scoped somewhere else, so the rows it names stay exactly as
	// unwritable as before.
	got := ledgerProblems(t,
		[]Retirement{{
			Concept: "v1:cluster:releaseCut", Field: "status", Kind: KindField,
			Migration: "20260908010000_cluster_status_retired.up.sql",
		}},
		map[string]string{"20260908010000_cluster_status_retired.up.sql": stripClusterStatus})
	// Two: the entry names a migration that does not strip THIS concept, and
	// the migration's own strip is now unrecorded.
	if len(got) != 2 {
		t.Fatalf("want both directions reported, got %d: %v", len(got), got)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "strips no \"status\" from v1:cluster:releaseCut") {
		t.Errorf("the mis-scoped entry must be named:\n%s", joined)
	}
}

func TestAnEntryWithBothAMigrationAndAWaiverIsReported(t *testing.T) {
	got := ledgerProblems(t,
		[]Retirement{{
			Concept: "v1:cluster:cluster", Field: "status", Kind: KindField,
			Migration: "20260908010000_cluster_status_retired.up.sql",
			Waiver:    "nothing ever wrote it",
		}},
		map[string]string{"20260908010000_cluster_status_retired.up.sql": stripClusterStatus})
	wantOneProblemContaining(t, got, "names both a migration and a waiver")
}

func TestAnEntryWithNeitherIsReported(t *testing.T) {
	got := ledgerProblems(t,
		[]Retirement{{Concept: "v1:cluster:cluster", Field: "status", Kind: KindField}},
		map[string]string{"x.up.sql": "SELECT 1;"})
	wantOneProblemContaining(t, got, "neither a migration nor a waiver")
}

func TestAnUnknownKindIsReported(t *testing.T) {
	got := ledgerProblems(t,
		[]Retirement{{Concept: "v1:cluster:cluster", Field: "status", Kind: "renamed", Waiver: "x"}},
		map[string]string{"x.up.sql": "SELECT 1;"})
	wantOneProblemContaining(t, got, "which is not one of")
}

func TestAnEnumEntryWithNoValueIsReported(t *testing.T) {
	got := ledgerProblems(t,
		[]Retirement{{Concept: "v1:cluster:cluster", Field: "provider", Kind: KindEnumValue, Waiver: "x"}},
		map[string]string{"x.up.sql": "SELECT 1;"})
	wantOneProblemContaining(t, got, "retires an enum value and names none")
}

func TestADuplicateEntryIsReported(t *testing.T) {
	// One retirement, one line. Two entries for the same key make it
	// impossible to tell which is the live record, and the second would also
	// silence a later disagreement between them.
	got := ledgerProblems(t,
		[]Retirement{
			{Concept: "v1:x:y", Field: "f", Kind: KindField, Waiver: "a"},
			{Concept: "v1:x:y", Field: "f", Kind: KindField, Waiver: "b"},
		},
		map[string]string{"x.up.sql": "SELECT 1;"})
	wantOneProblemContaining(t, got, "twice")
}

func TestANonFieldEntryIsNotHeldToAStrip(t *testing.T) {
	// An enum-value retirement rewrites values and a new required field is a
	// backfill; neither is a `payload - 'x'` strip, so the file's existence
	// plus the note is all that is asserted. Pretending to verify those shapes
	// with a regex would refuse correct migrations.
	got := ledgerProblems(t,
		[]Retirement{{
			Concept: "v1:cluster:cluster", Field: "provider", Kind: KindEnumValue, Value: "gcp",
			Migration: "20260101000000_provider_gcp_rewrite.up.sql",
		}},
		map[string]string{"20260101000000_provider_gcp_rewrite.up.sql": `UPDATE "MemoryNodes"
  SET payload = jsonb_set(payload, '{provider}', '"azure"')
  WHERE concept = 'v1:cluster:cluster' AND payload->>'provider' = 'gcp';`})
	if len(got) != 0 {
		t.Fatalf("a value-rewrite migration was reported: %v", got)
	}
}

func TestACommentedOutStripIsNotAStrip(t *testing.T) {
	// These migrations carry long prose quoting the very pattern the reader
	// matches on. Comments are stripped first, and this is what says so.
	got := ledgerProblems(t, nil, map[string]string{
		"x.up.sql": `-- UPDATE "MemoryNodes" SET payload = payload - 'status' WHERE concept = 'v1:cluster:cluster'
SELECT 1;`,
	})
	if len(got) != 0 {
		t.Fatalf("a commented-out strip was read as real: %v", got)
	}
}

func TestDownMigrationsAreNotRead(t *testing.T) {
	// A down migration legitimately strips a key the CURRENT tree declares --
	// the attachment rename's down strips `blobUrl`, the live name -- so
	// reading them would report correct code.
	got := ledgerProblems(t, nil, map[string]string{
		"x.up.sql":   "SELECT 1;",
		"x.down.sql": `UPDATE "MemoryNodes" SET payload = payload - 'blobUrl' WHERE concept = 'v1:common:attachment';`,
	})
	if len(got) != 0 {
		t.Fatalf("a down migration was read: %v", got)
	}
}

func TestAnEmptyMigrationsDirectoryIsAnError(t *testing.T) {
	// A check over zero files passes for the wrong reason.
	got := VerifyLedger(Snapshot{}, t.TempDir())
	wantOneProblemContaining(t, got, "no up-migrations")
}

func TestEveryConceptInAnInListIsRead(t *testing.T) {
	// One statement scoped to several concepts. A reader that took only the
	// first would leave the rest of the list unrecorded and silent.
	got := ledgerProblems(t, nil, map[string]string{
		"x.up.sql": `UPDATE "MemoryNodes" SET payload = payload - 'planId'
  WHERE concept IN ('v1:worker:invocation', 'v1:worker:appSession');`,
	})
	if len(got) != 2 {
		t.Fatalf("want both concepts reported, got %d: %v", len(got), got)
	}
}

func TestTheChainedStripFormIsRead(t *testing.T) {
	// `payload - 'a' - 'b'` is one statement retiring two fields, and a reader
	// that took only the first would leave the second unrecorded.
	got := ledgerProblems(t, nil, map[string]string{
		"x.up.sql": `UPDATE "MemoryNodes" SET payload = payload - 'planId' - 'taskId'
  WHERE concept = 'v1:worker:invocation';`,
	})
	if len(got) != 2 {
		t.Fatalf("want both keys reported, got %d: %v", len(got), got)
	}
}
