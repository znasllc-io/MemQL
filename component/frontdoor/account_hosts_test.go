package frontdoor

import (
	"strings"
	"testing"
)

func TestAccountHostsAreTheThreeLabelsInOrder(t *testing.T) {
	got := AccountHosts("memql.acme.com")
	want := []AccountHost{
		{Role: AccountRoleApp, Name: "app.memql.acme.com"},
		{Role: AccountRoleAPI, Name: "api.memql.acme.com"},
		{Role: AccountRoleID, Name: "id.memql.acme.com"},
	}
	if len(got) != len(want) {
		t.Fatalf("AccountHosts returned %d hosts, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("host %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// An empty reservation must produce NO hosts. The alternative -- three
// single-label names -- is refused by every guard downstream, but only after
// it has been written to a row, offered to an operator as DNS records to
// create, and counted as a pointing check that can never pass.
func TestAnUnheldReservationHasNoHosts(t *testing.T) {
	if got := AccountHosts(""); got != nil {
		t.Errorf("AccountHosts(\"\") = %+v, want nil", got)
	}
	if got := AccountCertificateSANs(""); len(got) != 0 {
		t.Errorf("AccountCertificateSANs(\"\") = %v, want empty", got)
	}
}

// The certificate carries all three names, which is what makes activation
// all-or-nothing (design D8): an HTTP-01 order cannot go Ready unless every
// dnsName in it solves.
func TestTheCertificateNamesAllThreeHosts(t *testing.T) {
	sans := AccountCertificateSANs("memql.acme.com")
	hosts := AccountHosts("memql.acme.com")
	if len(sans) != len(hosts) {
		t.Fatalf("%d SANs for %d hosts -- every host must be certificated", len(sans), len(hosts))
	}
	for i, h := range hosts {
		if sans[i] != h.Name {
			t.Errorf("SAN %d = %q, want %q (SAN order must follow host order)", i, sans[i], h.Name)
		}
	}
	for _, s := range sans {
		if strings.Contains(s, "*") {
			t.Errorf("SAN %q is a wildcard: HTTP-01 cannot issue one, and one wildcard dnsName fails the whole order (memql#4224)", s)
		}
	}
}

// The account labels are deliberately NOT the cluster's own (design D3/D5, and
// the file's own doc comment). This pins the divergence so that "tidying" one
// set to match the other is a failing test rather than a silent product
// change under a client's brand.
func TestAccountLabelsAreNotTheClustersOwn(t *testing.T) {
	accountLabels := map[string]bool{}
	for _, r := range AccountRoles() {
		accountLabels[string(r)] = true
	}
	if !accountLabels["app"] || !accountLabels["id"] {
		t.Fatalf("account roles are %v; the design fixes them as app/api/id because these hosts appear under a CLIENT's brand", AccountRoles())
	}
	if accountLabels[string(RoleIdentity)] {
		t.Errorf("account roles must not carry the cluster's %q label -- a client's employees read %q", RoleIdentity, AccountRoleID)
	}
	if accountLabels[OsSite] {
		t.Errorf("account roles must not carry the cluster's %q label -- a client's employees read %q", OsSite, AccountRoleApp)
	}
	// api is the one label both sets share, and that is not an accident worth
	// breaking: it is what a person expects on both.
	if !accountLabels[string(RoleAPI)] {
		t.Errorf("account roles lost the shared %q label", RoleAPI)
	}
}

// A reserved name is a multi-label domain of the CLIENT's, never one of ours.
// Composing it must not accidentally reach for the cluster domain.
func TestAccountHostsComposeOnlyTheReservedName(t *testing.T) {
	const reserved = "memql.acme.com"
	for _, h := range AccountHosts(reserved) {
		if !strings.HasSuffix(h.Name, "."+reserved) {
			t.Errorf("host %q is not under the reserved name %q", h.Name, reserved)
		}
		if strings.Count(h.Name, ".") != strings.Count(reserved, ".")+1 {
			t.Errorf("host %q adds more than one label to %q", h.Name, reserved)
		}
	}
}

// The strip and the compose are inverses, pinned against each other so the
// label lives in exactly one place. Without this, changing AccountRoleApp
// would leave the edge stripping a prefix nothing produces -- and the symptom
// is every client's front door resolving to nothing, with no error anywhere.
func TestTheAppHostRoundTrips(t *testing.T) {
	for _, reserved := range []string{"memql.acme.com", "memql.a-very-long-client-name.co.uk", "portal.acme.com"} {
		host := AccountRoleHost(AccountRoleApp, reserved)
		got, ok := AccountReservedNameFromAppHost(host)
		if !ok {
			t.Errorf("AccountReservedNameFromAppHost(%q) reported no match for a host this package composed", host)
			continue
		}
		if got != reserved {
			t.Errorf("round trip of %q gave %q", reserved, got)
		}
	}
}

func TestOnlyTheAppLabelResolvesADoor(t *testing.T) {
	// api. and id. are routed by Ingress straight to the bff and the identity
	// service; a host arriving at the EDGE under either label is a request
	// that should 404, not a door.
	for _, role := range []AccountRole{AccountRoleAPI, AccountRoleID} {
		host := AccountRoleHost(role, "memql.acme.com")
		if got, ok := AccountReservedNameFromAppHost(host); ok {
			t.Errorf("AccountReservedNameFromAppHost(%q) resolved to %q; only the %q label may", host, got, AccountRoleApp)
		}
	}

	for _, host := range []string{
		"memql.acme.com",   // the bare reserved name: the door serves BENEATH it, never at it
		"app.localhost",    // one label after the prefix: no reservation produces this
		"app.",             // degenerate
		"app",              // the label alone
		"",                 // nothing
		"notapp.acme.com",  // a name that merely ends in the label
		"www.app.acme.com", // the label in the wrong position
	} {
		if got, ok := AccountReservedNameFromAppHost(host); ok {
			t.Errorf("AccountReservedNameFromAppHost(%q) resolved to %q, want no match", host, got)
		}
	}
}
