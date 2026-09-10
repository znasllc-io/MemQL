package memqlws

import (
	"testing"
	"time"
)

func TestRecentTrafficWindow(t *testing.T) {
	s := &session{}
	if s.recentTraffic(time.Second) {
		t.Fatal("zero lastTraffic must not count as recent")
	}
	s.noteTraffic()
	if !s.recentTraffic(time.Second) {
		t.Fatal("just-noted traffic must be recent")
	}
	s.trafficMu.Lock()
	s.lastTraffic = time.Now().Add(-2 * time.Second)
	s.trafficMu.Unlock()
	if s.recentTraffic(time.Second) {
		t.Fatal("stale traffic must not be recent")
	}
}

func TestDefaultPingIntervalIsAggressiveKeepalive(t *testing.T) {
	if defaultPingInterval != 15*time.Second {
		t.Fatalf("defaultPingInterval = %v, want 15s (under typical 30s idle cuts)", defaultPingInterval)
	}
}
