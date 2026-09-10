package app

import (
	"github.com/znasllc-io/memql/component/automations"
	work "github.com/znasllc-io/memql/integrations/work"
	"time"
)

func workRunCanStart(req work.DispatchRequest, j *automations.RunJournal, now time.Time) bool {
	// A renewable heartbeat fences the current executor after the arbitration
	// lease expires. Failed steps have no live intent and remain resumable.
	if j.Status == "running" && j.HasRunningStep && !j.HeartbeatAt.IsZero() && now.Sub(j.HeartbeatAt) < work.DefaultAbandonedAfterSeconds*time.Second {
		return false
	}
	return req.CanDispatchStoredRun(j.GoalId, j.Status, j.WaitingOn, now)
}
