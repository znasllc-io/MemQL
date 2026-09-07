package worker

import (
	"time"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
)

// modelpull_loopback.go provides an in-process ModelPullHandle for code that
// stands in for a machine's runtime.
//
// WHY THIS IS NOT IN A _test.go FILE -- the argument modelcall_loopback.go
// makes, unchanged. The seam is CROSS-PACKAGE: a ModelPullFunc is installed on
// a *Worker by whoever owns the stream, the only in-tree owner is the gRPC
// server here, and the real runtime lives in the memql-cockpit repository. So
// a test exercising the ENGINE side of a pull -- the cross-replica hop in
// integrations/agent/worker being the one that matters -- has to supply a
// ModelPullFunc from another package and cannot reach an unexported
// constructor.

// NewModelPullLoopback returns a handle driven directly rather than by a
// worker stream, along with the two functions a stand-in runtime uses: `emit`
// delivers one observation, and `finish` closes the pull.
//
// Cancellation is observable: the returned handle's Cancel writes into the
// context the caller passed to its runtime, so a stand-in that parks on
// ctx.Done() behaves like a real one being told to stop -- which is what lets
// a test assert that a stop actually crossed the hop rather than that the
// caller merely gave up waiting.
func NewModelPullLoopback(req ModelPullRequest, onCancel func(reason string)) (
	handle *ModelPullHandle,
	emit func(ModelPullProgress),
	finish func(ModelPullOutcome),
) {
	limits := req.Limits.withDefaults()
	h := &ModelPullHandle{
		requestId:    req.RequestId,
		model:        req.Model,
		limits:       limits,
		progress:     make(chan ModelPullProgress, modelPullProgressBuffer),
		done:         make(chan struct{}),
		clock:        time.Now,
		lastActivity: time.Now(),
	}
	h.cancelFn = func(c *memqlv1.ModelPullCancel) error {
		if onCancel != nil {
			onCancel(c.GetReason())
		}
		return nil
	}
	return h, h.deliverProgress, func(out ModelPullOutcome) { h.finish(out, nil) }
}
