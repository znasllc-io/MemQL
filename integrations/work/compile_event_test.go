package work

import (
	"strings"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/events"
)

func compilingEvent(id, owner, goalId, automation string) events.Event {
	return events.Event{
		Topic: "graph.node.created.v1:work:run",
		Payload: map[string]any{
			"id": id,
			"payload": map[string]any{
				"status":         runStatusCompiling,
				"automationName": automation,
				"ownerUserId":    owner,
				"goalId":         goalId,
				"input":          map[string]any{"month": "2026-09"},
			},
		},
	}
}

// TestHandleCompileEventClaimsAndCompiles is the planner side of the bff
// handoff: a compiling run event reaches a node with a Compiler and is
// claimed + compiled rather than ignored.
func TestHandleCompileEventClaimsAndCompiles(t *testing.T) {
	i, eng := newTestIntegration(t)
	rec := &recordingCompiler{done: make(chan CompileRequest, 1)}
	i.SetCompiler(rec)
	claimer := &stubClaimer{grant: true}
	i.SetRunClaimer(claimer)
	eng.reply("workGoalForOwner", map[string]any{
		"id": "v1:work:goal:g1", "ownerUserId": "u-alice",
		"statement": "reconcile the September invoices",
	})

	// Drive the subscriber synchronously: HandleCompileEvent spawns a
	// goroutine, so wait on the compiler channel instead of racing.
	i.HandleCompileEvent(compilingEvent("v1:work:run:r1", "u-alice", "v1:work:goal:g1", compilingAutomationName))

	select {
	case req := <-rec.done:
		if req.RunId != "v1:work:run:r1" || req.GoalId != "v1:work:goal:g1" {
			t.Errorf("compile handed %+v", req)
		}
		if req.Statement != "reconcile the September invoices" {
			t.Errorf("statement = %q, want it loaded from the goal row", req.Statement)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("compiler was never called")
	}
	if len(claimer.keys) != 1 || claimer.keys[0] != "v1:work:run:r1" {
		t.Errorf("claim keys = %v, want the run id under the compile namespace", claimer.keys)
	}
	if len(claimer.names) != 1 || claimer.names[0] != compileClaimName {
		t.Errorf("claim names = %v, want %q", claimer.names, compileClaimName)
	}
}

// TestHandleCompileEventIgnoresNonCompiling mirrors HandleRunEvent's filter:
// a running / terminal event must not start compile.
func TestHandleCompileEventIgnoresNonCompiling(t *testing.T) {
	i, _ := newTestIntegration(t)
	rec := &recordingCompiler{done: make(chan CompileRequest, 1)}
	i.SetCompiler(rec)
	i.SetRunClaimer(&stubClaimer{grant: true})

	i.HandleCompileEvent(runEvent("r1", runStatusRunning, "work.invoice", "u1"))
	select {
	case req := <-rec.done:
		t.Fatalf("compiled a non-compiling run: %+v", req)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestHandleCompileEventIgnoresAlreadyCompiledRun: a late replay of a create
// whose automationName was rewritten must not compile again.
func TestHandleCompileEventIgnoresAlreadyCompiledRun(t *testing.T) {
	i, _ := newTestIntegration(t)
	rec := &recordingCompiler{done: make(chan CompileRequest, 1)}
	i.SetCompiler(rec)
	i.SetRunClaimer(&stubClaimer{grant: true})

	i.HandleCompileEvent(compilingEvent("r1", "u1", "g1", "work.invoice.reconcile"))
	select {
	case req := <-rec.done:
		t.Fatalf("re-compiled a run that already has a template: %+v", req)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestHandleCompileEventSilentWithoutCompiler: every bff sees the event.
func TestHandleCompileEventSilentWithoutCompiler(t *testing.T) {
	i := New(nil, nil)
	// Must not panic and must not invent work.
	i.HandleCompileEvent(compilingEvent("r1", "u1", "g1", compilingAutomationName))
}

func TestErrNoCompileSurfaceNamesTheProblem(t *testing.T) {
	if !strings.Contains(errNoCompileSurface.Error(), "no compile surface") {
		t.Errorf("errNoCompileSurface = %v", errNoCompileSurface)
	}
}
