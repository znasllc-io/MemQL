package node

import (
	"context"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/events"
)

// newPlannerConsumerDelivery builds a executing replica-side RunDelivery wired to a
// shared store + bus sink, with the run-loop context primed + the durable
// consumer subscription opened so the route-gap replay path works without going
// through the full component lifecycle. Mirrors newConsumerDelivery for chat.
func newPlannerConsumerDelivery(t *testing.T, ctx context.Context, nodeID string, store outboxStore, sink *busSink) *RunDelivery {
	t.Helper()
	d := NewRunDelivery(
		&Identity{ID: nodeID, Type: NodeTypePlanner},
		NewSubstrate(store, time.Minute, nil, nil),
		events.NewBus(),
		true, // runsSteps
		nil,
	)
	if d == nil {
		t.Fatal("NewRunDelivery returned nil")
	}
	d.republish = sink.publish
	d.ctx = ctx
	d.startConsumer(ctx)
	return d
}

// newPlanProducer builds a producer-only RunDelivery (e.g. the BFF that writes
// new Plans, or the executing replica writing its own status transitions on another node).
func newPlanProducer(t *testing.T, nodeID string, nodeType NodeType, store outboxStore) *RunDelivery {
	t.Helper()
	p := NewRunDelivery(
		&Identity{ID: nodeID, Type: nodeType},
		NewSubstrate(store, time.Minute, nil, nil),
		events.NewBus(),
		false, // producer-only
		nil,
	)
	if p == nil {
		t.Fatal("producer NewRunDelivery returned nil")
	}
	return p
}

// TestPlanCrossReplicaDeliveryViaRouteGap is the focused proof for memql#1495: a
// plan-created event PRODUCED ON THE BFF reaches the executing replica replica that
// consumes plan lifecycle events, even when there is NO mesh route between them
// -- the exact route-gap (peer-table decay, #1388) where the mesh fast-path
// broadcast silently drops and only the #1389 DB-poll watchdog otherwise
// recovers.
//
// It mirrors the chat-reply cross-replica proof in-process: producer node (bff)
// and consumer node (executing replica) share ONLY the durable outbox store -- there is no
// mesh connection between them. On the pre-#1495 path the plan-created event
// lands on the bff's local bus, rides the mesh broadcast, and -- with the
// executing replica routeless -- never reaches the executing replica's HandlePlanCreated subscriber,
// so the Plan strands. With RunDelivery the executing replica subscribes to the durable
// plan-lifecycle stream and catches the event on cursor-pull replay.
func TestPlanCrossReplicaDeliveryViaRouteGap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// One durable store, shared across replicas (the Postgres backbone). No mesh
	// link between producer (bff) and consumer (executing replica) -- the substrate is the
	// only path, the #1388 route-gap.
	store := newFakeOutboxStore()

	// Consumer: executing replica-2 subscribes to the durable plan-lifecycle stream and
	// fans it back onto its local bus, where HandlePlanCreated already consumes.
	sink := &busSink{}
	consumer := newPlannerConsumerDelivery(t, ctx, "executing replica-2", store, sink)
	_ = consumer

	// Producer: a brand-new Plan is created on the bff (createPlan),
	// which durably Publishes it via RunDelivery while routeless to executing replica-2.
	producer := newPlanProducer(t, "bff-1", NodeTypeBFF, store)
	createdTopic := events.BuildTopicWithConcept(events.TopicGraphNodeCreated, conceptRun)
	producer.onLocalEvent(ctx, events.Event{
		Topic:     createdTopic,
		Kind:      events.KindNodeCreated,
		Timestamp: time.Now(),
		Payload: map[string]any{
			"id": "v1:work:run:abc",
			"payload": map[string]any{
				"kind":   "userGoal",
				"status": "planning",
			},
		},
	})

	// The plan-created event must reach executing replica-2's local bus exactly once.
	got := waitForEvents(t, sink, 1)
	if got[0].Topic != createdTopic {
		t.Fatalf("delivered wrong topic: got %q want %q", got[0].Topic, createdTopic)
	}
	if id, _ := got[0].Payload["id"].(string); id != "v1:work:run:abc" {
		t.Fatalf("delivered wrong payload id: got %q", id)
	}
	// Stamped remote so the local EventBridge will NOT loop it back onto the
	// mesh, and RunDelivery's own producer side skips re-publishing it.
	if !got[0].IsRemote() {
		t.Fatalf("re-published event must be marked remote (OriginNodeId set), got %q", got[0].OriginNodeId)
	}

	// Cursor advanced past the delivered seq -- a reconnect would not replay it.
	if err := waitForCursor(t, store, runLifecycleKey, "executing replica-2", 1); err != nil {
		t.Fatalf("cursor did not advance after delivery: %v", err)
	}
}

