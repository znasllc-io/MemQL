package worker

import (
	"context"
	"testing"
	"time"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	"github.com/znasllc-io/memql/component/worker/probe"
)

func newTestProbeHandle(limits ModelProbeLimits) *ModelProbeHandle {
	return NewModelProbeHandle("req-1", "qwen3.5:9b", limits, func(*memqlv1.ModelProbeCancel) error { return nil }, nil, nil)
}

// drain ranges the progress channel to completion and reports that it ended.
// It is the shape of the real consumer -- a goroutine the caller then waits on
// -- which is what makes "every giving-up path finishes the handle" a property
// worth testing rather than an invariant worth writing down.
func drainProbe(h *ModelProbeHandle) <-chan int {
	done := make(chan int, 1)
	go func() {
		n := 0
		for range h.Progress() {
			n++
		}
		done <- n
	}()
	return done
}

func TestEveryGivingUpPathFinishesTheHandle(t *testing.T) {
	// finish is the only thing that closes the progress channel, and the
	// ordinary consumer is a `for range` in a goroutine the caller waits on. A
	// Wait that gave up WITHOUT finishing would leave that goroutine ranging
	// forever and its caller blocked on it forever -- and the idle path is
	// where it bites, because it is reached precisely because the machine has
	// gone quiet, so the End it would be hoping for is the message least likely
	// to arrive.
	t.Run("idle ceiling", func(t *testing.T) {
		h := newTestProbeHandle(ModelProbeLimits{Timeout: time.Minute, IdleTimeout: 20 * time.Millisecond})
		ended := drainProbe(h)
		if _, err := h.Wait(context.Background()); err != ErrModelProbeIdle {
			t.Fatalf("err %v, want %v", err, ErrModelProbeIdle)
		}
		select {
		case <-ended:
		case <-time.After(2 * time.Second):
			t.Fatal("the progress consumer was never released; Wait gave up without finishing the handle")
		}
	})

	t.Run("whole-probe ceiling", func(t *testing.T) {
		h := newTestProbeHandle(ModelProbeLimits{Timeout: 20 * time.Millisecond, IdleTimeout: time.Minute})
		ended := drainProbe(h)
		if _, err := h.Wait(context.Background()); err == nil {
			t.Fatal("expected the ceiling to end the wait")
		}
		select {
		case <-ended:
		case <-time.After(2 * time.Second):
			t.Fatal("the progress consumer was never released")
		}
	})

	t.Run("caller context", func(t *testing.T) {
		h := newTestProbeHandle(ModelProbeLimits{Timeout: time.Minute, IdleTimeout: time.Minute})
		ended := drainProbe(h)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := h.Wait(ctx); err == nil {
			t.Fatal("expected the cancelled context to end the wait")
		}
		select {
		case <-ended:
		case <-time.After(2 * time.Second):
			t.Fatal("the progress consumer was never released")
		}
	})
}

func TestAnEndArrivingAfterAGiveUpDoesNotOverwriteTheVerdict(t *testing.T) {
	// finish is idempotent, so the FIRST verdict wins -- and it is the right
	// one, because it is this node's own account of why it stopped waiting. A
	// late End is a no-op rather than a race.
	h := newTestProbeHandle(ModelProbeLimits{Timeout: time.Minute, IdleTimeout: 20 * time.Millisecond})
	go drainProbe(h)
	if _, err := h.Wait(context.Background()); err != ErrModelProbeIdle {
		t.Fatalf("err %v", err)
	}
	h.Finish(ModelProbeOutcome{Ok: true}, nil)
	outcome, err := h.Wait(context.Background())
	if err != ErrModelProbeIdle || outcome.Ok {
		t.Fatalf("a late End must not overwrite the give-up verdict, got %+v / %v", outcome, err)
	}
}

func TestProgressIsCountableUnlikeAPulls(t *testing.T) {
	// The one thing the probe does NOT inherit from the pull. A pull's counters
	// restart per blob and go backwards, so it carries `Latest` and refuses to
	// invent a denominator; a probe's suite has a known length, so a surface may
	// draw a real fraction.
	h := newTestProbeHandle(ModelProbeLimits{Timeout: time.Minute, IdleTimeout: time.Minute})
	h.DeliverProgress(ModelProbeProgress{CaseId: "structured.triage", Completed: 1, Total: 10, Ok: true})
	h.DeliverProgress(ModelProbeProgress{CaseId: "structured.intake", Completed: 2, Total: 10, Ok: false, Error: "schema violation"})
	got := h.Latest()
	if got.Completed != 2 || got.Total != 10 {
		t.Fatalf("latest %+v", got)
	}
	if got.Ok || got.Error != "schema violation" {
		t.Fatalf("a failed case must carry the machine's own sentence, got %+v", got)
	}
}

