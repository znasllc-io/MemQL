package node

import (
	"sync"

	nodev1 "github.com/znasllc-io/memql/component/node/gen"
)

// serializedStream wraps a NodeService bidi stream so every Send is
// mutually exclusive.
//
// gRPC's contract: one goroutine may Send while another Recv's, but two
// goroutines must never call Send on the same stream concurrently. The
// server heartbeat ticker (serverHeartbeatLoop) and every forward path
// (AI / workbench / worker / model / deploy-control) all Send on the
// peer stream. Without this lock, a heartbeat and a ModelForwardDelta
// racing under Ask load corrupt the stream; the peer then sees
// context.Canceled, logs "peer connection lost, reconnecting" with an
// empty peer_id (ParentConnector dials before NodeWelcome), and flaps
// on a ~read-liveness cadence. Prod aks saw exactly that on agent and
// workbench to bff-active while quiet sibling replicas stayed up.
type serializedStream struct {
	nodev1.NodeService_StreamServer
	mu sync.Mutex
}

func serializeStream(stream nodev1.NodeService_StreamServer) *serializedStream {
	return &serializedStream{NodeService_StreamServer: stream}
}

func (s *serializedStream) Send(msg *nodev1.NodeServerMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.NodeService_StreamServer.Send(msg)
}
