package memql

// THE ACCOUNT'S DOMAIN WALK, GUARDED (epic memql#5165, design D9 and D10).
//
// Two rules that a mutation body cannot state, beside the self-archive guard
// and for the same reason its siblings live here: one reads an environment
// value, and both judge a COMBINATION of fields rather than a single one.
//
//  1. `joinOnDomain: true` BEFORE `domainStatus == "verified"`. The flag says
//     "a person arriving with a verified email on this domain joins this
//     account's group". Before ownership is proven, the domain is a string
//     somebody typed -- so accepting the flag would let anyone who can create
//     an account claim `gmail.com` and collect every arriving Gmail user into
//     their group. Refused as `domain_not_verified`, never silently corrected:
//     an operator who set the flag meant to set it, and quietly storing
//     `false` would leave them believing joining is on.
//
//  2. A `memqlDomain` UNDER THIS CLUSTER'S OWN DOMAIN, or equal to one of its
//     front-door hosts. Reserving `os.<domain>` or `api.<domain>` for a client
//     is a request to serve a name the platform already answers on, and the
//     custom-domain policy refuses the same two things for the same reason
//     (platform_custom_domain_policy.go). The cluster's domain comes from the
//     environment, so no mutation body can ask.
//
// NO ESCAPE FOR EITHER, deliberately -- not internal origin, not the cluster
// owner. The reconciler and the seed both write values that PASS these rules,
// so an escape would buy nothing but a way for a future writer to put an
// unservable name into the row and discover it at certificate-issuance time.
//
// A NOTE ON WHAT IS NOT GUARDED HERE. `domainStatus: "verified"` itself is
// writable by the reconciler and by anyone who may write the row, and that is
// not an oversight: `@requiresRank("admin")` on the account mutations is what
// bounds who may write at all, and a client's domain is the operator's record
// to state. The rule this file enforces is narrower and sharper -- that the
// two things which CONSUME verification cannot get ahead of it.

import (
	"context"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/frontdoor"
)

// The typed codes. Written as constants because three places have to agree on
// them: this file, the tests, and the OS keys that copy them (design section
// D's rule for every refusal in this epic).
const (
	accountDomainNotVerifiedCode     = "domain_not_verified"
	accountDomainUnderClusterCode    = "domain_under_cluster_domain"
	accountDomainIsFrontDoorHostCode = "domain_is_front_door_host"
)

// validateAccountDomainPolicy runs both rules against the MERGED payload.
//
// The merged payload, not the delta, for the reason the self-archive guard
// reads it: whatever combination of caller arguments and stored row produced
// this state, the question is the same one -- and a caller who flips
// `joinOnDomain` on a row whose stored status is `unverified` sends a delta
// naming only the flag.
func (e *MemQLEngine) validateAccountDomainPolicy(ctx context.Context, payload map[string]any) error {
	_ = ctx
	if payload == nil {
		return nil
	}
	if err := validateAccountJoinOnDomain(payload); err != nil {
		return err
	}
	return validateAccountMemqlDomain(payload)
}

// validateAccountJoinOnDomain refuses joining before ownership is proven (D9).
func validateAccountJoinOnDomain(payload map[string]any) error {
	joining, present := payload["joinOnDomain"]
	if !present {
		return nil
	}
	on, isBool := joining.(bool)
	if !isBool || !on {
		return nil
	}
	if strings.TrimSpace(stringFromAny(payload["domainStatus"])) == "verified" {
		return nil
	}
	return fmt.Errorf(
		"v1:accounts:account: %s -- joinOnDomain may only be set once domainStatus is \"verified\" "+
			"(epic memql#5165, D9). Until ownership is proven the domain is a string somebody typed, "+
			"and joining on it would place every arriving person with an address on that domain into "+
			"this account's group. Publish the TXT record at _memql-verify.<domain> and let the "+
			"reconciler verify it first",
		accountDomainNotVerifiedCode)
}

// validateAccountMemqlDomain refuses a reserved name this cluster already
// serves (D10).
//
// Reuses the front-door derivation rather than a second list of hosts. One
// derivation, three consumers is the rule the front door already states, and a
// second copy here would disagree exactly when a role is added -- which is the
// moment the check matters.
func validateAccountMemqlDomain(payload map[string]any) error {
	name := strings.ToLower(strings.TrimSpace(stringFromAny(payload["memqlDomain"])))
	if name == "" {
		return nil
	}
	domain := customDomainPolicyDomain()
	if domain == "" {
		// Unreachable through customDomainPolicyDomain, which never returns
		// empty. Stated anyway: an empty domain makes both tests below admit
		// everything, and admitting a name without knowing our own domain is
		// the fail-OPEN direction on a guard whose whole job is to refuse
		// names we already answer on.
		return fmt.Errorf(
			"v1:accounts:account: cannot check memqlDomain %q -- this cluster's own domain did not "+
				"resolve, and admitting a name without one would admit every name",
			name)
	}
	if name == frontdoor.Apex(domain) || strings.HasSuffix(name, frontdoor.DomainDerivationSuffix(domain)) {
		return fmt.Errorf(
			"v1:accounts:account: %s -- %q is under this cluster's own domain (%s), which the platform "+
				"already routes through its one `*.%s` front-door rule. A reserved MemQL name is a "+
				"host this cluster will serve FOR the client and must not collide with a host it "+
				"already serves for itself",
			accountDomainUnderClusterCode, name, domain, domain)
	}
	for _, h := range frontdoor.Hosts(domain) {
		if name == strings.ToLower(strings.TrimSpace(h.Name)) {
			return fmt.Errorf(
				"v1:accounts:account: %s -- %q is this cluster's own %s host. Reserving it would put a "+
					"client's name on the host the platform's own %s surface answers on",
				accountDomainIsFrontDoorHostCode, name, h.Role, h.Role)
		}
	}
	return nil
}
