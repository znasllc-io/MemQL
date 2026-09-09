package node

import (
	"testing"

	nodev1 "github.com/znasllc-io/memql/component/node/gen"
)

func TestPeerConnectionScopedWorkCannotCrossReconnect(t *testing.T) {
	pc := newPeerConnection(&Identity{ID: "bff"}, "agent", "unused", testLogger())
	msg := &nodev1.NodeClientMessage{MessageId: "once"}
	if _, err := pc.SendOnStream(msg, nil); err == nil {
		t.Fatal("disconnected work was accepted")
	}
	first := &peerStream{done: make(chan struct{}), sendCh: make(chan *nodev1.NodeClientMessage, 1)}
	pc.current = first
	done, err := pc.SendOnStream(msg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.SendOnStream(msg, done); err == nil {
		t.Fatal("full scoped outbox silently accepted work")
	}
	pc.mu.Lock()
	pc.endStreamLocked()
	pc.mu.Unlock()
	select {
	case <-done:
	default:
		t.Fatal("lost attempt did not notify its requests")
	}
	second := &peerStream{done: make(chan struct{}), sendCh: make(chan *nodev1.NodeClientMessage, 1)}
	pc.current = second
	if _, err := pc.SendOnStream(msg, done); err == nil {
		t.Fatal("continuation was accepted by another attempt")
	}
	select {
	case <-pc.sendCh:
		t.Fatal("scoped work entered replayable outbox")
	default:
	}
	select {
	case <-second.sendCh:
		t.Fatal("failed attempt's pending work was replayed")
	default:
	}
	nextDone, err := pc.SendOnStream(&nodev1.NodeClientMessage{MessageId: "fresh"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := <-second.sendCh; got.MessageId != "fresh" {
		t.Fatalf("wrong work on new attempt: %s", got.MessageId)
	}
	pc.Close()
	select {
	case <-nextDone:
	default:
		t.Fatal("Close did not immediately notify active requests")
	}
	if _, err := pc.SendOnStream(msg, nil); err == nil {
		t.Fatal("closed connection accepted work")
	}
}
