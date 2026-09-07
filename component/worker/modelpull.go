package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
)

// modelpull.go implements the model-pull envelope (epic memql#5103, design D3).
//
// ===========================================================================
// WHY A PULL IS NOT A ToolDispatch AND NOT A ModelCall
// ===========================================================================
// It is not a dispatch because the engine-driven `workerHost.exec` path is
// capped at 600s with buffered output, and pulling a 40GB model exceeds both:
// the download takes as long as the link takes, and the runtime prints a
// newline-less progress bar the whole way. It is not a ModelCall because
// nothing about a call fits -- there are no messages, no tools, no tokens and
// no generated text, so reusing ModelCallStart would have meant a message
// whose every field is empty for one of its two uses.
//
// What it shares with both is the SHAPE: a start, a stream, and an end
// correlated by request id. That is deliberate reuse of a pattern rather than
// of a message.
//
// ===========================================================================
// THE ONE RULE THIS FILE DOES NOT INHERIT
// ===========================================================================
// ModelCallHandle drops an out-of-order or duplicate delta, because a
// generation is a record and splicing a replayed fragment into the middle
// produces text no later reader can tell is wrong.
//
// A PULL'S COUNTERS ARE NOT A RECORD AND THEY LEGITIMATELY GO BACKWARDS.
// Ollama fetches a model as a set of blobs and reports completed/total PER
// BLOB, restarting at zero for each one. Applying the model call's rule here
// would swallow the first observation of every layer after the first, leaving
// a bar that fills once and then freezes -- which reads as a hung download of
// a pull that is working perfectly. So every observation is delivered, and
// the honesty is moved into what the fields MEAN: they describe the current
// layer, and `Latest` is the last observation rather than a running total,
// because there is no running total the runtime gave us.

// ErrModelPullUnsupported is returned when the selected worker's stream
// cannot carry a pull -- a cockpit that predates this feature. It is a
// distinct error rather than a generic refusal because the fix is "update the
// cockpit", which no other refusal on this path implies.
var ErrModelPullUnsupported = errors.New("worker: worker does not support model pulls")

// ErrModelPullNotFound is returned when a progress or end message names a
// pull this node is not hosting.
var ErrModelPullNotFound = errors.New("worker: model pull not found")

// ErrModelPullIdle is returned when a pull stops reporting for longer than
// its idle ceiling.
var ErrModelPullIdle = errors.New("worker: model pull went silent")

// modelPullProgressBuffer bounds the per-pull progress channel.
//
// Small on purpose. A pull emits observations at the runtime's own cadence --
// a few a second at most -- and the consumer is a throttled row writer, so a
// deep buffer would only ever hold stale observations of a counter whose
// whole value is being current.
const modelPullProgressBuffer = 32

// ModelPullTimeoutDefault is the whole-pull ceiling when the caller names
// none. Generous by the standards of every other worker path, because it is
// bounded by a download rather than by compute: a 40GB model on a domestic
// connection is legitimately hours.
const ModelPullTimeoutDefault = 4 * time.Hour

// ModelPullIdleDefault is how long a pull may report nothing before it is
// given up on. A large layer on a slow link is minutes of legitimate silence
// between status lines, so this is minutes rather than seconds -- but it is
// not absent, or a stalled pull would hold its slot until the process
// restarted.
const ModelPullIdleDefault = 5 * time.Minute

// ModelPullRequest is what a caller asks the machine to fetch.
type ModelPullRequest struct {
	// RequestId correlates progress and the end back to this request. The
	// caller supplies it; the stream stamps one when it is blank.
	RequestId string
	// Model is the model id in the runtime's own vocabulary --
	// `llama3.1:8b`, `hf.co/owner/repo:Q4_K_M`. Passed through VERBATIM: the
	// router selects on this exact string, so a translation anywhere between
	// the OS and the runtime would make a pulled model unreachable under the
	// name it was pulled with.
	Model  string
	Limits ModelPullLimits
}

// ModelPullLimits are the ceilings one pull runs under.
type ModelPullLimits struct {
	// Timeout is the whole-pull ceiling.
	Timeout time.Duration
	// IdleTimeout is how long the machine may report nothing.
	IdleTimeout time.Duration
}

func (l ModelPullLimits) withDefaults() ModelPullLimits {
	if l.Timeout <= 0 {
		l.Timeout = ModelPullTimeoutDefault
	}
	if l.IdleTimeout <= 0 {
		l.IdleTimeout = ModelPullIdleDefault
	}
	return l
}

// ModelPullProgress is one observation of a pull in flight.
//
// EVERY FIELD DESCRIBES THE CURRENT LAYER, not the pull. See the file header:
// the counters restart per blob, so `CompletedBytes` over `TotalBytes` is a
// true fraction of one step and a meaningless fraction of the whole.
type ModelPullProgress struct {
	// CompletedBytes fetched for the current layer.
	CompletedBytes uint64
	// TotalBytes in the current layer, or 0 when the runtime did not say. A
	// total the runtime has not stated is never guessed: an invented
	// denominator produces a percentage that is wrong in a way nobody can
	// see.
	TotalBytes uint64
	// Status is the runtime's own line, verbatim ("pulling 8eeb52dfb3bb",
	// "verifying sha256 digest"). It is what actually tells a person where
	// they are, which is why it is carried rather than parsed into a phase
	// enum this side would have to keep in step with somebody else's release.
	Status string
	// Layer names the blob this observation is about, when the runtime names
	// one. Empty during phases that are not a download.
	Layer string
}

