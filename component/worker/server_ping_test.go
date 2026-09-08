package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// server_ping_test.go covers the cluster's half of liveness (epic memql#5218,
// D11): the Ping the agent sends down the stream, the Pong that answers it,
// and the round trip that lands on the Worker and, at the next heartbeat
// flush, in the store call.
//
// The handler and flush halves run on a session with NO stream, the heartbeat
// harness's shape -- sendPing tolerates a nil stream precisely so that harness
// can drive it. The wire half runs on the fakeWorkerStream so the Ping itself
// can be read back.

// pingClock is a settable clock. The tests below move it by hand between the
// Ping and the Pong, so the round trip is a number the test chose rather than
// whatever the scheduler happened to allow. No goroutine reads it while it is
// moved: the pinger is never started on these sessions.
type pingClock struct{ now time.Time }

func (c *pingClock) fn() func() time.Time { return func() time.Time { return c.now } }

// newPingTestSession is newHeartbeatTestSession with a stream, so a test can
// see the Ping go out AND drive the heartbeat flush on the same session.
func newPingTestSession(store Store, clock func() time.Time) (*streamSession, *fakeWorkerStream) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, store, NewRegistry(nil, clock), nil, clock, testNodeId)
	stream := &fakeWorkerStream{}
	w := &Worker{RegistrationId: "reg-1", OwnerUserId: "user-1"}
	ctx, cancel := context.WithCancel(context.Background())
	return newStreamSession(srv, stream, w, ctx, cancel), stream
}

// lastPing returns the most recent Ping the session put on the stream.
func lastPing(t *testing.T, stream *fakeWorkerStream) *memqlv1.Ping {
	t.Helper()
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.sent) == 0 {
		t.Fatalf("no message reached the stream")
	}
	msg := stream.sent[len(stream.sent)-1]
	ping := msg.GetPing()
	if ping == nil {
		t.Fatalf("last message is not a Ping: %T", msg.GetPayload())
	}
	return ping
}

func pongFor(requestId string, sentAt time.Time) *memqlv1.WorkerClientMessage {
	return &memqlv1.WorkerClientMessage{
		Payload: &memqlv1.WorkerClientMessage_Pong{
			Pong: &memqlv1.Pong{
				RequestId:  requestId,
				SentAt:     timestamppb.New(sentAt),
				ReceivedAt: timestamppb.New(sentAt.Add(5 * time.Millisecond)),
			},
		},
	}
}

// TestHandlePong_RecordsTheRoundTripAndTheNextFlushCarriesIt is the whole
// path: Ping out with the agent's clock on it, Pong back for that id, the
// round trip on the Worker, and rttMs / rttAt in the very next store call.
func TestHandlePong_RecordsTheRoundTripAndTheNextFlushCarriesIt(t *testing.T) {
	t0 := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	clock := &pingClock{now: t0}
	store := &fakeRegistrationStore{}
	session, stream := newPingTestSession(store, clock.fn())
	defer session.cancel()

	session.sendPing()
	ping := lastPing(t, stream)
	if ping.GetRequestId() == "" {
		t.Fatalf("a Ping must carry a request id")
	}
	if got := ping.GetSentAt().AsTime(); !got.Equal(t0) {
		t.Fatalf("Ping.sent_at must be the agent's clock at send, got %v want %v", got, t0)
	}
	stream.mu.Lock()
	msgId := stream.sent[0].GetMessageId()
	stream.mu.Unlock()
	if msgId != ping.GetRequestId() {
		t.Fatalf("the envelope's message_id should be the Ping's request id, got %q want %q", msgId, ping.GetRequestId())
	}

	// 12 ms later the answer arrives -- through handle, so the oneof arm is
	// what is exercised, not the handler in isolation.
	clock.now = t0.Add(12 * time.Millisecond)
	if err := session.handle(context.Background(), pongFor(ping.GetRequestId(), t0), "10.0.0.1:1"); err != nil {
		t.Fatalf("handle(Pong): %v", err)
	}
	rttMs, at := session.worker.RoundTrip()
	if rttMs != 12 {
		t.Fatalf("worker round trip = %d ms, want 12", rttMs)
	}
	if !at.Equal(clock.now) {
		t.Fatalf("worker pong time = %v, want %v", at, clock.now)
	}

	// The next flush carries the figure.
	beatAt(session, clock.now)
	if len(store.lastSeenFlushes) != 1 {
		t.Fatalf("expected one flush, got %d", len(store.lastSeenFlushes))
	}
	flush := store.lastSeenFlushes[0]
	if flush.RttMs != 12 || !flush.RttAt.Equal(clock.now) {
		t.Fatalf("flush carried rtt (%d ms, %v), want (12 ms, %v)", flush.RttMs, flush.RttAt, clock.now)
	}

	// And it is re-asserted on the flush after, not sent once: the row always
	// carries the latest figure this replica holds, dated when it was
	// measured rather than when it was flushed.
	measuredAt := clock.now
	clock.now = clock.now.Add(HeartbeatBatchInterval)
	beatAt(session, clock.now)
	if len(store.lastSeenFlushes) != 2 {
		t.Fatalf("expected two flushes, got %d", len(store.lastSeenFlushes))
	}
	if got := store.lastSeenFlushes[1]; got.RttMs != 12 || !got.RttAt.Equal(measuredAt) {
		t.Fatalf("second flush must carry the same measurement (12 ms, %v), got (%d ms, %v)", measuredAt, got.RttMs, got.RttAt)
	}
}

