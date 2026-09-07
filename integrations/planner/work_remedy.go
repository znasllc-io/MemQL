package planner

// work_remedy.go -- the planner half of the work spine's failure path
// (epic memql#5127, design D12).
//
// The executor classifies a failed run and lands the ACT on the run as a
// `waiting` state; integrations/work's sweep reads it and hands the two acts
// that need more than a row to this seam. It lives here for the reason
// work_compile_adapter.go lives here: the machinery is the planner's, the
// WRITES are integrations/work's, and the split is not stylistic. The planner
// is not in the call-origin allowlist, so a @serverOnly write from here is
// REFUSED with one WARN nothing above it hears.
//
// REPAIR AND REPLAN ARE DIFFERENT SIZES OF WRONG, and conflating them is the
// mistake this file exists to avoid. A contract miss means one step did
// something other than what it promised: the plan is fine and the step is not.
// A plan miss means the plan is wrong from that step ON: every remaining step
// is suspect and the completed ones are not. So repair touches one step and
// replan re-emits a suffix -- and neither of them ever re-runs from the start,
// which is the unguided rerun the debugging literature measured as
// substantially worse than localized repair.

import (
	"context"
	"fmt"
	"strings"
	"time"

	workintegration "github.com/znasllc-io/memql/integrations/work"
)

// replanContextLoader is the read half. It is an interface rather than the
// concrete integration so this file can be tested without a database, and so
// the direction of the dependency stays "planner asks work", never the reverse.
type replanContextLoader interface {
	LoadReplanContext(ctx context.Context, ownerUserId, runId, stepKey string) (workintegration.ReplanContext, error)
}

// remedyWriter is the write half. Both writes go through integrations/work for
// the call-origin reason in this file's header.
type remedyWriter interface {
	RecordCompileOutcome(ctx context.Context, ownerUserId, runId string, fields map[string]any) error
}

// WorkRemedy satisfies workintegration.Remedy.
type WorkRemedy struct {
	loop   *PlannerAgentLoop
	reader replanContextLoader
	writer remedyWriter
	now    func() time.Time
}

// NewWorkRemedy returns nil without a loop or without the work integration, so
// app wiring can call SetRemedy unconditionally and a node that runs no
// planner installs nothing. A nil remedy leaves replan and repair waits parked
// and logged, which is visible; a remedy that cannot record its outcome would
// move the run in the log and never in the graph.
func NewWorkRemedy(loop *PlannerAgentLoop, work *workintegration.Integration) *WorkRemedy {
	if loop == nil || work == nil {
		return nil
	}
	return &WorkRemedy{loop: loop, reader: work, writer: work, now: time.Now}
}

// Replan re-plans the gap from the failed step on, KEEPING the completed
// prefix, and puts the run back to `running` with the new template.
//
// The prefix is loaded rather than reconstructed, and a read failure ABORTS
// rather than replanning against an empty one: an empty prefix tells the model
// nothing has been done, so it re-emits every completed step, and the side
// effects those steps already had happen a second time. That is a worse
// outcome than a run that stays parked, so the failure mode chosen here is the
// one a person can see.
func (r *WorkRemedy) Replan(ctx context.Context, runId, ownerUserId, stepKey, reason string) bool {
	if r == nil || r.loop == nil || r.loop.engine == nil {
		return false
	}
	rc, err := r.reader.LoadReplanContext(ctx, ownerUserId, runId, stepKey)
	if err != nil {
		r.warn("work remedy: could not read the run's replan context; the run stays parked rather than re-planning against an empty prefix", runId, err)
		return false
	}
	if rc.FailedStep == nil {
		r.warn("work remedy: the run has no failed step to replan from", runId, fmt.Errorf("no failed step at %q", stepKey))
		return false
	}

	data := map[string]any{
		"statement":      rc.Statement,
		"completedSteps": rc.CompletedSteps,
		"failedStep":     rc.FailedStep,
		"now":            r.clock().UTC().Format(time.RFC3339),
	}
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		data["remainingGoal"] = trimmed
	}

	// replanGap declares @level("reasoning"), so the router resolves it at the
	// level the prompt asked for and the shipped reasoningParks rule decides
	// what happens when no door can serve it. Nothing here names a model.
	out, err := r.loop.engine.InvokeAI(ctx, "replanGap", data)
	if err != nil {
		r.warn("work remedy: the replan call failed; the run stays parked", runId, err)
		return false
	}
	draft := strings.TrimSpace(fmt.Sprint(out))
	if draft == "" {
		r.warn("work remedy: the replan returned nothing", runId, fmt.Errorf("empty draft"))
		return false
	}

	// The draft lands on the run as its new template and the run goes back to
	// `running`. waitingOn is CLEARED in the same write: a run at `running`
	// that still names a wait is one every sweep will keep trying to serve.
	if err := r.writer.RecordCompileOutcome(ctx, ownerUserId, runId, map[string]any{
		"status":    "running",
		"waitingOn": map[string]any{},
		"outcome": map[string]any{
			"replannedFrom":  stepKey,
			"replannedAt":    r.clock().UTC().Format(time.RFC3339),
			"prefixKept":     len(rc.CompletedSteps),
			"replanDraftLen": len(draft),
		},
	}); err != nil {
		r.warn("work remedy: could not record the replan outcome", runId, err)
		return false
	}
	if r.loop.logger != nil {
		r.loop.logger.Info("work remedy: re-planned the gap from a failed step, keeping the completed prefix",
			"run", runId, "step", stepKey, "prefixKept", len(rc.CompletedSteps))
	}
	return true
}

