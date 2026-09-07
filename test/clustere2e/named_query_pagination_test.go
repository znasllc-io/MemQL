//go:build clustere2e

// named_query_pagination_test.go is the CROSS-NODE proof for issue 5.2
// (memql#1966) and its 5.3 backfill: the authored hot-offender queries declare
// `sort` + `paginate` and must page through the KEYSET CURSOR primitive (5.12 /
// memql#1985) over the live NAMED-QUERY surface -- not the inline
// `sort(paginate(...))` raw string the 5.12 codec proof (keyset_cursor_test.go)
// exercises.
//
// WHY A SEPARATE TEST FROM keyset_cursor_test.go
// ----------------------------------------------
// keyset_cursor_test.go proves the engine primitive against a handwritten
// query string. THIS test proves the actual authored query (`notes`) -- the
// string the generated SDK *Build helper emits -- carries the sort+paginate
// directives end-to-end and threads the opaque cursor through
// `ExecuteQueryMsg.cursor` / `ResultMeta.cursor` on the generic executeNamed
// path. If a future edit drops the paginate directive, this test regresses;
// the 5.12 codec test would not.
//
// It is ENGINE-owned. THE PER-SCOPE HALF IS GONE and the loss is stated at
// its subtest below rather than here: `spaceUtterances` left with cognition
// (memql#4988), its replacement `plansForSpace` left with v1:planner:plan
// (memql#5053), and no surviving named query pairs a `paginate` directive
// with an argument that narrows to one test run. So this is an owner-scoped
// pagination proof only. (The 5.2
// offender this file drove before EITHER of those, queryActiveSpaces, was
// product-pack DSL the parity cluster never loaded; memql#4212.) The
// owner-scoped `notes` query (5.3 backfill, `paginate 50`) is unchanged.
//
// HOW IT EXERCISES THE HOP
// ------------------------
// nginx round-robins each new gRPC connection across the bff replicas, so two
// separate connections (connA, connB) typically land on different replicas.
// We seed an ordered set in a fresh scope, take PAGE 1 (+ its nextCursor) on
// connA via the named query, then replay that cursor on connB and assert
// PAGE 2 continues with no overlap (no dup) and no gap -- regardless of which
// replica minted vs. resolved the cursor. The cursor carries only the keyset
// position (createdAt, id) + a sort-signature guard, no server session state,
// so it is replica-agnostic by construction.
//
// RUN
//
//	MEMQL_E2E_TOKEN=<user JWT> go test -tags clustere2e -count=1 \
//	  -timeout=300s ./test/clustere2e/... -run TestNamedQueryPaginationCrossNode
//
// or `make cluster-e2e`.
package clustere2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/znasllc-io/memql/core/id"
	memqlclient "github.com/znasllc-io/memql/sdk/go/client"
)

// assertNoDupNoGap checks the walked ids equal the expected newest-first set
// exactly -- which simultaneously rules out overlap (a dup would make the walk
// longer than the set) and gaps (a skip would break the position match).
func assertNoDupNoGap(t *testing.T, got, wantNewestFirst []string) {
	t.Helper()
	seen := map[string]int{}
	for _, idv := range got {
		seen[idv]++
		if seen[idv] > 1 {
			t.Fatalf("cross-node walk produced an OVERLAP: %s appears %d times", idv, seen[idv])
		}
	}
	if len(got) != len(wantNewestFirst) {
		t.Fatalf("walk returned %d rows, want %d (gap or truncation across the replica hop)", len(got), len(wantNewestFirst))
	}
	for i, want := range wantNewestFirst {
		// #2441: query results now carry BARE ids; compare on the bare form.
		if bareID(got[i]) != bareID(want) {
			t.Fatalf("walk position %d = %s, want %s (gap/order break across the replica hop)", i, got[i], want)
		}
	}
}

