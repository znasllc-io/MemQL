package memql

import (
	"context"
	"strings"
	"testing"
)

// The account domain guards (epic memql#5165, D9 and D10).
//
// Every case asserts the TYPED CODE rather than the sentence: the codes are
// what the OS keys its copy on, so a refusal that changed its wording would
// still be recognised and one that changed its code would not.

func TestJoinOnDomainIsRefusedBeforeVerification(t *testing.T) {
	for _, status := range []string{"", "unverified", "verifying"} {
		payload := map[string]any{"joinOnDomain": true, "domainStatus": status}
		err := validateAccountJoinOnDomain(payload)
		if err == nil {
			t.Fatalf("domainStatus %q with joinOnDomain: want a refusal, got nil", status)
		}
		if !strings.Contains(err.Error(), accountDomainNotVerifiedCode) {
			t.Fatalf("domainStatus %q: error %q does not name %q", status, err, accountDomainNotVerifiedCode)
		}
	}
}

func TestJoinOnDomainIsAdmittedOnceVerified(t *testing.T) {
	// The positive control. Without it every assertion above could pass
	// against a guard that refuses unconditionally.
	if err := validateAccountJoinOnDomain(map[string]any{"joinOnDomain": true, "domainStatus": "verified"}); err != nil {
		t.Fatalf("verified + joinOnDomain: want admit, got %v", err)
	}
	// And the flag OFF is never the guard's business, at any status.
	for _, status := range []string{"", "unverified", "verifying", "verified"} {
		if err := validateAccountJoinOnDomain(map[string]any{"joinOnDomain": false, "domainStatus": status}); err != nil {
			t.Fatalf("joinOnDomain false at %q: want admit, got %v", status, err)
		}
	}
	// An absent flag is not a false one, and neither is refused.
	if err := validateAccountJoinOnDomain(map[string]any{"domainStatus": "unverified"}); err != nil {
		t.Fatalf("absent joinOnDomain: want admit, got %v", err)
	}
}

func TestMemqlDomainIsRefusedUnderTheClusterDomain(t *testing.T) {
	t.Setenv(memqlDomainEnv, "memql.localhost")
	for _, name := range []string{"memql.memql.localhost", "acme.memql.localhost", "memql.localhost"} {
		err := validateAccountMemqlDomain(map[string]any{"memqlDomain": name})
		if err == nil {
			t.Fatalf("memqlDomain %q: want a refusal, got nil", name)
		}
		if !strings.Contains(err.Error(), accountDomainUnderClusterCode) {
			t.Fatalf("memqlDomain %q: error %q does not name %q", name, err, accountDomainUnderClusterCode)
		}
	}
}

func TestMemqlDomainIsRefusedForAFrontDoorHost(t *testing.T) {
	t.Setenv(memqlDomainEnv, "example.test")
	// api.<domain> is a front-door host AND under the cluster domain. The
	// under-domain test runs first, so what this asserts is that the host
	// list is consulted at all -- with a name that is a front-door host
	// without being under the derivation suffix.
	//
	// Every front-door host IS a single label under the domain (the routing
	// fact the front door records), so there is no such name: the two rules
	// overlap completely and the under-domain refusal is what an operator
	// sees. Asserting the overlap is the honest test, because claiming the
	// host branch fires when it cannot would be a test that measures nothing.
	err := validateAccountMemqlDomain(map[string]any{"memqlDomain": "api.example.test"})
	if err == nil {
		t.Fatal("a front-door host: want a refusal, got nil")
	}
	if !strings.Contains(err.Error(), accountDomainUnderClusterCode) &&
		!strings.Contains(err.Error(), accountDomainIsFrontDoorHostCode) {
		t.Fatalf("a front-door host: error %q names neither refusal code", err)
	}
}

func TestMemqlDomainAdmitsAClientsOwnName(t *testing.T) {
	t.Setenv(memqlDomainEnv, "example.test")
	for _, name := range []string{"memql.acme.com", "workspace.acme.co.uk", ""} {
		if err := validateAccountMemqlDomain(map[string]any{"memqlDomain": name}); err != nil {
			t.Fatalf("memqlDomain %q: want admit, got %v", name, err)
		}
	}
}

func TestAccountDomainPolicyRunsBothRules(t *testing.T) {
	t.Setenv(memqlDomainEnv, "example.test")
	eng := &MemQLEngine{}
	ctx := context.Background()

	if err := eng.validateAccountDomainPolicy(ctx, map[string]any{
		"domainStatus": "verified",
		"memqlDomain":  "memql.acme.com",
		"joinOnDomain": true,
	}); err != nil {
		t.Fatalf("a fully legal row: want admit, got %v", err)
	}
	if err := eng.validateAccountDomainPolicy(ctx, map[string]any{
		"domainStatus": "verified",
		"memqlDomain":  "os.example.test",
	}); err == nil {
		t.Fatal("a reserved name on a verified account: want a refusal, got nil")
	}
	if err := eng.validateAccountDomainPolicy(ctx, nil); err != nil {
		t.Fatalf("a nil payload: want admit, got %v", err)
	}
}
