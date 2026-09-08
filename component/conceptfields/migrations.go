package conceptfields

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Reading the migrations corpus.
//
// ===========================================================================
// THESE READERS ARE SHARED WITH memql#5199's GATE, DELIBERATELY
// ===========================================================================
// TestRetiredConceptFieldsAreMigratedSafely (repo root) asks "is this strip
// safe"; VerifyLedger below asks "does this strip have a record, and does this
// record have a strip". They are opposite directions over the SAME corpus, and
// two readers of `payload - 'x'` would be two definitions of what a strip is --
// so a migration could satisfy one gate's parse and fall outside the other's
// without anything saying so. One reader, two questions.
//
// Comments are stripped FIRST and that is load-bearing: these migrations carry
// long prose explaining what they strip and why, and every such comment quotes
// the very pattern matched here.

var sqlComment = regexp.MustCompile(`(?m)--.*$`)

// payloadStrip matches `payload - 'key'`, including the chained form
// `payload - 'a' - 'b'` (each `- 'x'` matches separately).
var payloadStrip = regexp.MustCompile(`-\s*'([^']+)'`)
var payloadMinus = regexp.MustCompile(`payload\s*-\s*'`)

var conceptEq = regexp.MustCompile(`concept\s*=\s*'([^']+)'`)
var conceptIn = regexp.MustCompile(`concept\s+IN\s*\(([^)]*)\)`)
var quoted = regexp.MustCompile(`'([^']*)'`)

// SQLStatements splits on `;` after removing comments. The statement is the
// right unit: the concept predicate that makes a strip safe has to be in the
// same one, and a WHERE clause two statements away protects nothing.
func SQLStatements(sql string) []string {
	return strings.Split(sqlComment.ReplaceAllString(sql, ""), ";")
}