// TestPlanStatusTransitionsAllDeliverDespiteStablePlanID is the regression proof
// that a plan's status transitions all deliver despite sharing one logical plan
// id. MemQL is append-only: every status transition (planning -> queued ->
// running -> succeeded) re-inserts under the SAME plan id, and the outbox
// enforces uniqueness on EventID. With EventID = bare plan id, only the FIRST
// transition would land durably and the executing replica would never see the rest -- the
// same failure shape as the chat-reply presence bug (memql#1324). The
// per-occurrence EventID (runId@emissionTs) keeps each transition its own
// durable row, delivered in order.
func TestPlanStatusTransitionsAllDeliverDespiteStablePlanID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newFakeOutboxStore()
	sink := &busSink{}
	_ = newPlannerConsumerDelivery(t, ctx, "executing replica-2", store, sink)

	producer := newPlanProducer(t, "executing replica-1", NodeTypePlanner, store)
	updatedTopic := events.BuildTopicWithConcept(events.TopicGraphNodeUpdated, conceptRun)
	const planID = "v1:work:run:xyz"
	base := time.Now()
	for i, status := range []string{"queued", "running", "succeeded"} {
		producer.onLocalEvent(ctx, events.Event{
			Topic: updatedTopic,
			Kind:  events.KindNodeUpdated,
			// Distinct emission instants, as the engine stamps via
			// events.NewEvent on each write.
			Timestamp: base.Add(time.Duration(i) * time.Millisecond),
			Payload: map[string]any{
				"id":      planID,
				"payload": map[string]any{"status": status},
			},
		})
	}

	got := waitForEvents(t, sink, 3)
	for i, want := range []string{"queued", "running", "succeeded"} {
		payload, _ := got[i].Payload["payload"].(map[string]any)
		if status, _ := payload["status"].(string); status != want {
			t.Fatalf("delivery %d: got status %q want %q (all transitions of a stable plan id must deliver, in order)", i, status, want)
		}
	}
}

// TestRunDeliveryExactlyOncePerOccurrence proves the substrate's
// per-subscription EventID dedup for plan events: the SAME plan occurrence
// arriving twice (the durable replay racing the mesh fast-path of the same
// occurrence) is re-published onto the executing replica's local bus exactly once. This is
// the substrate-level half of the exactly-once story; the executing replica's per-plan
// once-guard (memql#1155) is the second layer, exercised in the executing replica package
// tests.
func TestRunDeliveryExactlyOncePerOccurrence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newFakeOutboxStore()
	sink := &busSink{}
	_ = newPlannerConsumerDelivery(t, ctx, "executing replica-1", store, sink)

	dl := Deliverable{
		EventID:    "v1:work:run:once@123",
		Key:        runLifecycleKey,
		Topic:      events.BuildTopicWithConcept(events.TopicGraphNodeCreated, conceptRun),
		Kind:       events.KindNodeCreated,
		Payload:    map[string]any{"id": "v1:work:run:once", "payload": map[string]any{"status": "planning"}},
		OriginNode: "bff-1",
	}
	sub := NewSubstrate(store, time.Minute, nil, nil)
	// Publish the same EventID twice; the outbox idempotently no-ops the second.
	if _, err := sub.Publish(ctx, dl); err != nil {
		t.Fatalf("publish 1: %v", err)
	}
	if _, err := sub.Publish(ctx, dl); err != nil {
		t.Fatalf("publish 2: %v", err)
	}

	got := waitForEvents(t, sink, 1)
	if len(got) != 1 {
		t.Fatalf("exactly-once violated: delivered %d copies", len(got))
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(sink.snapshot()); n != 1 {
		t.Fatalf("exactly-once violated: delivered %d copies", n)
	}
}