// TestNamedQueryPaginationCrossNode proves the 5.2 hot-offender NAMED queries
// page via the keyset cursor across replicas: a bounded first page + cursor,
// then a clean cross-node continuation with no dup / no gap.
func TestNamedQueryPaginationCrossNode(t *testing.T) {
	// The token is still resolved so an auth failure surfaces HERE rather than
	// as an empty page later. Its user id is no longer read: `notes` scopes to
	// `actor.userId` server-side, and the subject that took an explicit owner
	// argument left with v1:planner:plan.
	tok := token(t)
	_ = userIDFromToken(t, tok)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	conns := openConnections(ctx, t, tok, 2)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	connA, connB := conns[0], conns[1]

	// namedPageSize MUST match the `paginate <N>` literal authored on the query
	// in dsl/ (notes/queries.memql notes). The cross-node proof seeds strictly
	// more than this so the NAMED query itself mints a real nextCursor on its
	// full first page (rather than exhausting the set in one page).
	const namedPageSize = 50

	// THE SECOND SUBJECT IS GONE, AND IT IS NOT COMING BACK AS A RENAME
	// (memql#5053). This test used to prove the property on TWO named queries
	// -- `plansForSpace` and `notes` -- and `plansForSpace` went with
	// `v1:planner:plan`. Its replacement had to satisfy three things at once:
	// a `paginate` directive, an argument that narrows to ONE TEST RUN, and a
	// row this suite can write cheaply. `missingCapabilitiesByStatus` paginates
	// 50 and is the probe concept's own read, but it narrows by STATUS -- every
	// probe row ever written by every run shares `status: "open"`, so the
	// "exactly one full page, then the remainder" assertion below would be
	// decided by how long the cluster has been up. That is not a subject, it is
	// a flake.
	//
	// So this runs on one named query rather than a worse two. The same thing
	// happened once before -- the comment above records the first subject
	// leaving with cognition (memql#4988) -- and the shape of the loss is worth
	// noting: a suite that probes a PROPERTY through whatever concepts happen
	// to be around loses coverage every time a concept retires, silently,
	// unless somebody says so where the count is.

	t.Run("notes", func(t *testing.T) {
		qcA := memqlclient.NewQueryClient(connA.Dispatcher())

		// Seed > one page of notes owned by this user so the named query mints
		// a real cursor on its full first page. Each create stamps
		// ownerUserId=actor.userId, satisfying notes' authz gate.
		const total = namedPageSize + 6
		sent := make(map[string]struct{}, total)
		for i := 0; i < total; i++ {
			nid := "v1:notes:note:" + id.NewShortId()
			if _, err := qcA.CreateNote(ctx, memqlclient.CreateNoteArgs{
				NoteId: nid,
				Title:  fmt.Sprintf("named-query note probe %03d", i),
				Body:   "clustere2e named-query pagination probe",
			}); err != nil {
				t.Fatalf("create note %d: %v", i, err)
			}
			sent[nid] = struct{}{}
			time.Sleep(8 * time.Millisecond)
		}

		// notes is self-scoped (filters on ownerUserId==actor.userId), so the
		// result is bounded to OUR notes -- but the cluster may carry other
		// notes this user owns from prior runs. We therefore assert page
		// mechanics (bounded first page + cursor) and that every seeded id is
		// walked exactly once with no dup, rather than an exact total count.
		query := memqlclient.NotesBuild(memqlclient.NotesArgs{})

		qcB := memqlclient.NewQueryClient(connB.Dispatcher())
		cursor := ""
		walked := map[string]struct{}{}
		var firstPageLen int
		var mintedCursor bool
		for page := 0; ; page++ {
			qc := qcB
			if page == 0 {
				qc = qcA // mint on A, resolve later pages on B.
			}
			res, err := qc.ExecutePaginated(ctx, query, cursor)
			if err != nil {
				t.Fatalf("notes named query page %d: %v", page, err)
			}
			if page == 0 {
				firstPageLen = len(res.Rows)
			}
			if len(res.Rows) > namedPageSize {
				t.Fatalf("notes page %d returned %d rows, exceeds the bounded %d", page, len(res.Rows), namedPageSize)
			}
			for _, r := range res.Rows {
				rid := bareID(rowID(r))
				if _, dup := walked[rid]; dup {
					t.Fatalf("notes cross-node walk OVERLAP on %s", rid)
				}
				walked[rid] = struct{}{}
			}
			if res.NextCursor == "" {
				break
			}
			mintedCursor = true
			cursor = res.NextCursor
			if page > 64 {
				t.Fatal("notes pagination did not terminate")
			}
		}
		// Every seeded note must appear exactly once across the cross-node walk.
		// #2441: rows carry BARE ids; compare on the bare form.
		for nid := range sent {
			if _, ok := walked[bareID(nid)]; !ok {
				t.Fatalf("notes cross-node walk dropped seeded note %s (gap)", nid)
			}
		}
		// We seeded > namedPageSize, so the named query MUST have minted a cursor
		// (proving the bounded-page + continuation path on the real query def).
		if !mintedCursor {
			t.Fatalf("notes seeded %d (> page %d) but the named query never minted a cursor", total, namedPageSize)
		}
		if firstPageLen != namedPageSize {
			t.Fatalf("notes first page size = %d, want the bounded %d", firstPageLen, namedPageSize)
		}
	})
}
