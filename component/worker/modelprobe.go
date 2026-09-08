package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	"github.com/znasllc-io/memql/component/worker/probe"
)

// modelprobe.go implements the model-probe envelope (epic memql#5146, D3).
//
// ===========================================================================
// THE PULL'S SHAPE, FOR THE PULL'S REASONS
// ===========================================================================
// A probe runs a suite of generations against a local runtime and takes
// minutes, which is past the engine-driven `workerHost.exec` path's 600s cap
// and past its buffer. So: a start, a stream of observations, and an end,
// correlated by request id -- the same pattern, deliberately, rather than the
// same message.
//
// ===========================================================================
// TWO THINGS IT DOES NOT INHERIT
// ===========================================================================
// A PULL'S COUNTERS GO BACKWARDS; A PROBE'S DO NOT. Ollama reports bytes per
// blob, restarting at zero for each, so the pull carries `Latest` rather than a
// fold and refuses to invent a denominator. A probe has a KNOWN number of cases
// and completes them in order, so `Completed` and `Total` are a real fraction
// and a surface may draw one.
//
// A PULL HAS NOTHING TO FALL THROUGH TO AND NEITHER DOES THIS, but the reasons
// differ and both are worth saying. A pull names one machine because a person
// pressed Pull on that machine's page. A probe names one machine because a
// measurement OF A DIFFERENT MACHINE answers a question nobody asked -- the
// whole content of the figure is which hardware produced it.

// ErrModelProbeUnsupported is returned when the selected worker's stream cannot
// carry a probe -- a cockpit that predates the feature. Distinct from a generic
// refusal because the fix is "update the cockpit", which no other refusal here
// implies.
var ErrModelProbeUnsupported = errors.New("worker: worker does not support model probes")

// ErrModelProbeNotFound is returned when a progress or end message names a
// probe this node is not hosting.
var ErrModelProbeNotFound = errors.New("worker: model probe not found")

// ErrModelProbeIdle is returned when a probe stops reporting past its idle
// ceiling.
var ErrModelProbeIdle = errors.New("worker: model probe went silent")

// DefaultModelProbeTimeout is the whole-probe ceiling.
//
// Generous, because the suite runs ten generations and the 32K case on a
// modest machine is minutes on its own -- but BOUNDED, because a probe that
// never ends holds a person's GPU and reports nothing.
const DefaultModelProbeTimeout = 30 * time.Minute

// DefaultModelProbeIdleTimeout is how long the machine may report nothing
// between cases. One case, not the whole suite: a machine that has gone quiet
// mid-suite is one whose runtime died, and waiting the whole ceiling for that
// answer wastes the difference.
const DefaultModelProbeIdleTimeout = 5 * time.Minute

// ModelProbeRequest is one probe.
type ModelProbeRequest struct {
	// RequestId correlates progress and the end back to this request. The
	// caller supplies it; the stream stamps one when it is blank.
	RequestId string
	// RegistrationId is carried even though the stream identifies the machine,
	// for ModelPullStart's reason: one machine may hold more than one
	// registration over its life.
	RegistrationId string
	// Model is the model id in the runtime's own vocabulary, VERBATIM.
	Model string
	// SuiteVersion is the suite the figures are to be scored under. Blank means
	// this engine's pinned version.
	SuiteVersion string
	Limits       ModelProbeLimits
}

// ModelProbeLimits are the ceilings one probe runs under.
type ModelProbeLimits struct {
	Timeout     time.Duration
	IdleTimeout time.Duration
}

func (l ModelProbeLimits) withDefaults() ModelProbeLimits {
	if l.Timeout <= 0 {
		l.Timeout = DefaultModelProbeTimeout
	}
	if l.IdleTimeout <= 0 {
		l.IdleTimeout = DefaultModelProbeIdleTimeout
	}
	return l
}

// ModelProbeProgress is one case finished.
type ModelProbeProgress struct {
	CaseId string
	// Completed and Total are a REAL fraction, unlike the pull's per-layer
	// counters: the suite's length is known before it starts.
	Completed uint32
	Total     uint32
	Ok        bool
	// Error is the runtime's own sentence for a case that failed. Empty when
	// Ok. Never invented on this side.
	Error string
}

// ModelProbeOutcome is the terminal state of a probe.
type ModelProbeOutcome struct {
	Model string
	Ok    bool
	// Error is the machine's own sentence when the probe itself failed.
	Error string
	// SuiteVersion is echoed back by the machine, so a measurement can never be
	// filed under a suite the machine did not actually run.
	SuiteVersion string
	Figures      probe.Figures
}

