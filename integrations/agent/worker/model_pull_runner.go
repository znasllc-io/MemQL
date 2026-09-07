//go:build agent

package worker

// The agent side of a model pull (epic memql#5103, design D3).
//
// ===========================================================================
// THE ACT AND THE DOWNLOAD RUN ON DIFFERENT NODES, NECESSARILY
// ===========================================================================
// `fleetModelPull` is served by whatever node took the call -- usually a bff,
// which has no worker registry at all, because everything that can reach a
// machine's stream is behind the `agent` build tag. So the act DECIDES and
// writes a v1:worker:modelPull row, and an agent replica picks that row up.
// This file is the picking up.
//
// ===========================================================================
// THE CLAIM IS `targetNodeId`, AND IT NEEDS NO LOCK
// ===========================================================================
// The act stamps the replica that held the machine's stream when the pull was
// asked for. Exactly one replica matches, so exactly one acts -- no advisory
// lock, no election, no shared counter, and no window in which two replicas
// download the same forty gigabytes onto the same disk.
//
// If the machine has RECONNECTED to a sibling since (a cockpit reconnects on
// every restart and every network blip, and pressing Pull just after waking a
// laptop hits this squarely) the claiming replica finds it missing from its own
// registry and forwards over ModelPullForward rather than dropping the request.
//
// If the named replica is GONE, nobody claims the row and the scheduled sweep
// fails it. That is the one path where a person waits before learning, and it
// is the one no check at request time could have caught.
//
// ===========================================================================
// AN EVENT, NOT A POLL, AND THE REASON IS AUTHORITY RATHER THAN LATENCY
// ===========================================================================
// A poller would have to ASK which pulls are open, and that read spans every
// owner -- which on an owner-tiered concept means the cluster's maintenance
// principal, reserved for engine-owned scheduled automations whose reads span
// owners by nature (component/auth/maintenance_actor.go).
//
// The event carries the row, `ownerUserId` included, so this handler never
// performs a cross-owner read at all: it is handed one owner's row and does
// every subsequent read and write under exactly that owner's borrowed
// authority -- the pattern that file names as the fix for precisely this case.
// The sweep, whose read genuinely does span owners, IS a scheduled automation
// and IS on that list. Two mechanisms, two authorities, each the smallest that
// answers its own question.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/events"
	langparser "github.com/znasllc-io/memql/component/language/parser"
	workerservice "github.com/znasllc-io/memql/component/worker"
)

// ModelPullTopic is the event a requested pull raises. It reaches every agent
// replica because v1:worker:modelPull carries a broadcast routing rule
// (component/node/routing.go); without that rule this handler would fire only
// on the node that served the act, which is the one node that cannot do the
// work.
const ModelPullTopic = "graph.node.created.v1:worker:modelPull"

// PullProgressInterval is the floor between two progress writes for one pull.
//
// A runtime emits several observations a second. A row version each would be
// tens of thousands of versions for one download -- on an append-only,
// BROADCAST concept, so the cost lands on the database, on the mesh AND on
// every subscribed browser. Two seconds is under the threshold at which a bar
// reads as stuck, and three orders of magnitude cheaper.
const PullProgressInterval = 2 * time.Second

// modelPullWriteBuffer bounds the queue between the observation callback and
// the goroutine that writes rows. Small, because its only job is to decouple
// the two: what is worth keeping is the LATEST observation, and a deep buffer
// would just hold stale ones while the writer caught up.
const modelPullWriteBuffer = 8

// modelPullRow is the slice of a v1:worker:modelPull row this runner acts on.
// A struct, so every decision below is a function of values and is testable
// without an engine, a cluster or a machine.
type modelPullRow struct {
	PullId       string
	OwnerUserId  string
	WorkerId     string
	Model        string
	Status       string
	TargetNodeId string
}

// claimedBy reports whether this replica is the one that should act on the row.
//
// A BLANK targetNodeId IS CLAIMED BY NOBODY, deliberately. Reading it as "any
// replica may take this" would put every replica on the same download; reading
// it as "this one" does the same thing with an extra step. It is left to the
// sweep, which fails it with a sentence naming what happened.
//
// An empty SelfNodeId -- no MEMQL_NODE_ID in the environment -- claims nothing,
// which is the fail-closed direction: better a pull the sweep reports than a
// single-node cluster and a two-replica one behaving differently here.
func claimedBy(row modelPullRow, selfNodeId string) bool {
	target := strings.TrimSpace(row.TargetNodeId)
	self := strings.TrimSpace(selfNodeId)
	return target != "" && self != "" && target == self
}

