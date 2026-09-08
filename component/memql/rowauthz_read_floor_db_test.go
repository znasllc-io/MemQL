package memql

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	langparser "github.com/znasllc-io/memql/component/language/parser"
)

// THE FOUR BENCHMARK READS, THROUGH THE REAL ENGINE (memql#5216).
//
// This is the test `tierDecidesTheRead` promises. Those four constructs are
// adjudicated there -- their filters deliberately carry no caller-scope
// conjunct, because `@requiresRank("admin")` bounds the callers to exactly the
// set `@rowAuthz(clusterOwner, rankFloor="admin")` admits every row for -- and
// the map's own note says a new entry needs "the test that would fail if the
// reasoning were wrong". Both halves, or the adjudication is a claim rather
// than a finding:
//
//	an ADMIN gets rows          the defect this issue is filed for. Before the
//	                            floor the tier answered zero for them, and the
//	                            Benchmarks section rendered that as every figure
//	                            being UNMEASURED -- a refusal wearing the
//	                            costume of a measurement.
//	a WRITER gets none          the floor is a floor. Without this half, a rule
//	                            that admitted everyone would pass the first.
//
// Postgres-gated: skips when no database is reachable, and MEMQL_REQUIRE_DB=1
// turns that skip into a failure.

// seedBenchRun writes one v1:bench:run row.
//
// A RAW INSERT UNDER INTERNAL ORIGIN, which is scaffolding rather than a path
// under test: the shipped writers are `createBenchRun` / `createBenchSample`,
// both @serverOnly, so a test cannot call them and should not want to. What
// this stands in for is cmd/memql-bench, and the row it produces is the same
// shape.
func seedBenchRun(t *testing.T, eng *MemQLEngine, id string) {
	t.Helper()
	seedBenchRow(t, eng, "v1:bench:run", id, map[string]any{
		"tier":              "ci",
		"commit":            "abc1234",
		"corpusFingerprint": "fp-" + id,
		"scenarioCount":     3,
		"verdict":           "pass",
		"startedAt":         time.Now().UTC().Format(time.RFC3339),
	})
}

func seedBenchSample(t *testing.T, eng *MemQLEngine, id, runId, metric string) {
	t.Helper()
	seedBenchRow(t, eng, "v1:bench:sample", id, map[string]any{
		"benchRunId": runId,
		"family":     "speed",
		"scenarioId": "s-" + id,
		"arm":        "platform",
		"metric":     metric,
		"unit":       "ms",
		"tier":       "ci",
		"commit":     "abc1234",
		"measuredOn": time.Now().UTC().Format("2006-01-02"),
	})
}

func seedBenchRow(t *testing.T, eng *MemQLEngine, concept, id string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	q := fmt.Sprintf(`insert(%s, id=%s, payload=%s)`,
		langparser.QuoteString(concept), langparser.QuoteString(id), string(raw))
	seedCtx := auth.ContextWithInternalOrigin(
		auth.ContextWithAccess(context.Background(), &auth.AccessContext{
			UserId: "bench-db-seeder",
			Role:   auth.RoleOwner,
		}))
	seedCtx = auth.ContextWithToken(seedCtx, &auth.TokenInfo{Subject: "bench-db-seeder"})
	if _, err := eng.Execute(seedCtx, q); err != nil {
		t.Fatalf("seed %s %s: %v", concept, id, err)
	}
}