// TestHandlePong_OnlyTheOutstandingIdCounts: an unknown id changes nothing,
// and so does the id of a Ping a later one has superseded. Both are what a
// late, duplicated or invented Pong looks like, and a figure the agent did not
// ask for is not a measurement.
func TestHandlePong_OnlyTheOutstandingIdCounts(t *testing.T) {
	t0 := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	clock := &pingClock{now: t0}
	store := &fakeRegistrationStore{}
	session := newHeartbeatTestSession(store, clock.fn())
	defer session.cancel()

	// A nil stream: the Ping is recorded as outstanding and nothing is sent.
	session.sendPing()
	first := session.outstandingPing
	if first == "" {
		t.Fatalf("sendPing must record an outstanding id even with no stream")
	}

	clock.now = t0.Add(20 * time.Millisecond)
	session.handlePong(&memqlv1.Pong{RequestId: "not-a-ping-we-sent"})
	if rtt, at := session.worker.RoundTrip(); rtt != 0 || !at.IsZero() {
		t.Fatalf("an unknown id must record nothing, got (%d ms, %v)", rtt, at)
	}
	if session.outstandingPing != first {
		t.Fatalf("an unknown id must not consume the outstanding Ping")
	}

	// A second Ping supersedes the first; the first's Pong is now stale.
	session.sendPing()
	second := session.outstandingPing
	if second == first {
		t.Fatalf("each Ping must carry a fresh id")
	}
	session.handlePong(&memqlv1.Pong{RequestId: first})
	if rtt, at := session.worker.RoundTrip(); rtt != 0 || !at.IsZero() {
		t.Fatalf("a superseded id must record nothing, got (%d ms, %v)", rtt, at)
	}

	// The outstanding one is answered, once. The second Ping was sent at
	// t0+20ms, so its round trip is 30 ms.
	clock.now = t0.Add(50 * time.Millisecond)
	session.handlePong(&memqlv1.Pong{RequestId: second})
	if rtt, _ := session.worker.RoundTrip(); rtt != 30 {
		t.Fatalf("round trip = %d ms, want 30", rtt)
	}
	// Answering it again is a duplicate and moves nothing.
	clock.now = t0.Add(500 * time.Millisecond)
	session.handlePong(&memqlv1.Pong{RequestId: second})
	if rtt, at := session.worker.RoundTrip(); rtt != 30 || !at.Equal(t0.Add(50*time.Millisecond)) {
		t.Fatalf("a duplicate Pong must not re-measure, got (%d ms, %v)", rtt, at)
	}
}

// TestHandleHeartbeat_NoPongLeavesTheFlushUnmeasured: with no Pong at all the
// flush passes a zero rttAt, which the store reads as "not measured" and
// leaves out of the write. A cockpit predating the message looks exactly like
// this, and it must not read as a machine with a 0 ms round trip.
func TestHandleHeartbeat_NoPongLeavesTheFlushUnmeasured(t *testing.T) {
	t0 := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	store := &fakeRegistrationStore{}
	session := newHeartbeatTestSession(store, func() time.Time { return t0 })
	defer session.cancel()

	session.sendPing() // asked, never answered
	beatAt(session, t0)
	if len(store.lastSeenFlushes) != 1 {
		t.Fatalf("expected one flush, got %d", len(store.lastSeenFlushes))
	}
	if got := store.lastSeenFlushes[0]; got.RttMs != 0 || !got.RttAt.IsZero() {
		t.Fatalf("an unanswered Ping must flush as not measured, got (%d ms, %v)", got.RttMs, got.RttAt)
	}
}

