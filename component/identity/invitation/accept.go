package invitation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/znasllc-io/memql/component/identity"
	"github.com/znasllc-io/memql/component/identity/enrolment"
	"github.com/znasllc-io/memql/component/identity/registration"
	"github.com/znasllc-io/memql/core/id"
)

// AcceptDeps is what spending an invitation needs from the identity node.
type AcceptDeps struct {
	// Store owns the invitation and user rows.
	Store *identity.Store
	// Enrolments owns the enrolment-token rows the /enroll page redeems.
	Enrolments *enrolment.Store
	// InternalEmail reports whether an address belongs to one of the
	// cluster's own domains (MEMQL_IDENTITY_INTERNAL_DOMAINS). Nil means no
	// address does.
	InternalEmail func(email string) bool
	// InternalDefaultRole is the role an internal address lands with when
	// the invitation named none.
	InternalDefaultRole string

	// PlaceInGroups writes one membership per group the invitation carried,
	// and then applies domain join (epic memql#5165, section G). Nil on a
	// node with no groups plug-in wired, which is not an error: the user row
	// is written either way, and an arrival that lands in no group is a
	// person who signs in and sees their own work.
	//
	// A FUNCTION rather than a store handle, because this package must not
	// depend on integrations/: the seam is what the node wires in.
	// IT RETURNS NOTHING, and that is the contract rather than an omission.
	// By the time it runs the user row exists and the invitation is spent,
	// so there is no failure it could report that the accept could act on --
	// returning an error would render "your invitation could not be
	// accepted" to somebody whose account was just created, and leave them
	// unable to retry because the invitation is single-use. The
	// implementation logs; an admin fixes a missing membership in one click.
	PlaceInGroups func(ctx context.Context, userId, email string, groupIds []string, issuedBy string)
}

// AcceptResult is what the accept hands back to the page.
type AcceptResult struct {
	// UserId is the canonical id of the account that now exists.
	UserId string
	// EnrolmentCode is the freshly-minted enrolment token plaintext. It is
	// returned exactly once and held nowhere else.
	EnrolmentCode string
	// Email is the address the account was provisioned for.
	Email string
	// InvitationId is the canonical id of the row that was spent.
	InvitationId string
}

// ErrNotRedeemable is returned when the presented token resolves to nothing
// that can be spent: unknown, not a user invitation, revoked, already used
// or expired.
var ErrNotRedeemable = errors.New("identity: invitation is not redeemable")

const conceptUser = "v1:identity:user"

