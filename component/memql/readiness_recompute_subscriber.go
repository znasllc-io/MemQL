package memql

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/znasllc-io/memql/component/events"
)

// READINESS RECOMPUTES ON THE EVENTS THAT CHANGE IT (design record
// docs/superpowers/specs/2026-09-07-core-gate-and-honest-install-design.md, D5).
//
// The shipped rows were rewritten at boot, on a providers reload, on an email
// configure, or by a manual recompute -- and nothing else. So pairing a
// machine, revoking one, or a cockpit re-advertising its models left the `ai`
// verdict exactly as it was at boot. A person who had just done the one thing
// the wizard asked of them watched a rail that did not move, and there was
// nothing on any surface to tell them why.
//
// ===========================================================================
// NOT A TIMER
// ===========================================================================
// A poll is late after a change and wasteful when nothing changed, and every
// tick now costs a cluster-wide registration read on every node.
// TestNothingRewritesWithoutAnEvent pins that this stays true.
//
// ===========================================================================
// DEBOUNCED, BECAUSE A RECONNECT IS A BURST
// ===========================================================================
// A cockpit reconnecting re-advertises every model and every app it holds, as
// a run of graph.node.updated events inside one second. One rewrite per event
// would be one full module evaluation per event, each reading every
// registration in the cluster, on every node -- because those events are
// BROADCAST (component/node/routing.go).
//
// ===========================================================================
// AND IT SERIALIZES
// ===========================================================================
// Every rewrite this node performs from an event goes through one loop, so two
// triggers arriving together cannot run two evaluations that race on the same
// deterministic row ids. That is why the providers-reload subscriber notifies
// this rather than calling WriteModuleReadiness itself.
type ReadinessRecomputeSubscriber struct {
	write    func(context.Context) error
	logger   *slog.Logger
	debounce time.Duration

	// pending is a buffered channel of ONE, and that IS the coalescing: a
	// burst of a thousand events costs one slot and one rewrite.
	pending chan string

	mu   sync.Mutex
	stop context.CancelFunc
}

// ReadinessRecomputeDebounce is how long a burst is collapsed for.
//
// Two seconds: long enough to swallow a reconnect's whole advertisement, short
// enough that a person who has just paired a machine sees the wizard move
// before they have finished reading the confirmation.
const ReadinessRecomputeDebounce = 2 * time.Second

// ReadinessBootRewriteDelay is the ONE re-write after boot.
//
// It is not a poll and it does not repeat. It covers a node whose integration
// materialized lazily AFTER the boot write ran -- the case the email plug-in's
// own resolution logged twenty seconds late on the owner's cluster -- and
// nothing else. One extra evaluation per process lifetime.
const ReadinessBootRewriteDelay = 30 * time.Second

// readinessRegistrationPattern is the one graph subscription this node needs.
//
// Composed through GraphSubscriptionPatterns rather than spelled here, so the
// `graph.node.<action>.<concept>` grammar has one author. With no actions it
// yields `graph.node.*.<concept>` -- `*` is exactly the one action segment,
// and a concept id carries no dots, so the trailing remainder is one segment
// too.
//
// ALL THREE VERBS MATTER and each is a different fact: created is a machine
// paired, updated is a reconnect re-advertising its models and apps or an
// owner revoking it, deleted is a registration removed. All three are
// BROADCAST cross-node (component/node/routing.go), so every replica hears
// them and rewrites its own row -- without that, a machine paired through one
// bff would leave every other replica reporting the old verdict, and the fold
// would read the disagreement as `partial`.
func readinessRegistrationPatterns() []string {
	return events.GraphSubscriptionPatterns(WorkerRegistrationConcept, nil)
}

