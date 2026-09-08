package memql

import (
	"context"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/frontdoor"
)

// The reserved-name guard's third and fourth refusals (epic memql#5168,
// design D7/H).
//
// Record A's guard (`account_domain_validation.go`) already refuses a
// `memqlDomain` that is under this cluster's own domain or equal to one of its
// front-door hosts. Both of those are answerable from the name alone. These
// two are not: they ask whether anything ELSE on this cluster already answers
// on one of the three hosts the name would serve, which needs a read.
//
// # A SEPARATE FILE, AND THAT IS DELIBERATE
//
// Record A owns `account_domain_validation.go` and is landing at the same time
// as this epic. Everything here composes onto that file through ONE call, so
// the two changes do not overlap in a way a merge has to arbitrate -- and the
// question this asks (is a HOST taken?) is a different question from the one
// that file asks (is this NAME allowed to exist?), which is reason enough on
// its own.
//
// # WHY IT REFUSES AT THE ACCOUNT WRITE RATHER THAN AT THE DOOR
//
// A door is opened by the sweep, under a synthetic actor, from a reservation
// that was already accepted. Refusing there would mean an operator types a
// name, sees it accepted, and finds out two minutes later from a row nobody
// told them to look at. The account write is where a person is standing.
//
// The sweep is not thereby unguarded: `executeWrite`'s own row-authz and the
// `@serverOnly` annotation still gate every door mutation, and the collision
// this prevents cannot be reintroduced by the sweep, which derives its hosts
// from the same reserved name this admitted.

// The typed codes. They lead the error string so a caller matches on the code
// rather than on prose, the discipline record A's three codes already follow.
const (
	accountNameCollidesWithSiteCode         = "name_collides_with_site"
	accountNameCollidesWithCustomDomainCode = "name_collides_with_custom_domain"
)

// validateAccountFrontDoorHosts refuses a reserved name whose derived hosts
// are already answered by something else on this cluster.
//
// # THE READS ARE UN-NARROWED, AND THAT IS THE POINT
//
// `liveSiteIdsForHostname` and `liveCustomDomainIdsForHostname` read WITHOUT
// row-authz narrowing, exactly as the custom-domain guard uses them: a
// hostname another user holds must collide even when the caller cannot see
// that row. A narrowed read would let one operator reserve a name that another
// operator's deployable already answers on, and the edge would then resolve
// one Host to two rows and answer from whichever came back first.
//
// # IT FAILS CLOSED
//
// A read error is a refusal, not a pass. "We could not check" and "there is
// nothing to find" are different answers, and an unavailable database must not
// be the way a colliding name gets in -- the sibling guards say the same and
// for the same reason.
func (e *MemQLEngine) validateAccountFrontDoorHosts(ctx context.Context, payload map[string]any) error {
	name := strings.ToLower(strings.TrimSpace(stringFromAny(payload["memqlDomain"])))
	if name == "" {
		return nil
	}

	// AN ENGINE THAT WAS NEVER GIVEN A STORE IS NOT A STORE THAT IS DOWN, and
	// the difference is NOT the nil-ness of the handle.
	//
	// The first version of this asked `e.database() == nil` and admitted. That
	// reasoning -- "a store-less engine holds no live deployable and no live
	// custom domain, so there is nothing to collide with" -- is true of an
	// engine CONSTRUCTED without a store, which was the case in front of me,
	// and false of a live one. `database()` prefers `dbGetter`, whose whole
	// stated purpose is handling RECONNECTION, and both concrete getters
	// return nil at runtime: `app.BunDB` is "nil until the database phase has
	// run", and `Database.BunDB` is nil "if the database is not connected".
	//
	// So the admit branch fired in two windows on a real cluster -- during
	// boot, and during any disconnection -- and in both the deployables and
	// custom domains DO exist and are merely unreadable. That is not "nothing
	// to collide with"; it is "I cannot see what I would collide with", which
	// is precisely the case this guard's own rule says must refuse. A
	// collision admitted during a database blip leaves two live claims on one
	// hostname with nothing in any log saying why.
	//
	// The distinction is available by reading the two fields rather than the
	// result. Caught in review by the author of memql#5165, whose own unit
	// test was the thing that surfaced it.
	e.dbMu.RLock()
	neverConfigured := e.dbGetter == nil && e.db == nil
	e.dbMu.RUnlock()
	if neverConfigured {
		return nil
	}

	for _, h := range frontdoor.AccountHosts(name) {
		host := strings.ToLower(strings.TrimSpace(h.Name))
		if host == "" {
			continue
		}

		holders, err := e.liveSiteIdsForHostname(ctx, host)
		if err != nil {
			return fmt.Errorf(
				"v1:accounts:account: cannot verify that %q is free for the %s host of %q: %w",
				host, h.Role, name, err)
		}
		if len(holders) > 0 {
			return fmt.Errorf(
				"v1:accounts:account: %s -- reserving %q would serve %q, which deployable %q already "+
					"answers on. A hostname resolves to exactly one site, so a second claim makes "+
					"which one answers depend on row order",
				accountNameCollidesWithSiteCode, name, host, holders[0])
		}

		claimers, err := e.liveCustomDomainIdsForHostname(ctx, host)
		if err != nil {
			return fmt.Errorf(
				"v1:accounts:account: cannot verify that %q is free for the %s host of %q: %w",
				host, h.Role, name, err)
		}
		if len(claimers) > 0 {
			return fmt.Errorf(
				"v1:accounts:account: %s -- reserving %q would serve %q, which custom domain %q is "+
					"already bound to. Remove that binding first; a removed one frees its hostname "+
					"and its row survives as history",
				accountNameCollidesWithCustomDomainCode, name, host, claimers[0])
		}
	}
	return nil
}
