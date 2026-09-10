//go:build planner

package app

import (
	"context"
	"time"

	"github.com/znasllc-io/memql/component/events"
	"github.com/znasllc-io/memql/component/healing"
	"github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/core/common"
	"github.com/znasllc-io/memql/integrations/planner"
)

// integrations_work_compile.go -- joins the two halves of epic A2's compile
// (memql#4966).
//
// integrations/work declares `Compiler` as a seam and integrations/planner
// implements it, and until this call is made NOTHING JOINS THEM: createGoal
// opens the goal and its first run. Without this wiring a planner never
// installs a Compiler and never subscribes HandleCompileEvent, so a goal
// accepted on the bff (event-forward path) also never compiles. createGoal
// refuses when neither a local compiler nor EnableCompileViaEvent is set;
// with the event path enabled but no planner subscriber, the abandoned sweep
// closes the run with a "never reached a compile surface" sentence rather
// than claiming the bff node was lost.
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
	// The claimer is what stops two planner replicas from compiling one run
	// twice when both see the graph event. Same guard the agent uses for
	// execution; installed here because the agent wire is tagged out of this
	// binary.
	if a.clusterGuard != nil {
		work.SetRunClaimer(a.clusterGuard)
	}
	// created AND updated: a run is normally created in `compiling` on the
	// bff, and the create event is the handoff. Subscribing to update as well
	// covers a run rewritten back into compiling (rare) and matches the agent
	// dispatcher's posture for the same topics.
	for _, topic := range []string{
		"graph.node.created.v1:work:run",
		"graph.node.updated.v1:work:run",
	} {
		a.eventBus.Subscribe(topic, work.HandleCompileEvent,
			events.WithSubscriberName("work:run-compile"))
	}
	a.Logger.Info("work compile wired to the planner's authoring pipeline; this node compiles runs opened anywhere in the cluster",
		"component", "work")

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
	// ITS PROVIDER NOW COMES FROM THE ROUTER (epic memql#5127, design D2).
	// The note that used to stand here said this site and safety's would be
	// re-pointed together because they are the same shape -- a leaf package
	// taking an injected common.ChatStructuredProvider from its caller -- and
	// that is what happened. component/healing is unchanged; only the hand
	// that fills its argument is.
	//
	// It named `DefaultProviderName()`, which is the registry's default
	// dressed as a choice, and that default is exactly what the rules decide
	// now. So the request names no provider at all.
	provider, _, err := memql.ResolveAITyped[common.ChatStructuredProvider](
		context.Background(), a.engine, healingPatchResolveRequest())
	if err != nil {
		a.Logger.Warn("work healer not subscribed: no structured-output model is reachable on this node, so a precondition miss will be recorded and not proposed against",
			"component", "work",
			"error", err)
		return
	}
	if healer := pi.WorkHealer(healing.NewRepairLoop(provider), work, workHealApprovalTTL); healer != nil {
		a.eventBus.Subscribe(events.TopicPreconditionMissed, healer.HandlePreconditionMissed,
			events.WithSubscriberName("work:heal"))
		a.Logger.Info("work healer subscribed: a precondition miss now proposes typed patches as a planReview approval",
			"component", "work")
	}
}

// workHealApprovalTTL is how long a healing proposal waits for a person. A
// week: the proposal is a patch to a template, and the person who can judge it
// is not necessarily the person who was watching when it failed.
const workHealApprovalTTL = 7 * 24 * time.Hour
