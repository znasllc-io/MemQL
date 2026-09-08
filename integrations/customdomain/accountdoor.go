package customdomain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	langparser "github.com/znasllc-io/memql/component/language/parser"
	"github.com/znasllc-io/memql/component/memql"
)

// accountdoor.go -- the graph seam for v1:platform:accountFrontDoor (epic
// memql#5168, design B/D).
//
// Every read and write is a NAMED construct rendered as MemQL text and handed
// to the engine, under the same synthetic cluster-owner actor store.go
// documents. Nothing here reaches the database.

// Door is the projection of one v1:platform:accountFrontDoor row the sweep and
// the rail work with.
type Door struct {
	ID            string
	AccountID     string
	ReservedName  string
	Status        string
	HostChecks    map[string]HostCheck
	FailureReason string
	FailureDetail string
	LastCheckedAt string
	VerifiedAt    string
	IssuedAt      string
	RemovedAt     string
}

// HostCheck is what one pass saw at one of the three hosts.
//
// IT GATES NOTHING. Activation is all-or-nothing and the certificate enforces
// it (design D8); this exists so the Accounts rail can name WHICH CNAME is
// still wrong instead of saying three names are not pointing and leaving an
// operator to test each one by hand.
type HostCheck struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// DoorStore reads and writes front doors through the engine.
type DoorStore struct{ engine Engine }

// NewDoorStore wraps an engine.
func NewDoorStore(engine Engine) *DoorStore { return &DoorStore{engine: engine} }

// ToReconcile returns every door the sweep still has work for.
//
// NOT the paginated `accountFrontDoorsAll`, for ToReconcile's reason: a sweep
// that read a page would silently never reconcile the doors past it, and the
// symptom -- a client's name that verifies for nobody, with nothing in any log
// -- is indistinguishable from a DNS problem on their side.
func (s *DoorStore) ToReconcile(ctx context.Context) ([]Door, error) {
	rows, err := s.rows(ctx, "query accountFrontDoorsToReconcile()")
	if err != nil {
		return nil, err
	}
	out := make([]Door, 0, len(rows))
	for _, r := range rows {
		out = append(out, doorFromRow(r))
	}
	return out, nil
}

// ForAccount returns every door ever opened for one account, newest first.
// There is one LIVE door per account; there may be several removed ones,
// because changing a client's domain tears the old one down.
func (s *DoorStore) ForAccount(ctx context.Context, accountID string) ([]Door, error) {
	rows, err := s.rows(ctx, fmt.Sprintf(
		"query accountFrontDoorsForAccount(accountId: %s)", langparser.QuoteString(accountID)))
	if err != nil {
		return nil, err
	}
	out := make([]Door, 0, len(rows))
	for _, r := range rows {
		out = append(out, doorFromRow(r))
	}
	return out, nil
}

// Create opens a door at `pending_dns`.
func (s *DoorStore) Create(ctx context.Context, d Door) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation createAccountFrontDoor(doorId: %s, accountId: %s, reservedName: %s)",
		langparser.QuoteString(d.ID),
		langparser.QuoteString(d.AccountID),
		langparser.QuoteString(d.ReservedName)))
}

// RecordCheck records a pass that looked and did not advance.
func (s *DoorStore) RecordCheck(ctx context.Context, doorID, status string, checks map[string]HostCheck, reason, detail string, at time.Time) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation recordAccountFrontDoorCheck(doorId: %s, status: %s, hostChecks: %s, failureReason: %s, failureDetail: %s, lastCheckedAt: %s)",
		langparser.QuoteString(doorID),
		langparser.QuoteString(status),
		renderHostChecks(checks),
		langparser.QuoteString(reason),
		langparser.QuoteString(detail),
		langparser.QuoteString(stamp(at))))
}