// NewReadinessRecomputeSubscriber builds this node's subscriber.
func NewReadinessRecomputeSubscriber(eng *MemQLEngine) *ReadinessRecomputeSubscriber {
	if eng == nil {
		return nil
	}
	sub := newReadinessRecomputeSubscriberFor(func(ctx context.Context) error {
		_, err := eng.WriteModuleReadiness(ctx)
		return err
	}, ReadinessRecomputeDebounce)
	sub.logger = eng.safeLogger()
	return sub
}

func newReadinessRecomputeSubscriberFor(write func(context.Context) error, debounce time.Duration) *ReadinessRecomputeSubscriber {
	return &ReadinessRecomputeSubscriber{
		write:    write,
		debounce: debounce,
		pending:  make(chan string, 1),
	}
}

// Notify records that something changed. NON-BLOCKING and coalescing, so a
// caller on a hot path never waits and a burst never queues.
func (s *ReadinessRecomputeSubscriber) Notify(reason string) {
	if s == nil {
		return
	}
	select {
	case s.pending <- reason:
	default:
	}
}

// Start runs the debounce loop until ctx is done or Stop is called. Calling it
// twice is a no-op, so a node that wires it in two places does not get two
// loops writing the same rows.
func (s *ReadinessRecomputeSubscriber) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stop != nil {
		s.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.stop = cancel
	s.mu.Unlock()
	go s.loop(runCtx)
}

// Stop ends the loop. Safe to call twice, and safe on a nil receiver.
func (s *ReadinessRecomputeSubscriber) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}

func (s *ReadinessRecomputeSubscriber) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case reason := <-s.pending:
			// Wait out the window BEFORE writing, so a burst that is still
			// arriving is one rewrite rather than the first of many.
			timer := time.NewTimer(s.debounce)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			// Anything that arrived during the window is covered by the
			// rewrite about to run, so its slot is cleared rather than
			// queuing a second identical pass.
			select {
			case <-s.pending:
			default:
			}
			// A FAILURE LEAVES THE PREVIOUS ROWS STANDING AND THE LOOP ALIVE.
			// A stale verdict somebody can act on beats a subscriber that died
			// on one transient error and left this node reporting boot's
			// answer for the rest of the process.
			if err := s.write(ctx); err != nil && s.logger != nil {
				s.logger.Warn("module readiness: event-driven rewrite failed; the previous rows stand",
					"component", ComponentName, "reason", reason, "error", err)
			}
		}
	}
}

// StartReadinessRecomputeSubscriber wires this node's readiness rewrites to the
// events that change them, and returns the subscriber so a caller can Notify it
// from a path that is not an event (the providers reload).
//
// The subscription is scoped to ctx, exactly as StartProvidersReloadSubscriber
// is: when ctx is cancelled the unsubscribe runs and the loop exits.
func (e *MemQLEngine) StartReadinessRecomputeSubscriber(ctx context.Context) *ReadinessRecomputeSubscriber {
	if e == nil {
		return nil
	}
	sub := NewReadinessRecomputeSubscriber(e)
	sub.Start(ctx)
	e.readinessRecompute = sub

	if e.eventBus == nil {
		return sub
	}
	for _, pattern := range readinessRegistrationPatterns() {
		unsubscribe := e.eventBus.Subscribe(
			pattern,
			func(events.Event) { sub.Notify("registration") },
			events.WithSubscriberName("readiness:recompute"),
		)
		if unsubscribe == nil {
			continue
		}
		go func() {
			<-ctx.Done()
			unsubscribe()
		}()
	}
	return sub
}

// NotifyReadinessRecompute asks this node to rewrite its readiness rows, on the
// debounce, from a path that is not a graph event.
//
// A no-op when the subscriber is not running, which is every hand-built engine
// in a test and every binary that has not called
// StartReadinessRecomputeSubscriber. The caller that needs the rewrite to have
// HAPPENED calls WriteModuleReadiness directly instead; this one is for the
// callers that only need it to happen soon.
func (e *MemQLEngine) NotifyReadinessRecompute(reason string) {
	if e == nil {
		return
	}
	e.readinessRecompute.Notify(reason)
}
