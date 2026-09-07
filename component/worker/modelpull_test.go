package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
)

// newTestModelPull builds a handle with no stream behind it, the way
// newTestModelCall does and for the same reason: the progress discipline is a
// property of the handle, and the rule that keeps a progress bar honest should
// not need a cluster to demonstrate.
func newTestModelPull(t *testing.T, limits ModelPullLimits) (*ModelPullHandle, *[]*memqlv1.ModelPullCancel) {
	t.Helper()
	var sent []*memqlv1.ModelPullCancel
	h := &ModelPullHandle{
		requestId:    "pull-1",
		model:        "llama3.1:8b",
		limits:       limits.withDefaults(),
		progress:     make(chan ModelPullProgress, modelPullProgressBuffer),
		done:         make(chan struct{}),
		clock:        time.Now,
		lastActivity: time.Now(),
		cancelFn: func(c *memqlv1.ModelPullCancel) error {
			sent = append(sent, c)
			return nil
		},
	}
	return h, &sent
}

func drainPull(h *ModelPullHandle) []ModelPullProgress {
	var out []ModelPullProgress
	for {
		select {
		case p, ok := <-h.Progress():
			if !ok {
				return out
			}
			out = append(out, p)
		default:
			return out
		}
	}
}

// ===========================================================================
// THE HEADLINE PROPERTY: A PULL'S COUNTERS GO BACKWARDS AND THAT IS CORRECT
// ===========================================================================
// This is the one place a pull must NOT inherit the model call's rule. A
// generation's deltas are a record and an out-of-order one is dropped; a
// pull's counters are PER LAYER and restart at zero for every blob, so the
// same "monotonic or drop" rule would silently swallow the start of every
// layer after the first -- leaving a bar that fills once and then freezes,
// which reads as a hung download of a pull that is working perfectly.
func TestModelPullKeepsCountersThatRestartPerLayer(t *testing.T) {
	h, _ := newTestModelPull(t, ModelPullLimits{})

	h.deliverProgress(ModelPullProgress{Layer: "sha256:aaa", CompletedBytes: 900, TotalBytes: 1000, Status: "pulling aaa"})
	// A NEW layer: completed drops from 900 to 10 and total changes. Both are
	// true statements about different blobs.
	h.deliverProgress(ModelPullProgress{Layer: "sha256:bbb", CompletedBytes: 10, TotalBytes: 4000, Status: "pulling bbb"})
	h.deliverProgress(ModelPullProgress{Layer: "sha256:bbb", CompletedBytes: 4000, TotalBytes: 4000, Status: "pulling bbb"})
	// A phase that is not a download at all names no layer and counts nothing.
	h.deliverProgress(ModelPullProgress{Status: "verifying sha256 digest"})

	got := drainPull(h)
	if len(got) != 4 {
		t.Fatalf("expected all 4 observations to survive, got %d: %+v", len(got), got)
	}
	if got[1].CompletedBytes != 10 {
		t.Fatalf("the second layer's first observation was dropped or rewritten: %+v", got[1])
	}
	if got[3].Layer != "" || got[3].Status != "verifying sha256 digest" {
		t.Fatalf("a non-download phase must ride through unchanged, got %+v", got[3])
	}
}

// The handle reports the LAST observation rather than a running total,
// because there is no running total to report: see the field's own note.
func TestModelPullLatestIsTheLastObservationNotASum(t *testing.T) {
	h, _ := newTestModelPull(t, ModelPullLimits{})

	h.deliverProgress(ModelPullProgress{Layer: "a", CompletedBytes: 900, TotalBytes: 1000, Status: "pulling a"})
	h.deliverProgress(ModelPullProgress{Layer: "b", CompletedBytes: 10, TotalBytes: 4000, Status: "pulling b"})

	latest := h.Latest()
	if latest.CompletedBytes != 10 || latest.TotalBytes != 4000 || latest.Layer != "b" {
		t.Fatalf("Latest() = %+v, want the second observation verbatim", latest)
	}
}

// A pull that ends carries the machine's own verdict, including whether the
// cluster can see the model yet. `readvertised=false` is a materially
// different answer from a failure and from a success, and all three have to
// survive to the caller.
func TestModelPullEndCarriesReadvertised(t *testing.T) {
	h, _ := newTestModelPull(t, ModelPullLimits{})
	h.finish(ModelPullOutcome{Ok: true, Model: "llama3.1:8b", Readvertised: false}, nil)

	out, err := h.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !out.Ok {
		t.Fatalf("outcome should be ok: %+v", out)
	}
	if out.Readvertised {
		t.Fatalf("readvertised must ride through as false, got %+v", out)
	}
}

