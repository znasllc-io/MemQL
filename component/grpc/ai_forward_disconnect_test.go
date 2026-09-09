package memql

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	"github.com/znasllc-io/memql/component/node"
	nodev1 "github.com/znasllc-io/memql/component/node/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The transport is a real TCP NodeService hop. The remote service accepts work
// then loses its stream before it can answer, while the browser stays connected.
type disconnectNodeService struct {
	nodev1.UnimplementedNodeServiceServer
	welcomes  atomic.Int32
	onRequest func(context.Context, *nodev1.AiForwardRequest, func(*nodev1.NodeServerMessage) error)
	nodeID    string
	nodeType  node.NodeType
	drop      chan struct{}
	requests  chan *nodev1.AiForwardRequest
}

func (s *disconnectNodeService) Stream(stream nodev1.NodeService_StreamServer) error {
	if _, err := stream.Recv(); err != nil {
		return err
	}
	if err := stream.Send(&nodev1.NodeServerMessage{Payload: &nodev1.NodeServerMessage_NodeWelcome{NodeWelcome: &nodev1.NodeWelcome{NodeId: s.nodeID, NodeType: string(s.nodeType)}}}); err != nil {
		return err
	}
	var drop <-chan struct{}
	if s.welcomes.Add(1) == 1 {
		drop = s.drop
	}
	go func() {
		for {
			m, err := stream.Recv()
			if err != nil {
				return
			}
			if req := m.GetAiForwardRequest(); req != nil {
				s.requests <- req
				if s.onRequest != nil {
					s.onRequest(stream.Context(), req, stream.Send)
				}
			}
		}
	}()
	select {
	case <-drop:
		return status.Error(codes.Unavailable, "test peer transport lost")
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
}
func startDisconnectPeer(t *testing.T, kind node.NodeType, handlers ...func(context.Context, *nodev1.AiForwardRequest, func(*nodev1.NodeServerMessage) error)) (*disconnectNodeService, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remote := &disconnectNodeService{nodeID: "remote-" + string(kind), nodeType: kind, drop: make(chan struct{}), requests: make(chan *nodev1.AiForwardRequest, 16)}
	if len(handlers) > 0 {
		remote.onRequest = handlers[0]
	}
	server := grpc.NewServer()
	nodev1.RegisterNodeServiceServer(server, remote)
	go server.Serve(l)
	t.Cleanup(server.Stop)
	return remote, l.Addr().String()
}
func awaitDisconnectCondition(t *testing.T, fn func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(message)
}
func TestAiForwardPeerDisconnectTerminatesOnlyLostHop(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "streaming"}[streaming], func(t *testing.T) {
			agent, agentAddr := startDisconnectPeer(t, node.NodeTypeAgent)
			planner, plannerAddr := startDisconnectPeer(t, node.NodeTypePlanner)
			browserCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			identity := &node.Identity{ID: "browser-bff", Type: node.NodeTypeBFF}
			peers := node.NewPeerManager(identity, testLogger())
			router := NewAiForwardRouter(peers, testLogger())
			dialer := node.NewWorkerDialer(identity, peers, nil, nil, []node.WorkerTarget{{NodeType: node.NodeTypeAgent, Address: agentAddr}, {NodeType: node.NodeTypePlanner, Address: plannerAddr}}, testLogger())
			dialer.SetAiForwardResponseSink(router)
			dialer.Start(browserCtx)
			awaitDisconnectCondition(t, func() bool {
				for _, kind := range []node.NodeType{node.NodeTypeAgent, node.NodeTypePlanner} {
					ps := peers.SnapshotByType(kind)
					if len(ps) != 1 || ps[0].Connection == nil {
						return false
					}
				}
				return true
			}, "peer handshakes did not complete")
			unaffected, err := router.Forward(browserCtx, "other-request", node.NodeTypePlanner, testForwardedPrincipal(t), &memqlv1.MemqlClientMessage{MessageId: "other-request"})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-planner.requests:
			case <-time.After(time.Second):
				t.Fatal("healthy peer did not receive request")
			}
			client := newRecordingClientStream(browserCtx)
			session := &streamSession{service: &service{logger: testLogger(), aiForwarder: router, deliverySubstrate: newFakeSubstrate(), streamNodeID: "browser-bff"}, stream: client, logger: testLogger(), access: &auth.AccessContext{UserId: "v1:identity:user:alice", Role: auth.RoleWriter}, credentialClass: auth.ForwardedClassUser, accessLoaded: true, badgeStamped: true}
			envelope := &memqlv1.MemqlClientMessage{MessageId: "browser-message", Payload: &memqlv1.MemqlClientMessage_AiChat{AiChat: &memqlv1.AiChatMsg{}}}
			consumerDone := make(chan struct{})
			if streaming {
				err = session.proxyAIStream(envelope, "lost-request", node.NodeTypeAgent, func(ctx context.Context, correlate, requestID string) {
					session.consumeTokenStream(ctx, correlate, requestID)
					close(consumerDone)
				})
			} else {
				err = session.proxyAI(envelope, "lost-request", node.NodeTypeAgent)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-agent.requests:
			case <-time.After(time.Second):
				t.Fatal("lost peer never received request")
			}
			close(agent.drop)
			awaitDisconnectCondition(t, func() bool { return len(client.snapshot()) > 0 }, "browser received no terminal error after its peer disconnected")
			msgs := client.snapshot()
			qe := msgs[0].GetQueryError()
			if qe == nil || qe.GetError().GetCode() != codes.Unavailable.String() || qe.GetRequestId() != "lost-request" || msgs[0].GetCorrelateTo() != "browser-message" {
				t.Fatalf("wrong terminal: %v", msgs)
			}
			if streaming {
				select {
				case <-consumerDone:
				case <-time.After(time.Second):
					t.Fatal("failed stream left its substrate consumer alive")
				}
			}
			if browserCtx.Err() != nil {
				t.Fatal("browser context was canceled")
			}
			if router.HasInflight("lost-request") {
				t.Fatal("lost request remains inflight")
			}
			if !router.HasInflight("other-request") {
				t.Fatal("unrelated peer request was terminated")
			}
			select {
			case msg, ok := <-unaffected:
				t.Fatalf("unrelated peer response changed: %v %v", msg, ok)
			default:
			}
			awaitDisconnectCondition(t, func() bool { return agent.welcomes.Load() >= 2 }, "peer did not reconnect")
			if err := router.ForwardContinuation("lost-request", testForwardedPrincipal(t), envelope); err == nil {
				t.Fatal("failed request accepted continuation on a new stream")
			}
			select {
			case req := <-agent.requests:
				t.Fatalf("lost request automatically replayed: %s", req.GetRequestId())
			default:
			}
			_, err = router.Forward(browserCtx, "new-request", node.NodeTypeAgent, testForwardedPrincipal(t), &memqlv1.MemqlClientMessage{MessageId: "new-request"})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case req := <-agent.requests:
				if req.GetRequestId() != "new-request" {
					t.Fatalf("replayed old request %s", req.GetRequestId())
				}
			case <-time.After(time.Second):
				t.Fatal("reconnected peer did not receive fresh work")
			}
			selected, err := router.selectPeer(node.NodeTypePlanner)
			if err != nil {
				t.Fatal(err)
			}
			peers.DetachConnection(planner.nodeID)
			if selected.Connection == nil {
				t.Fatal("selection retained mutable peer entry across detach")
			}
			cancel()
			awaitDisconnectCondition(t, func() bool { return !router.HasInflight("other-request") }, "cancellation did not clear detached peer request")
			if len(client.snapshot()) != 1 {
				t.Fatalf("duplicate terminal responses: %v", client.snapshot())
			}

		})
	}
}