// MarkVerified promotes a door to `issuing`: all three hosts point here.
func (s *DoorStore) MarkVerified(ctx context.Context, doorID string, checks map[string]HostCheck, at time.Time) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation markAccountFrontDoorVerified(doorId: %s, hostChecks: %s, verifiedAt: %s, lastCheckedAt: %s)",
		langparser.QuoteString(doorID),
		renderHostChecks(checks),
		langparser.QuoteString(stamp(at)),
		langparser.QuoteString(stamp(at))))
}

// RecordIssuingProgress records a pass where the objects are applied and the
// certificate is not Ready yet. It writes NO failureReason, for
// RecordIssuingProgress's reason one concept over: waiting for a three-name
// ACME order is not a failure, and a typed reason would make an ordinary wait
// render as something a person should go and fix.
func (s *DoorStore) RecordIssuingProgress(ctx context.Context, doorID, note string, at time.Time) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation recordAccountFrontDoorIssuingProgress(doorId: %s, failureDetail: %s, lastCheckedAt: %s)",
		langparser.QuoteString(doorID),
		langparser.QuoteString(note),
		langparser.QuoteString(stamp(at))))
}

// RecordIssuanceFailure keeps a door in `issuing` and records why.
func (s *DoorStore) RecordIssuanceFailure(ctx context.Context, doorID, reason, detail string, at time.Time) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation recordAccountFrontDoorIssuanceFailure(doorId: %s, failureReason: %s, failureDetail: %s, lastCheckedAt: %s)",
		langparser.QuoteString(doorID),
		langparser.QuoteString(reason),
		langparser.QuoteString(detail),
		langparser.QuoteString(stamp(at))))
}

// MarkLive closes the walk at `live`. From this write on the edge resolves
// app.<reservedName> to the OS site with the account in context.
func (s *DoorStore) MarkLive(ctx context.Context, doorID string, at time.Time) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation markAccountFrontDoorLive(doorId: %s, issuedAt: %s, lastCheckedAt: %s)",
		langparser.QuoteString(doorID),
		langparser.QuoteString(stamp(at)),
		langparser.QuoteString(stamp(at))))
}

// RequestRemoval moves a door to `removing` because the reservation behind it
// is gone (design D9).
//
// THE THREE HOSTS STOP RESOLVING AT THIS WRITE rather than at the Ingress
// deletion -- `liveAccountFrontDoorByReservedName` filters `status=="live"` --
// so a cluster stops answering on a name whose ownership proof was discarded
// immediately, and the objects come down on the sweep's own schedule.
func (s *DoorStore) RequestRemoval(ctx context.Context, doorID, reason, detail string, at time.Time) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation requestAccountFrontDoorRemoval(doorId: %s, failureReason: %s, failureDetail: %s, lastCheckedAt: %s)",
		langparser.QuoteString(doorID),
		langparser.QuoteString(reason),
		langparser.QuoteString(detail),
		langparser.QuoteString(stamp(at))))
}

// RecordRemovalFailure keeps a door in `removing` and records why.
//
// NOT RecordIssuanceFailure, which stamps `issuing`. Routing a failed teardown
// through that one walked the row `removing` -> `issuing`, and the next pass
// re-applied the certificate and all four Ingresses for a name whose
// reservation had been withdrawn -- putting a door the cluster had stopped
// claiming back into service, and making design D9's "the only transition out
// of live" reversible by any transient unbind failure.
func (s *DoorStore) RecordRemovalFailure(ctx context.Context, doorID, reason, detail string, at time.Time) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation recordAccountFrontDoorRemovalFailure(doorId: %s, failureReason: %s, failureDetail: %s, lastCheckedAt: %s)",
		langparser.QuoteString(doorID),
		langparser.QuoteString(reason),
		langparser.QuoteString(detail),
		langparser.QuoteString(stamp(at))))
}

