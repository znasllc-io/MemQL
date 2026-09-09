package memql

import (
	"context"
	"testing"
	"time"

	memqlengine "github.com/znasllc-io/memql/component/memql"
	nodev1 "github.com/znasllc-io/memql/component/node/gen"
	"github.com/znasllc-io/memql/core/airoute"
	"github.com/znasllc-io/memql/core/common"
)

// Emulates a live provider waiting for more model output after its first delta.
// It releases its resources only when the actual CallChatStream context ends.
type waitingChatStreamProvider struct{ stopped chan struct{} }

func (p waitingChatStreamProvider) CallChatStream(ctx context.Context, _ []common.ChatMessage) (<-chan common.StreamChunk, error) {
	out := make(chan common.StreamChunk)
	go func() {
		defer close(p.stopped)
		defer close(out)
		select {
		case out <- common.StreamChunk{Content: "first token"}:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	}()
	return out, nil
}

type waitingChatResolver struct{ provider waitingChatStreamProvider }

func (r waitingChatResolver) ResolveFor(context.Context, airoute.ResolveRequest) (memqlengine.ResolvedProvider, error) {
	return memqlengine.ResolvedProvider{Client: r.provider}, nil
}

func TestStreamingPublishFailureCancelsProviderButKeepsPeerContext(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(map[int]string{1: "start", 2: "delta"}[failAt], func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			provider := waitingChatStreamProvider{stopped: make(chan struct{})}
			engine := &memqlengine.MemQLEngine{}
			engine.SetAIResolver(waitingChatResolver{provider})
			substrate := &failingStreamPublish{DeliverySubstrate: newFakeSubstrate(), at: failAt}
			svc := testServiceWithSubstrate(substrate, "agent")
			svc.engine = engine
			responses := make(chan *nodev1.NodeServerMessage, 2)
			session := &streamSession{service: svc, logger: testLogger(), stream: &forwardedStream{ctx: parent, requestId: "live", send: func(m *nodev1.NodeServerMessage) error { responses <- m; return nil }}}
			session.handleAiChatStream(parent, "live", "browser", nil, "")
			select {
			case <-provider.stopped:
			case <-time.After(time.Second):
				t.Fatal("failed response delivery left provider work alive")
			}
			if parent.Err() != nil {
				t.Fatal("request failure canceled the shared peer context")
			}
			select {
			case response := <-responses:
				if !response.GetAiForwardResponse().GetDone() {
					t.Fatal("missing terminal error")
				}
			default:
				t.Fatal("browser not notified")
			}
		})
	}
}
