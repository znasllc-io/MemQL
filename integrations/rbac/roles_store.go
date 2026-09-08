package rbac

// WHAT THE ROLE BUILTINS READ (epic memql#5166).
//
// Three reads, and each is answered where the answer actually lives:
//
//   - the role itself, through the DSL (`roleBySlug`), because the row is
//     public reference data every client already reads that way;
//   - the CATALOG, through component/auth's installed resolver, because "is
//     this slug taken" and "does the caller hold this pair" are questions about
//     the resolved ladder rather than about any one row;
//   - the HOLDERS, through the same collapsed SQL read component/memql's
//     principalRoles performs, for the reason that one gives: v1:identity:user
//     is the concept that churns hardest, and scanning every version of it to
//     count a few dozen people is a cost nobody notices until a long-lived
//     cluster gets slow.
//
// THE HOLDER COUNT IS A COUNT, NOT A DISCLOSURE. It never returns a row, a
// name or an address -- only how many people would be stranded by retiring a
// role -- and the caller has already been checked for update-on-role, which
// admits nobody who cannot read the user list anyway. That is why it is
// answered from the database rather than through a new @serverOnly query with
// its own shape, classification and generated SDK surface.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uptrace/bun"

	componentAuth "github.com/znasllc-io/memql/component/auth"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	langparser "github.com/znasllc-io/memql/component/language/parser"
)

const (
	conceptIdentityUser       = "v1:identity:user"
	conceptIdentityInvitation = "v1:identity:invitation"
)

// catalog returns the installed capability catalog, or nil.
//
// Read through component/auth rather than off the engine, so the guards resolve
// through exactly the resolver every OTHER gate in the cluster resolves
// through. A second reader would be a second answer to "does this caller hold
// that grant", and this is the file that decides what a new role may hold.
func (i *Integration) catalog() roleCatalogReader {
	installed := componentAuth.InstalledCapabilityCatalog()
	if installed == nil {
		return nil
	}
	reader, ok := installed.(roleCatalogReader)
	if !ok {
		// The interface component/auth declares is a SUBSET of what these
		// guards need -- Slugs() is not on it, because nothing else wants to
		// enumerate. A catalog that does not satisfy the wider one is a test
		// double or a future implementation, and refusing is the fail-closed
		// answer: authoring a role against a catalog that cannot list its own
		// rungs would skip the rank-taken guard entirely.
		return nil
	}
	return reader
}

// exec runs one MemQL statement.
func (i *Integration) exec(ctx context.Context, query string) error {
	if i.engine == nil {
		return errNoEngine
	}
	_, err := i.engine.Execute(ctx, query)
	return err
}

type integrationError string

func (e integrationError) Error() string { return string(e) }

const errNoEngine = integrationError("rbac: no engine wired; the role builtins cannot write")

// readRole reads one role through the DSL.
//
// ACTIVE ONLY, which `roleBySlug` already enforces. A deactivated role reads as
// absent here on purpose: D8 keeps it as history, and history is not editable.
// Its slug and its rung stay TAKEN, which the catalog answers -- so a retired
// role cannot be edited and cannot be recreated under its own name either.
func (i *Integration) readRole(ctx context.Context, slug string) (*roleRow, error) {
	if i.engine == nil {
		return nil, errNoEngine
	}
	res, err := i.engine.Execute(ctx, `query roleBySlug(slug: `+langparser.QuoteString(slug)+`)`)
	if err != nil {
		return nil, err
	}
	if res == nil || res.Bundle == nil || len(res.Bundle.Nodes) == 0 {
		return nil, nil
	}
	n := res.Bundle.Nodes[0]
	if n == nil || n.Payload == nil {
		return nil, nil
	}
	fields := n.Payload.GetFields()
	str := func(k string) string {
		if v, present := fields[k]; present && v != nil {
			return strings.TrimSpace(v.GetStringValue())
		}
		return ""
	}
	row := &roleRow{
		slug:        str("slug"),
		name:        str("name"),
		description: str("description"),
		accountId:   str("accountId"),
	}
	if v, present := fields["rank"]; present && v != nil {
		row.rank = int(v.GetNumberValue())
	}
	if v, present := fields["predefined"]; present && v != nil {
		row.predefined = v.GetBoolValue()
	}
	grants, err := i.readGrants(ctx, slug)
	if err != nil {
		return nil, err
	}
	row.grants = grants
	return row, nil
}

// readGrants reads a role's ACTIVE grants through the DSL.
func (i *Integration) readGrants(ctx context.Context, slug string) ([]componentAuth.VerbResource, error) {
	res, err := i.engine.Execute(ctx,
		`query capabilitiesForRole(roleSlug: `+langparser.QuoteString(slug)+`)`)
	if err != nil {
		return nil, err
	}
	if res == nil || res.Bundle == nil {
		return nil, nil
	}
	out := make([]componentAuth.VerbResource, 0, len(res.Bundle.Nodes))
	for _, n := range res.Bundle.Nodes {
		if n == nil || n.Payload == nil {
			continue
		}
		f := n.Payload.GetFields()
		verb, resource := "", ""
		if v, ok := f["verb"]; ok && v != nil {
			verb = strings.TrimSpace(v.GetStringValue())
		}
		if v, ok := f["resourceType"]; ok && v != nil {
			resource = strings.TrimSpace(v.GetStringValue())
		}
		// A DENY row is not a grant this path can edit: no v1 path writes one
		// (D9) and roleUpdate's "grants as a whole" semantics would retire one
		// silently on the first edit. Skipping it leaves it exactly as it was.
		if v, ok := f["effect"]; ok && v != nil && strings.TrimSpace(v.GetStringValue()) == "deny" {
			continue
		}
		if verb == "" || resource == "" {
			continue
		}
		out = append(out, componentAuth.VerbResource{Verb: verb, Resource: resource})
	}
	return out, nil
}