// modelPullRowFromEvent reads a graph node event into the row this acts on.
//
// The concept's fields sit under `payload`, which is the envelope a graph event
// carries; the row id is at the top level. `ok` is false for anything that is
// not a usable pull request, so a malformed event is ignored rather than
// half-acted-on.
func modelPullRowFromEvent(ev events.Event) (modelPullRow, bool) {
	if ev.Payload == nil {
		return modelPullRow{}, false
	}
	fields, _ := ev.Payload["payload"].(map[string]any)
	if fields == nil {
		// Some producers flatten. Reading both is cheaper than deciding which
		// path an event came through, and the alternative fails silently.
		fields = ev.Payload
	}
	row := modelPullRow{
		PullId:       eventString(ev.Payload, "id"),
		OwnerUserId:  eventString(fields, "ownerUserId"),
		WorkerId:     eventString(fields, "workerId"),
		Model:        eventString(fields, "model"),
		Status:       eventString(fields, "status"),
		TargetNodeId: eventString(fields, "targetNodeId"),
	}
	if row.PullId == "" {
		row.PullId = eventString(fields, "id")
	}
	if row.PullId == "" || row.WorkerId == "" || row.Model == "" || row.OwnerUserId == "" {
		return modelPullRow{}, false
	}
	return row, true
}

func eventString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// progressThrottle decides whether an observation is worth a row write.
//
// TIME OR MEANING, whichever comes first. A pure interval throttle would sit
// on the transition from one layer to the next -- which is exactly the moment
// the counters jump backwards, and exactly the moment a person staring at a bar
// needs the new status line to explain why. So a changed status line or layer
// always writes, whatever the clock says.
type progressThrottle struct {
	mu         sync.Mutex
	lastWrite  time.Time
	lastStatus string
	lastLayer  string
	seen       bool
}

func (t *progressThrottle) admit(p workerservice.ModelPullProgress, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	changed := !t.seen || p.Status != t.lastStatus || p.Layer != t.lastLayer
	if !changed && now.Sub(t.lastWrite) < PullProgressInterval {
		return false
	}
	t.seen = true
	t.lastWrite = now
	t.lastStatus = p.Status
	t.lastLayer = p.Layer
	return true
}

// -----------------------------------------------------------------------------
// The runner
// -----------------------------------------------------------------------------

// ModelPullForwarder is the cross-replica half. Nil disables forwarding, which
// makes a machine that has moved a refusal rather than a silence.
type ModelPullForwarder interface {
	ForwardModelPull(
		ctx context.Context,
		nodeId string,
		registrationId string,
		ownerUserId string,
		model string,
		timeout time.Duration,
		onProgress func(workerservice.ModelPullProgress),
	) (ModelPullOutcome, error)
}

// ModelPullRunner drives this replica's model pulls.
type ModelPullRunner struct {
	dispatcher *Dispatcher
	forwarder  ModelPullForwarder
	selfNodeId string
	clock      func() time.Time

	mu      sync.Mutex
	running map[string]struct{}
}

// NewModelPullRunner builds the runner for one agent replica.
func NewModelPullRunner(d *Dispatcher, forwarder ModelPullForwarder, selfNodeId string) *ModelPullRunner {
	if d == nil {
		return nil
	}
	return &ModelPullRunner{
		dispatcher: d,
		forwarder:  forwarder,
		selfNodeId: selfNodeId,
		clock:      time.Now,
		running:    map[string]struct{}{},
	}
}

// Subscribe registers the handler on the event bus and returns the
// unsubscribe. Nil-safe at every level, because a node without an event bus is
// a node that cannot serve pulls and should say so once rather than panic.
func (r *ModelPullRunner) Subscribe(bus *events.Bus) func() {
	if r == nil || bus == nil {
		return func() {}
	}
	return bus.Subscribe(ModelPullTopic, r.HandleModelPullCreated,
		events.WithSubscriberName("agentworker:model-pull"))
}

// HandleModelPullCreated claims a requested pull for this replica and drives it.
func (r *ModelPullRunner) HandleModelPullCreated(ev events.Event) {
	if r == nil {
		return
	}
	row, ok := modelPullRowFromEvent(ev)
	if !ok || row.Status != "requested" {
		return
	}
	if !claimedBy(row, r.selfNodeId) {
		return
	}
	if !r.begin(row.PullId) {
		return
	}
	go func() {
		defer r.end(row.PullId)
		// The pull's own context, NOT the event's: an event handler's context
		// is scoped to delivering the event, and a download outlives it by
		// hours. The ceiling is the handle's.
		ctx, cancel := context.WithTimeout(context.Background(), workerservice.ModelPullTimeoutDefault)
		defer cancel()
		r.drive(ctx, row)
	}()
}

func (r *ModelPullRunner) begin(pullId string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, live := r.running[pullId]; live {
		return false
	}
	r.running[pullId] = struct{}{}
	return true
}

func (r *ModelPullRunner) end(pullId string) {
	r.mu.Lock()
	delete(r.running, pullId)
	r.mu.Unlock()
}

