package memql

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/events"
)

// The debounce these cases run at. Short enough that the suite does not wait,
// long enough that a burst genuinely lands inside one window on a loaded
// machine. The PRODUCTION value is asserted separately, below, because a test
// that ran at the real two seconds would be a slow test asserting a constant.
const testDebounce = 60 * time.Millisecond

func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s did not happen within %s", what, limit)
}

// A BURST IS ONE REWRITE.
//
// A cockpit reconnecting re-advertises every model and every app it holds,
// which is a run of graph.node.updated events inside a second. One rewrite per
// event would be one full module evaluation per event -- each of which reads
// every registration in the cluster, on every node, because those events are
// broadcast.
func TestABurstRewritesOnce(t *testing.T) {
	var writes atomic.Int32
	sub := newReadinessRecomputeSubscriberFor(func(context.Context) error {
		writes.Add(1)
		return nil
	}, testDebounce)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub.Start(ctx)

	for i := 0; i < 50; i++ {
		sub.Notify("registration")
	}
	waitFor(t, 3*time.Second, "the first rewrite", func() bool { return writes.Load() >= 1 })
	// Well past a second window, so a second rewrite would have landed by now.
	time.Sleep(4 * testDebounce)
	if got := writes.Load(); got != 1 {
		t.Fatalf("%d rewrites for one burst, want 1", got)
	}
}

// A CHANGE AFTER THE WINDOW IS ITS OWN REWRITE. Collapsing everything forever
// would be a feed that stops moving at the first machine anybody pairs.
func TestAChangeAfterTheWindowRewritesAgain(t *testing.T) {
	var writes atomic.Int32
	sub := newReadinessRecomputeSubscriberFor(func(context.Context) error {
		writes.Add(1)
		return nil
	}, testDebounce)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub.Start(ctx)

	sub.Notify("registration")
	waitFor(t, 3*time.Second, "the first rewrite", func() bool { return writes.Load() == 1 })
	sub.Notify("providers")
	waitFor(t, 3*time.Second, "the second rewrite", func() bool { return writes.Load() == 2 })
}

// NOTHING RECOMPUTES ON A TIMER (D5). A rewrite costs a cluster-wide
// registration read on every node; a cluster where nothing changes must cost
// nothing. A poll is also LATE after a change, which is the half of the
// argument a person actually feels.
func TestNothingRewritesWithoutAnEvent(t *testing.T) {
	var writes atomic.Int32
	sub := newReadinessRecomputeSubscriberFor(func(context.Context) error {
		writes.Add(1)
		return nil
	}, testDebounce)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub.Start(ctx)
	time.Sleep(10 * testDebounce)
	if got := writes.Load(); got != 0 {
		t.Fatalf("%d rewrites with no event; a timer-driven recompute is exactly what D5 refuses", got)
	}
}

// A FAILED REWRITE DOES NOT STOP THE SUBSCRIBER. The previous rows stand and
// the next event tries again; a loop that died on one transient error would
// leave this node reporting boot's verdict for the rest of the process, and
// nothing would ever say so.
func TestAFailedRewriteDoesNotStopTheSubscriber(t *testing.T) {
	var writes atomic.Int32
	sub := newReadinessRecomputeSubscriberFor(func(context.Context) error {
		if writes.Add(1) == 1 {
			return errors.New("the write did not land")
		}
		return nil
	}, testDebounce)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub.Start(ctx)

	sub.Notify("registration")
	waitFor(t, 3*time.Second, "the failing rewrite", func() bool { return writes.Load() == 1 })
	sub.Notify("registration")
	waitFor(t, 3*time.Second, "the rewrite after it", func() bool { return writes.Load() == 2 })
}

// Cancelling the context ends the loop. Without this a node that stopped its
// engine would keep a goroutine writing rows against a closed database.
func TestStoppingEndsTheLoop(t *testing.T) {
	var writes atomic.Int32
	sub := newReadinessRecomputeSubscriberFor(func(context.Context) error {
		writes.Add(1)
		return nil
	}, testDebounce)
	ctx, cancel := context.WithCancel(context.Background())
	sub.Start(ctx)
	cancel()
	time.Sleep(2 * testDebounce)
	sub.Notify("registration")
	time.Sleep(6 * testDebounce)
	if got := writes.Load(); got != 0 {
		t.Fatalf("%d rewrites after the context was cancelled", got)
	}
}

// Every method is nil-safe, which is what lets NotifyReadinessRecompute be
// called from a path that runs on a hand-built engine in a test and on a
// binary that never started the subscriber.
func TestANilSubscriberIsANoOp(t *testing.T) {
	var sub *ReadinessRecomputeSubscriber
	sub.Notify("registration")
	sub.Start(context.Background())
	sub.Stop()
	var eng *MemQLEngine
	eng.NotifyReadinessRecompute("registration")
	// An engine with no subscriber wired is the ordinary case for every test
	// in this package, and it must not panic either.
	(&MemQLEngine{}).NotifyReadinessRecompute("registration")
}

// The one graph subscription this node opens, and it must actually match the
// topics the CDC path publishes.
//
// A pattern that matched nothing would be the worst kind of failure here: the
// subscriber would run, the loop would be alive, every test above would pass,
// and the feed would never move -- which is indistinguishable from the defect
// this whole task exists to fix.
func TestTheRegistrationPatternsMatchTheCdcTopics(t *testing.T) {
	patterns := readinessRegistrationPatterns()
	if len(patterns) == 0 {
		t.Fatal("no registration patterns at all")
	}
	for _, topic := range []string{
		events.TopicNodeCreated(WorkerRegistrationConcept),
		events.TopicNodeUpdated(WorkerRegistrationConcept),
		events.TopicNodeDeleted(WorkerRegistrationConcept),
	} {
		matched := false
		for _, p := range patterns {
			if events.Match(p, topic) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("%q matches none of %v -- the subscriber would run and never hear a machine "+
				"being paired, which looks exactly like the defect it exists to fix", topic, patterns)
		}
	}
	// And it must not match every graph event on the bus: a readiness rewrite
	// on every row written anywhere would be a cluster-wide registration read
	// per write.
	for _, topic := range []string{
		events.TopicNodeCreated("v1:identity:user"),
		events.TopicNodeUpdated("v1:work:step"),
		events.TopicNodeCreated(ModuleReadinessConcept),
	} {
		for _, p := range patterns {
			if events.Match(p, topic) {
				t.Errorf("%q matches %q; the subscription must be the registration concept alone", p, topic)
			}
		}
	}
	// The readiness rows are the sharpest of those: a rewrite triggered by a
	// readiness write would rewrite on its own output, forever.
	for _, p := range patterns {
		if strings.Contains(p, ModuleReadinessConcept) {
			t.Fatalf("pattern %q names the readiness concept itself, which is a write loop", p)
		}
	}
}

// The production constants, asserted where a reader looking for them will find
// them rather than only in a comment.
func TestTheProductionTimingsAreWhatTheRecordSays(t *testing.T) {
	if ReadinessRecomputeDebounce != 2*time.Second {
		t.Errorf("the debounce is %s; D5 says two seconds", ReadinessRecomputeDebounce)
	}
	if ReadinessBootRewriteDelay != 30*time.Second {
		t.Errorf("the boot re-write delay is %s; D5 says thirty seconds", ReadinessBootRewriteDelay)
	}
}