// Repair records the violation on the run and hands it back to be resumed from
// the failed step.
//
// WHAT THIS IS NOT, and the honesty matters more than the code. A full guided
// repair re-runs the step with the violation IN ITS OWN BODY, and no step type
// in this tree reads guidance. So this records what was violated where every
// reader can see it and resumes -- which is a retry that carries a note, and
// is the whole of what the current step types can act on.
//
// It is also UNREACHABLE TODAY. The only rule that produces a contract symptom
// keys on Signal.PostconditionFailed, and nothing in the automations executor
// evaluates postconditions, so no failure classifies as a contract miss. This
// arm exists so a producer arriving later is served rather than parked; do not
// read its presence as evidence that contract misses are being caught.
func (r *WorkRemedy) Repair(ctx context.Context, runId, ownerUserId, stepKey, violation string) bool {
	if r == nil || r.loop == nil {
		return false
	}
	if err := r.writer.RecordCompileOutcome(ctx, ownerUserId, runId, map[string]any{
		"status":    "running",
		"waitingOn": map[string]any{},
		"outcome": map[string]any{
			"repairedStep": stepKey,
			"violation":    violation,
			"repairedAt":   r.clock().UTC().Format(time.RFC3339),
		},
	}); err != nil {
		r.warn("work remedy: could not record the repair outcome", runId, err)
		return false
	}
	if r.loop.logger != nil {
		r.loop.logger.Info("work remedy: recorded a contract violation on a run and resumed it from the failed step",
			"run", runId, "step", stepKey)
	}
	return true
}

func (r *WorkRemedy) clock() time.Time {
	if r == nil || r.now == nil {
		return time.Now()
	}
	return r.now()
}

func (r *WorkRemedy) warn(msg, runId string, err error) {
	if r == nil || r.loop == nil || r.loop.logger == nil {
		return
	}
	r.loop.logger.Warn(msg, "run", runId, "error", err)
}

// WorkRemedy builds the remedy off this integration's own loop, mirroring
// WorkCompiler. app/ asks the integration rather than reaching for the loop,
// which is unexported for the reason every seam here is: the loop's lifecycle
// is the integration's, and a caller holding it across a re-materialize would
// hold a loop wired to a dead engine.
func (p *PlannerIntegration) WorkRemedy(work *workintegration.Integration) *WorkRemedy {
	if p == nil || p.agentLoop == nil {
		return nil
	}
	return NewWorkRemedy(p.agentLoop, work)
}

// WorkHealer builds the healing subscriber off this integration's own loop.
// The proposer is supplied by the caller because it needs a structured-output
// provider, which the integration does not hold.
func (p *PlannerIntegration) WorkHealer(proposer patchProposer, raiser approvalRaiser, ttl time.Duration) *WorkHealer {
	if p == nil || p.agentLoop == nil {
		return nil
	}
	return NewWorkHealer(proposer, raiser, p.agentLoop, ttl)
}