// TestRunDeliverySelfEchoSuppressed proves a executing replica does NOT re-publish a plan
// event it PRODUCED itself (origin == self). The original local-bus event
// already delivered that occurrence to this executing replica's consumers, so re-emitting
// the durable copy would feed a second (deduped, but wasteful) trigger. The
// cursor must still advance past the self-produced row so a reconnect does not
// replay it. A remote occurrence (origin != self) is re-published exactly once.
func TestRunDeliverySelfEchoSuppressed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newFakeOutboxStore()
	sink := &busSink{}
	// The consumer executing replica is "executing replica-self": it both consumes and produced the
	// status transition.
	_ = newPlannerConsumerDelivery(t, ctx, "executing replica-self", store, sink)

	sub := NewSubstrate(store, time.Minute, nil, nil)
	topic := events.BuildTopicWithConcept(events.TopicGraphNodeUpdated, conceptRun)
	// A self-produced occurrence (origin == the consumer node) followed by a
	// remote one (a Plan created on the bff).
	if _, err := sub.Publish(ctx, Deliverable{
		EventID: "p-self@1", Key: runLifecycleKey,
		Topic:      topic,
		Kind:       events.KindNodeUpdated,
		Payload:    map[string]any{"id": "v1:work:run:self", "payload": map[string]any{"status": "running"}},
		OriginNode: "executing replica-self",
	}); err != nil {
		t.Fatalf("publish self: %v", err)
	}
	if _, err := sub.Publish(ctx, Deliverable{
		EventID: "p-remote@1", Key: runLifecycleKey,
		Topic:      events.BuildTopicWithConcept(events.TopicGraphNodeCreated, conceptRun),
		Kind:       events.KindNodeCreated,
		Payload:    map[string]any{"id": "v1:work:run:remote", "payload": map[string]any{"status": "planning"}},
		OriginNode: "bff-1",
	}); err != nil {
		t.Fatalf("publish remote: %v", err)
	}

	// Only the remote occurrence is re-published; the self one is suppressed.
	got := waitForEvents(t, sink, 1)
	time.Sleep(50 * time.Millisecond)
	all := sink.snapshot()
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 re-published event (remote only), got %d", len(all))
	}
	if id, _ := got[0].Payload["id"].(string); id != "v1:work:run:remote" {
		t.Fatalf("re-published the wrong row: got %q want v1:work:run:remote", id)
	}
	// Cursor advanced past BOTH rows (self row Acked even though not re-published).
	if err := waitForCursor(t, store, runLifecycleKey, "executing replica-self", 2); err != nil {
		t.Fatalf("cursor must advance past the suppressed self row too: %v", err)
	}
}

// TestRunDeliveryProducerOnlyPublishesLocal proves the producer side only
// durably publishes events PRODUCED locally: a plan event that arrived FROM A
// PEER (IsRemote) is not re-published to the substrate (its origin node already
// did), so the durable stream isn't double-written.
func TestRunDeliveryProducerOnlyPublishesLocal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newFakeOutboxStore()
	producer := newPlanProducer(t, "bff-1", NodeTypeBFF, store)

	// A remote plan event (arrived over the mesh from another node) must NOT be
	// published to the outbox by this node.
	producer.onLocalEvent(ctx, events.Event{
		Topic:        events.BuildTopicWithConcept(events.TopicGraphNodeCreated, conceptRun),
		Kind:         events.KindNodeCreated,
		Payload:      map[string]any{"id": "v1:work:run:fromPeer"},
		OriginNodeId: "workbench-1", // remote
	})
	if rows, _ := store.ReadAfter(ctx, runLifecycleKey, 0, 0); len(rows) != 0 {
		t.Fatalf("remote plan event must not be published to the substrate, found %d rows", len(rows))
	}

	// A locally-produced plan event IS published.
	producer.onLocalEvent(ctx, events.Event{
		Topic:     events.BuildTopicWithConcept(events.TopicGraphNodeCreated, conceptRun),
		Kind:      events.KindNodeCreated,
		Timestamp: time.Now(),
		Payload:   map[string]any{"id": "v1:work:run:local"},
	})
	rows, _ := store.ReadAfter(ctx, runLifecycleKey, 0, 0)
	if len(rows) != 1 {
		t.Fatalf("local plan event must be published exactly once, found %d rows", len(rows))
	}
}

// TestPlanTopicGate documents which topics the plan-delivery migration owns:
// v1:executing replica:plan created + updated. Deleted plan events and every other concept
// stay off the substrate publish path. The concept id is pinned as a LITERAL on
// purpose -- the chat-reply presence-id drift (memql#1316) showed a symbolic
// comparison can never catch a constant that drifted from the DSL.
func TestPlanTopicGate(t *testing.T) {
	if conceptRun != "v1:work:run" {
		t.Fatalf("plan concept id drifted from the DSL: got %q", conceptRun)
	}
	on := []string{
		events.BuildTopicWithConcept(events.TopicGraphNodeCreated, conceptRun),
		events.BuildTopicWithConcept(events.TopicGraphNodeUpdated, conceptRun),
	}
	for _, topic := range on {
		if !isRunTopic(topic) {
			t.Fatalf("topic %q must be on the substrate path", topic)
		}
	}
	off := []string{
		events.BuildTopicWithConcept(events.TopicGraphNodeDeleted, conceptRun),
		events.BuildTopicWithConcept(events.TopicGraphNodeCreated, "v1:executing replica:task"),
		events.BuildTopicWithConcept(events.TopicGraphNodeCreated, testDeliveryConcept),
		events.BuildTopicWithConcept(events.TopicGraphNodeCreated, "v1:cluster:node"),
		"automation.completed",
	}
	for _, topic := range off {
		if isRunTopic(topic) {
			t.Fatalf("topic %q must NOT be diverted to the substrate", topic)
		}
	}
}
