//go:build agent

package worker

import (
	"context"
	"errors"
	"fmt"
	"testing"

	memqlengine "github.com/znasllc-io/memql/component/memql"
	workerservice "github.com/znasllc-io/memql/component/worker"
	"github.com/znasllc-io/memql/core/common"
)

func contextAllocationHop(t *testing.T, capacity int, embeddings bool) (*FleetInference, *modelHop, chan workerservice.ModelCallRequest) {
	t.Helper()
	seen := make(chan workerservice.ModelCallRequest, 8)
	h := newModelHop(t, func(_ context.Context, req workerservice.ModelCallRequest, _ func(workerservice.ModelCallDelta)) workerservice.ModelCallOutcome {
		seen <- req
		return workerservice.ModelCallOutcome{Content: "answer", FinishReason: workerservice.ModelFinishStop}
	}, func(c *Candidate, w *workerservice.Worker) {
		value := (ModelAttributes{ContextWindow: capacity, StructuredOutput: true, Tools: true, Vision: true, Embeddings: embeddings}).String()
		c.Labels[workerservice.ModelLabel(hopModel)] = value
		w.Labels[workerservice.ModelLabel(hopModel)] = value
	})
	f := newFleetInference(t, h.store)
	f.selfNodeId, f.forward = nodeA, h.link.router
	return f, h, seen
}

// The actual provider computes a slightly different requirement for different
// prompts. Both must reach the remote worker at the same allocation, otherwise
// Ollama tears down and reloads the model for ordinary wording changes.
func TestDifferentPromptsReuseFleetContextAllocationAcrossHop(t *testing.T) {
	f, h, seen := contextAllocationHop(t, 262144, false)
	registry := memqlengine.NewProviderRegistryForTest()
	registry.SetFleetInference(f)
	ctx := common.ContextWithFleetRegistration(authorityCtx(t, h.owner), "laptop")
	entry, ok := registry.EntryForContext(ctx, "fleet:"+hopModel)
	if !ok || !entry.Available {
		t.Fatalf("provider unavailable: %+v", entry)
	}
	provider := entry.Client.(common.ChatAIProvider)
	for _, prompt := range []string{"Say hello", "Write twenty short sentences about local inference."} {
		if _, err := provider.CallChat(ctx, []common.ChatMessage{{Role: "user", Content: prompt}}); err != nil {
			t.Fatal(err)
		}
		got := <-seen
		if got.Params.ContextTokens != 8192 {
			t.Errorf("prompt %q allocated %d; want stable8192", prompt, got.Params.ContextTokens)
		}
		if got.Messages[0].Content != prompt {
			t.Fatalf("prompt changed: %+v", got.Messages)
		}
	}
}

func TestFleetContextAllocationHonorsFloorAndSelectedMachineCapacity(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct{ required, capacity, want int }{
		{4106, 262144, 8192}, {4112, 262144, 8192}, {8192, 262144, 8192},
		{8193, 262144, 16384}, {32768, 262144, 32768}, {32769, 262144, 65536},
		{4112, 6000, 6000}, {6000, 6000, 6000}, {50000, 60000, 60000},
		{maxInt - 1, maxInt, maxInt},
	} {
		t.Run(fmt.Sprintf("%d_of_%d", tc.required, tc.capacity), func(t *testing.T) {
			f, h, seen := contextAllocationHop(t, tc.capacity, false)
			// A larger alternate makes the catalog ceiling larger than the pinned
			// machine's capacity. Allocation must read the selected machine itself.
			h.store.machines = append(h.store.machines, modelMachine("larger", map[string]ModelAttributes{hopModel: {ContextWindow: maxInt}}))
			result, err := f.Call(authorityCtx(t, h.owner), memqlengine.FleetCallRequest{ActingUserId: h.owner, RegistrationId: "laptop", ModelId: hopModel, Kind: memqlengine.FleetKindChat, ContextTokens: tc.required})
			if err != nil {
				t.Fatal(err)
			}
			if result.ExecutionSurface != "fleet:laptop" {
				t.Fatalf("pin changed: %+v", result)
			}
			if got := (<-seen).Params.ContextTokens; got != int64(tc.want) {
				t.Errorf("allocation=%d want%d", got, tc.want)
			}
		})
	}
}

func TestFleetContextAllocationNeverAdmitsAnUndersizedMachine(t *testing.T) {
	f, h, seen := contextAllocationHop(t, 8192, false)
	_, err := f.Call(authorityCtx(t, h.owner), memqlengine.FleetCallRequest{ActingUserId: h.owner, ModelId: hopModel, Kind: memqlengine.FleetKindChat, ContextTokens: 8193})
	if !errors.Is(err, memqlengine.ErrFleetUnavailable) {
		t.Fatalf("required floor lost: %v", err)
	}
	if len(seen) != 0 {
		t.Fatal("undersized machine received generation")
	}
}

func TestFleetContextAllocationLeavesEmbeddingsUnchanged(t *testing.T) {
	f, h, seen := contextAllocationHop(t, 32768, true)
	_, err := f.Call(authorityCtx(t, h.owner), memqlengine.FleetCallRequest{ActingUserId: h.owner, ModelId: hopModel, Kind: memqlengine.FleetKindEmbedding, ContextTokens: 4112, EmbeddingInput: []string{"embed all of this"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := (<-seen).Params.ContextTokens; got != 4112 {
		t.Errorf("embedding allocation changed: %d", got)
	}
}

func TestFleetContextAllocationRecomputesOnBeforeStartFallback(t *testing.T) {
	f, h, seen := contextAllocationHop(t, 32768, false)
	first := modelMachine("a-disconnected", map[string]ModelAttributes{hopModel: {ContextWindow: 6000}})
	first.ConnectedNodeId = nodeA
	h.store.machines = append([]Candidate{first}, h.store.machines...)
	result, err := f.Call(authorityCtx(t, h.owner), memqlengine.FleetCallRequest{ActingUserId: h.owner, ModelId: hopModel, Kind: memqlengine.FleetKindChat, ContextTokens: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExecutionSurface != "fleet:laptop" {
		t.Fatalf("wrong destination: %+v", result)
	}
	if got := (<-seen).Params.ContextTokens; got != 8192 {
		t.Fatalf("fallback inherited first candidate's capped allocation: %d", got)
	}
}

func TestFleetContextAllocationIsTheSameForLocalChatAndVision(t *testing.T) {
	for _, kind := range []string{"", memqlengine.FleetKindChat, memqlengine.FleetKindVision} {
		t.Run(kind, func(t *testing.T) {
			f, h, seen := contextAllocationHop(t, 32768, false)
			f.selfNodeId = nodeB
			f.registry = h.link.handler.registry
			_, err := f.Call(authorityCtx(t, h.owner), memqlengine.FleetCallRequest{ActingUserId: h.owner, ModelId: hopModel, Kind: kind, ContextTokens: 4112})
			if err != nil {
				t.Fatal(err)
			}
			if got := (<-seen).Params.ContextTokens; got != 8192 {
				t.Fatalf("local%s context=%d", kind, got)
			}
		})
	}
}