// A pull can fail INSIDE a successful stream -- Ollama reports errors in the
// body of an HTTP 200 -- so an end that says ok=false with text is the normal
// shape of a failure, not a dropped connection.
func TestModelPullFailureArrivesAsAnEndNotADrop(t *testing.T) {
	h, _ := newTestModelPull(t, ModelPullLimits{})
	h.finish(ModelPullOutcome{Ok: false, Model: "llama3.1:8b", Error: "max retries exceeded: EOF"}, nil)

	out, err := h.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait returned a transport error for an in-band failure: %v", err)
	}
	if out.Ok || out.Error == "" {
		t.Fatalf("an in-band failure must arrive as ok=false with text, got %+v", out)
	}
}

// Walking away cancels ON THE MACHINE. A caller that closes the page must not
// leave a 40GB download running on somebody's laptop.
func TestModelPullCancelsOnTheMachineWhenTheCallerLeaves(t *testing.T) {
	h, sent := newTestModelPull(t, ModelPullLimits{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := h.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
	if len(*sent) != 1 || (*sent)[0].GetReason() != "caller_cancelled" {
		t.Fatalf("expected one caller_cancelled on the wire, got %+v", *sent)
	}
}

// The idle ceiling exists because a pull can stall with the connection open.
// It is generous -- a large layer on a slow link is minutes of legitimate
// silence between status lines -- but it is not absent, or a stalled pull
// would hold its slot until the process restarted.
func TestModelPullGivesUpWhenTheMachineGoesSilent(t *testing.T) {
	h, sent := newTestModelPull(t, ModelPullLimits{Timeout: time.Hour, IdleTimeout: 20 * time.Millisecond})

	if _, err := h.Wait(context.Background()); !errors.Is(err, ErrModelPullIdle) {
		t.Fatalf("Wait = %v, want ErrModelPullIdle", err)
	}
	if len(*sent) != 1 || (*sent)[0].GetReason() != "idle_timeout" {
		t.Fatalf("expected one idle_timeout on the wire, got %+v", *sent)
	}
}

// Progress RESETS the idle clock. A pull streaming steadily for an hour is
// working, and a watchdog that ignored its observations would kill it.
func TestModelPullProgressResetsTheIdleClock(t *testing.T) {
	h, _ := newTestModelPull(t, ModelPullLimits{Timeout: time.Hour, IdleTimeout: 120 * time.Millisecond})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 6; i++ {
			time.Sleep(30 * time.Millisecond)
			h.deliverProgress(ModelPullProgress{Layer: "a", CompletedBytes: uint64(i), Status: "pulling a"})
		}
		h.finish(ModelPullOutcome{Ok: true}, nil)
	}()

	out, err := h.Wait(context.Background())
	<-done
	if err != nil {
		t.Fatalf("a steadily-streaming pull was killed as idle: %v", err)
	}
	if !out.Ok {
		t.Fatalf("outcome = %+v, want ok", out)
	}
}

// A machine whose stream has no pull hook is a machine running a cockpit that
// predates this feature. It must say so rather than appear to accept a pull
// that nothing will run.
func TestStartModelPullRefusesAStreamWithNoHook(t *testing.T) {
	w := &Worker{RegistrationId: "reg-1"}
	if _, err := w.StartModelPull(context.Background(), ModelPullRequest{Model: "llama3.1:8b"}); !errors.Is(err, ErrModelPullUnsupported) {
		t.Fatalf("StartModelPull = %v, want ErrModelPullUnsupported", err)
	}
}

func TestStartModelPullRefusesABlankModel(t *testing.T) {
	w := &Worker{RegistrationId: "reg-1"}
	w.SetModelPullFunc(func(context.Context, ModelPullRequest) (*ModelPullHandle, error) {
		t.Fatal("the hook must not be reached for a blank model")
		return nil, nil
	})
	if _, err := w.StartModelPull(context.Background(), ModelPullRequest{Model: "  "}); err == nil {
		t.Fatal("StartModelPull accepted a blank model")
	}
}

// A pull does NOT check RunsModel, and that is the whole point of the
// feature: the model is precisely what the machine does not have yet. This
// pins the difference from StartModelCall, which refuses a model the machine
// never advertised.
func TestStartModelPullAcceptsAModelTheMachineDoesNotYetOffer(t *testing.T) {
	w := &Worker{RegistrationId: "reg-1"}
	var got ModelPullRequest
	w.SetModelPullFunc(func(_ context.Context, req ModelPullRequest) (*ModelPullHandle, error) {
		got = req
		return &ModelPullHandle{}, nil
	})

	if w.RunsModel("llama3.1:8b") {
		t.Fatal("fixture error: the machine should advertise nothing")
	}
	if _, err := w.StartModelPull(context.Background(), ModelPullRequest{Model: "llama3.1:8b"}); err != nil {
		t.Fatalf("StartModelPull refused a model the machine has not got: %v", err)
	}
	if got.Model != "llama3.1:8b" {
		t.Fatalf("request reached the hook as %+v", got)
	}
}
