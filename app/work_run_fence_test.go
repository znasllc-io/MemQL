package app

import (
	"github.com/znasllc-io/memql/component/automations"
	work "github.com/znasllc-io/memql/integrations/work"
	"testing"
	"time"
)

func TestExecutionHopFencesLiveStepAfterClaimLeaseExpires(t *testing.T) {
	now := time.Now()
	req := work.DispatchRequest{RunId: "r", GoalId: "g", Status: "running"}
	// The execution lease can be retaken after four minutes, while a model
	// still runs on another replica. Only the stored heartbeat/intent is current.
	j := &automations.RunJournal{RunId: "r", GoalId: "g", Status: "running", HasRunningStep: true, HeartbeatAt: now.Add(-15 * time.Second), FailedStep: "old-failure"}
	if workRunCanStart(req, j, now) {
		t.Fatal("second replica would re-enter a live step after lease expiry")
	}
	j.HasRunningStep = false
	if !workRunCanStart(req, j, now) {
		t.Fatal("failed-step retry was fenced despite no running intent")
	}
	j.HasRunningStep = true
	j.HeartbeatAt = now.Add(-5 * time.Minute)
	if !workRunCanStart(req, j, now) {
		t.Fatal("stale crashed step cannot be recovered")
	}
	j.Status = "succeeded"
	if workRunCanStart(req, j, now) {
		t.Fatal("terminal run admitted a stale event")
	}
}
