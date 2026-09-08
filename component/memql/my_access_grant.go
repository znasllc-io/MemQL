package memql

// THE CALLER'S GRANT, as MyAccess reports it (epic memql#5165, section H).
//
// Which client groups a person is in, and therefore whose rows they may reach.
//
// # It is built from the SAME resolution the row gate uses
//
// `resolveAccountScope` is the one function that answers "which accounts does
// this actor reach", and this reads it rather than repeating its rules. That is
// the whole point: a second implementation would eventually disagree with the
// gate, and the disagreement would present as a client showing a person a
// client they cannot actually read anything of -- or, worse, hiding one they
// can. What the wire says and what a read returns come from one place.
//
// # The groups are the ROWS; the scope is the RULE plus the rows
//
// `Groups` lists the membership rows a person actually has. `EveryAccount` is
// the staff rule (design D6) -- developer rank and above are standing members
// of every account-kind group, as a rule the engine applies rather than rows
// anything writes. So a developer with no membership rows correctly reports an
// empty group list and `EveryAccount` set, and a client that reads the list
// alone would show them as belonging to nothing, which is the opposite of the
// truth.

import (
	"context"
	"sort"
	"strings"

	"github.com/uptrace/bun"

	"github.com/znasllc-io/memql/component/auth"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
)

// AccessGrantGroup is one group the caller belongs to.
type AccessGrantGroup struct {
	ID   string
	Name string
	// Kind is "account" or "custom".
	Kind      string
	AccountID string
	// AccountName is resolved here rather than left to the client: a person
	// reads "Acme", not an account id, and a client would otherwise need a
	// round trip per group to render its own list.
	AccountName string
}

// AccessGrant is the whole answer.
type AccessGrant struct {
	Groups     []AccessGrantGroup
	AccountIDs []string
	// EveryAccount is set for staff. AccountIDs is EMPTY when it is, and
	// empty means "all", not "none".
	EveryAccount bool
}

// ResolveAccessGrant answers the caller's grant.
//
// BEST-EFFORT BY CONSTRUCTION: every failure answers an empty grant rather
// than an error. This is one section of an identity reply that already
// succeeded, and failing the whole reply because a membership read stumbled
// would sign somebody out over a detail their session does not depend on.
func (e *MemQLEngine) ResolveAccessGrant(ctx context.Context) AccessGrant {
	var out AccessGrant
	if e == nil {
		return out
	}
	ac, _ := auth.AccessFromContext(ctx)
	if ac == nil || strings.TrimSpace(ac.UserId) == "" {
		return out
	}

	scope := e.resolveAccountScope(ctx)
	if scope == nil {
		return out
	}
	out.EveryAccount = scope.everyAccount
	if !scope.everyAccount {
		// The scope carries THREE spellings of each id (bare, canonical,
		// and the concept-prefixed form) because the row gate compares
		// against stored values it does not control. A CLIENT gets one --
		// the bare one, which is the wire contract at every seam.
		seen := map[string]struct{}{}
		for id := range scope.accounts {
			bare := BareShortId(id)
			if bare == "" {
				continue
			}
			if _, dup := seen[bare]; dup {
				continue
			}
			seen[bare] = struct{}{}
			out.AccountIDs = append(out.AccountIDs, bare)
		}
		sort.Strings(out.AccountIDs)
	}

	out.Groups = e.accessGrantGroups(ctx, ac.UserId)
	return out
}

// staged-data: MUST-NOT-GATE -- it must AGREE with the row gate, which does
// not gate either. A staged group hidden here but admitted there tells a client
// it belongs to fewer groups than its own reads will return, which is the one
// disagreement this whole function exists to prevent.
//
// accessGrantGroups reads the caller's active memberships and the groups they
// name, with each group's account name folded in.
func (e *MemQLEngine) accessGrantGroups(ctx context.Context, userId string) []AccessGrantGroup {
	db := e.database()
	if db == nil {
		return nil
	}
	groupIds := activeGroupIdsForUser(ctx, db, userId)
	if len(groupIds) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(groupIds)*2)
	for _, g := range groupIds {
		wanted[g] = struct{}{}
		if bare := BareShortId(g); bare != "" {
			wanted[bare] = struct{}{}
		}
	}

	var nodes []memorynodes.MemoryNode
	if err := db.NewSelect().
		Model(&nodes).
		DistinctOn("id").
		Where("concept = ?", conceptIdentityGroup).
		OrderExpr(`id ASC, "createdAt" DESC`).
		Scan(ctx); err != nil {
		return nil
	}

	out := make([]AccessGrantGroup, 0, len(groupIds))
	accountIds := map[string]struct{}{}
	for i := range nodes {
		id := strings.TrimSpace(nodes[i].ID)
		if _, want := wanted[id]; !want {
			if _, wantBare := wanted[BareShortId(id)]; !wantBare {
				continue
			}
		}
		payload := accountRowPayload(nodes[i])
		if payload == nil || strings.TrimSpace(stringFromAny(payload["status"])) != "active" {
			continue
		}
		g := AccessGrantGroup{
			ID:        BareShortId(id),
			Name:      strings.TrimSpace(stringFromAny(payload["name"])),
			Kind:      strings.TrimSpace(stringFromAny(payload["kind"])),
			AccountID: BareShortId(strings.TrimSpace(stringFromAny(payload["accountId"]))),
		}
		if g.AccountID != "" {
			accountIds[g.AccountID] = struct{}{}
		}
		out = append(out, g)
	}
	// SORTED BY NAME, so a client renders a stable list without sorting it
	// itself and two replicas answer identically.
	sort.Slice(out, func(a, b int) bool {
		if out[a].Name != out[b].Name {
			return out[a].Name < out[b].Name
		}
		return out[a].ID < out[b].ID
	})

	names := e.accountNames(ctx, db, accountIds)
	for i := range out {
		out[i].AccountName = names[out[i].AccountID]
	}
	return out
}

// staged-data: MUST-NOT-GATE -- gating buys no privacy and costs a label. The
// caller's membership already grants them that account's ROWS, so withholding
// its NAME shows them a group they demonstrably belong to labelled with an
// account id and nothing else.
//
// accountNames resolves display names for the accounts a caller's groups name.
//
// ONE READ for the whole set rather than one per group: a person is in a
// handful of groups, and a query each would be a round trip per row on a reply
// that already answered everything else.
func (e *MemQLEngine) accountNames(ctx context.Context, db *bun.DB, wanted map[string]struct{}) map[string]string {
	out := map[string]string{}
	if len(wanted) == 0 {
		return out
	}
	var nodes []memorynodes.MemoryNode
	if err := db.NewSelect().
		Model(&nodes).
		DistinctOn("id").
		Where("concept = ?", conceptAccountsAccount).
		OrderExpr(`id ASC, "createdAt" DESC`).
		Scan(ctx); err != nil {
		return out
	}
	for i := range nodes {
		bare := BareShortId(strings.TrimSpace(nodes[i].ID))
		if _, want := wanted[bare]; !want {
			continue
		}
		payload := accountRowPayload(nodes[i])
		if payload == nil {
			continue
		}
		out[bare] = strings.TrimSpace(stringFromAny(payload["name"]))
	}
	return out
}
