package node

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nodev1 "github.com/znasllc-io/memql/component/node/gen"
	"google.golang.org/grpc/metadata"
)

// concurrentProbeStream fails the test if two Sends overlap. That is the
// gRPC contract violation the production mesh flap was: heartbeat + forward
// reply racing on one stream.
type concurrentProbeStream struct {
	fakeStream
	inFlight atomic.Int32
	overlap  atomic.Int32
	sends    atomic.Int32
}

func (c *concurrentProbeStream) Send(msg *nodev1.NodeServerMessage) error {
	if c.inFlight.Add(1) > 1 {
		c.overlap.Add(1)
	}
	defer c.inFlight.Add(-1)
	time.Sleep(2 * time.Millisecond)
	c.sends.Add(1)
	return c.fakeStream.Send(msg)
}

func TestSerializedStreamRejectsConcurrentSend(t *testing.T) {
	probe := &concurrentProbeStream{fakeStream: *newFakeStream()}
	out := serializeStream(probe)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = out.Send(buildServerHeartbeat(nodev1.NodeHealthStatus_NODE_HEALTH_HEALTHY))
		}()
	}
	wg.Wait()

	if got := probe.overlap.Load(); got != 0 {
		t.Fatalf("serialized Send overlapped %d times; gRPC forbids concurrent Send", got)
	}
	if got := probe.sends.Load(); got != 32 {
		t.Fatalf("sends = %d, want 32", got)
	}
}

// Ensure the wrapper still exposes Context for forward handlers that read it.
func TestSerializedStreamPreservesContext(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("k", "v"))
	base := newFakeStream()
	base.ctx = ctx
	out := serializeStream(base)
	if out.Context() != ctx {
		t.Fatal("serializedStream must preserve the underlying stream Context")
	}
}
