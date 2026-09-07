package node

import "testing"

// The OS keeps a live feed of readiness rows on every replica, and the rows
// are written by whichever node evaluated. Without these rules default-deny
// leaves the feed correct on load and frozen after, which looks like it works.
func TestModuleReadinessRowsBroadcast(t *testing.T) {
	for _, topic := range []string{
		"graph.node.created.v1:platform:moduleReadiness",
		"graph.node.updated.v1:platform:moduleReadiness",
	} {
		d := evaluateRouting(defaultRoutingRules(), topic)
		if !d.Forward || !d.Broadcast {
			t.Errorf("%s: want forward+broadcast, got %+v", topic, d)
		}
	}
	// The negative control: a platform concept with no rule stays local.
	if d := evaluateRouting(defaultRoutingRules(), "graph.node.created.v1:platform:moduleVerdict"); d.Forward {
		t.Errorf("moduleVerdict is virtual and must not be routed, got %+v", d)
	}
}
