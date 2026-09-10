package automations

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/memql"
)

type heartbeatJournal struct{ pulses chan string }

func (r *heartbeatJournal) Execute(_ context.Context, q string) (*memql.ExecuteResult, error) {
	if strings.HasPrefix(q, "updateWorkRun(") && strings.Contains(q, "heartbeatAt:") {
		r.pulses <- q
	}
	return &memql.ExecuteResult{}, nil
}

type slowJournalStep struct {
	pulses   chan string
	received chan string
}

func (s slowJournalStep) Execute(_ context.Context, step *Step, _ *StepContext) (*StepResult, error) {
	select {
	case pulse := <-s.pulses:
		s.received <- pulse
	case <-time.After(time.Second):
		s.received <- ""
	}
	return &StepResult{StepId: step.ID, Status: "completed"}, nil
}

func TestExecutingStepKeepsItsRunAlive(t *testing.T) {
	rec := &heartbeatJournal{pulses: make(chan string, 16)}
	got := make(chan string, 1)
	e := NewExecutor(ExecutorOptions{StepRegistry: slowJournalStep{pulses: rec.pulses, received: got}})
	e.journal = newWorkJournal(rec, nil)
	e.journal.heartbeatEvery = 10 * time.Millisecond
	// A regular execution has no initial adoption heartbeat; the pulse must
	// occur while the step is still waiting, not when its receipt is written.
	result, err := e.Execute(context.Background(), adoptProbeAutomation(), "test")
	if err != nil || result.Status != "completed" {
		t.Fatalf("execute: %v %v", result, err)
	}
	if q := <-got; q == "" {
		t.Fatal("no heartbeat during a running step; a slow model looks abandoned")
	}
}
