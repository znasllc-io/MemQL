package work

// failure_waits.go -- the sweep's half of the work spine's failure path
// (epic memql#5127, design D12).
//
// The executor classifies a failed run's symptom and lands the ACT on the run
// as a `waiting` state; this is where the act is served. The split is not
// tidiness: the executor runs on an agent replica and compile, replan and the
// sweeps run on the planner (work-spine design record, section H), so an act
// performed where it was decided would be a call into machinery that node does
// not have, on a run another node owns.
//
// THREE KINDS ARRIVE HERE AND ONE OF THEM NEEDS NOTHING NEW.
//
//   - `retry` is a due timer plus a re-dispatch: the symptom said the failure
//     was a blip, the retry budget had room, and the run resumes from the step
//     that was in flight because the seam replays the journal.
//   - `replan` and `repair` need a REMEDY -- the machinery that invokes
//     replanGap or re-runs a step with the violation as guidance. That lives
//     in the planner, so it arrives here as a seam.
//
// A NODE WITH NO REMEDY LEAVES THEM PARKED, and that is the honest default.
// The alternative -- treating "I cannot remedy this" as "somebody else will"
// -- is how a run reaches `abandoned` while the thing that could have fixed it
// was simply on another replica. A parked run is visible, waits on a state a
// person can read, and is picked up by the next pass on a node that can serve
// it.

import (
	"context"
	"errors"
	"time"
)

// The wait kinds the failure path writes. They must agree with
// component/automations' constants of the same names; the two sides are in
// different modules and a mismatch is a run waiting for something no sweep
// looks for, which presents as a run that simply never moves.
const (
	waitKindRetry  = "retry"
	waitKindReplan = "replan"
	waitKindRepair = "repair"
)

// Remedy is the seam the two acts that need more than a row go through.
// Implemented by integrations/planner, wired from app/ on the planner node.
type Remedy interface {
	// Replan re-plans from the failed step on, KEEPING the completed prefix.
	// Never from the start: the steps that already succeeded are evidence
	// rather than a draft, and re-planning over them is the unguided rerun
	// the debugging literature measured as substantially worse than localized
	// repair. Returns whether it took the run.
	Replan(ctx context.Context, runId, ownerUserId, stepKey, reason string) bool

	// Repair re-runs ONE failed step with the violation as guidance, leaving
	// every other step alone. Returns whether it took the run.
	Repair(ctx context.Context, runId, ownerUserId, stepKey, violation string) bool
}

// SetRemedy installs the remedy seam. A nil remedy is a working state: replan
// and repair waits stay parked and are logged once per pass rather than
// silently dropped.
func (i *Integration) SetRemedy(r Remedy) {
	if i == nil {
		return
	}
	i.mu.Lock()
	i.remedy = r
	i.mu.Unlock()
}

func (i *Integration) remedyRef() Remedy {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.remedy
}

// failureWait reads the wait a classified failure wrote. ok is false for every
// other wait shape, so this function decides nothing about approvals, timers or
// the inference park.
func failureWait(run map[string]any) (kind, stepKey, reason, resumeAt string, ok bool) {
	waiting := rowMap(run, "waitingOn")
	if waiting == nil {
		return "", "", "", "", false
	}
	k, _ := waiting["kind"].(string)
	switch trim(k) {
	case waitKindRetry, waitKindReplan, waitKindRepair:
	default:
		return "", "", "", "", false
	}
	subject, _ := waiting["subject"].(string)
	why, _ := waiting["reason"].(string)
	at, _ := waiting["resumeAt"].(string)
	return trim(k), trim(subject), trim(why), trim(at), true
}