func TestBenchReadsAnswerForAdminAndRefuseBelowTheFloor(t *testing.T) {
	eng, _, _ := sharedReadMergeEngine(t)
	suffix := uniqueSuffix("benchfloor")

	runID := "run-" + suffix
	metric := "speed.wallClockMs." + suffix
	admin := "admin-" + suffix
	writer := "writer-" + suffix

	seedPrincipal(t, eng, admin, auth.RoleAdmin)
	seedPrincipal(t, eng, writer, auth.RoleWriter)
	seedBenchRun(t, eng, runID)
	seedBenchSample(t, eng, "sample-"+suffix, runID, metric)

	// A refusal and an empty result are DIFFERENT ANSWERS, and the distinction
	// is the whole subject here -- so -1 marks the refusal rather than folding
	// it into zero.
	rowsOf := func(t *testing.T, ctx context.Context, q string) int {
		t.Helper()
		res, err := eng.Execute(ctx, q)
		if err != nil {
			return -1
		}
		return len(res.Bundle.GetNodes())
	}

	for _, tc := range []struct{ name, query string }{
		{"benchRuns", `query benchRuns()`},
		{"benchRunById", fmt.Sprintf(`query benchRunById(benchRunId: %s)`, langparser.QuoteString(runID))},
		{"benchSamplesForRun", fmt.Sprintf(`query benchSamplesForRun(benchRunId: %s)`, langparser.QuoteString(runID))},
		{"benchSamplesForMetric", fmt.Sprintf(`query benchSamplesForMetric(metric: %s)`, langparser.QuoteString(metric))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// THE DEFECT THIS ISSUE IS FILED FOR. An admin is admitted to the
			// Benchmarks section, so a read that answers zero for them shows
			// every figure as unmeasured -- which is the suite's word for
			// "nobody has measured this", not for "you may not read it".
			if got := rowsOf(t, rankActorCtx(admin, auth.RoleAdmin), tc.query); got <= 0 {
				t.Fatalf("%s answered %d for an ADMIN. Settings -> Benchmarks admits "+
					"`{ min: \"admin\" }`, so this renders the whole screen as unmeasured -- "+
					"a permission answer wearing the costume of a measurement (memql#5216).",
					tc.name, got)
			}
			// ...and the floor is a floor. Without this half, a rule that
			// admitted everyone would pass the assertion above.
			if got := rowsOf(t, rankActorCtx(writer, auth.RoleWriter), tc.query); got > 0 {
				t.Fatalf("%s answered %d rows to a WRITER -- these reads are admin-floored, "+
					"on both the surface and the rows", tc.name, got)
			}
		})
	}
}

// The WRITE half, which the floor deliberately does not touch.
//
// `rankFloor` relaxes the READ; the tier's write rule is still plain
// clusterOwner. Without this, "relaxes the read and leaves the write where it
// was" is a sentence in a comment rather than a property -- and these are the
// rows a forged benchmark number would be forged into.
//
// Asserted at the GUARD rather than through a mutation, because both shipped
// writers are @serverOnly and a test cannot reach them. That is the outer
// protection and it is gated elsewhere (server_only_parsed_test.go pins both by
// name); this is the inner one, and the question it answers is what the guard
// would say if that outer layer were ever removed.
func TestTheReadFloorDoesNotWidenBenchWrites(t *testing.T) {
	eng, _, _ := sharedReadMergeEngine(t)
	suffix := uniqueSuffix("benchwrite")
	admin := "admin-" + suffix
	seedPrincipal(t, eng, admin, auth.RoleAdmin)

	payload := []byte(`{"tier":"ci","commit":"abc1234"}`)
	ctx := contextWithRankScopeMemo(rankActorCtx(admin, auth.RoleAdmin), eng)

	if got := rowAuthzAdmitsWrite(ctx, "v1:bench:run", "v1:bench:run:"+suffix, payload); got == rowAuthzAdmit {
		t.Fatal("an ADMIN was admitted to WRITE v1:bench:run. The read floor must relax the " +
			"read only -- these rows are what README's published claims rest on, and a wider " +
			"write is a primitive for forging them.")
	}

	// The control: the READ is admitted for the same caller on the same row,
	// so the test above is measuring the read/write split rather than a guard
	// that denies everything.
	if got := rowAuthzAdmits(ctx, "v1:bench:run", "v1:bench:run:"+suffix, payload); got != rowAuthzAdmit {
		t.Fatalf("the same admin was refused the READ (%v). Without this the write assertion "+
			"above would pass against a floor that admits nobody at all.", got)
	}
}