// ModelPullOutcome is the terminal state of a pull.
type ModelPullOutcome struct {
	Model string
	// Ok is the machine's report that the model is present AND allowed. The
	// cockpit writes `models.allow` on success, because a model pulled and
	// not allowed is invisible to the fleet -- and the OS would be saying a
	// pull succeeded while the model never appeared.
	Ok bool
	// Error is the runtime's own failure text. A pull can fail inside an HTTP
	// 200 (the runtime reports errors in the stream body), so a machine that
	// saw one reports it here rather than by dropping the stream.
	Error string
	// Readvertised reports whether the machine pushed its new label set at
	// once rather than waiting for the next fingerprint change (design D3).
	// False means the model is on disk and the cluster will not see it until
	// the machine reconnects -- a materially different thing to tell the
	// person watching from either success or failure.
	Readvertised bool
}

// ModelPullHandle is the caller's view of a running pull.
type ModelPullHandle struct {
	requestId string
	model     string
	limits    ModelPullLimits

	progress chan ModelPullProgress
	done     chan struct{}

	mu      sync.Mutex
	latest  ModelPullProgress
	outcome ModelPullOutcome
	endErr  error
	ended   bool
	// lastActivity is when the last observation arrived, or the pull's start.
	// Read by the idle watchdog.
	lastActivity time.Time
	closeOne     sync.Once

	// cancelFn sends a ModelPullCancel to the worker.
	cancelFn func(*memqlv1.ModelPullCancel) error
	// detach removes the pull from its stream's table.
	detach func()
	clock  func() time.Time
}

// RequestId returns the id this pull is correlated by.
func (h *ModelPullHandle) RequestId() string {
	if h == nil {
		return ""
	}
	return h.requestId
}

// Model returns the model being pulled.
func (h *ModelPullHandle) Model() string {
	if h == nil {
		return ""
	}
	return h.model
}

// Progress is the stream of observations. Closed when the pull ends.
func (h *ModelPullHandle) Progress() <-chan ModelPullProgress {
	if h == nil {
		return nil
	}
	return h.progress
}

