package auth

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ContextWithPersistedOwner borrows the authority of an owned persisted row for
// durable execution on another replica. The caller must have loaded
// that row through an authorized read; ownerUserId must never come directly
// from a client argument or an unverified envelope.
//
// This starts a durable operation's writer-scoped assertion. It is not a way
// to forward a live session: badge/session forwards must preserve their own
// credential class and ceiling. BindForwardedContext also clears internal
// origin, so a maintenance caller lends neither its rank nor its trust.
func ContextWithPersistedOwner(ctx context.Context, ownerUserId string) (context.Context, error) {
	owner := strings.TrimSpace(ownerUserId)
	if owner == "" {
		return nil, fmt.Errorf("auth: a persisted owner is required to assert forwarded authority")
	}
	now := time.Now()
	access := &AccessContext{UserId: owner, Role: RoleWriter}
	authority, err := ForwardedAuthorityForUser(access, ForwardedClassUser, "", time.Time{}, now)
	if err != nil {
		return nil, fmt.Errorf("auth: could not assert the persisted owner's authority: %w", err)
	}
	verified, err := VerifyForwardedAuthority(authority, now)
	if err != nil {
		return nil, fmt.Errorf("auth: persisted-owner authority did not verify: %w", err)
	}
	return BindForwardedContext(ctx, authority.Principal().Claims, verified, authority), nil
}
