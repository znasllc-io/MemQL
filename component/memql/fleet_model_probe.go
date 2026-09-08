package memql

// The `fleetModelProbe` ACT (epic memql#5146, design D3).
//
// The pull's act, one question later: a pull puts a model on a machine, and
// this measures what it does there. It reuses `modelPullMachineFor` -- the
// AUTHORIZED read through `workersForUser` -- so a machine that is not the
// caller's is not in the answer and this file does not have to be trusted to
// check.
//
// IT RETURNS AT ONCE, for the pull's reason. A suite takes minutes; a call that
// blocked until it finished would hold a gRPC stream for the whole of it and
// lose the measurement if the person closed the tab. The v1:worker:modelProbe
// row is how anyone watches, and the measurement it points at is the answer.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	langparser "github.com/znasllc-io/memql/component/language/parser"
	"github.com/znasllc-io/memql/core/id"
)

// ModelProbeResultConcept is the canonical id of the act's answer.
const ModelProbeResultConcept = "v1:worker:modelProbeResult"

// ModelProbeConcept is the canonical id of the probe record.
const ModelProbeConcept = "v1:worker:modelProbe"

// ProbeSuiteVersion is the suite this engine asks for.
//
// It MIRRORS component/worker/probe.SuiteVersion rather than importing it,
// because component/memql cannot reach component/worker -- worker reaches
// identity, which reaches this package. The two are held equal by
// TestProbeSuiteVersionMatchesTheSuite in component/worker, on the legal side
// of the edge; a drift here would file measurements under a suite the machine
// never ran.
const ProbeSuiteVersion = "1"

// evaluateFleetModelProbeExpression serves the `fleetModelProbe` builtin.
func (e *MemQLEngine) evaluateFleetModelProbeExpression(ctx context.Context, args map[string]any) ([]memorynodes.MemoryNode, error) {
	if e == nil {
		return nil, fmt.Errorf("engine is nil")
	}
	acting := strings.TrimSpace(actingUserFromContext(ctx))
	if acting == "" {
		return nil, modelPullRefusal{
			Code:    "not_your_machine",
			Message: "no machine of yours has that registration id",
		}
	}

	registrationId := strings.TrimSpace(stringArg(args, "registrationId"))
	machine, err := e.modelPullMachineFor(ctx, registrationId)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(stringArg(args, "model"))
	if model == "" {
		return nil, modelPullRefusal{
			Code:    "model_required",
			Message: "fleetModelProbe needs the model id to measure, in the runtime's own vocabulary",
		}
	}

	// The MACHINE-level refusals are the pull's, and reusing them is the point:
	// offline is offline and revoked is revoked, whichever act is asking, and a
	// second set of sentences for the same two states would drift.
	plan, err := planModelPull(machine, acting, model, time.Now())
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	probeId := ModelProbeConcept + ":" + id.NewShortId()
	call, err := langparser.RenderCall("createModelProbe", map[string]any{
		"probeId":      probeId,
		"workerId":     plan.WorkerId,
		"model":        model,
		"suiteVersion": ProbeSuiteVersion,
		"targetNodeId": plan.TargetNodeId,
		"requestedAt":  now,
	})
	if err != nil {
		return nil, fmt.Errorf("fleetModelProbe: render the probe record: %w", err)
	}
	// Written under the CALLER'S actor so the row belongs to the person who
	// pressed the button, with internal origin because createModelProbe is
	// @serverOnly: the caller may own the row and still must not name the
	// replica that will act on it.
	if _, err := e.Execute(auth.ContextWithInternalOrigin(ctx), call); err != nil {
		return nil, fmt.Errorf("fleetModelProbe: open the probe record: %w", err)
	}

	return singleVirtualRow(ModelProbeResultConcept, probeId, map[string]any{
		"probeId":      probeId,
		"workerId":     plan.WorkerId,
		"model":        model,
		"suiteVersion": ProbeSuiteVersion,
		"status":       "requested",
		"targetNodeId": plan.TargetNodeId,
		"requestedAt":  now,
		"machine":      machineLabel(machine),
	})
}
