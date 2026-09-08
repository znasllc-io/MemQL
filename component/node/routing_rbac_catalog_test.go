package node

import "testing"

// THE ROLE CATALOG REACHES EVERY REPLICA (epic memql#5166).
//
// A role row is written on whichever replica served the builtin, and read by
// every gate on every replica -- the data-plane capability gate, the row gate's
// rank ladder, @requiresRank and @requiresCapability all resolve through the
// catalog these rows build. Default-deny would keep the event on the writer, so
// the new role holds its grants there and nothing anywhere else.
//
// The symptom is a permission that applies to roughly half the requests, with
// every replica reporting healthy: the person who created the role watches it
// work on one page and refuse on the next.
func TestRbacCatalogRowsBroadcast(t *testing.T) {
	for _, topic := range []string{
		"graph.node.created.v1:rbac:role",
		"graph.node.updated.v1:rbac:role",
		"graph.node.created.v1:rbac:capability",
		"graph.node.updated.v1:rbac:capability",
	} {
		d := evaluateRouting(defaultRoutingRules(), topic)
		if !d.Forward || !d.Broadcast {
			t.Errorf("%s: want forward+broadcast, got %+v", topic, d)
		}
	}

	// THE REACHABLE NEGATIVE. Without it a rule broadened to `v1:rbac:*` --
	// or to `graph.node.#` -- would pass every assertion above while forwarding
	// far more than this epic checked was safe to forward.
	if d := evaluateRouting(defaultRoutingRules(), "graph.node.created.v1:identity:authSession"); d.Forward {
		t.Errorf("an unrelated identity concept must stay local under default-deny, got %+v", d)
	}
}
