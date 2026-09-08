package customdomain

import (
	"context"
	"fmt"

	"github.com/znasllc-io/memql/component/memql"
)

// accountdoor_accounts.go -- the two reads that tell the sweep which doors
// SHOULD exist (epic memql#5168, design D).
//
// # WHY THE SWEEP IS DRIVEN FROM THE ACCOUNT SIDE
//
// A door's own work list (`accountFrontDoorsToReconcile`) excludes `live`,
// because a healthy cluster's steady state is doors that are already serving
// and re-checking them every two minutes would make the good case the
// expensive one. But `live` is exactly the state a WITHDRAWN reservation has
// to reach out of (design D9), so something has to notice.
//
// That something is this: one read of the accounts that HOLD a reservation,
// one read of the doors that are open, and the difference in both directions.
// A reservation with no door opens one; a door with no reservation, or one
// whose name has moved, is asked to tear down. The cost is a read whose size
// is the number of accounts an operator typed, once every two minutes.

// Reservation is one account's held MemQL name.
type Reservation struct {
	AccountID    string
	ReservedName string
}

// DoorAccountReader reads the account side of the walk.
type DoorAccountReader struct{ engine Engine }

// NewDoorAccountReader wraps an engine.
func NewDoorAccountReader(engine Engine) *DoorAccountReader {
	return &DoorAccountReader{engine: engine}
}

// HeldReservations returns every active account that HOLDS a reserved name.
//
// HELD is the whole predicate, and it is `memqlReservedAt` rather than
// `memqlDomain` (record A, D10). A name is stamped reserved in the SAME write
// that verifies the domain, and only when it also passed the guardrails -- so
// a non-empty `memqlDomain` with no `memqlReservedAt` is a name somebody typed
// that this cluster has not agreed to serve, and opening a door for it would
// request a certificate for a host whose ownership is unproven.
func (s *DoorAccountReader) HeldReservations(ctx context.Context) ([]Reservation, error) {
	rows, err := s.rows(ctx, "query accountsHoldingAReservedName()")
	if err != nil {
		return nil, err
	}
	out := make([]Reservation, 0, len(rows))
	for _, r := range rows {
		name := rowString(r, "memqlDomain")
		if name == "" || rowString(r, "memqlReservedAt") == "" {
			// Defence in depth: the query filters on both, and a row reaching
			// here without them would open a door with no hosts.
			continue
		}
		out = append(out, Reservation{
			AccountID:    memql.BareShortId(rowString(r, "id")),
			ReservedName: name,
		})
	}
	return out, nil
}

// LiveAndPendingDoors returns every door that is not `removed` -- the set to
// compare against the reservations above.
//
// INCLUDES `live`, which is what makes the D9 teardown reachable at all, and
// includes `removing` so a door already coming down is not asked to come down
// again on every pass.
func (s *DoorAccountReader) LiveAndPendingDoors(ctx context.Context) ([]Door, error) {
	rows, err := s.rows(ctx, "query accountFrontDoorsOpen()")
	if err != nil {
		return nil, err
	}
	out := make([]Door, 0, len(rows))
	for _, r := range rows {
		out = append(out, doorFromRow(r))
	}
	return out, nil
}

func (s *DoorAccountReader) rows(ctx context.Context, query string) ([]map[string]any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("customdomain: no engine wired")
	}
	res, err := s.engine.Execute(doorContext(ctx), query)
	if err != nil {
		return nil, fmt.Errorf("customdomain: %s: %w", firstWord(query), err)
	}
	return memql.MaterializeRows(res), nil
}
