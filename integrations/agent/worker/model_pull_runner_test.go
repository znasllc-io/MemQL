//go:build agent

package worker

import (
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/events"
	workerservice "github.com/znasllc-io/memql/component/worker"
)

// ===========================================================================
// THE CLAIM
// ===========================================================================
// Exactly one agent replica may act on a pull row. These pin the three ways
// that could go wrong: nobody acting, everybody acting, and the wrong one
// acting.

func TestOnlyTheNamedReplicaClaimsAPull(t *testing.T) {
	row := modelPullRow{PullId: "p1", TargetNodeId: "agent-1"}

	if !claimedBy(row, "agent-1") {
		t.Fatal("the named replica must claim its own row")
	}
	if claimedBy(row, "agent-0") {
		t.Fatal("a replica that is not named must not claim the row -- two replicas would download the same model onto the same disk")
	}
}

// A row naming no replica is claimed by NOBODY. Reading a blank as "anyone may
// take it" is the failure this pins: it would put every replica in the fleet on
// the same forty-gigabyte download.
func TestARowNamingNoReplicaIsClaimedByNobody(t *testing.T) {
	row := modelPullRow{PullId: "p1", TargetNodeId: ""}
	for _, self := range []string{"agent-0", "agent-1", ""} {
		if claimedBy(row, self) {
			t.Fatalf("replica %q claimed a row that names none", self)
		}
	}
}

// A replica with no node id of its own claims nothing. Fail-closed: the
// alternative is a single-node cluster behaving differently from a two-replica
// one at exactly the point where that difference is invisible.
func TestAReplicaWithNoNodeIdClaimsNothing(t *testing.T) {
	if claimedBy(modelPullRow{TargetNodeId: "agent-1"}, "") {
		t.Fatal("a replica with no MEMQL_NODE_ID claimed a row")
	}
}

// ===========================================================================
// THE EVENT
// ===========================================================================

func TestAPullEventIsReadFromTheGraphEnvelope(t *testing.T) {
	row, ok := modelPullRowFromEvent(events.Event{Payload: map[string]any{
		"id": "pull-1",
		"payload": map[string]any{
			"ownerUserId":  "v1:identity:user:alice",
			"workerId":     "reg-1",
			"model":        "llama3.1:8b",
			"status":       "requested",
			"targetNodeId": "agent-1",
		},
	}})
	if !ok {
		t.Fatal("a well-formed pull event was rejected")
	}
	if row.PullId != "pull-1" || row.WorkerId != "reg-1" || row.Model != "llama3.1:8b" {
		t.Fatalf("row = %+v", row)
	}
	if row.TargetNodeId != "agent-1" || row.OwnerUserId != "v1:identity:user:alice" {
		t.Fatalf("the claim and the owner must both survive the envelope: %+v", row)
	}
}

// A flattened envelope reads identically. Both shapes reach a handler
// depending on which path an event came through, and picking one would fail
// silently on the other -- a pull nobody ever starts, with no error anywhere.
func TestAFlattenedPullEventReadsTheSame(t *testing.T) {
	row, ok := modelPullRowFromEvent(events.Event{Payload: map[string]any{
		"id":           "pull-2",
		"ownerUserId":  "v1:identity:user:alice",
		"workerId":     "reg-1",
		"model":        "llama3.1:8b",
		"status":       "requested",
		"targetNodeId": "agent-1",
	}})
	if !ok || row.TargetNodeId != "agent-1" || row.Model != "llama3.1:8b" {
		t.Fatalf("flattened envelope read as %+v (ok=%v)", row, ok)
	}
}

// An event missing anything the runner needs is ignored rather than
// half-acted-on: a pull with no owner cannot be written under any authority,
// and one with no model has nothing to ask the machine for.
func TestAnIncompletePullEventIsIgnored(t *testing.T) {
	cases := map[string]map[string]any{
		"no owner":  {"id": "p", "workerId": "w", "model": "m"},
		"no worker": {"id": "p", "ownerUserId": "u", "model": "m"},
		"no model":  {"id": "p", "ownerUserId": "u", "workerId": "w"},
		"no id":     {"ownerUserId": "u", "workerId": "w", "model": "m"},
	}
	for name, payload := range cases {
		if _, ok := modelPullRowFromEvent(events.Event{Payload: payload}); ok {
			t.Errorf("%s: an incomplete event was accepted", name)
		}
	}
}

// ===========================================================================
// THE THROTTLE
// ===========================================================================

// Two observations inside the interval that say the SAME thing produce one
// write. This is the whole reason the throttle exists: a runtime emits several
// a second, and a row version each is tens of thousands of versions -- on an
// append-only, broadcast concept.
func TestTheThrottleCollapsesRepeatedObservations(t *testing.T) {
	tr := &progressThrottle{}
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	p := workerservice.ModelPullProgress{Layer: "a", Status: "pulling a", CompletedBytes: 1}

	if !tr.admit(p, base) {
		t.Fatal("the first observation must always write")
	}
	p.CompletedBytes = 2
	if tr.admit(p, base.Add(200*time.Millisecond)) {
		t.Fatal("a second observation of the same step inside the interval must not write")
	}
	p.CompletedBytes = 3
	if !tr.admit(p, base.Add(PullProgressInterval+time.Millisecond)) {
		t.Fatal("an observation past the interval must write")
	}
}

// ===========================================================================
// A CHANGED STEP ALWAYS WRITES, WHATEVER THE CLOCK SAYS
// ===========================================================================
// The layer boundary is exactly where the counters jump backwards, and exactly
// where a person staring at a bar needs the new status line to explain why. A
// pure interval throttle would sit on that transition and leave the bar
// looking broken for up to two seconds with no words beside it.
func TestTheThrottleNeverSitsOnALayerChange(t *testing.T) {
	tr := &progressThrottle{}
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

	if !tr.admit(workerservice.ModelPullProgress{Layer: "a", Status: "pulling a", CompletedBytes: 999}, base) {
		t.Fatal("the first observation must write")
	}
	// A millisecond later, a different blob: the counter resets to 5.
	if !tr.admit(workerservice.ModelPullProgress{Layer: "b", Status: "pulling b", CompletedBytes: 5}, base.Add(time.Millisecond)) {
		t.Fatal("a new layer must write immediately, however recently the last write was")
	}
	// And a phase that is not a download at all.
	if !tr.admit(workerservice.ModelPullProgress{Status: "verifying sha256 digest"}, base.Add(2*time.Millisecond)) {
		t.Fatal("a new status line must write immediately")
	}
}
