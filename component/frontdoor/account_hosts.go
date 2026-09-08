package frontdoor

import "strings"

// The hosts served under an ACCOUNT's reserved MemQL name (epic memql#5168).
//
// A client whose own domain is `acme.com` reserves `memql.acme.com` on this
// cluster, and three hosts are served beneath it:
//
//	app.memql.acme.com   the OS shell           -> the edge node
//	api.memql.acme.com   the engine's API edge  -> bff (h2c) + bff-http
//	id.memql.acme.com    sign-in                -> the identity node
//
// # THE LABELS DIVERGE FROM THE CLUSTER'S OWN, AND THAT IS A DECISION
//
// The cluster serves `os.` / `api.` / `identity.` under its own domain
// (Hosts, above). These are `app.` / `api.` / `id.` -- deliberately not the
// same two of the three, and the design record (D of the access program,
// docs/superpowers/specs/2026-09-08-per-account-front-door-design.md) says why:
// these hosts appear under a CLIENT's brand and are read by their employees,
// who have never heard of MemQL OS. `app` and `id` are what a person expects
// to find there. The cluster's own labels do not change, and neither set is
// derived from the other.
//
// # WHY THIS IS NOT A SECOND FRONT-DOOR DERIVATION
//
// It very nearly would be, and the distinction is the whole reason the design
// was its own session. `MEMQL_DOMAIN` remains the ONLY input to Hosts,
// CertificateSANs and component/envregistry's env derivation: a reserved name
// never enters a rendered overlay, never changes an env value, and never
// widens the issuer. What it does is put four more Ingress objects and one
// more Certificate into the cluster AT RUNTIME, through the reconciler path
// that already serves a client's own domain (integrations/customdomain).
//
// So this file is not a second copy of the host set -- it is the host set for
// a different, runtime-supplied domain, and the two never mix. Nothing here is
// read by cmd/frontdoorhosts.
//
// # ONE SLICE, FIVE CONSUMERS
//
// AccountHosts is the ONE place these three labels are spelled. The
// certificate's SAN list, the Ingress set the bind script applies, the
// per-host pointing checks the reconciler runs, the map the OS rail renders
// and the edge's `app.` resolution all walk it. A second spelling anywhere is
// a certificate for a host nothing routes, or a rule with no SAN behind it --
// the memql#4224 failure, one domain over.

// AccountRole is the role of one host under an account's reserved name.
//
// CLOSED, like Roles. Adding one is a design change: it is a fourth name to
// point at the cluster, a fourth SAN on an ACME order that is already
// all-or-nothing, and a fourth thing an operator has to get right before a
// client's front door comes up at all.
type AccountRole string

const (
	// AccountRoleApp is the OS shell under the client's name. It resolves to
	// the same `os` site row every cluster serves -- one shell, one product,
	// with the account in context (design D4). It is NOT a site of its own.
	AccountRoleApp AccountRole = "app"
	// AccountRoleAPI is the engine's API edge under the client's name: the
	// same gRPC h2c catch-all and the same generated HTTP path block the
	// cluster's own `api.` host carries, byte for byte (design D5).
	AccountRoleAPI AccountRole = "api"
	// AccountRoleID is sign-in under the client's name.
	//
	// The TOKEN it mints still says `iss = https://identity.<cluster-domain>`
	// (design D3): one identity service, one keyset, one issuer, so
	// component/identity/verifier is untouched. What is per-name is only what
	// a browser sees -- the sign-in origin, the WebAuthn RP id, the redirect
	// URI and the CORS origin.
	AccountRoleID AccountRole = "id"
)

// AccountRoles is the closed set, in the order every consumer walks it: the
// shell first, because it is the one a person types.
func AccountRoles() []AccountRole {
	return []AccountRole{AccountRoleApp, AccountRoleAPI, AccountRoleID}
}

// AccountRoleHost is `<role>.<reservedName>`.
func AccountRoleHost(role AccountRole, reservedName string) string {
	return string(role) + "." + reservedName
}

// AccountHost is one host served under a reserved name.
type AccountHost struct {
	// Role is which of the three this is.
	Role AccountRole
	// Name is the hostname itself.
	Name string
}

// AccountHosts is the whole host set for one reserved name, in AccountRoles
// order.
//
// An EMPTY reservedName returns nil rather than three hosts named `app.`,
// `api.` and `id.`. A reservation that is not held must produce no hosts at
// all: three single-label names would be refused by every guard downstream,
// but only after they had been written to a row, offered to an operator as
// records to create, and counted as a pointing check that can never pass.
func AccountHosts(reservedName string) []AccountHost {
	if reservedName == "" {
		return nil
	}
	roles := AccountRoles()
	out := make([]AccountHost, 0, len(roles))
	for _, r := range roles {
		out = append(out, AccountHost{Role: r, Name: AccountRoleHost(r, reservedName)})
	}
	return out
}

// AccountCertificateSANs is the SAN set one reserved name's Certificate must
// carry: all three hosts, in AccountHosts order.
//
// ALL THREE ON ONE CERTIFICATE, AND THAT IS THE ACTIVATION RULE (design D8).
// A front door goes live only when all three names point at the cluster, and
// this is what enforces it -- an HTTP-01 order cannot go Ready unless every
// dnsName in it solves, so the certificate IS the all-or-nothing check rather
// than a rule the reconciler would have to police. One order, one rate-limit
// unit against Let's Encrypt, one Ready condition to promote on.
//
// No wildcard appears here for the same reason it does not in
// CertificateSANs: these are three exact hosts, which is precisely what
// HTTP-01 can issue.
func AccountCertificateSANs(reservedName string) []string {
	hosts := AccountHosts(reservedName)
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, h.Name)
	}
	return out
}

// AccountReservedNameFromAppHost is the inverse of
// AccountRoleHost(AccountRoleApp, name): it recovers the reserved name from
// the `app.` host the edge was asked for, and reports whether the host was one
// at all.
//
// # WHY THE EDGE STRIPS RATHER THAN THE QUERY COMPOSING
//
// A row stores `memql.acme.com`, not `app.memql.acme.com`, and a DSL filter
// compiles to SQL over stored fields -- it cannot prepend a label. So one side
// has to do the composition, and doing it HERE keeps the pair together: the
// label appears once, in AccountRoleApp, and a round-trip test pins the two
// functions against each other. The alternative, a denormalized `appHost`
// column, is a second spelling that drifts the first time the label changes.
//
// ONLY the `app.` label is recognised, and that is not an omission. `api.` and
// `id.` are routed by Ingress straight to the bff and the identity service and
// never reach the edge at all, so a host arriving here under either of those
// labels is not a front door being resolved -- it is a request that should
// 404, exactly as it does today.
//
// A bare reserved name with no label ("memql.acme.com") is NOT a match: the
// front door serves three hosts beneath the name and nothing at the name
// itself, and answering there would be the cluster claiming an apex nobody
// asked it to serve.
func AccountReservedNameFromAppHost(host string) (string, bool) {
	name, ok := strings.CutPrefix(host, string(AccountRoleApp)+".")
	if !ok || name == "" {
		return "", false
	}
	// A reserved name is a client's own multi-label domain. One label after
	// the prefix would mean the host was `app.<tld>`, which no reservation can
	// produce and no guard would have admitted.
	if !strings.Contains(name, ".") {
		return "", false
	}
	return name, true
}
