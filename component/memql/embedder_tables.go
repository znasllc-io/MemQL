package memql

import (
	"context"
	"database/sql"
	"fmt"
)

// One vector table per width (epic memql#5137, D6).
//
// ===========================================================================
// WHY NOT ONE TABLE WITH AN UNTYPED COLUMN
// ===========================================================================
// pgvector's `vector` type can be declared without dimensions, and a column
// declared that way accepts vectors of any width. It cannot be INDEXED: hnsw
// and ivfflat both require a fixed dimension, so an untyped column is a table
// that writes fine and searches by sequential scan forever. That failure looks
// exactly like the feature working, right up until the corpus is large enough
// for it not to.
//
// ===========================================================================
// WHY NOT RE-EMBED IN PLACE
// ===========================================================================
// Because a half-written vector column is not a degraded search space, it is a
// corrupt one: half the rows answer in the old model's geometry and half in the
// new one's, and cosine distance between them is a number with no meaning. It
// does not error, it ranks. So a switch fills a NEW table and flips the binding
// only when the count matches -- until then every read follows the old binding
// and the old table, and a switch interrupted half way leaves a cluster that
// still works.

// createVectorTableSQL builds the DDL for one width.
//
// The two partial HNSW indexes mirror the original `node_vectors` migration:
// `profile` and `content` are the two vector fields the engine writes, and
// indexing them separately keeps each index over one field's rows rather than
// over a mixed set where most entries can never match.
func createVectorTableSQL(dims int) []string {
	table := VectorTableFor(dims)
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id           TEXT NOT NULL,
  concept      TEXT NOT NULL,
  vector_field TEXT NOT NULL,
  embedding    vector(%d) NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (id, vector_field)
)`, table, dims),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_profile_hnsw
  ON %s USING hnsw (embedding vector_cosine_ops)
  WHERE vector_field = 'profile'`, table, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_content_hnsw
  ON %s USING hnsw (embedding vector_cosine_ops)
  WHERE vector_field = 'content'`, table, table),
	}
}

// EnsureVectorTable creates the table for a width if it does not exist.
//
// IDEMPOTENT BY CONSTRUCTION (`IF NOT EXISTS` throughout), because activation
// is re-run on every boot for the active binding: the alternative is a cluster
// that works until somebody restarts it after a manual table drop, which is a
// failure nobody would connect to the restart.
//
// DDL AT RUNTIME IS DELIBERATE HERE and is worth defending, because this repo's
// convention is migrations. A migration cannot express this table: its width is
// not known until somebody activates a binding, and the set of widths a cluster
// needs is a function of which models its operator chose. A migration per width
// would mean shipping one for every width in the catalog and creating eight
// empty tables on every cluster.
func EnsureVectorTable(ctx context.Context, db *sql.DB, dims int) error {
	// THE WIDTH IS CHECKED FIRST, before the database, and the order is the
	// fix rather than a style choice. A width of zero would create
	// `node_vectors_0` -- which pgvector refuses to index and which nothing
	// could ever search -- and it is a fact about the ARGUMENT, knowable
	// without a connection. Checking the database first reported "no database"
	// for a caller whose actual bug was a binding with no width, which sends
	// them to look at their DSN.
	if dims <= 0 {
		return fmt.Errorf("ensure vector table: refusing to create a table for width %d; a binding must know its width before it has a vector", dims)
	}
	if db == nil {
		return fmt.Errorf("ensure vector table: no database")
	}
	for _, stmt := range createVectorTableSQL(dims) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure %s: %w", VectorTableFor(dims), err)
		}
	}
	return nil
}

// CountVectors reports how many rows the table for a width holds.
//
// It is what decides whether a re-embed is COMPLETE, and the comparison it
// serves is against the count in the OLD table -- not against a target computed
// somewhere else. Counting both sides the same way is what makes "the counts
// match" mean the thing it sounds like.
func CountVectors(ctx context.Context, db *sql.DB, dims int) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("count vectors: no database")
	}
	var n int64
	// #nosec G201 -- the table name is built by VectorTableFor from an integer,
	// never from caller input, so there is no string to inject through.
	q := fmt.Sprintf(`SELECT count(*) FROM %s`, VectorTableFor(dims))
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", VectorTableFor(dims), err)
	}
	return n, nil
}

// ReembedComplete reports whether the new width's table has caught up with the
// old one's.
//
// EQUAL, NOT "AT LEAST", and the direction matters. A new table holding MORE
// rows than the old one means something else is writing into it -- a concurrent
// re-embed, a second activation -- and flipping the binding on that would make
// the extra rows authoritative without anybody having decided they should be.
//
// A zero-row OLD table is complete trivially, which is right: a cluster that
// has embedded nothing has nothing to carry over, and its first binding should
// activate immediately rather than waiting for a count that will never move.
func ReembedComplete(oldCount, newCount int64) bool {
	if oldCount == 0 {
		return true
	}
	return newCount == oldCount
}
