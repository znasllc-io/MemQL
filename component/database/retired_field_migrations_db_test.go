package database_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/znasllc-io/memql/component/database/dbtest"
)

// AN EXTERNAL TEST PACKAGE, and not by preference: dbtest imports
// memory-nodes, which imports this package, so an in-package test importing
// dbtest is an import cycle. That costs access to `timescaleMigrationsFS`, so
// the SQL is read from DISK instead -- still the committed file, which is the
// property that matters.

const migrationsDir = "memory-nodes/migrations"

func readMigrationSQL(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(migrationsDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// THE FOUR RETIRED-FIELD MIGRATIONS, RUN AGAINST A REAL DATABASE (memql#5210).
//
// ===========================================================================
// WHY A LIVE TEST AND NOT A SQL-SHAPE ONE
// ===========================================================================
// Its sibling in this package (document_version_migration_test.go) asserts a
// migration's TEXT, which is the right instrument for a migration whose effect
// is a policy the database applies later. These four DELETE DATA, and the whole
// hazard they exist for is a strip that reaches further than its author meant:
// `gender` is retired on v1:agents:agent and LIVE on v1:identity:user, so a
// name-scoped strip would take a real field off every user row in the cluster.
//
// A regex cannot show that it does not. Rows can. Each case below seeds the
// retired key on the concept the migration names AND on a concept it must not
// touch, runs the statement, and asserts both halves -- the removal and the
// survival. The survival half is the one that matters: without it every case
// here would still pass against a migration that stripped by key name alone.
//
// ===========================================================================
// EVERY VERSION, NOT THE NEWEST
// ===========================================================================
// The table is a time series and a row's history is its versions. A read-merge
// reads the newest, but an audit walk returns all of them, and half a history
// validating is worse than none -- so each case seeds TWO versions of one id
// and asserts both are repaired.
//
// Postgres-gated: skips when no database is reachable, and MEMQL_REQUIRE_DB=1
// turns that skip into a failure. The db-tests lane sets it.

const retiredFieldTestTable = `MemoryNodes`

func retiredFieldDB(t *testing.T) *bun.DB {
	t.Helper()
	ctx := context.Background()
	reachable, err := dbtest.EnsureSchema(ctx)
	if err != nil && reachable {
		t.Fatalf("ensuring schema: %v", err)
	}
	if !reachable {
		dbtest.Unreachable(t, "the retired-field migrations", dbtest.DSN(), err)
		return nil
	}
	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN()))), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedRetiredRow writes ONE append-only version directly, bypassing the
// mutation validators on purpose: the whole point is a payload the concept's
// schema would refuse, which is exactly what a cluster that crossed a
// retirement is holding and what no validated write can produce.
func seedRetiredRow(t *testing.T, ctx context.Context, db *bun.DB, concept, id string, at time.Time, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling %s: %v", id, err)
	}
	_, err = db.NewRaw(
		`INSERT INTO "`+retiredFieldTestTable+`" (id, "createdAt", "createdBy", concept, type, schema, payload, metadata, provenance)
		 VALUES (?, ?, ?, ?, 'node', '{}'::jsonb, ?::jsonb, '{}'::jsonb, '{"kind":"direct","name":"retired-field-migration-test"}'::jsonb)
		 ON CONFLICT (id, "createdAt") DO NOTHING`,
		id, at.UTC(), "retired-field-test", concept, string(raw)).Exec(ctx)
	if err != nil {
		t.Fatalf("seeding %s@%s: %v", id, at, err)
	}
}

// payloadKeys reads back every version of one id, newest first.
func payloadKeys(t *testing.T, ctx context.Context, db *bun.DB, id string) []map[string]any {
	t.Helper()
	// `?`, not `$1`: bun rewrites its own placeholder, and a positional one
	// reaches the driver unbound.
	rows, err := db.QueryContext(ctx,
		`SELECT payload FROM "`+retiredFieldTestTable+`" WHERE id = ? ORDER BY "createdAt" DESC`, id)
	if err != nil {
		t.Fatalf("reading %s: %v", id, err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scanning %s: %v", id, err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decoding %s: %v", id, err)
		}
		out = append(out, m)
	}
	return out
}

// runMigrationUp executes the named up-migration's statements in order.
//
// It runs the COMMITTED FILE rather than a statement retyped here, which is the
// property that makes this a test of the migration and not of a copy of it.
func runMigrationUp(t *testing.T, ctx context.Context, db *bun.DB, name string) {
	t.Helper()
	body := readMigrationSQL(t, name)
	for _, stmt := range strings.Split(stripSQLComments(body), ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v\n\nstatement:\n%s", name, err, stmt)
		}
	}
}