// drive runs one pull to its end, writing progress on a throttle.
//
// ===========================================================================
// THE ROW WRITE NEVER HAPPENS ON THE CALLER'S GOROUTINE
// ===========================================================================
// `onProgress` is invoked from two places, and one of them must not block. On
// the forwarded path it is reached through ForwardRouter.DispatchModelPullProgress,
// which the WorkerDialer and ParentConnector call INLINE from their peer
// connection's receive callback -- so a synchronous `engine.Execute` there
// stalls every inbound message from that peer for the length of a graph
// mutation, and a burst of layer transitions is a burst of those.
//
// So observations are handed to a writer goroutine over a small buffered
// channel and `onProgress` never waits. Dropping under pressure is correct
// here and nowhere else on this surface: a progress row's whole value is being
// current, so the NEXT observation carries the same question a moment later.
// The terminal write is not on this path and is never dropped.
func (r *ModelPullRunner) drive(ctx context.Context, row modelPullRow) {
	throttle := &progressThrottle{}
	writes := make(chan workerservice.ModelPullProgress, modelPullWriteBuffer)
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for p := range writes {
			r.recordProgress(ctx, row, p)
		}
	}()

	onProgress := func(p workerservice.ModelPullProgress) {
		if !throttle.admit(p, r.clock()) {
			return
		}
		select {
		case writes <- p:
		default:
			// The writer is behind. See above: a dropped observation is a stale
			// bar for a moment, and blocking here would put a database write on
			// the mesh's read path.
		}
	}

	outcome, err := r.pull(ctx, row, onProgress)
	close(writes)
	writer.Wait()
	switch {
	case err != nil:
		r.finish(ctx, row, "failed", err.Error(), false)
	case !outcome.Ok:
		message := strings.TrimSpace(outcome.ErrorMessage)
		if message == "" {
			message = "The machine reported that the pull did not finish, and said no more than that."
		}
		r.finish(ctx, row, "failed", message, false)
	default:
		r.finish(ctx, row, "succeeded", "", outcome.Readvertised)
	}
}

// pull reaches the machine, locally or across the hop.
func (r *ModelPullRunner) pull(
	ctx context.Context,
	row modelPullRow,
	onProgress func(workerservice.ModelPullProgress),
) (ModelPullOutcome, error) {
	if w := r.dispatcher.registry.WorkerById(row.WorkerId); w != nil {
		return r.pullLocally(ctx, w, row, onProgress)
	}

	// The machine is not on this replica any more. Re-read where it went and
	// forward there, under the OWNER'S borrowed authority -- the same authority
	// the receiving side verifies the machine's ownership against.
	ownerCtx := r.ownerActor(ctx, row.OwnerUserId)
	target, err := r.currentNodeFor(ownerCtx, row)
	if err != nil {
		return ModelPullOutcome{Model: row.Model}, err
	}
	if target == "" {
		return ModelPullOutcome{Model: row.Model}, fmt.Errorf(
			"the machine disconnected before the pull could start; it has to be connected for a pull to run")
	}
	if target == strings.TrimSpace(r.selfNodeId) {
		// The graph says this replica holds it and the registry says
		// otherwise. The registry is the ground truth about a live stream, so
		// this is a stale row rather than a routing question, and forwarding
		// to ourselves would loop.
		return ModelPullOutcome{Model: row.Model}, fmt.Errorf(
			"the machine's connection ended on this node; it has to reconnect before a pull can start")
	}
	if r.forwarder == nil {
		return ModelPullOutcome{Model: row.Model}, fmt.Errorf(
			"the machine moved to another node (%s) and this one cannot forward to it", target)
	}
	authorityCtx, err := r.borrowedAuthority(ctx, row.OwnerUserId)
	if err != nil {
		return ModelPullOutcome{Model: row.Model}, err
	}
	return r.forwarder.ForwardModelPull(
		authorityCtx, target, row.WorkerId, row.OwnerUserId, row.Model,
		workerservice.ModelPullTimeoutDefault, onProgress)
}

func (r *ModelPullRunner) pullLocally(
	ctx context.Context,
	w *workerservice.Worker,
	row modelPullRow,
	onProgress func(workerservice.ModelPullProgress),
) (ModelPullOutcome, error) {
	handle, err := w.StartModelPull(ctx, workerservice.ModelPullRequest{
		RequestId: row.PullId,
		Model:     row.Model,
	})
	if err != nil {
		return ModelPullOutcome{Model: row.Model}, err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for p := range handle.Progress() {
			onProgress(p)
		}
	}()
	out, waitErr := handle.Wait(ctx)
	wg.Wait()
	if waitErr != nil {
		return ModelPullOutcome{Model: row.Model}, waitErr
	}
	return ModelPullOutcome{
		Model:        out.Model,
		Ok:           out.Ok,
		Readvertised: out.Readvertised,
		ErrorMessage: out.Error,
	}, nil
}