func TestAiForwardDisconnectRetainsTerminalWithFullBuffer(t *testing.T) {
	router := NewAiForwardRouter(nil, testLogger())
	entry := &inflightEntry{respCh: make(chan *memqlv1.MemqlServerMessage, 1), done: make(chan struct{})}
	entry.respCh <- &memqlv1.MemqlServerMessage{}
	router.inflight["full"] = entry
	router.failInflight("full", entry, "connection lost")
	msg, ok := <-entry.respCh
	if !ok || msg.GetQueryError().GetError().GetCode() != codes.Unavailable.String() {
		t.Fatalf("terminal was dropped: %v", msg)
	}
	if _, ok := <-entry.respCh; ok {
		t.Fatal("response channel still open")
	}
	select {
	case <-entry.done:
	default:
		t.Fatal("request watcher was not stopped")
	}
}

func TestAiForwardDisconnectRacesTerminalResponse(t *testing.T) {
	for i := 0; i < 100; i++ {
		router := NewAiForwardRouter(nil, testLogger())
		entry := &inflightEntry{respCh: make(chan *memqlv1.MemqlServerMessage, 2), done: make(chan struct{})}
		router.inflight["race"] = entry
		finished := make(chan struct{})
		go func() {
			router.Dispatch(&nodev1.AiForwardResponse{RequestId: "race", Done: true, MemqlServerMsg: encodeForwardErrorBytes("race", "remote error")})
			close(finished)
		}()
		router.failInflight("race", entry, "transport lost")
		<-finished
		count := 0
		for range entry.respCh {
			count++
		}
		if count != 1 {
			t.Fatalf("race produced %d terminal responses", count)
		}
	}
}