// StrippedPayloadKeys reads every key a `payload - 'x'` expression removes,
// including the chained `payload - 'a' - 'b'` form.
func StrippedPayloadKeys(stmt string) []string {
	loc := payloadMinus.FindStringIndex(stmt)
	if loc == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range payloadStrip.FindAllStringSubmatch(stmt[loc[0]:], -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// ScopedConcepts reads every concept id the statement pins itself to, handling
// `concept = 'x'` and `concept IN ('x', 'y')`.
//
// EVERY id in an IN-list, not just the first. One statement can be right about
// one concept and wrong about the next, and a reader that stopped at the first
// would clear exactly that migration.
func ScopedConcepts(stmt string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, m := range conceptEq.FindAllStringSubmatch(stmt, -1) {
		add(m[1])
	}
	for _, m := range conceptIn.FindAllStringSubmatch(stmt, -1) {
		for _, q := range quoted.FindAllStringSubmatch(m[1], -1) {
			add(q[1])
		}
	}
	sort.Strings(out)
	return out
}

// Strip is one (concept, key) pair an up-migration removes.
type Strip struct {
	Migration string
	Concept   string
	Key       string
}

// StripsIn reads every (concept, key) an up-migration under dir strips.
//
// UP MIGRATIONS ONLY. A down migration legitimately strips a key the CURRENT
// tree declares -- 20260603000000_attachment_rename_gcsurl_to_bloburl.down.sql
// strips `blobUrl`, which is the live name -- so reading them would report
// correct code as a violation.
func StripsIn(dir string) ([]Strip, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no up-migrations under %s: a check over zero files passes for the wrong reason", dir)
	}
	var out []Strip
	for _, path := range paths {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		base := filepath.Base(path)
		for _, stmt := range SQLStatements(string(raw)) {
			keys := StrippedPayloadKeys(stmt)
			if len(keys) == 0 {
				continue
			}
			for _, concept := range ScopedConcepts(stmt) {
				for _, key := range keys {
					out = append(out, Strip{Migration: base, Concept: concept, Key: key})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Concept != out[j].Concept {
			return out[i].Concept < out[j].Concept
		}
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Migration < out[j].Migration
	})
	return out, nil
}

// VerifyLedger checks the snapshot's ledger against the migrations corpus, in
// BOTH directions.
//
// Forward: an entry naming a migration must name one that exists, and -- for a
// field retirement -- one that actually strips that key from that concept. An
// entry that named a migration doing something else would read as repaired and
// repair nothing.
//
// Inverse: every strip in the corpus must have an entry. This is what gives the
// ledger's append-only discipline something behind it: deleting an entry whose
// migration is still committed fails the build, so the only way to erase a
// retirement from the record is to delete its migration too, which is a diff
// nobody merges by accident.
func VerifyLedger(snap Snapshot, migrationsDir string) []string {
	var problems []string

	strips, err := StripsIn(migrationsDir)
	if err != nil {
		return []string{fmt.Sprintf("reading migrations: %v", err)}
	}

	recorded := map[string]Retirement{}
	for _, r := range snap.Retired {
		key := r.Key()
		if prior, dup := recorded[key]; dup {
			problems = append(problems, fmt.Sprintf(
				"the ledger records %s twice (migration %q and %q). One retirement, one line: a reader "+
					"cannot tell which of two entries is the live record.",
				describe(r), prior.Migration, r.Migration))
			continue
		}
		recorded[key] = r
		problems = append(problems, checkEntryShape(r)...)
		problems = append(problems, checkEntryMigration(r, migrationsDir, strips)...)
	}

	// Inverse direction.
	for _, s := range strips {
		key := Narrowing{Concept: s.Concept, Field: s.Key, Kind: KindField}.Key()
		if _, ok := recorded[key]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s strips %q from %s and the ledger has no entry for it. A migration is the repair; the "+
				"ledger entry is the RECORD, and without one the retirement is invisible to anybody "+
				"reading the snapshot. Add:\n"+
				"      {\"concept\": %q, \"field\": %q, \"kind\": %q, \"migration\": %q, \"note\": \"<the issue that retired it>\"}",
			s.Migration, s.Key, s.Concept, s.Concept, s.Key, KindField, s.Migration))
	}

	sort.Strings(problems)
	return problems
}

func checkEntryShape(r Retirement) []string {
	var problems []string
	switch r.Kind {
	case KindField, KindEnumValue, KindRequired, KindConcept:
	default:
		problems = append(problems, fmt.Sprintf(
			"the ledger entry for %s declares kind %q, which is not one of %s/%s/%s/%s",
			r.Concept, r.Kind, KindField, KindEnumValue, KindRequired, KindConcept))
	}
	if r.Kind != KindConcept && strings.TrimSpace(r.Field) == "" {
		problems = append(problems, fmt.Sprintf(
			"the ledger entry for %s (kind %q) names no field", r.Concept, r.Kind))
	}
	if r.Kind == KindEnumValue && strings.TrimSpace(r.Value) == "" {
		problems = append(problems, fmt.Sprintf(
			"the ledger entry for %s.%s retires an enum value and names none", r.Concept, r.Field))
	}
	hasMigration := strings.TrimSpace(r.Migration) != ""
	hasWaiver := strings.TrimSpace(r.Waiver) != ""
	switch {
	case hasMigration && hasWaiver:
		problems = append(problems, fmt.Sprintf(
			"the ledger entry for %s names both a migration and a waiver. They are the two ANSWERS to "+
				"the same question -- the rows were repaired, or they never needed repairing -- and an "+
				"entry claiming both says neither.", describe(r)))
	case !hasMigration && !hasWaiver:
		problems = append(problems, fmt.Sprintf(
			"the ledger entry for %s names neither a migration nor a waiver. One of the two is what "+
				"makes it a record rather than a note.", describe(r)))
	}
	return problems
}

func checkEntryMigration(r Retirement, migrationsDir string, strips []Strip) []string {
	name := strings.TrimSpace(r.Migration)
	if name == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(migrationsDir, name)); err != nil {
		return []string{fmt.Sprintf(
			"the ledger entry for %s names migration %q, which is not in %s. A named file that is not "+
				"there is the same as no migration, and it reads as repaired.",
			describe(r), name, migrationsDir)}
	}
	if r.Kind != KindField {
		// A `payload - 'x'` strip is the only repair shape this reader can
		// verify. An enum-value retirement rewrites values and a new required
		// field is a backfill; both are real migrations in shapes no regex
		// should pretend to check, so the file's existence plus the note is
		// what is asserted, and this comment is where that limit is written
		// down rather than left to be discovered.
		return nil
	}
	for _, s := range strips {
		if s.Migration == name && s.Concept == r.Concept && s.Key == r.Field {
			return nil
		}
	}
	return []string{fmt.Sprintf(
		"the ledger entry for %s names migration %q, and that migration strips no %q from %s. "+
			"Either the entry names the wrong file or the migration is scoped to a different concept -- "+
			"and a strip scoped elsewhere leaves these rows exactly as unwritable as before.",
		describe(r), name, r.Field, r.Concept)}
}

func describe(r Retirement) string {
	if r.Kind == KindConcept {
		return r.Concept
	}
	if r.Kind == KindEnumValue {
		return fmt.Sprintf("%s.%s = %q", r.Concept, r.Field, r.Value)
	}
	return r.Concept + "." + r.Field
}