// holdersOf counts the ACTIVE users currently carrying a role slug or one of
// its aliases.
//
// COLLAPSED IN SQL, the same DISTINCT ON (id) ... ORDER BY id, createdAt DESC
// component/memql's principalRoles performs, and for its reason: rows are
// append-only, v1:identity:user churns on lastSeenAt, and the version this
// resolves a role from must be the version a read of that user returns.
//
// ALIASES COUNT. Every ordinary principal's row spells the member tier
// `writer`; a holder check that compared the catalog slug alone would report
// zero holders for `user` and let it be retired out from under everybody.
//
// staged-data: MUST-NOT-GATE -- a staged v1:identity:user row excluded here is
// a HOLDER THIS COUNT CANNOT SEE, so `roleDeactivate`'s only job -- refusing to
// strand the people on a role -- fails open for exactly them. They are then
// left holding a retired role, which resolves to nothing, everywhere, until
// somebody re-roles them; and nothing reports it, because the retirement
// succeeded. Staging governs whether a row is PUBLISHED; it must not decide
// whether a person counts as holding a role.
func (i *Integration) holdersOf(ctx context.Context, slug string) ([]string, error) {
	db := i.db()
	if db == nil {
		// NO DATABASE IS NOT "NO HOLDERS". Answering zero here would let a
		// retirement through on exactly the node that cannot check it, which is
		// the fail-OPEN direction; the error refuses the operation instead.
		return nil, errNoDatabase
	}
	// Every spelling that RESOLVES TO THIS RUNG counts as this role, which is
	// what CanonicalSlug answers. Rank equality would not do: two roles may
	// legally share a rank -- the ranks are spaced for slotting, not
	// partitioned -- so the test has to be the resolved name.
	matches := func(role string) bool { return strings.EqualFold(strings.TrimSpace(role), slug) }
	if cat := i.catalog(); cat != nil {
		canonical := cat.CanonicalSlug(slug)
		matches = func(role string) bool {
			role = strings.ToLower(strings.TrimSpace(role))
			return role != "" && cat.CanonicalSlug(role) == canonical
		}
	}

	var nodes []memorynodes.MemoryNode
	if err := db.NewSelect().
		Model(&nodes).
		DistinctOn("id").
		Where("concept = ?", conceptIdentityUser).
		OrderExpr(`id ASC, "createdAt" DESC`).
		Scan(ctx); err != nil {
		return nil, err
	}
	var holders []string
	for idx := range nodes {
		var payload map[string]any
		if err := json.Unmarshal(nodes[idx].Payload, &payload); err != nil {
			continue
		}
		if active, present := payload["active"].(bool); present && !active {
			continue
		}
		if role, _ := payload["role"].(string); matches(role) {
			holders = append(holders, nodes[idx].ID)
		}
	}
	return holders, nil
}

// pendingInvitationsFor counts the pending invitations naming a role.
//
// An invitation is a PROMISE of a role somebody has not redeemed yet, so
// retiring the role it names would leave a link that lands its recipient on a
// role holding nothing -- which they would discover after clicking, having
// already been told they were being given something.
//
// staged-data: MUST-NOT-GATE -- for holdersOf's reason, one step earlier. A
// staged invitation excluded here is a promise this count cannot see, so the
// role it names is retired out from under a link already in somebody's inbox.
func (i *Integration) pendingInvitationsFor(ctx context.Context, slug string) (int, error) {
	db := i.db()
	if db == nil {
		return 0, errNoDatabase
	}
	var nodes []memorynodes.MemoryNode
	if err := db.NewSelect().
		Model(&nodes).
		DistinctOn("id").
		Where("concept = ?", conceptIdentityInvitation).
		OrderExpr(`id ASC, "createdAt" DESC`).
		Scan(ctx); err != nil {
		return 0, err
	}
	count := 0
	for idx := range nodes {
		var payload map[string]any
		if err := json.Unmarshal(nodes[idx].Payload, &payload); err != nil {
			continue
		}
		if status, _ := payload["status"].(string); strings.TrimSpace(status) != "pending" {
			continue
		}
		if role, _ := payload["inviteeRole"].(string); strings.EqualFold(strings.TrimSpace(role), slug) {
			count++
		}
	}
	return count, nil
}

const errNoDatabase = integrationError(
	"rbac: this node has no database, so the number of people holding a role cannot be " +
		"checked; refusing rather than reporting zero")

// db returns the pooled handle, or nil.
func (i *Integration) db() *bun.DB {
	if i.bunDB == nil {
		return nil
	}
	return i.bunDB()
}