func stripSQLComments(sqlText string) string {
	var b strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// retirementCase is one migration, the concept it repairs, and the concept it
// must leave alone.
type retirementCase struct {
	name string
	// migration is the committed up-migration this case runs.
	migration string
	// concept + keys are what must be stripped.
	concept string
	keys    []string
	// bystanderConcept + bystanderKey are a concept the SAME key name is live
	// on, which the migration must not touch. Empty when no such concept
	// exists -- the strip is still concept-scoped, and a case with no
	// bystander says so rather than inventing one.
	bystanderConcept string
	bystanderKey     string
	// survivor is a key on the repaired concept that must remain: a migration
	// that emptied the payload would pass every removal assertion.
	survivor string
}

func TestTheRetiredFieldMigrationsStripExactlyWhatTheyName(t *testing.T) {
	db := retiredFieldDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()

	cases := []retirementCase{{
		name:      "the conversational agent fields",
		migration: "20260908020000_agent_conversational_fields_retired.up.sql",
		concept:   "v1:agents:agent",
		keys:      []string{"gender", "audioControl", "videoControl", "triggerBehavior"},
		// THE MEASURED TRAP. `gender` is a live, declared field on
		// v1:identity:user, so a name-scoped strip would delete personal data
		// from every user row in the cluster.
		bystanderConcept: "v1:identity:user",
		bystanderKey:     "gender",
		survivor:         "name",
	}, {
		name:             "the planner retirement's pointers",
		migration:        "20260908030000_planner_retirement_row_keys.up.sql",
		concept:          "v1:worker:invocation",
		keys:             []string{"planId", "taskId"},
		bystanderConcept: "v1:data:log",
		bystanderKey:     "planId",
		survivor:         "ownerUserId",
	}, {
		name:             "the environment collapse",
		migration:        "20260908040000_deployment_environment_retired.up.sql",
		concept:          "v1:cluster:node",
		keys:             []string{"environment"},
		bystanderConcept: "v1:agents:agent",
		bystanderKey:     "environment",
		survivor:         "nodeType",
	}, {
		name:             "the retired policy slug",
		migration:        "20260908050000_agent_role_recommended_policy_slug_retired.up.sql",
		concept:          "v1:agents:agentRole",
		keys:             []string{"recommendedPolicySlug"},
		bystanderConcept: "v1:agents:agent",
		bystanderKey:     "recommendedPolicySlug",
		survivor:         "name",
	}}

	stamp := time.Now().UTC()
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := fmt.Sprintf("%d-%d", stamp.UnixNano(), i)
			target := c.concept + ":retired-" + run
			bystander := c.bystanderConcept + ":bystander-" + run

			// TWO VERSIONS of the target, so "every version, not the newest"
			// is asserted rather than assumed.
			for v := 0; v < 2; v++ {
				payload := map[string]any{c.survivor: "kept"}
				for _, k := range c.keys {
					payload[k] = "retired-value"
				}
				seedRetiredRow(t, ctx, db, c.concept, target,
					stamp.Add(time.Duration(i*10+v)*time.Second), payload)
			}
			seedRetiredRow(t, ctx, db, c.bystanderConcept, bystander,
				stamp.Add(time.Duration(i*10+5)*time.Second),
				map[string]any{c.bystanderKey: "live-value", "keep": "me"})

			runMigrationUp(t, ctx, db, c.migration)

			versions := payloadKeys(t, ctx, db, target)
			if len(versions) != 2 {
				t.Fatalf("expected 2 seeded versions of %s, read %d", target, len(versions))
			}
			for v, payload := range versions {
				for _, k := range c.keys {
					if _, still := payload[k]; still {
						t.Errorf("version %d of %s still carries %q -- the migration did not reach it. "+
							"Every version must be repaired: a read-merge reads the newest, but an audit "+
							"walk returns all of them", v, target, k)
					}
				}
				if payload[c.survivor] != "kept" {
					t.Errorf("version %d of %s lost %q, which the migration does not name. "+
						"A strip that empties the payload passes every removal assertion above",
						v, target, c.survivor)
				}
			}

			// THE HALF THAT MATTERS. Without this, every case here passes
			// against a migration that stripped by key name alone.
			by := payloadKeys(t, ctx, db, bystander)
			if len(by) != 1 {
				t.Fatalf("expected 1 bystander version, read %d", len(by))
			}
			if by[0][c.bystanderKey] != "live-value" {
				t.Errorf("%s lost %q from %s. The strip is not scoped to its concept, and on a real "+
					"cluster that is live data deleted from a concept this migration never named",
					bystander, c.bystanderKey, c.bystanderConcept)
			}
		})
	}
}

// The idempotency claim every one of the four makes in its own comment: the
// `payload ?|` guard matches nothing on a cluster whose rows were all written
// after the retirement, so a second run is free and a re-run is safe.
func TestTheRetiredFieldMigrationsAreIdempotent(t *testing.T) {
	db := retiredFieldDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	id := fmt.Sprintf("v1:agents:agent:clean-%d", time.Now().UnixNano())
	seedRetiredRow(t, ctx, db, "v1:agents:agent", id, time.Now().UTC(),
		map[string]any{"name": "already clean"})

	for run := 0; run < 2; run++ {
		runMigrationUp(t, ctx, db, "20260908020000_agent_conversational_fields_retired.up.sql")
	}
	got := payloadKeys(t, ctx, db, id)
	if len(got) != 1 || got[0]["name"] != "already clean" || len(got[0]) != 1 {
		t.Fatalf("a row with none of the retired keys was altered by the migration: %v", got)
	}
}