// TestHandlePong_ClampsAtZero: a clock that steps backwards between the Ping
// and the Pong is a fact about the clock, not a negative latency.
func TestHandlePong_ClampsAtZero(t *testing.T) {
	t0 := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	clock := &pingClock{now: t0}
	session := newHeartbeatTestSession(&fakeRegistrationStore{}, clock.fn())
	defer session.cancel()

	session.sendPing()
	clock.now = t0.Add(-time.Second)
	session.handlePong(&memqlv1.Pong{RequestId: session.outstandingPing})
	rtt, at := session.worker.RoundTrip()
	if rtt != 0 {
		t.Fatalf("round trip must clamp at 0, got %d", rtt)
	}
	if at.IsZero() {
		t.Fatalf("a clamped measurement is still a measurement; rttAt must be set")
	}
}

// TestRunPinger_SendsTheFirstPingAfterTheDelayAndThenOnTheInterval runs the
// real pinger goroutine against the stream's send seam, with the two
// durations shortened on the server so the test takes milliseconds. That the
// production durations ARE the constants is TestNewServer_PingsOnTheConstants.
func TestRunPinger_SendsTheFirstPingAfterTheDelayAndThenOnTheInterval(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) }
	session, stream := newPingTestSession(&fakeRegistrationStore{}, clock)
	session.server.pingFirst = 60 * time.Millisecond
	session.server.pingEvery = 30 * time.Millisecond

	started := time.Now()
	session.startPinger()
	// Nothing goes out at once: the first Ping waits for the delay.
	stream.mu.Lock()
	immediate := len(stream.sent)
	stream.mu.Unlock()
	if immediate != 0 {
		t.Fatalf("the pinger must wait FirstPingDelay before the first Ping, sent %d at once", immediate)
	}

	waitForSends := func(n int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			stream.mu.Lock()
			got := len(stream.sent)
			stream.mu.Unlock()
			if got >= n {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("the pinger never sent %d Pings", n)
	}
	waitForSends(1)
	if elapsed := time.Since(started); elapsed < 60*time.Millisecond {
		t.Fatalf("first Ping arrived after %v, before the %v delay", elapsed, 60*time.Millisecond)
	}
	// The interval re-arms: a second Ping follows, with a fresh id.
	waitForSends(2)
	stream.mu.Lock()
	first, second := stream.sent[0].GetPing(), stream.sent[1].GetPing()
	stream.mu.Unlock()
	if first == nil || second == nil {
		t.Fatalf("both messages must be Pings")
	}
	if first.GetRequestId() == "" || first.GetRequestId() == second.GetRequestId() {
		t.Fatalf("each Ping must carry a fresh id, got %q then %q", first.GetRequestId(), second.GetRequestId())
	}
	if first.GetSentAt() == nil {
		t.Fatalf("a Ping must carry sent_at")
	}

	// Closing the session stops it.
	session.cancel()
	time.Sleep(50 * time.Millisecond)
	stream.mu.Lock()
	atCancel := len(stream.sent)
	stream.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	stream.mu.Lock()
	after := len(stream.sent)
	stream.mu.Unlock()
	if after != atCancel {
		t.Fatalf("the pinger must stop with the session, sent %d more after cancel", after-atCancel)
	}
}

// TestNewServer_PingsOnTheConstants holds the pinger's production timing to
// the two documented constants. The timing test above shortens them on the
// server, which is exactly the seam a drift could hide behind.
func TestNewServer_PingsOnTheConstants(t *testing.T) {
	srv := newServer(nil, &fakeRegistrationStore{}, NewRegistry(nil, nil), nil, nil, testNodeId)
	if srv.pingFirst != FirstPingDelay {
		t.Fatalf("pingFirst = %v, want FirstPingDelay (%v)", srv.pingFirst, FirstPingDelay)
	}
	if srv.pingEvery != PingInterval {
		t.Fatalf("pingEvery = %v, want PingInterval (%v)", srv.pingEvery, PingInterval)
	}
	if FirstPingDelay >= PingInterval {
		t.Fatalf("the first Ping must come sooner than the interval: %v >= %v", FirstPingDelay, PingInterval)
	}
	if FirstPingDelay >= HeartbeatBatchInterval {
		t.Fatalf("the first Ping must land inside the first heartbeat flush window: %v >= %v", FirstPingDelay, HeartbeatBatchInterval)
	}
}
