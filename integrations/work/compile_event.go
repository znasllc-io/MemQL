package work

import (
	"context"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/events"
	"github.com/znasllc-io/memql/component/memql"
)

// compile_event.go -- the planner's half of createGoal when the goal was
// accepted on another node (memql BFF createGoal + nil compiler).
//
// # Why an event, not a NodeService forward
//
// Exactly the argument dispatch.go makes for run execution: the run row's
// own graph event is already broadcast (and, with RunDelivery, durably
// delivered) to every replica. A planner that watches `compiling` the way
// an agent watches `running` compiles a goal opened on the bff without a
// new message pair, a new routing rule or a receiving handler. Inventing a
// cross-node "please compile" RPC would buy what the event already does.
//
// # The bff does not invent a plan
//
// createGoal on a node with no local Compiler still opens the goal and its
// first run -- that ordering is the design -- and reports
// compileDispatched:true because the run row's create event IS the handoff.
// EnableCompileViaEvent is what makes that honest: without it, createGoal
// REFUSES with "no compile surface" rather than accepting a run the
// abandoned sweep would later close as a lost node.

// EnableCompileViaEvent marks this replica as able to hand compile to the
// cluster through the run graph event. Called once from mesh bootstrap on
// every node that accepts createGoal but does not run compile itself.
//
// First call wins, matching SetCompiler: two halves of one deployment must
// not disagree about whether a nil compiler is a forward or a refuse.
func (i *Integration) EnableCompileViaEvent() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.compileViaEvent {
		i.compileViaEvent = true
	}
}

func (i *Integration) compileViaEventEnabled() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.compileViaEvent
}

// hasCompileSurface reports whether createGoal can honestly say compile will
// run: either this replica compiles, or the run event will reach a replica that does.
func (i *Integration) hasCompileSurface() bool {
	return i.compilerRef() != nil || i.compileViaEventEnabled()
}

// errNoCompileSurface is the loud refuse. Kept as one sentence so every caller -- createGoal, fork, responsibility -- says the same thing.
var errNoCompileSurface = fmt.Errorf("work: no compile surface — createGoal needs a planner (local compiler) or mesh event forward; refusing rather than leaving a run stuck in compiling")

// compileClaimName is the claim namespace for compile, distinct from
// runClaimName so a compile claim never collides with an execution claim for the same run id.
const compileClaimName = "work.run.compile"

// HandleCompileEvent is the event-bus subscriber for
// graph.node.{created,updated}.v1:work:run on a planner replica.
//
// It is the mirror of HandleRunEvent: that one claims and executes a run that reached `running`; this one claims and compiles a run that is still
// `compiling`. A bff that accepted createGoal with no local compiler is
// why it exists.
func (i *Integration) HandleCompileEvent(ev events.Event) {
	req, ok := compileEventFields(ev)
	if !ok {
		return
	}
	if i.compilerRef() == nil {
		// Not a compiling node. Silent -- every bff and identity replica
		// sees this event, and a log line per run event per replica would
		// drown the one case that matters.
		return
	}
	go func() {
		_ = i.startCompile(context.WithoutCancel(context.Background()), req)
	}()
}

// compileEventFields pulls what compile needs out of a run graph event.
// Statement is intentionally absent: it lives on the goal row, and
// startCompile loads it under the owner's borrowed authority before the
// compiler runs. Carrying a stale copy on the event would be a second source of truth for the one field catalog match keys on.
func compileEventFields(ev events.Event) (CompileRequest, bool) {
	if ev.Payload == nil {
		return CompileRequest{}, false
	}
	runId, _ := ev.Payload["id"].(string)
	if runId == "" {
		return CompileRequest{}, false
	}
	payload, _ := ev.Payload["payload"].(map[string]any)
	if payload == nil {
		return CompileRequest{}, false
	}
	status, _ := payload["status"].(string)
	if status != runStatusCompiling {
		return CompileRequest{}, false
	}
	// A run still carrying the sentinel automation name is waiting for
	// compile. One whose automationName has already been rewritten has
	// been compiled (or opened as a direct goal) and must not be compiled
	// again off a late replay of its create event.
	if name, _ := payload["automationName"].(string); name != "" && name != compilingAutomationName {
		return CompileRequest{}, false
	}
	owner, _ := payload["ownerUserId"].(string)
	goalId, _ := payload["goalId"].(string)
	input, _ := payload["input"].(map[string]any)
	ceilings, _ := payload["ceilings"].(map[string]any)
	return CompileRequest{
		GoalId:      goalId,
		RunId:       runId,
		OwnerUserId: owner,
		Input:       input,
		Ceilings:    ceilings,
	}, true
}

// startCompile claims the run for compile (when a claimer is installed) and
// hands it to the local Compiler on a detached goroutine.
//
// Returns whether this replica took responsibility for compiling -- a lost
// claim means another planner already has it, which from the caller's point
// of view is still "compile is happening".
func (i *Integration) startCompile(ctx context.Context, req CompileRequest) bool {
	c := i.compilerRef()
	if c == nil {
		return false
	}
	if strings.TrimSpace(req.RunId) == "" {
		return false
	}

	claimer := i.runClaimerRef()
	if claimer != nil {
		if !claimer.ClaimWithTTL(ctx, compileClaimName, req.RunId, runClaimTTL) {
			return true
		}
	} else {
		// No claimer: still compile. Double-compile on a multi-planner
		// deployment without a claimer is possible and wrong, and the
		// wiring installs the claimer for exactly that reason. Refusing
		// here would strand every goal on a planner whose claimer failed
		// to wire, which is the worse of the two failures.
		i.log().Warn("work: compiling without a cross-replica claim; a second planner replica would compile the same run",
			"component", "work.compile", "run", req.RunId)
	}

	if strings.TrimSpace(req.Statement) == "" && strings.TrimSpace(req.GoalId) != "" {
		req.Statement = i.loadGoalStatement(ctx, req.OwnerUserId, req.GoalId)
	}

	base := ownerActor(context.WithoutCancel(ctx), req.OwnerUserId)
	base = memql.ContextWithBudgetScope(base, compileBudgetScopes(req)...)
	i.log().Info("work: claimed a run for compile",
		"component", "work.compile", "run", req.RunId, "goal", req.GoalId, "node", selfNodeId())
	go c.Compile(base, req)
	return true
}

// loadGoalStatement reads the goal's statement under the owner's authority.
// An empty result is left empty: compile then fails loudly onto the run
// rather than inventing a statement nobody wrote.
func (i *Integration) loadGoalStatement(ctx context.Context, ownerUserId, goalId string) string {
	goal, err := i.store().goalForOwner(ownerActor(ctx, ownerUserId), goalId)
	if err != nil || goal == nil {
		if err != nil {
			i.log().Warn("work: could not read the goal's statement for compile; the compiler will see an empty statement",
				"component", "work.compile", "goal", goalId, "err", err)
		}
		return ""
	}
	return rowString(goal, "statement")
}
