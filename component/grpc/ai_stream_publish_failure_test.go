package memql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	"github.com/znasllc-io/memql/component/node"
	nodev1 "github.com/znasllc-io/memql/component/node/gen"
	"github.com/znasllc-io/memql/core/common"
	"google.golang.org/grpc/codes"
)

type failingStreamPublish struct {
	node.DeliverySubstrate
	at, calls int
}

func (f *failingStreamPublish) Publish(ctx context.Context, d node.Deliverable) (int64, error) {
	f.calls++
	if f.calls == f.at {
		return 0, errors.New("test durable publication failed")
	}
	return f.DeliverySubstrate.Publish(ctx, d)
}

// Drive the actual producer, NodeService response hop, forward router and token
// consumer. A failed durable write must report an error over the still-live
// mesh, rather than retire the inflight while the browser waits for a missing row.
func TestTokenPublishFailureTerminatesBrowserAcrossHop(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
		chunks []common.StreamChunk
	}{
		{"start", 1, []common.StreamChunk{{Done: true}}},
		{"delta", 2, []common.StreamChunk{{Content: "partial"}, {Done: true}}},
		{"complete", 3, []common.StreamChunk{{Content: "partial"}, {Done: true}}},
		{"implicit_complete", 3, []common.StreamChunk{{Content: "partial"}}},
		{"provider_error", 2, []common.StreamChunk{{Error: errors.New("provider failed")}}},
		{"success", 0, []common.StreamChunk{{Content: "complete"}, {Done: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := newFakeSubstrate()
			failing := &failingStreamPublish{DeliverySubstrate: sub, at: tc.failAt}
			producerDone := make(chan struct{})
			_, addr := startDisconnectPeer(t, node.NodeTypeAgent, func(ctx context.Context, req *nodev1.AiForwardRequest, send func(*nodev1.NodeServerMessage) error) {
				producer := &streamSession{service: testServiceWithSubstrate(failing, "agent"), stream: &forwardedStream{ctx: ctx, requestId: req.GetRequestId(), send: send}, logger: testLogger()}
				chunks := make(chan common.StreamChunk, len(tc.chunks))
				for _, chunk := range tc.chunks {
					chunks <- chunk
				}
				close(chunks)
				producer.produceTokenStreamToSubstrate(ctx, req.GetRequestId(), chunks)
				close(producerDone)
			})
			browserCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			identity := &node.Identity{ID: "browser-bff", Type: node.NodeTypeBFF}
			peers := node.NewPeerManager(identity, testLogger())
			router := NewAiForwardRouter(peers, testLogger())
			dialer := node.NewWorkerDialer(identity, peers, nil, nil, []node.WorkerTarget{{NodeType: node.NodeTypeAgent, Address: addr}}, testLogger())
			dialer.SetAiForwardResponseSink(router)
			dialer.Start(browserCtx)
			awaitDisconnectCondition(t, func() bool {
				ps := peers.SnapshotByType(node.NodeTypeAgent)
				return len(ps) == 1 && ps[0].Connection != nil
			}, "handshake failed")
			client := newRecordingClientStream(browserCtx)
			service := testServiceWithSubstrate(sub, "browser-bff")
			service.aiForwarder = router
			session := &streamSession{service: service, stream: client, logger: testLogger(), access: &auth.AccessContext{UserId: "v1:identity:user:alice", Role: auth.RoleWriter}, accessLoaded: true, badgeStamped: true, credentialClass: auth.ForwardedClassUser}
			consumerDone := make(chan struct{})
			err := session.proxyAIStream(&memqlv1.MemqlClientMessage{MessageId: "browser-message"}, "publish-request", node.NodeTypeAgent, func(ctx context.Context, correlate, requestID string) {
				session.consumeTokenStream(ctx, correlate, requestID)
				close(consumerDone)
			})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-producerDone:
			case <-time.After(time.Second):
				t.Fatal("producer did not finish")
			}
			select {
			case <-consumerDone:
			case <-time.After(time.Second):
				t.Fatal("browser waits forever for missing durable terminal")
			}
			if browserCtx.Err() != nil {
				t.Fatal("browser context canceled")
			}
			awaitDisconnectCondition(t, func() bool { return !router.HasInflight("publish-request") }, "forward inflight leaked")
			var results, failures int
			for _, msg := range client.snapshot() {
				if msg.GetAiChatResult() != nil {
					results++
					if msg.GetAiChatResult().GetMessage().GetContent() != "complete" {
						t.Errorf("false partial success: %v", msg)
					}
				}
				if qe := msg.GetQueryError(); qe != nil {
					failures++
					if qe.GetError().GetCode() != codes.Unavailable.String() || qe.GetRequestId() != "publish-request" || msg.GetCorrelateTo() != "browser-message" {
						t.Errorf("wrong error: %v", msg)
					}
				}
			}
			if tc.failAt == 0 {
				if results != 1 || failures != 0 {
					t.Fatalf("success: results=%d errors=%d", results, failures)
				}
			} else if results != 0 || failures != 1 {
				t.Fatalf("failure: results=%d errors=%d", results, failures)
			}
		})
	}
}