// serveFailureWait handles one classified-failure wait. It reports whether the
// run was moved, so the caller knows not to consider it for anything else.
func (i *Integration) serveFailureWait(ctx context.Context, run map[string]any, runId, owner string, now time.Time, res *WaitSweepResult) (handled bool) {
	kind, stepKey, reason, resumeAt, ok := failureWait(run)
	if !ok {
		return false
	}

	switch kind {
	case waitKindRetry:
		// A retry that carries no resumeAt is not due yet and never will be,
		// which is a bug on the writing side rather than a reason to retry
		// immediately -- retrying at once is what the backoff exists to
		// prevent.
		at, parsed := parseTime(resumeAt)
		if !parsed || at.After(now) {
			return true
		}
		if i.redispatchStale(ownerActor(ctx, owner), run, runId, owner) {
			i.log().Info("work: a failure the rules called transient came due and was handed back to the cluster",
				"component", "work.sweep", "run", runId, "step", stepKey, "reason", reason)
			res.Redispatched++
		}
		return true

	case waitKindReplan, waitKindRepair:
		remedy := i.remedyRef()
		if remedy == nil {
			i.log().Info("work: a run is waiting on a remedy this node cannot serve, so it stays parked",
				"component", "work.sweep", "run", runId, "kind", kind, "step", stepKey)
			return true
		}
		took := false
		if kind == waitKindReplan {
			took = remedy.Replan(ctx, runId, owner, stepKey, reason)
		} else {
			took = remedy.Repair(ctx, runId, owner, stepKey, reason)
		}
		if took {
			i.log().Info("work: a classified failure was handed to its remedy",
				"component", "work.sweep", "run", runId, "kind", kind, "step", stepKey)
			res.Redispatched++
		}
		return true
	}
	return false
}

// ReplanContext is what the replanGap prompt needs and only this package can
// read: the goal in the person's own words, the steps that already succeeded,
// and the step the plan broke at.
//
// THE COMPLETED PREFIX IS THE POINT. The steps that already ran are EVIDENCE,
// not a draft: re-planning from the start is the unguided rerun the debugging
// literature measured as substantially worse than localized repair, and it
// also re-executes side effects that already happened. The prompt is shown
// them as fixed and instructed never to re-emit them.
type ReplanContext struct {
	Statement      string
	CompletedSteps []map[string]any
	FailedStep     map[string]any
}

// LoadReplanContext reads the context for one run's replan under the OWNER's
// borrowed authority, the way every read in this package does. A run whose
// steps cannot be read yields an error rather than an empty prefix: replanning
// against a prefix that is empty because the read failed would re-emit every
// completed step, which is precisely the failure the prefix exists to prevent.
func (i *Integration) LoadReplanContext(ctx context.Context, ownerUserId, runId, stepKey string) (ReplanContext, error) {
	if i == nil {
		return ReplanContext{}, errNoIntegration
	}
	actorCtx := ownerActor(ctx, ownerUserId)
	st := i.store()

	run, err := st.runForOwner(actorCtx, runId)
	if err != nil {
		return ReplanContext{}, err
	}
	out := ReplanContext{}
	if goalId := rowString(run, "goalId"); goalId != "" {
		if goal, gerr := st.goalForOwner(actorCtx, goalId); gerr == nil && goal != nil {
			out.Statement = rowString(goal, "statement")
		}
	}

	steps, err := st.query(actorCtx, "query "+call("workStepsForOwnerRun", map[string]any{"runId": runId}))
	if err != nil {
		return ReplanContext{}, err
	}
	for _, step := range steps {
		key := rowString(step, "key")
		entry := map[string]any{
			"key":  key,
			"call": rowString(step, "call"),
		}
		switch rowString(step, "status") {
		case "done":
			entry["result"] = step["result"]
			out.CompletedSteps = append(out.CompletedSteps, entry)
		case "failed":
			// The named step wins when it is present, so a run with more than
			// one failure replans from the one the classifier judged rather
			// than from whichever the read returned last.
			if out.FailedStep == nil || key == stepKey {
				entry["errorMessage"] = rowString(step, "errorMessage")
				entry["symptom"] = rowString(step, "symptom")
				out.FailedStep = entry
			}
		}
	}
	return out, nil
}

// errNoIntegration is returned rather than a nil-pointer panic when a caller
// holds a nil integration. It is its own value so a test can tell it apart
// from a read failure.
var errNoIntegration = errors.New("work: the integration is nil")
