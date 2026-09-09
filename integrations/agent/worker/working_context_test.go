//go:build agent

package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	memqlengine "github.com/znasllc-io/memql/component/memql"
	workerservice "github.com/znasllc-io/memql/component/worker"
)

func TestWorkingContextGatesActualMachineCall(t *testing.T) {
	f := newFleetInference(t, &fakeFleet{machines: []Candidate{
		modelMachine("small-window", map[string]ModelAttributes{smallModel: {ContextWindow: 8192}}),
	}})
	_, err := f.Call(context.Background(), memqlengine.FleetCallRequest{ActingUserId: "alice", ModelId: smallModel, Kind: memqlengine.FleetKindChat, ContextTokens: 32768})
	var unavailable *memqlengine.FleetUnavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("expected capability refusal, got %v", err)
	}
	if why := unavailable.Considered["small-window"]; !strings.Contains(why, "context window") {
		t.Fatalf("machine reached dispatch without context gate: %+v", unavailable.Considered)
	}
}

// The catalog can advertise a large window while another machine offering the
// same model has a smaller one. Test the actual choice, then cross the replica
// boundary with the request that choice admitted.
func TestWorkingContextChoosesCapableMachineAcrossReplica(t *testing.T) {
	h := newModelHop(t, func(_ context.Context, req workerservice.ModelCallRequest, emit func(workerservice.ModelCallDelta)) workerservice.ModelCallOutcome {
		if req.Params.ContextTokens != 32768 {
			t.Errorf("runtime window=%d", req.Params.ContextTokens)
		}
		emit(workerservice.ModelCallDelta{Seq: 1, Content: "wide-window answer"})
		return workerservice.ModelCallOutcome{FinishReason: workerservice.ModelFinishStop}
	}, func(c *Candidate, w *workerservice.Worker) {
		c.Labels[workerservice.ModelLabel(hopModel)] = "ctx=65536"
		w.Labels[workerservice.ModelLabel(hopModel)] = "ctx=65536"
	})
	small := modelMachine("small-window", map[string]ModelAttributes{hopModel: {ContextWindow: 8192}})
	small.ConnectedNodeId = nodeA
	h.store.machines = append([]Candidate{small}, h.store.machines...)
	f := newFleetInference(t, h.store)
	f.selfNodeId = nodeA
	f.forward = h.link.router
	// If the gate is lost, this otherwise preferred local worker must not run.
	w := &workerservice.Worker{RegistrationId: small.RegistrationId, OwnerUserId: h.owner, Capabilities: []string{workerservice.ModelCapability}, Concurrency: map[string]uint32{workerservice.ModelCapability: 2}}
	w.SetModelCallFunc(func(_ context.Context, req workerservice.ModelCallRequest) (*workerservice.ModelCallHandle, error) {
		t.Error("small-window machine reached runtime")
		handle, _, finish := workerservice.NewModelCallLoopback(req, func(string) {})
		finish(workerservice.ModelCallOutcome{FinishReason: workerservice.ModelFinishStop})
		return handle, nil
	})
	f.registry.Add(w)
	result, err := f.Call(authorityCtx(t, h.owner), memqlengine.FleetCallRequest{ActingUserId: h.owner, ModelId: hopModel, Kind: memqlengine.FleetKindChat, ContextTokens: 32768})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "wide-window answer" || result.ExecutionSurface != "fleet:laptop" {
		t.Fatalf("wrong serving machine: %+v", result)
	}
}
