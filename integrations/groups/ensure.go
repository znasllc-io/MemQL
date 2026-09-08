package groups

// ensure.go -- the account's own group (epic memql#5165, D5).
//
// Every account gets one, at a DERIVED id, and that is what makes the two
// callers safe to run repeatedly: the `ensureAccountGroup` automation fires on
// account creation, and the seed materializer sweeps every active account on
// every boot. A minted id would have made the second one write a new group per
// boot forever.
//
// NOT @sdk, deliberately. This is the engine placing a row; a caller-reachable
// version would let anyone mint the one group an account's whole membership
// hangs on, and then tie it to an account of their choosing.

import (
	"context"
	"strings"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/memql"
)

// AccountGroupID derives the account-kind group's id from its account's.
//
// `acct-` rather than a hash, because an operator reading a membership row in
// a log should be able to tell which account it grants without a second query.
func AccountGroupID(accountID string) string {
	return "acct-" + memql.BareShortId(strings.TrimSpace(accountID))
}

// handleGroupEnsureForAccount writes the account-kind group if none is active.
//
// NO CALLER GUARD, and that is the one place in this package where the absence
// of a check is deliberate rather than an omission: the builtin carries no
// `@sdk`, so it is not on the client wire at all, and both callers are the
// engine itself -- an automation running under its own actor and the boot
// sweep running under the seed materializer's. Adding a capability check would
// refuse both, since neither is a person.
func (i *Integration) handleGroupEnsureForAccount(ctx context.Context, args map[string]any, _ int) ([]memorynodes.MemoryNode, error) {
	accountID := memql.BareShortId(strings.TrimSpace(asString(args["accountId"])))
	if accountID == "" {
		return nil, refusal(CodeAccountNotFound, "an account id is required")
	}
	account, err := i.store.AccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, refusal(CodeAccountNotFound, accountID+" names no account")
	}
	// An ARCHIVED account gets no group. The cascade archives the group when
	// the account is archived, so writing one here would undo that on the
	// next boot sweep -- a backfill and a cascade fighting each other, with
	// the boot winning and nobody watching.
	if strings.TrimSpace(rowString(account, "status")) != StatusActive {
		return i.node("groupEnsureForAccount", map[string]any{
			"accountId": accountID, "groupId": "", "created": false,
		})
	}

	groupID := AccountGroupID(accountID)
	existing, err := i.store.GroupByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Status == StatusActive {
		return i.node("groupEnsureForAccount", map[string]any{
			"accountId": accountID, "groupId": existing.ID, "created": false,
		})
	}

	name := strings.TrimSpace(rowString(account, "name"))
	if name == "" {
		// An account with no name is a row mid-configuration -- the self
		// account's own seed lands before the first-run card names it. The
		// group still has to exist, so it says what it is rather than
		// carrying an empty name a picker would render as a blank line.
		name = "Account " + accountID
	}
	g := Group{
		ID:          groupID,
		Name:        name,
		Description: "Everyone who works on " + name + ".",
		Kind:        KindAccount,
		AccountID:   accountID,
		Status:      StatusActive,
	}
	if err := i.store.WriteGroup(ctx, g); err != nil {
		return nil, err
	}
	return i.node("groupEnsureForAccount", map[string]any{
		"accountId": accountID, "groupId": g.ID, "created": true,
	})
}

// ArchiveGroupsForAccount archives every group tied to one account and removes
// their memberships -- the `archiveAccountGroup` cascade (D5).
//
// BOTH KINDS, not just the account-kind one. A custom group tied to an
// archived account grants access to a client that is gone; leaving it active
// would mean the account's archive changed what its screens show and not what
// its people can reach.
func (i *Integration) ArchiveGroupsForAccount(ctx context.Context, accountID string) (groupsArchived, membershipsRemoved int, err error) {
	accountID = memql.BareShortId(strings.TrimSpace(accountID))
	if accountID == "" {
		return 0, 0, refusal(CodeAccountNotFound, "an account id is required")
	}
	tied, err := i.store.GroupsForAccount(ctx, accountID)
	if err != nil {
		return 0, 0, err
	}
	for _, g := range tied {
		if g.Status != StatusActive {
			continue
		}
		removed, err := i.archiveGroup(ctx, g, "")
		if err != nil {
			return groupsArchived, membershipsRemoved, err
		}
		groupsArchived++
		membershipsRemoved += removed
	}
	return groupsArchived, membershipsRemoved, nil
}
