package memql

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	memoryNodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/envregistry"
	"github.com/znasllc-io/memql/component/memql/readiness"
)

var inferenceNow = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func aiModule() envregistry.Module {
	return envregistry.Module{Name: "ai", Core: true, Description: "d", Evaluator: envregistry.EvaluatorInferenceStatus}
}

func rowsResolver(rows ...readiness.RegistrationFacts) readinessResolvers {
	return readinessResolvers{
		Registrations: func(context.Context) ([]readiness.RegistrationFacts, error) { return rows, nil },
	}
}

// EVERY NODE TYPE, ONE REPORT.
//
// This is the defect the fold made visible and could not fix. Only an agent
// binary held SetFleetInference and SetAppInference, so `ai` was `configured`
// there and `unconfigured` on every other replica; the fold's disagreement
// rule turned that into `partial`, permanently, on any multi-node cluster --
// a cluster with a working inference door reporting as half set up, on every
// surface, with nothing wrong anywhere.
func TestEveryNodeTypeProducesTheSameInferenceReport(t *testing.T) {
	r := rowsResolver(readiness.RegistrationFacts{
		Labels:     map[string]string{"model:llama3.1:8b": "ctx=8192,structured=true"},
		LastSeenAt: inferenceNow,
	})
	// The seven node types (root CLAUDE.md, Distributed Node Architecture).
	types := []string{"identity", "bff", "agent", "planner", "workbench", "mcp", "edge"}
	var first readiness.NodeReport
	for i, nodeType := range types {
		got := evaluateModule(context.Background(), r, aiModule(), "node-"+nodeType, nodeType, inferenceNow)
		if i == 0 {
			first = got
			if first.State != readiness.Configured {
				t.Fatalf("a qualifying machine did not configure ai on %s: %+v", nodeType, first)
			}
			continue
		}
		if got.State != first.State {
			t.Errorf("%s reports %s and %s reports %s -- the fold reads any disagreement as partial, "+
				"which is how `ai` came to be stuck at partial on every multi-node cluster",
				nodeType, got.State, first.NodeType, first.State)
		}
		if len(got.Lanes) != len(first.Lanes) {
			t.Errorf("%s reports %d lanes and %s reports %d", nodeType, len(got.Lanes), first.NodeType, len(first.Lanes))
		}
	}
}

// Configured and live are two facts and the row carries both: a laptop closing
// does not change what a cluster is configured to do.
func TestASleepingMachineIsConfiguredAndNotLive(t *testing.T) {
	r := rowsResolver(readiness.RegistrationFacts{
		Labels:     map[string]string{"model:llama3.1:8b": "ctx=8192,structured=true"},
		LastSeenAt: inferenceNow.Add(-readiness.OnlineWindow - time.Minute),
	})
	got := evaluateModule(context.Background(), r, aiModule(), "bff-1", "bff", inferenceNow)
	if got.State != readiness.Configured {
		t.Fatalf("a sleeping machine un-configured the cluster: %s", got.State)
	}
	if readiness.InferenceLive(got.Lanes) {
		t.Errorf("no door is open, so the report must not say one is: %+v", got.Lanes)
	}
	for _, lane := range got.Lanes {
		if lane.Name != readiness.InferenceLaneLocal {
			continue
		}
		if !lane.Complete {
			t.Errorf("the local lane is not complete: %+v", lane)
		}
		for _, slot := range lane.Slots {
			if slot.Name == readiness.InferenceLiveSlot && slot.Present {
				t.Errorf("a machine outside the online window reported live")
			}
		}
	}
}

// A read that FAILS leaves every door SHUT. The core gate branches on this,
// and "we could not ask" reported as an open door sends somebody into a
// console whose every feature then refuses.
func TestAFailedRegistrationReadShutsEveryDoor(t *testing.T) {
	r := readinessResolvers{
		Registrations: func(context.Context) ([]readiness.RegistrationFacts, error) {
			return nil, errors.New("the read did not land")
		},
	}
	got := evaluateModule(context.Background(), r, aiModule(), "bff-1", "bff", inferenceNow)
	if got.State != readiness.Unconfigured {
		t.Fatalf("a failed read produced %s, want unconfigured", got.State)
	}
	if len(got.Lanes) != 3 {
		t.Errorf("a failed read still reports the three doors, all shut: %+v", got.Lanes)
	}
}

// Federation alone configures the module, and it needs no rows at all -- which
// is why a cloud cluster with no fleet is not held on the gate.
func TestFederationAloneConfiguresTheModule(t *testing.T) {
	r := readinessResolvers{FederationConfigured: func() bool { return true }}
	got := evaluateModule(context.Background(), r, aiModule(), "bff-1", "bff", inferenceNow)
	if got.State != readiness.Configured {
		t.Fatalf("federation alone produced %s", got.State)
	}
}

