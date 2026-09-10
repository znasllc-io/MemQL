package automations

import (
	"context"
	"time"
)

// A step may spend minutes waiting for a model or a remote worker. Its
// execution remains live between journal boundaries, so renew the heartbeat
// while the step runs. Stop and join before writing its receipt or closing the
// run: a concurrent read-merge heartbeat must not overwrite a terminal update.
func (j *workJournal) duringStep(ctx context.Context, runId string) func() {
	if j == nil {
		return func() {}
	}
	every := j.heartbeatEvery
	if every <= 0 {
		every = 15 * time.Second
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pulseCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				j.call(pulseCtx, "updateWorkRun", map[string]any{"runId": runId, "heartbeatAt": rfc3339(time.Now())})
				stop()
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (e *Executor) executeJournaledStep(ctx context.Context, journal *workJournal, step *Step, stepCtx *StepContext) (*StepResult, error) {
	stop := journal.duringStep(ctx, stepCtx.Execution.ID)
	defer stop()
	return e.executeStep(ctx, step, stepCtx)
}