func TestStartModelProbeRefusesAModelTheMachineDoesNotAdvertise(t *testing.T) {
	// The difference from a pull. A pull exists precisely because the machine
	// has not got the model; a probe measures a model the machine HAS, and
	// probing one it does not advertise would measure a download.
	w := &Worker{RegistrationId: "reg-1", Labels: map[string]string{ModelLabel("present:9b"): "1"}}
	w.SetModelProbeFunc(func(context.Context, ModelProbeRequest) (*ModelProbeHandle, error) {
		return newTestProbeHandle(ModelProbeLimits{}), nil
	})
	if _, err := w.StartModelProbe(context.Background(), ModelProbeRequest{Model: "absent:9b"}); err == nil {
		t.Fatal("probing a model the machine does not advertise must be refused")
	}
	if _, err := w.StartModelProbe(context.Background(), ModelProbeRequest{Model: "present:9b"}); err != nil {
		t.Fatalf("probing an advertised model must be admitted: %v", err)
	}
}

func TestStartModelProbeRefusesASuiteVersionThisEngineDoesNotPin(t *testing.T) {
	// Refused HERE rather than on the machine, so the refusal names the engine's
	// pinned version instead of arriving as a probe that ran and reported
	// figures nobody can compare.
	w := &Worker{RegistrationId: "reg-1", Labels: map[string]string{ModelLabel("present:9b"): "1"}}
	w.SetModelProbeFunc(func(context.Context, ModelProbeRequest) (*ModelProbeHandle, error) {
		return newTestProbeHandle(ModelProbeLimits{}), nil
	})
	if _, err := w.StartModelProbe(context.Background(), ModelProbeRequest{Model: "present:9b", SuiteVersion: "99"}); err == nil {
		t.Fatal("an unknown suite version must be refused before the wire")
	}
}

func TestAFigureTheMachineDidNotSendReadsAsUnmeasured(t *testing.T) {
	// A number is what gets ranked on, so a figure nobody sent must not become
	// one. The proto's zero value for a message field is nil, which is exactly
	// the shape "the cockpit did not report this" arrives in.
	figures := FiguresFromProto(&memqlv1.ModelProbeEnd{RequestId: "req-1", Ok: true})
	if err := figures.Validate(); err != nil {
		t.Fatalf("every figure must be measured or name its absence: %v", err)
	}
	reason, _, absent := figures.StructuredValidity.Reason()
	if !absent || reason != probe.AbsentUnmeasured {
		t.Fatalf("an unsent figure must read as unmeasured, got %q absent=%v", reason, absent)
	}
}

func TestAMeasuredFigureCrossesTheWireIntact(t *testing.T) {
	figures := FiguresFromProto(&memqlv1.ModelProbeEnd{
		RequestId: "req-1",
		Ok:        true,
		StructuredValidity: &memqlv1.ProbeFigure{
			Measured: true, Median: 0.8, SpreadLow: 0.8, SpreadHigh: 0.8, N: 5,
		},
	})
	s, ok := figures.StructuredValidity.Stat()
	if !ok {
		t.Fatal("a measured figure must survive the wire")
	}
	if s.Median != 0.8 || s.N != 5 {
		t.Fatalf("got %+v", s)
	}
}

func TestAMeasuredZeroCrossesTheWireAsAZeroAndNotAsAnAbsence(t *testing.T) {
	// The wire's version of the pair the probe package exists for. A model that
	// failed every structured case reports 0.0 with n=5, and that must arrive
	// as a measurement -- `measured: true` is the discriminant, not the value.
	figures := FiguresFromProto(&memqlv1.ModelProbeEnd{
		RequestId:          "req-1",
		StructuredValidity: &memqlv1.ProbeFigure{Measured: true, Median: 0, SpreadLow: 0, SpreadHigh: 0, N: 5},
	})
	s, ok := figures.StructuredValidity.Stat()
	if !ok {
		t.Fatal("a measured zero must arrive measured; it says do not route structured work here")
	}
	if s.Median != 0 || s.N != 5 {
		t.Fatalf("got %+v", s)
	}
}