// currentNodeFor re-reads which replica holds the machine now.
func (r *ModelPullRunner) currentNodeFor(ctx context.Context, row modelPullRow) (string, error) {
	machines, err := r.dispatcher.store.WorkersForOwner(ctx, row.OwnerUserId)
	if err != nil {
		return "", fmt.Errorf("could not re-read where the machine is connected: %w", err)
	}
	for _, m := range machines {
		if m.RegistrationId != row.WorkerId {
			continue
		}
		if !m.RevokedAt.IsZero() {
			return "", fmt.Errorf("the machine was revoked, so its worker token no longer works")
		}
		return strings.TrimSpace(m.ConnectedNodeId), nil
	}
	return "", fmt.Errorf("the machine is no longer registered to this owner")
}

// -----------------------------------------------------------------------------
// Authority
// -----------------------------------------------------------------------------

// ownerActor is the borrowed-authority context every row read and write here
// runs under.
//
// v1:worker:modelPull is owner-tiered, so an unstamped write is refused and an
// unstamped READ returns zero rows -- which would present as a pull that
// silently never updates, on a surface whose whole job is showing that it does.
// The owner value comes off the event's own row, so it can never name a user
// the requester could not act as.
func (r *ModelPullRunner) ownerActor(ctx context.Context, ownerUserId string) context.Context {
	return auth.ContextWithInternalOrigin(auth.ContextWithUserActor(ctx, ownerUserId))
}

// borrowedAuthority builds the assertion the forward hop requires.
//
// The receiving replica checks the machine against THIS subject and never
// against the envelope's owner field, which is what makes the ownership
// boundary hold across the hop -- and here it is the last gate before gigabytes
// are written to somebody's disk.
func (r *ModelPullRunner) borrowedAuthority(ctx context.Context, ownerUserId string) (context.Context, error) {
	access := &auth.AccessContext{UserId: ownerUserId, Role: auth.RoleWriter}
	authority, err := auth.ForwardedAuthorityForUser(access, "", "", time.Time{}, r.clock())
	if err != nil {
		return nil, fmt.Errorf("could not assert the machine owner's authority for the hop: %w", err)
	}
	verified, err := auth.VerifyForwardedAuthority(authority, r.clock())
	if err != nil {
		return nil, fmt.Errorf("could not assert the machine owner's authority for the hop: %w", err)
	}
	return auth.BindForwardedContext(ctx, authority.Principal().Claims, verified, authority), nil
}

// -----------------------------------------------------------------------------
// Row writes
// -----------------------------------------------------------------------------

func (r *ModelPullRunner) recordProgress(ctx context.Context, row modelPullRow, p workerservice.ModelPullProgress) {
	call, err := langparser.RenderCall("recordModelPullProgress", map[string]any{
		"pullId":         row.PullId,
		"statusLine":     p.Status,
		"layer":          p.Layer,
		"completedBytes": int64(p.CompletedBytes),
		"totalBytes":     int64(p.TotalBytes),
		"updatedAt":      r.clock().UTC().Format(time.RFC3339),
	})
	if err != nil {
		r.dispatcher.logger.Warn("model pull: render progress", "pull_id", row.PullId, "error", err)
		return
	}
	if _, err := r.dispatcher.engine.Execute(r.ownerActor(ctx, row.OwnerUserId), call); err != nil {
		// A dropped progress write is a stale bar for a couple of seconds. It
		// is logged at debug and never retried: the next observation carries
		// the same question, and blocking the download on a database write
		// would let the row's health decide the pull's.
		r.dispatcher.logger.Debug("model pull: record progress", "pull_id", row.PullId, "error", err)
	}
}

func (r *ModelPullRunner) finish(ctx context.Context, row modelPullRow, status, message string, readvertised bool) {
	call, err := langparser.RenderCall("finishModelPull", map[string]any{
		"pullId":       row.PullId,
		"status":       status,
		"statusLine":   "",
		"errorMessage": message,
		"readvertised": readvertised,
		"endedAt":      r.clock().UTC().Format(time.RFC3339),
	})
	if err != nil {
		r.dispatcher.logger.Warn("model pull: render finish", "pull_id", row.PullId, "error", err)
		return
	}
	// The finishing write does NOT ride the pull's context: that context may
	// be the one that just expired, and a terminal status written under a dead
	// context is a pull that stays `running` forever with nothing to explain
	// it. It gets its own short ceiling instead.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, err := r.dispatcher.engine.Execute(r.ownerActor(writeCtx, row.OwnerUserId), call); err != nil {
		r.dispatcher.logger.Warn("model pull: close the pull record",
			"pull_id", row.PullId, "status", status, "error", err)
	}
}