// Accept spends a user invitation: it provisions the user row for the invited
// address and role, marks the invitation accepted, and mints the enrolment
// token that the /enroll page will consume.
//
// One function rather than three seams because the three steps have to agree
// about ordering and failure, and that argument belongs in one place next to
// the stores rather than being reassembled by every caller from parts.
func Accept(ctx context.Context, deps AcceptDeps, plainToken, sourceIP string) (AcceptResult, error) {
	if deps.Store == nil || deps.Enrolments == nil {
		return AcceptResult{}, errors.New("invitation.Accept: Store and Enrolments are required")
	}

	// RE-RESOLVED HERE, NOT TRUSTED FROM THE PAGE. This function spends a
	// credential; it does its own lookup so the decision to spend is made
	// against the row as it is now, not as some earlier request reported it.
	row, err := deps.Store.LookupInvitationByTokenHash(ctx, Hash(plainToken))
	if err != nil {
		return AcceptResult{}, err
	}
	if row == nil || !strings.EqualFold(strings.TrimSpace(row.Kind), "user") ||
		!row.Active || !strings.EqualFold(strings.TrimSpace(row.Status), "pending") ||
		(!row.ExpiresAt.IsZero() && !row.ExpiresAt.After(time.Now().UTC())) {
		return AcceptResult{}, ErrNotRedeemable
	}

	// THE ORDER OF THE NEXT THREE WRITES IS THE WHOLE FAILURE STORY, and it
	// is chosen the way IssueUserInvitation chooses its own.
	//
	// User first. It is the durable thing the invitee actually needs, and
	// creating it twice is what we must avoid -- so it happens once, before
	// anything that could make us retry.
	//
	// Mark accepted second. This is what makes the invitation single-use, and
	// it must land BEFORE a usable credential exists: if the process died
	// between minting the enrolment token and stamping the row, the
	// invitation would still read as pending and a forwarded copy could be
	// redeemed again for a second account. Marking first can only fail the
	// other way -- a spent invitation and no enrolment link -- which strands
	// the invitee with a clear message and a row an admin can see, instead
	// of quietly leaving a live credential behind.
	//
	// Enrolment token last, because it is the only one of the three the
	// caller can be handed again by simply issuing a fresh invitation.
	userId, err := identity.NewRandomId("")
	if err != nil {
		return AcceptResult{}, err
	}
	internal := deps.InternalEmail != nil && deps.InternalEmail(row.Email)
	role := strings.TrimSpace(row.Role)
	if role == "" && internal {
		role = deps.InternalDefaultRole
	}
	seed := identity.UserProfileSeed{
		// Stamped at creation for the reason memql#4304 gives: the flag
		// should be right from the first sign-in rather than appearing
		// later, and the heuristic never runs again.
		SharedMailbox: registration.LooksLikeSharedMailbox(row.Email),
	}
	if err := deps.Store.CreateUserOnFirstLogin(ctx, userId, row.Email, row.Email, role, internal, seed); err != nil {
		return AcceptResult{}, err
	}
	if err := deps.Store.MarkUserInvitationAccepted(ctx, row.ID, userId); err != nil {
		return AcceptResult{}, err
	}

	// The groups this invitation carried, plus domain join (epic memql#5165,
	// section G).
	//
	// AFTER the accepted stamp, not before, and the ordering argument above
	// carries straight over: the single-use mark must land before anything
	// that could fail leaves the invitation spendable again. A membership
	// that fails to write is a person in the cluster but not in their
	// client's group -- recoverable by an admin in one click. A second
	// redemption is not recoverable at all.
	//
	// It cannot fail the accept, and that is the seam's contract rather than
	// an omission here -- see PlaceInGroups' own doc.
	if deps.PlaceInGroups != nil {
		// The address is treated as VERIFIED: the link was delivered to it
		// and the person holding it just followed it, which is the same
		// evidence a magic-link sign-in rests on.
		deps.PlaceInGroups(ctx, userId, row.Email, row.GroupIds, row.InviterId)
	}

	plain, hash, err := enrolment.Mint()
	if err != nil {
		return AcceptResult{}, err
	}
	enrolmentId, err := enrolment.NewId()
	if err != nil {
		return AcceptResult{}, err
	}
	// ATTRIBUTED TO THE INVITER, AND THE INVITATION RIDES ITS OWN FIELD
	// (memql#4880). The first version of this wrote issuedBy as
	// "invitation:<id>", on the reasoning that no person issued this token --
	// the invitee's own click did. The engine refused it on every accept ever
	// attempted: enrolmentToken declares issuedBy a parent edge onto
	// v1:identity:user, and canonicalization rejects an id under any other
	// concept at write time. The user row and the accepted stamp had already
	// landed by then, so the invitee got "we could not finish setting up your
	// account" with an account that existed and a link that now read as used.
	//
	// The inviter IS the authority here -- the invitation carried theirs and
	// nobody else's -- which is what the field's own description says it
	// holds. What the first version wanted an operator to be able to see,
	// that the credential came from an invitation, is now a fact the row
	// states in a field declared for it.
	issuedBy := strings.TrimSpace(row.InviterId)
	if issuedBy == "" {
		return AcceptResult{}, errors.New("invitation.Accept: invitation names no inviter, so the enrolment token has nobody to attribute to")
	}
	expiresAt := time.Now().UTC().Add(enrolment.DefaultTTL)
	if err := deps.Enrolments.CreateForInvitation(ctx, enrolmentId, userId, hash, issuedBy, row.ID, expiresAt, sourceIP); err != nil {
		return AcceptResult{}, err
	}

	return AcceptResult{
		UserId:        id.BuildNodeId(conceptUser, userId),
		EnrolmentCode: plain,
		Email:         row.Email,
		InvitationId:  row.ID,
	}, nil
}
