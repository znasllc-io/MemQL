package groups

import (
	"context"
	"strings"
	"testing"
)

// Domain join (epic memql#5165, D9). Every refusal path returns a no-op rather
// than an error, so the tests assert the ABSENCE of a write as well as the
// absence of a group id -- a function that returned "" while still writing
// would pass a check on the return value alone.

func joinFixture(t *testing.T) (*Integration, *stubEngine) {
	t.Helper()
	i, stub := newFixture(t)
	stub.joinDomains = map[string]string{"acme.com": "acme"}
	return i, stub
}

func TestDomainJoinPlacesAVerifiedArrival(t *testing.T) {
	i, stub := joinFixture(t)
	groupID, err := i.ApplyDomainJoin(context.Background(), "u-new", "someone@acme.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if groupID != "acct-acme" {
		t.Fatalf("joined %q, want %q", groupID, "acct-acme")
	}
	writes := stub.writesNamed("writeGroupMembership")
	if len(writes) != 1 {
		t.Fatalf("want one membership write, got %d", len(writes))
	}
	if !strings.Contains(writes[0], `origin: "domain"`) {
		t.Fatalf("the membership does not record how they got here: %s", writes[0])
	}
	// addedBy is EMPTY: no person did this, and stamping a synthetic id
	// would put a principal that does not exist into the answer to "who let
	// them in".
	if !strings.Contains(writes[0], `addedBy: ""`) {
		t.Fatalf("the membership names an actor that does not exist: %s", writes[0])
	}
}

func TestDomainJoinIsANoOpOnAnUnverifiedAddress(t *testing.T) {
	// The sharpest of the three conditions: an unverified claim is a string
	// a provider did not check, and joining on it is the same class of
	// mistake as LINKING on it.
	i, stub := joinFixture(t)
	groupID, err := i.ApplyDomainJoin(context.Background(), "u-new", "someone@acme.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if groupID != "" {
		t.Fatalf("joined %q on an unverified address", groupID)
	}
	if len(stub.writesNamed("writeGroupMembership")) != 0 {
		t.Fatal("an unverified address still wrote a membership")
	}
}

func TestDomainJoinIsANoOpWhenNoAccountClaimsTheDomain(t *testing.T) {
	i, stub := joinFixture(t)
	for _, email := range []string{"someone@other.com", "someone@sub.acme.com", "", "nobody", "trailing@"} {
		groupID, err := i.ApplyDomainJoin(context.Background(), "u-new", email, true)
		if err != nil {
			t.Fatalf("%q: %v", email, err)
		}
		if groupID != "" {
			t.Fatalf("%q joined %q", email, groupID)
		}
	}
	if len(stub.writesNamed("writeGroupMembership")) != 0 {
		t.Fatal("an unmatched domain still wrote a membership")
	}
}

func TestDomainJoinIsIdempotentByTheDerivedId(t *testing.T) {
	// Re-running writes a new VERSION at the same id rather than a second
	// row, which is what makes it safe to call on every arrival.
	i, stub := joinFixture(t)
	for n := 0; n < 3; n++ {
		if _, err := i.ApplyDomainJoin(context.Background(), "u-new", "someone@acme.com", true); err != nil {
			t.Fatal(err)
		}
	}
	writes := stub.writesNamed("writeGroupMembership")
	if len(writes) != 3 {
		t.Fatalf("want three writes, got %d", len(writes))
	}
	for n, w := range writes {
		if !strings.Contains(w, `membershipId: "acct-acme-u-new"`) {
			t.Fatalf("write %d is not at the derived id: %s", n, w)
		}
	}
}

func TestDomainJoinRefusesWhenTheAccountsGroupIsGone(t *testing.T) {
	// The account is verified and joining, but its group is archived -- an
	// account archived between the read and here. Joining somebody to a
	// group that grants nothing is worse than not joining them: it reads as
	// success on every screen.
	i, stub := joinFixture(t)
	stub.groups["acct-acme"]["status"] = StatusArchived
	groupID, err := i.ApplyDomainJoin(context.Background(), "u-new", "someone@acme.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if groupID != "" {
		t.Fatalf("joined %q to an archived group", groupID)
	}
	if len(stub.writesNamed("writeGroupMembership")) != 0 {
		t.Fatal("an archived group still gained a member")
	}
}

func TestEmailDomainReadsTheLastAt(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a@acme.com", "acme.com"},
		{"A@ACME.COM", "acme.com"},
		{"  a@acme.com  ", "acme.com"},
		// A local part may legally contain an `@` in quoted form. Splitting
		// on the FIRST would read this as domain `b"@acme.com`, which
		// matches nothing -- a miss rather than a wrong join, but a miss
		// nobody would be able to explain.
		{`"a@b"@acme.com`, "acme.com"},
		{"noat", ""},
		{"trailing@", ""},
		{"", ""},
	} {
		if got := emailDomain(tc.in); got != tc.want {
			t.Fatalf("emailDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