// The floor the gate uses has ONE value. component/memql/readiness cannot
// import its parent, so this is where the two are held equal -- and the
// direction of the failure matters: a readiness floor ABOVE the catalog's
// would hold somebody on the gate while the router happily served them.
func TestTheContextFloorHasOneValue(t *testing.T) {
	if readiness.MinContextWindow != MinimumContextWindow {
		t.Fatalf("readiness.MinContextWindow is %d and MinimumContextWindow is %d",
			readiness.MinContextWindow, MinimumContextWindow)
	}
}

// THE EVALUATION CONTEXT IS THE CLUSTER'S, NEVER THE CALLER'S.
//
// An owner pulling readinessRecompute used to have their OWN machines resolved
// through the fleet seam and written as a node fact. The registration read is
// cluster-wide and must answer identically however it was triggered.
func TestTheEvaluationContextIsTheClustersOwn(t *testing.T) {
	caller := auth.ContextWithAccess(context.Background(), &auth.AccessContext{
		UserId: "v1:identity:user:someone",
		Role:   auth.RoleWriter,
	})
	ctx := readinessEvaluateContext(caller)
	ac, ok := auth.AccessFromContext(ctx)
	if !ok || ac == nil {
		t.Fatal("no AccessContext on the evaluation context")
	}
	if ac.UserId == "v1:identity:user:someone" {
		t.Fatal("the caller's identity survived into the evaluation, so a recompute writes " +
			"whoever pulled it as a cluster fact")
	}
	if !ac.IsClusterOwner() {
		t.Fatal("the evaluation is not a cluster owner, so allWorkersWithStatus answers ZERO ROWS " +
			"AND NO ERROR -- a cluster full of machines reporting `ai` unconfigured, silently")
	}
	if !ac.Unranked {
		t.Error("the evaluation actor holds a rung on the role ladder; it must be unranked (D4)")
	}
	if !ac.Synthetic {
		t.Error("the evaluation actor is not synthetic; the cluster is acting, not a person")
	}
	if !auth.OriginFromContext(ctx).IsInternal() {
		t.Error("the evaluation context is not internal origin")
	}
}

// The two contexts are DIFFERENT and each is the smallest thing that works.
// Folding them into one would either give the write a cluster owner it does
// not need, or give the evaluation a reader that cannot see a single machine.
func TestTheWriteContextStaysAReader(t *testing.T) {
	wctx := readinessWriteContext(context.Background())
	ac, ok := auth.AccessFromContext(wctx)
	if !ok || ac == nil {
		t.Fatal("no AccessContext on the write context")
	}
	if ac.Role != auth.RoleReader {
		t.Errorf("the write context carries role %q, want %q -- it writes a public concept with no "+
			"owner field behind @serverOnly plus internal origin, and any more authority than that "+
			"is authority nothing here uses", ac.Role, auth.RoleReader)
	}
}

// THE QUERY THE `ai` ARM RUNS MUST LOAD ON EVERY NODE THAT RUNS IT.
//
// `readInferenceRegistrations` executes `allWorkersWithStatus`, and the arm
// leaves every door SHUT when that read fails. So a node type whose embedded
// tree did not carry the query would report `ai` unconfigured while its
// siblings reported configured -- and the fold reads that disagreement as
// `partial`, permanently, which is the exact defect this task exists to end.
//
// The tree is embedded WITHOUT a build tag (dsl/embed.go), so every node type
// carries the same one. This asserts that rather than assuming it, because the
// failure it prevents is silent on every surface.
func TestTheRegistrationQueryLoadsFromTheEmbeddedTree(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	if _, err := LoadUnifiedConcepts(logger); err != nil {
		t.Fatalf("LoadUnifiedConcepts: %v", err)
	}
	concepts := memoryNodes.DefaultRegistry()
	registry, err := loadEmbeddedFunctions(logger, concepts)
	if err != nil {
		t.Fatalf("loadEmbeddedFunctions: %v", err)
	}
	if _, _, lerr := LoadUnifiedFunctions(logger, registry, concepts); lerr != nil {
		t.Fatalf("LoadUnifiedFunctions: %v", lerr)
	}
	// A REACHABLE POSITIVE: without it an empty registry passes the assertion
	// below over nothing, which is the failure a registry lookup is most prone
	// to.
	if !registry.Has("moduleReadinessAll") {
		t.Fatal("the readiness feed's own query is missing from this registry, so it is not the " +
			"one the engine loads and the check below would prove nothing")
	}
	if !registry.Has(readinessRegistrationQuery) {
		t.Fatalf("%q does not load from the embedded DSL tree. The ai readiness arm runs it and "+
			"leaves every door shut when the read fails, so this node type would report `ai` "+
			"unconfigured while its siblings reported configured.", readinessRegistrationQuery)
	}
}
