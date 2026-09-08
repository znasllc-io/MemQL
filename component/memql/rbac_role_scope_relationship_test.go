package memql

import (
	"testing"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
)

// v1:rbac:role's `scopedTo` EDGE POINTS AT THE ACCOUNTS DOMAIN (epic
// memql#5166, D10).
//
// WHY THIS NEEDS A TEST AT ALL. A relationship's `target=` is a BARE NAME, and
// the concept registry is FLAT: two domains declare a concept called `account`
// -- `v1:accounts:account` (the operator's customer record) and
// `v1:identity:account` -- and a bare name resolves first-wins across the whole
// registry. What disambiguates is the file-top `use accounts.concepts.{ account }`
// in dsl/rbac/concepts.memql.
//
// DELETE THAT IMPORT AND THE EDGE STILL LOADS. It resolves to the other
// `account`, silently, and the failure surfaces nowhere: the stored value and
// any filter argument canonicalize the same wrong way and therefore agree with
// each other, so a scoped role would keep working right up until something
// walked the edge or compared it against a row written by the accounts app.
//
// The same shadowing already sits in dsl/identity/concepts.memql, where
// `invitation.accountId` canonicalizes under `v1:identity:account` -- harmless
// there for the reason above, and left alone deliberately. This asserts the
// half that is NOT harmless: a role's scope is a customer of this operator, and
// auth.MayAssignRole refuses the role to anybody outside it.
func TestRoleScopeTargetsTheAccountsDomain(t *testing.T) {
	if _, err := LoadUnifiedConcepts(nil); err != nil {
		t.Fatalf("LoadUnifiedConcepts: %v", err)
	}
	c, err := memorynodes.Get(conceptRbacRole)
	if err != nil || c == nil {
		t.Fatalf("no %s concept: %v", conceptRbacRole, err)
	}

	var found bool
	for _, rel := range c.Relationships {
		if rel.Field != "accountId" {
			continue
		}
		found = true
		if rel.TargetConcept != "v1:accounts:account" {
			t.Errorf("role.accountId targets %q, want v1:accounts:account.\n"+
				"A bare `target=account` resolves first-wins across a FLAT registry and two "+
				"domains declare one. The file-top `use accounts.concepts.{ account }` in "+
				"dsl/rbac/concepts.memql is what picks the right one; without it the edge still "+
				"loads, pointing somewhere else, and nothing says so.",
				rel.TargetConcept)
		}
		if rel.As != "scopedTo" {
			t.Errorf("role.accountId is labelled %q, want scopedTo -- the domain verb is what "+
				"the edge MEANS, and `references` alone says only what the engine does with it",
				rel.As)
		}
	}
	if !found {
		t.Fatal("v1:rbac:role declares no relationship on accountId. D10 scopes a role to an " +
			"account, and the edge is how that scope is a fact about the graph rather than a " +
			"string somebody agreed to interpret.")
	}
}
