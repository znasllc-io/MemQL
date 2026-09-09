package planner

// agent_loop.go -- what is LEFT of the planner agent loop (memql#5052).
//
// # The loop is gone
//
// This file held the plan-driven orchestration cycle: HandlePlanCreated /
// HandlePlanUpdated off `graph.node.*.v1:planner:plan`, the
// invoke-and-dispatch iteration, `dispatchDecision`'s five actions
// (decompose / dispatchTask / createSpecialist / markPlanSucceeded /
// escalate), the phase stamping, the task insertion and the plan-status
// transitions. All of it read and wrote `v1:planner:plan` and
// `v1:planner:task`, and all of it is deleted.
//
// Section F of the design record put the destination plainly: "the
// `agent_loop*.go` family becomes the compile and miss handlers".
// `work_compile.go` and `work_heal.go` already ARE those, so this is the
// deletion of the half they replaced rather than a rewrite.
//
// # What survives, and why each thing is here
//
// `PlannerAgentLoop` survives as the CARRIER for the authoring pipeline. It is
// no longer a loop: `work_compile.go` is a method on it because the compile
// pass needs `runDesignPass`, `loadCatalog` and `emitAndRepairBundle`, which
// are methods on it too. Nothing in this file drives anything any more.
//
// The four helpers at the bottom are here because the SURVIVORS use them --
// `responsibility_intake.go`, `work_heal.go` and the authoring transcript --
// and they had no other home. A helper whose only home is a deleted file is
// how a clean deletion becomes a broken build.

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/znasllc-io/memql/component/auth"
)

// PlannerAgentLoop carries the authoring pipeline.
//
// The name is kept deliberately even though there is no loop left. It is the
// receiver on ~15 authoring methods and the type `work_compile_adapter.go`
// asserts against; renaming it would be a large diff whose only content is a
// rename, on the same commit that deletes 8,000 lines. That is a change worth
// making on its own, where it can be read.
type PlannerAgentLoop struct {
	engine Engine
	logger *slog.Logger

	// goals is the work spine's goal opener (memql#5051), threaded from the
	// integration.
	goalsMu sync.Mutex
	goals   responsibilityGoals

	// delegation is the container-executor delegation resolver. Section F of
	// the design record keeps it BY NAME, along with
	// component/planner.RegisterContainerExecutor -- both are load-bearing for
	// the cockpit-app surface, which is why they outlive the loop that used to
	// consult them.
	delegation DelegationResolver
}

// NewPlannerAgentLoop constructs the carrier.
func NewPlannerAgentLoop(engine Engine, logger *slog.Logger) *PlannerAgentLoop {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlannerAgentLoop{engine: engine, logger: logger}
}

// systemPlannerActor is the subject the planner's own reads run under.
const systemPlannerActor = "system:planner"

// workGoals is the loop's handle on the work spine's goal opener. Nil on a
// node without the work spine.
func (l *PlannerAgentLoop) workGoals() responsibilityGoals {
	if l == nil {
		return nil
	}
	l.goalsMu.Lock()
	defer l.goalsMu.Unlock()
	return l.goals
}

// SetWorkGoals installs it.
func (l *PlannerAgentLoop) SetWorkGoals(g responsibilityGoals) {
	if l == nil {
		return
	}
	l.goalsMu.Lock()
	defer l.goalsMu.Unlock()
	if l.goals == nil {
		l.goals = g
	}
}

// systemActorContext stamps the planner's own system subject.
func systemActorContext(ctx context.Context) context.Context {
	// A persisted work run already carries a verified user assertion. Model
	// attribution must keep that owner across the planner-to-agent hop.
	if _, ok := auth.ForwardedAuthorityFromContext(ctx); ok {
		return ctx
	}
	return auth.ContextWithToken(ctx, &auth.TokenInfo{
		Subject: systemPlannerActor,
		Claims: map[string]any{
			"sub":  systemPlannerActor,
			"role": "system",
		},
	})
}

// stripJSONFence removes a ```json ... ``` wrapper a model sometimes puts
// around its output.
func stripJSONFence(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(s, "```") {
		return raw
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return []byte(strings.TrimSpace(s))
}

// getString reads a string field off a row map, tolerating absence and a
// non-string value.
func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// truncate bounds a string for a log line or a description.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