// ModelProbeHandle is one probe in flight on this node.
type ModelProbeHandle struct {
	requestId string
	model     string
	limits    ModelProbeLimits

	progress chan ModelProbeProgress
	done     chan struct{}

	mu           sync.Mutex
	latest       ModelProbeProgress
	outcome      ModelProbeOutcome
	endErr       error
	ended        bool
	lastActivity time.Time
	closeOne     sync.Once

	cancelFn func(*memqlv1.ModelProbeCancel) error
	detach   func()
	clockFn  func() time.Time
}

// NewModelProbeHandle builds a handle. Exported for the stream session, which
// lives in this package, and for the tests.
func NewModelProbeHandle(
	requestId, model string,
	limits ModelProbeLimits,
	cancelFn func(*memqlv1.ModelProbeCancel) error,
	detach func(),
	clock func() time.Time,
) *ModelProbeHandle {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &ModelProbeHandle{
		requestId: requestId,
		model:     model,
		limits:    limits.withDefaults(),
		// Buffered to the suite's length, so an ordinary run never drops an
		// observation even if the consumer is slow. A probe's progress is a
		// short, countable sequence, which is exactly the case where buffering
		// is cheap and dropping is visible.
		progress:     make(chan ModelProbeProgress, len(probe.Cases())),
		done:         make(chan struct{}),
		lastActivity: clock(),
		cancelFn:     cancelFn,
		detach:       detach,
		clockFn:      clock,
	}
}

func (h *ModelProbeHandle) clock() time.Time {
	if h.clockFn == nil {
		return time.Now().UTC()
	}
	return h.clockFn()
}

// RequestId returns the correlation id.
func (h *ModelProbeHandle) RequestId() string {
	if h == nil {
		return ""
	}
	return h.requestId
}

// Model returns the model being probed.
func (h *ModelProbeHandle) Model() string {
	if h == nil {
		return ""
	}
	return h.model
}

// Progress is the stream of observations. Closed when the probe ends.
func (h *ModelProbeHandle) Progress() <-chan ModelProbeProgress {
	if h == nil {
		return nil
	}
	return h.progress
}