// MarkRemoved closes the walk. Terminal; the row survives as the audit.
func (s *DoorStore) MarkRemoved(ctx context.Context, doorID string, at time.Time) error {
	return s.exec(ctx, fmt.Sprintf(
		"mutation markAccountFrontDoorRemoved(doorId: %s, removedAt: %s, lastCheckedAt: %s)",
		langparser.QuoteString(doorID),
		langparser.QuoteString(stamp(at)),
		langparser.QuoteString(stamp(at))))
}

// doorContext is the actor and origin every read and write here runs under.
//
// SystemActorContext already stamps internal origin, so naming
// auth.ContextWithInternalOrigin again is idempotent -- and deliberate. Every
// mutation this file calls is @serverOnly, the engine refuses one whose origin
// is not internal, and the failure is a WARN in a log about "clients" on a
// cluster nobody is watching. The conformance gate that catches that reads
// THIS file, and it is right to: somebody grepping here for the stamp has to
// find it rather than chase it into store.go.
func doorContext(ctx context.Context) context.Context {
	return auth.ContextWithInternalOrigin(SystemActorContext(ctx))
}

func (s *DoorStore) rows(ctx context.Context, query string) ([]map[string]any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("customdomain: no engine wired")
	}
	res, err := s.engine.Execute(doorContext(ctx), query)
	if err != nil {
		return nil, fmt.Errorf("customdomain: %s: %w", firstWord(query), err)
	}
	return memql.MaterializeRows(res), nil
}

func (s *DoorStore) exec(ctx context.Context, query string) error {
	if s == nil || s.engine == nil {
		return fmt.Errorf("customdomain: no engine wired")
	}
	if _, err := s.engine.Execute(doorContext(ctx), query); err != nil {
		return fmt.Errorf("customdomain: %s: %w", firstWord(query), err)
	}
	return nil
}

// renderHostChecks encodes the per-host map as a MemQL object literal.
//
// JSON, because the compiler's object-literal parser accepts quoted keys and
// quoted string values and because encoding/json is the one encoder that
// cannot be talked into emitting an unescaped quote from a hostname or a
// resolver's error text. A hand-built literal here would be a quoting bug
// waiting for the first DNS server that answers with an apostrophe in it.
func renderHostChecks(checks map[string]HostCheck) string {
	if len(checks) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(checks)
	if err != nil {
		// Unreachable for this type; an empty object is the honest fallback,
		// since the alternative is failing a pass over a rendering detail.
		return "{}"
	}
	return string(raw)
}

func doorFromRow(r map[string]any) Door {
	return Door{
		ID:            memql.BareShortId(rowString(r, "id")),
		AccountID:     memql.BareShortId(rowString(r, "accountId")),
		ReservedName:  rowString(r, "reservedName"),
		Status:        rowString(r, "status"),
		HostChecks:    hostChecksFromRow(r["hostChecks"]),
		FailureReason: rowString(r, "failureReason"),
		FailureDetail: rowString(r, "failureDetail"),
		LastCheckedAt: rowString(r, "lastCheckedAt"),
		VerifiedAt:    rowString(r, "verifiedAt"),
		IssuedAt:      rowString(r, "issuedAt"),
		RemovedAt:     rowString(r, "removedAt"),
	}
}

// hostChecksFromRow decodes the stored object back into the typed map.
//
// A row that carries nothing, or something of the wrong shape, yields an EMPTY
// map rather than an error: the checks are a report, and a pass that cannot
// read the last one still knows how to run this one.
func hostChecksFromRow(v any) map[string]HostCheck {
	obj, ok := v.(map[string]any)
	if !ok || len(obj) == 0 {
		return map[string]HostCheck{}
	}
	out := make(map[string]HostCheck, len(obj))
	for role, raw := range obj {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		check := HostCheck{}
		if b, ok := m["ok"].(bool); ok {
			check.OK = b
		}
		if s, ok := m["reason"].(string); ok {
			check.Reason = s
		}
		if s, ok := m["detail"].(string); ok {
			check.Detail = s
		}
		out[role] = check
	}
	return out
}
