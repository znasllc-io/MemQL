//go:build planner

package app

import (
	"context"
	"time"

	"github.com/znasllc-io/memql/component/events"
	"github.com/znasllc-io/memql/component/healing"
	"github.com/znasllc-io/memql/integrations/planner"
)

// integrations_work_compile.go -- joins the two halves of epic A2's compile
// (memql#4966).
//
// integrations/work declares `Compiler` as a seam and integrations/planner
// implements it, and until this call is made NOTHING JOINS THEM: createGoal
// opens the goal and its first run, finds no compiler, and returns
// compileDispatched:false, leaving the run in `compiling` forever. From
// outside that is indistinguishable from a goal that was accepted and then
// ignored -- and the wait-and-abandon sweep deliberately does not touch a run
// in `compiling`, so nothing else would move it either.
//
// It runs on the PLANNER node only, which is what section H of the design
// record says: "The planner node keeps compile, the reactive loop and the
// sweeps; the agent node runs steps."

func (a *App) wireWorkCompiler() {
	if a.plannerIntegration == nil {
		return
	}
	work := a.lookupWorkIntegration()
	if work == nil {
		// A planner node whose work plug-in did not materialize. Loud,
		// because every goal created on this node will sit in `compiling`
		// and the symptom -- "my goal never started" -- names nothing.
		a.Logger.Warn("work compile not wired: the work integration did not materialize on this planner node; goals will open a run and never compile",
			"component", "work")
		return
	}
	// a.plannerIntegration is `any` on the App (the field is shared with
	// builds that do not link this package), so the assertion is where the
	// type comes back.
	pi, ok := a.plannerIntegration.(*planner.PlannerIntegration)
	if !ok || pi == nil {
		a.Logger.Warn("work compile not wired: the stashed planner integration is not the expected type", "component", "work")
		return
	}
	// The work integration is BOTH the seam compile is installed on and the
	// writer it records through -- the planner is not in the call-origin
	// allowlist and must not write @serverOnly constructs itself.
	compiler := pi.WorkCompiler(work)
	if compiler == nil {
		a.Logger.Warn("work compile not wired: the planner integration has no agent loop", "component", "work")
		return
	}
	work.SetCompiler(compiler)
	a.Logger.Info("work compile wired to the planner's authoring pipeline", "component", "work")

	// The OTHER direction, wired in the same breath (memql#5000): the
	// reactive loop opens a work GOAL for a due responsibility now, instead
	// of a Plan, and it cannot write one itself for the reason above. Without
	// this call a due responsibility opens nothing at all -- which the loop
	// reports as an error per spawn rather than silently, but the place to
	// prevent it is here.
	pi.SetWorkGoals(work)
	a.Logger.Info("the planner wired to the work spine; a due responsibility, the refresh cadence and an approved training request all open goals",
		"component", "work")
}

// wireWorkFailurePath joins the two acts a classified failure needs the
// planner for, and subscribes the healer that has never had a caller
// (epic memql#5127, design D12).
//
// # Why here
//
// Section H of the work-spine record: "the planner node keeps compile, the
// reactive loop and the sweeps; the agent node runs steps." A replan re-emits
// a plan, which is compile machinery, and repair records against a run the
// sweep is holding -- both belong on the node that already owns those.
//
// # The healer was complete, never ran, and looked complete
//
// integrations/planner/work_heal.go's own header records the shape of the gap
// it was written to close: the emitter existed, the mesh routing rule existed,
// the tests existed, and NewRepairLoop had no non-test caller. Writing the
// subscriber did not close it, because nothing constructed the subscriber
// either. This call is the last link, and it is why "a precondition miss
// raises a planReview" is a claim about a running system rather than about a
// package.
func (a *App) wireWorkFailurePath() {
	if a.plannerIntegration == nil {
		return
	}
	work := a.lookupWorkIntegration()
	if work == nil {
		a.Logger.Warn("work failure path not wired: the work integration did not materialize on this planner node; a classified failure will park and stay parked",
			"component", "work")
		return
	}
	pi, ok := a.plannerIntegration.(*planner.PlannerIntegration)
	if !ok || pi == nil {
		return
	}
	// The remedy: replan and repair. A nil remedy leaves those two waits
	// PARKED rather than abandoned, which is visible; the alternative --
	// treating "I cannot remedy this" as "somebody else will" -- is how a run
	// reaches `abandoned` while the thing that could have fixed it was on
	// another replica.
	if remedy := pi.WorkRemedy(work); remedy != nil {
		work.SetRemedy(remedy)
		a.Logger.Info("work failure path wired: a plan miss re-plans the gap keeping the completed prefix, and a contract miss records the violation and resumes",
			"component", "work")
	}

	// The healer: a precondition miss becomes a planReview approval, never a
	// silent edit (design D5).
	//
	// ITS PROVIDER STILL COMES OFF THE REGISTRY BY NAME, and that is a
	// deliberate seam of this epic rather than an oversight: the call-site
	// sweep that re-points every model call through the router covers this one
	// and safety's together, because they are the same shape -- a leaf package
	// that takes an injected common.ChatStructuredProvider from its caller.
	if provider := a.engine.StructuredChatProviderByName(context.Background(), a.engine.DefaultProviderName()); provider != nil {
		if healer := pi.WorkHealer(healing.NewRepairLoop(provider), work, workHealApprovalTTL); healer != nil {
			a.eventBus.Subscribe(events.TopicPreconditionMissed, healer.HandlePreconditionMissed,
				events.WithSubscriberName("work:heal"))
			a.Logger.Info("work healer subscribed: a precondition miss now proposes typed patches as a planReview approval",
				"component", "work")
		}
	} else {
		a.Logger.Warn("work healer not subscribed: no structured-output provider is registered on this node, so a precondition miss will be recorded and not proposed against",
			"component", "work")
	}
}

// workHealApprovalTTL is how long a healing proposal waits for a person. A
// week: the proposal is a patch to a template, and the person who can judge it
// is not necessarily the person who was watching when it failed.
const workHealApprovalTTL = 7 * 24 * time.Hour
