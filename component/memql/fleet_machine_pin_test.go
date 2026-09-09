package memql

import (
	"context"
	"sync"
	"testing"

	"github.com/znasllc-io/memql/core/common"
)

type pinCheckingFleet struct{ t *testing.T }

func (f pinCheckingFleet) Catalog(context.Context, string) ([]FleetModel, error)     { return nil, nil }
func (f pinCheckingFleet) ModelPreference(context.Context, string) ([]string, error) { return nil, nil }
func (f pinCheckingFleet) Call(_ context.Context, req FleetCallRequest) (FleetCallResult, error) {
	if want := req.Messages[0].Content; req.RegistrationId != want {
		f.t.Errorf("machine pin crossed calls: got %q, want %q", req.RegistrationId, want)
	}
	if req.ActingUserId != "alice" {
		f.t.Errorf("machine pin changed caller: %q", req.ActingUserId)
	}
	if req.OnDelta != nil {
		req.OnDelta("hello")
	}
	return FleetCallResult{Content: "hello"}, nil
}

func TestFleetMachinePinsArePerCallAcrossChatModes(t *testing.T) {
	r := newProviderRegistry()
	r.SetFleetInference(pinCheckingFleet{t})
	p := &fleetProvider{registry: r, modelId: "example:9b", actingUserId: "alice"}
	base := userCtx("alice")
	var wg sync.WaitGroup
	for _, pin := range []string{"", "v1:worker:registration:one", "v1:worker:registration:two"} {
		for _, stream := range []bool{false, true} {
			wg.Go(func() {
				ctx := common.ContextWithFleetRegistration(base, pin)
				messages := []common.ChatMessage{{Role: "user", Content: pin}}
				if stream {
					chunks, err := p.CallChatStream(ctx, messages)
					if err != nil {
						t.Error(err)
						return
					}
					for chunk := range chunks {
						if chunk.Error != nil {
							t.Error(chunk.Error)
						}
					}
				} else if _, err := p.CallChat(ctx, messages); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
	if common.FleetRegistrationFromContext(base) != "" {
		t.Fatal("pin mutated the shared caller context")
	}
}