// Latest is the most recent observation.
func (h *ModelProbeHandle) Latest() ModelProbeProgress {
	if h == nil {
		return ModelProbeProgress{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest
}

// Cancel asks the machine to stop. Idempotent.
func (h *ModelProbeHandle) Cancel(reason string) error {
	if h == nil {
		return ErrModelProbeNotFound
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
	return h.cancelFn(&memqlv1.ModelProbeCancel{RequestId: h.requestId, Reason: reason})
}

// Wait blocks until the probe ends, its ceiling expires, the context is
// cancelled, or the machine goes silent past the idle ceiling.
//
// EVERY GIVING-UP PATH FINISHES THE HANDLE, which is the pull's hard-won rule
// and not tidiness: `finish` is the only thing that closes `h.progress`, and
// the ordinary consumer of that channel is a `for range` in a goroutine the
// caller then waits on. A Wait that returned without finishing leaves that
// goroutine ranging forever and the caller blocked on it forever -- and the
// idle path is where it bites, because it is reached precisely BECAUSE the
// machine has gone quiet, so the End it would be hoping for is the message
// least likely to arrive.
//
// finish is idempotent, so a real End arriving afterwards is a no-op: the first
// verdict wins, and it is this node's own account of why it stopped waiting.
func (h *ModelProbeHandle) Wait(ctx context.Context) (ModelProbeOutcome, error) {
	if h == nil {
		return ModelProbeOutcome{}, ErrModelProbeNotFound
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
			h.finish(ModelProbeOutcome{Error: "caller_cancelled"}, err)
			return ModelProbeOutcome{}, err
		case now := <-tick.C:
			if now.After(deadline) {
				_ = h.Cancel("probe_timeout")
				err := fmt.Errorf("worker: model probe exceeded its %s ceiling", h.limits.Timeout)
				h.finish(ModelProbeOutcome{Error: "probe_timeout"}, err)
				return ModelProbeOutcome{}, err
			}
			h.mu.Lock()
			idleFor := now.Sub(h.lastActivity)
			h.mu.Unlock()
			if idleFor > h.limits.IdleTimeout {
				_ = h.Cancel("idle_timeout")
				h.finish(ModelProbeOutcome{Error: "idle_timeout"}, ErrModelProbeIdle)
				return ModelProbeOutcome{}, ErrModelProbeIdle
			}
		}
	}
}

func (h *ModelProbeHandle) idlePoll() time.Duration {
	p := h.limits.IdleTimeout / 4
	if p < 5*time.Millisecond {
		p = 5 * time.Millisecond
	}
	if p > 5*time.Second {
		p = 5 * time.Second
	}
	return p
}

// DeliverProgress hands one observation to the caller.
//
// THE SEND HAPPENS UNDER THE LOCK, paired with finish's close, because a send
// on a closed channel panics even inside a select. It costs nothing because the
// send is non-blocking.
func (h *ModelProbeHandle) DeliverProgress(p ModelProbeProgress) {
	if h == nil {
		return
	}
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
	}
}

// Finish records the terminal state and releases every waiter.
func (h *ModelProbeHandle) Finish(outcome ModelProbeOutcome, err error) {
	if h == nil {
		return
	}
	h.finish(outcome, err)
}

func (h *ModelProbeHandle) finish(outcome ModelProbeOutcome, err error) {
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

// ModelProbeFunc is the per-stream hook that opens a probe.
type ModelProbeFunc func(ctx context.Context, req ModelProbeRequest) (*ModelProbeHandle, error)

// SetModelProbeFunc wires the per-stream probe hook.
func (w *Worker) SetModelProbeFunc(fn ModelProbeFunc) {
	if w == nil {
		return
	}
	w.modelProbeFn = fn
}

// StartModelProbe asks this machine to measure a model it already has.
//
// IT DOES NOT TAKE A CONCURRENCY SLOT, for StartModelPull's reason: a slot is
// the machine's declared ceiling on INFERENCE calls it will serve for other
// people, and a probe is the machine measuring itself. Holding one for the
// length of a suite would make a machine look busy for work it can still do.
//
// IT DOES CHECK RunsModel, and that is where it differs from a pull. A pull
// exists precisely because the machine has not got the model; a probe measures
// a model the machine HAS, and probing one it does not advertise would measure
// a download rather than a model.
func (w *Worker) StartModelProbe(ctx context.Context, req ModelProbeRequest) (*ModelProbeHandle, error) {
	if w == nil || w.modelProbeFn == nil {
		return nil, ErrModelProbeUnsupported
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		return nil, fmt.Errorf("worker: model probe requires a model")
	}
	if !w.RunsModel(req.Model) {
		return nil, fmt.Errorf("worker: %s does not advertise %s, so there is nothing here to measure", w.RegistrationId, req.Model)
	}
	if strings.TrimSpace(req.SuiteVersion) == "" {
		req.SuiteVersion = probe.SuiteVersion
	}
	if _, err := probe.Suite(req.SuiteVersion); err != nil {
		return nil, err
	}
	return w.modelProbeFn(ctx, req)
}

// FiguresFromProto reads the four figures off a ModelProbeEnd.
//
// A figure the machine did not send at all reads as ABSENT with `unmeasured`,
// never as a zero -- the same direction FigureFromRow takes, and for the same
// reason: a number is what gets ranked on, so a figure nobody sent must not
// become one.
func FiguresFromProto(end *memqlv1.ModelProbeEnd) probe.Figures {
	return probe.Figures{
		StructuredValidity:  figureFromProto(end.GetStructuredValidity()),
		ToolCallCorrectness: figureFromProto(end.GetToolCallCorrectness()),
		ThroughputTps:       figureFromProto(end.GetThroughputTps()),
		TimeToFirstTokenMs:  figureFromProto(end.GetTtftMs()),
	}
}

func figureFromProto(p *memqlv1.ProbeFigure) probe.Figure {
	if p == nil {
		return probe.Absent(probe.AbsentUnmeasured, "")
	}
	if !p.GetMeasured() {
		return probe.Absent(probe.AbsentReason(p.GetAbsentReason()), p.GetAbsentDetail())
	}
	// Read back through the ROW shape rather than reconstructing a Stat here,
	// so there is exactly one place that decides what a measured figure looks
	// like and one place that refuses a malformed one.
	return probe.FigureFromRow(map[string]any{
		"measured":   true,
		"median":     p.GetMedian(),
		"spreadLow":  p.GetSpreadLow(),
		"spreadHigh": p.GetSpreadHigh(),
		"n":          float64(p.GetN()),
	})
}
