package memql

import (
	"testing"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	memqlengine "github.com/znasllc-io/memql/component/memql"
)

// The MyAccess grant mapping (epic memql#5165, section H).
//
// The interesting case is STAFF, where account_ids is EMPTY and empty means
// "all" rather than "none" -- a mapping that dropped the flag would be
// indistinguishable on the wire from a caller who belongs to nothing.

func TestApplyAccessGrantCarriesTheStaffFlag(t *testing.T) {
	result := &memqlv1.MyAccessResult{}
	applyAccessGrant(result, memqlengine.AccessGrant{EveryAccount: true})
	if !result.GetEveryAccount() {
		t.Fatal("every_account was dropped -- on the wire a staff caller would be " +
			"indistinguishable from somebody who belongs to nothing")
	}
	if len(result.GetAccountIds()) != 0 {
		t.Fatalf("account_ids = %v, want empty beside every_account", result.GetAccountIds())
	}
}

func TestApplyAccessGrantCarriesGroupsAndScope(t *testing.T) {
	result := &memqlv1.MyAccessResult{}
	applyAccessGrant(result, memqlengine.AccessGrant{
		Groups: []memqlengine.AccessGrantGroup{
			{ID: "acct-acme", Name: "Acme", Kind: "account", AccountID: "acme", AccountName: "Acme Ltd"},
			{ID: "g-1", Name: "Reviewers", Kind: "custom"},
		},
		AccountIDs: []string{"acme"},
	})
	if got := len(result.GetGroups()); got != 2 {
		t.Fatalf("groups = %d, want 2", got)
	}
	first := result.GetGroups()[0]
	if first.GetId() != "acct-acme" || first.GetKind() != "account" || first.GetAccountName() != "Acme Ltd" {
		t.Fatalf("the first group lost fields: %+v", first)
	}
	// A custom group tied to no account carries empty strings rather than
	// being dropped: it is a real membership, and a client that renders
	// groups would otherwise show a person in fewer groups than they are in.
	second := result.GetGroups()[1]
	if second.GetId() != "g-1" || second.GetAccountId() != "" {
		t.Fatalf("the untied group was mangled: %+v", second)
	}
	if len(result.GetAccountIds()) != 1 || result.GetAccountIds()[0] != "acme" {
		t.Fatalf("account_ids = %v", result.GetAccountIds())
	}
	if result.GetEveryAccount() {
		t.Fatal("every_account is set for a non-staff caller")
	}
}

func TestApplyAccessGrantOnAnEmptyGrant(t *testing.T) {
	// The negative control: a caller in no groups must produce an EMPTY
	// list rather than a nil the mapping never touched -- otherwise the
	// tests above could pass against a function that only ever appends.
	result := &memqlv1.MyAccessResult{}
	applyAccessGrant(result, memqlengine.AccessGrant{})
	if result.GetGroups() == nil {
		t.Fatal("groups is nil rather than an empty list")
	}
	if len(result.GetGroups()) != 0 || len(result.GetAccountIds()) != 0 || result.GetEveryAccount() {
		t.Fatalf("an empty grant produced %+v", result)
	}
	// And a nil result is not a panic.
	applyAccessGrant(nil, memqlengine.AccessGrant{EveryAccount: true})
}