// Latest is the most recent observation.
//
// THE LAST ONE, NOT A FOLD. There is no running total to report: the runtime
// counts per layer and never states a whole-pull denominator, so a sum across
// layers would be a number this side invented. A reader that wants the shape
// of the whole pull reads the status lines.
func (h *ModelPullHandle) Latest() ModelPullProgress {
	if h == nil {
		return ModelPullProgress{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest
}

// Cancel asks the machine to stop. Idempotent; a cancel on an already-ended
// pull is a no-op rather than an error.
//
// Best-effort by construction: the runtime may already have finished the
// layer it was on, and a partially fetched blob is resumed rather than
// discarded by the next pull of the same model. So a cancel stops the spend
// of bandwidth, not the existence of bytes.
func (h *ModelPullHandle) Cancel(reason string) error {
	if h == nil {
		return ErrModelPullNotFound
	}
	h.mu.Lock()
	ended := h.ended
	h.mu.Unlock()
	if ended {
		return nil
	}
	if h.cancelFn == nil {
		return nil
	}
	return h.cancelFn(&memqlv1.ModelPullCancel{RequestId: h.requestId, Reason: reason})
}

// Wait blocks until the pull ends, its ceiling expires, the context is
// cancelled, or the machine goes silent past the idle ceiling.
//
// Every giving-up path cancels on the machine first, so a caller who closes
// the page never leaves a multi-gigabyte download running on somebody's
// laptop.
//
// ===========================================================================
// EVERY EXIT FINISHES THE HANDLE, AND THAT IS NOT TIDINESS
// ===========================================================================
// `finish` is the only thing that closes `h.progress`, and the ordinary
// consumer of that channel is a `for range` in a goroutine the caller then
// `wg.Wait()`s on. So a Wait that returned WITHOUT finishing -- as the three
// giving-up paths below originally did, cancelling on the machine and hoping a
// ModelPullEnd would follow -- leaves that goroutine ranging forever and the
// caller blocked on it forever.
//
// The idle path is where that bites: it is reached precisely BECAUSE the
// machine has gone quiet, so the End it was hoping for is the message least
// likely to arrive. The result was a wedged forward handler that never answers
// its peer, and a runner goroutine that never releases its pull id.
//
// finish is idempotent (`closeOne`), so a real End arriving afterwards is a
// no-op rather than a race: the first verdict wins, which is the right one --
// it is this node's own account of why it stopped waiting.
func (h *ModelPullHandle) Wait(ctx context.Context) (ModelPullOutcome, error) {
	if h == nil {
		return ModelPullOutcome{}, ErrModelPullNotFound
	}
	deadline := h.clock().Add(h.limits.Timeout)
	tick := time.NewTicker(h.idlePoll())
	defer tick.Stop()

	for {
		select {
		case <-h.done:
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.outcome, h.endErr
		case <-ctx.Done():
			_ = h.Cancel("caller_cancelled")
			err := ctx.Err()
			h.finish(ModelPullOutcome{Error: "caller_cancelled"}, err)
			return ModelPullOutcome{}, err
		case now := <-tick.C:
			if now.After(deadline) {
				_ = h.Cancel("pull_timeout")
				err := fmt.Errorf("worker: model pull exceeded its %s ceiling", h.limits.Timeout)
				h.finish(ModelPullOutcome{Error: "pull_timeout"}, err)
				return ModelPullOutcome{}, err
			}
			h.mu.Lock()
			idleFor := now.Sub(h.lastActivity)
			h.mu.Unlock()
			if idleFor > h.limits.IdleTimeout {
				_ = h.Cancel("idle_timeout")
				h.finish(ModelPullOutcome{Error: "idle_timeout"}, ErrModelPullIdle)
				return ModelPullOutcome{}, ErrModelPullIdle
			}
		}
	}
}

// idlePoll is how often Wait re-examines the deadlines. Bounded so a short
// idle ceiling in a test does not spin and a long one in production does not
// wake the process needlessly.
func (h *ModelPullHandle) idlePoll() time.Duration {
	p := h.limits.IdleTimeout / 4
	if p < 5*time.Millisecond {
		p = 5 * time.Millisecond
	}
	if p > 5*time.Second {
		p = 5 * time.Second
	}
	return p
}

// deliverProgress hands one observation to the caller.
//
// NOTHING IS DROPPED -- see the file header. The only filtering is the one
// the channel itself imposes when a consumer has stopped reading, and a
// dropped observation there is a stale counter rather than a corrupted
// record, which is why the send does not block the machine's recv goroutine.
// THE SEND HAPPENS UNDER THE LOCK, and that is load-bearing rather than lazy.
// `finish` closes `h.progress`, and a send on a closed channel PANICS even
// inside a select -- so checking `ended` and then releasing the lock before
// sending leaves a window in which finish can close underneath. It is a real
// pairing here rather than a theoretical one: `openModelPull` registers the
// handle BEFORE it sends ModelPullStart, so its `start_send_failed` path
// finishes on the caller's goroutine while the stream's recv goroutine may be
// delivering for the same request id.
//
// Holding the lock across the send costs nothing because the send is
// NON-BLOCKING: it has a default arm, so it cannot wait on a consumer.
func (h *ModelPullHandle) deliverProgress(p ModelPullProgress) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ended {
		return
	}
	h.lastActivity = h.clock()
	h.latest = p

	select {
	case h.progress <- p:
	default:
		// A consumer that has fallen behind gets the NEXT observation rather
		// than this one. Blocking here would stall the stream's recv loop --
		// and every other message on it -- to deliver a byte count that is
		// superseded a moment later.
	}
}

// finish records the terminal state and releases every waiter.
//
// `close(h.progress)` happens UNDER h.mu, paired with deliverProgress's send:
// the two are the closing and the sending halves of one channel, and a send
// racing a close panics.
func (h *ModelPullHandle) finish(outcome ModelPullOutcome, err error) {
	h.closeOne.Do(func() {
		h.mu.Lock()
		h.ended = true
		if outcome.Model == "" {
			outcome.Model = h.model
		}
		h.outcome = outcome
		h.endErr = err
		close(h.progress)
		h.mu.Unlock()
		close(h.done)
		if h.detach != nil {
			h.detach()
		}
	})
}

// ModelPullFunc is the per-stream hook that opens a pull.
type ModelPullFunc func(ctx context.Context, req ModelPullRequest) (*ModelPullHandle, error)

// SetModelPullFunc wires the per-stream pull hook. Called once per stream
// alongside SetModelCallFunc.
func (w *Worker) SetModelPullFunc(fn ModelPullFunc) {
	if w == nil {
		return
	}
	w.modelPullFn = fn
}

// StartModelPull asks this machine to fetch a model.
//
// IT DOES NOT TAKE A CONCURRENCY SLOT, and that is the difference from
// StartModelCall. A slot is the machine's declared ceiling on how many
// INFERENCE calls it will serve at once; a pull is a download, it competes
// for bandwidth and disk rather than for the model runner, and holding an
// inference slot for hours would make a machine look busy for work it can
// still do. The pull's own ceiling is the runtime's: it serialises pulls
// itself.
//
// IT DOES NOT CHECK RunsModel EITHER, and that is the whole point of the
// feature: the model is precisely what the machine has not got yet. This is
// the one path on the worker surface where naming a model the machine does
// not advertise is correct rather than an error.
func (w *Worker) StartModelPull(ctx context.Context, req ModelPullRequest) (*ModelPullHandle, error) {
	if w == nil || w.modelPullFn == nil {
		return nil, ErrModelPullUnsupported
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("worker: model pull requires a model")
	}
	req.Model = strings.TrimSpace(req.Model)
	return w.modelPullFn(ctx, req)
}