func TestAiForwardRelayReportsMissingTerminalOnly(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		client := newRecordingClientStream(context.Background())
		session := &streamSession{stream: client, logger: testLogger()}
		responses := make(chan *memqlv1.MemqlServerMessage, 1)
		if terminal {
			responses <- &memqlv1.MemqlServerMessage{Payload: &memqlv1.MemqlServerMessage_AiChatResult{AiChatResult: &memqlv1.AiChatResult{RequestId: "req"}}}
		}
		close(responses)
		session.relayForwardedResponses("browser", "req", responses)
		msgs := client.snapshot()
		if len(msgs) != 1 {
			t.Fatalf("terminal=%v: got %d responses", terminal, len(msgs))
		}
		if terminal && msgs[0].GetQueryError() != nil {
			t.Fatal("successful completion gained an error")
		}
		if !terminal && msgs[0].GetQueryError().GetError().GetCode() != codes.Unavailable.String() {
			t.Fatalf("missing terminal was silent: %v", msgs)
		}
	}
}

func TestAiDispatchablePeerConcurrentDetach(t *testing.T) {
	remote, addr := startDisconnectPeer(t, node.NodeTypeAgent)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	identity := &node.Identity{ID: "browser-bff", Type: node.NodeTypeBFF}
	peers := node.NewPeerManager(identity, testLogger())
	router := NewAiForwardRouter(peers, testLogger())
	dialer := node.NewWorkerDialer(identity, peers, nil, nil, []node.WorkerTarget{{NodeType: node.NodeTypeAgent, Address: addr}}, testLogger())
	dialer.Start(ctx)
	awaitDisconnectCondition(t, func() bool {
		ps := peers.SnapshotByType(node.NodeTypeAgent)
		return len(ps) == 1 && ps[0].Connection != nil
	}, "handshake failed")
	selected := peers.SnapshotByType(node.NodeTypeAgent)[0]
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			peers.DetachConnection(remote.nodeID)
			peers.AttachConnection(remote.nodeID, selected.Connection)
		}
	}()
	for i := 0; i < 10000; i++ {
		router.hasDispatchablePeer(node.NodeTypeAgent)
	}
	<-done
	peers.AttachConnection(remote.nodeID, selected.Connection)
	if !router.hasDispatchablePeer(node.NodeTypeAgent) {
		t.Fatal("reattached peer is unavailable")
	}
	peers.DetachConnection(remote.nodeID)
	if router.hasDispatchablePeer(node.NodeTypeAgent) {
		t.Fatal("detached peer remains dispatchable")
	}
}
