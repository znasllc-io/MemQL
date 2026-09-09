package memql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	memqlengine "github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/component/node"
	nodev1 "github.com/znasllc-io/memql/component/node/gen"
	"github.com/znasllc-io/memql/core/airoute"
	"github.com/znasllc-io/memql/core/common"
	"google.golang.org/protobuf/proto"
)

type pinChatProvider struct{ t *testing.T }

func (p pinChatProvider) CallChat(ctx context.Context, messages []common.ChatMessage) (string, error) {
	want := messages[0].Content
	if got := common.FleetRegistrationFromContext(ctx); got != want {
		p.t.Errorf("receiver lost machine pin: got %q, want %q", got, want)
	}
	access, ok := auth.AccessFromContext(ctx)
	if !ok || access.UserId != "v1:identity:user:alice" {
		p.t.Error("receiving chat lost caller authority")
	}
	return "hello", nil
}
func (p pinChatProvider) CallChatStream(ctx context.Context, messages []common.ChatMessage) (<-chan common.StreamChunk, error) {
	_, err := p.CallChat(ctx, messages)
	if err != nil {
		return nil, err
	}
	out := make(chan common.StreamChunk, 1)
	out <- common.StreamChunk{Content: "hello", Done: true}
	close(out)
	return out, nil
}

type pinChatResolver struct{ provider pinChatProvider }

func (r pinChatResolver) ResolveFor(ctx context.Context, req airoute.ResolveRequest) (memqlengine.ResolvedProvider, error) {
	if req.ExplicitProvider != "fleet:example:9b" {
		return memqlengine.ResolvedProvider{}, fmt.Errorf("unexpected provider %s", req.ExplicitProvider)
	}
	return memqlengine.ResolvedProvider{Client: r.provider}, nil
}

func TestChatMachinePinSurvivesForwardedEnvelopeAndFreshReceiver(t *testing.T) {
	engine := &memqlengine.MemQLEngine{}
	engine.SetAIResolver(pinChatResolver{pinChatProvider{t}})
	svc := &service{engine: engine, logger: testLogger()}
	authority, err := auth.ForwardedAuthorityForUser(&auth.AccessContext{UserId: "v1:identity:user:alice", Role: auth.RoleWriter}, auth.ForwardedClassUser, "", time.Time{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []bool{false, true} {
		for _, pin := range []string{"one", "v1:worker:registration:two", ""} {
			t.Run(fmt.Sprintf("stream=%v/pin=%s", stream, pin), func(t *testing.T) {
				want := pin
				if pin == "one" {
					want = "v1:worker:registration:one"
				}
				msg := &memqlv1.AiChatMsg{RequestId: "pin-request", Provider: "fleet:example:9b", Stream: stream, FleetRegistrationId: pin, Messages: []*memqlv1.AiChatMessage{{Role: "user", Content: want}}}
				// Use the same full-envelope protobuf carriage as AiForwardRouter;
				// the receiver starts with no originating request's local context.
				raw := envelopeBytes(t, &memqlv1.MemqlClientMessage_AiChat{AiChat: msg})
				done := make(chan *memqlv1.MemqlServerMessage, 1)
				svc.HandleForwardedRequest(context.Background(), &nodev1.AiForwardRequest{
					RequestId: msg.RequestId, MemqlEnvelope: raw,
					Authority: node.ForwardedAuthorityToProto(authority, "bff-a", "bff"),
				}, func(m *nodev1.NodeServerMessage) error {
					if response := m.GetAiForwardResponse(); response != nil && response.GetDone() {
						decoded := &memqlv1.MemqlServerMessage{}
						if err := proto.Unmarshal(response.GetMemqlServerMsg(), decoded); err != nil {
							return err
						}
						done <- decoded
					}
					return nil
				})
				select {
				case response := <-done:
					if response.GetAiChatResult() == nil {
						t.Fatalf("chat did not complete: %v", response.GetPayload())
					}
				case <-time.After(3 * time.Second):
					t.Fatal("forwarded chat did not finish")
				}
			})
		}
	}
}

func TestChatMachinePinRequiresConcreteFleetAndValidRegistration(t *testing.T) {
	for _, provider := range []string{"", "cloud", "fleet:", "fleet:strongest", "fleet:fastest", "fleet:*"} {
		if _, err := chatCallContext(context.Background(), provider, "machine"); err == nil {
			t.Errorf("pin allowed provider %q", provider)
		}
	}
	for _, pin := range []string{"v1:identity:user:machine", "v1:worker:registration:", "v1:worker:registration:machine:extra", "bad:compound"} {
		if _, err := chatCallContext(context.Background(), "fleet:example:9b", pin); err == nil {
			t.Errorf("allowed invalid registration %q", pin)
		}
	}
	base := common.ContextWithFleetRegistration(context.Background(), "old")
	ctx, err := chatCallContext(base, "cloud", "")
	if err != nil || common.FleetRegistrationFromContext(ctx) != "" {
		t.Fatal("an unpinned request retained an inherited pin")
	}
}
