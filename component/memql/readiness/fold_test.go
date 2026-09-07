package readiness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type foldFixture struct {
	Name    string         `json:"name"`
	Now     time.Time      `json:"now"`
	Reports []NodeReport   `json:"reports"`
	Nodes   []NodeLiveness `json:"nodes"`
	Expect  []struct {
		Module       string   `json:"module"`
		State        State    `json:"state"`
		Disagreement []string `json:"disagreement"`
	} `json:"expect"`
}

// THE FIXTURES ARE SHARED. clients/os/test/system/readinessFold.test.ts reads
// the same directory, so a case added here is a case the TypeScript mirror
// must also pass; that is the whole parity mechanism.
func TestFoldFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "fold", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 7 {
		t.Fatalf("expected at least 7 fold fixtures, found %d -- the parity set is incomplete", len(paths))
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fx foldFixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		t.Run(fx.Name, func(t *testing.T) {
			got := Fold(fx.Reports, fx.Nodes, fx.Now)
			if len(got) != len(fx.Expect) {
				t.Fatalf("got %d verdicts, want %d: %+v", len(got), len(fx.Expect), got)
			}
			for i, want := range fx.Expect {
				if got[i].Module != want.Module || got[i].State != want.State {
					t.Errorf("verdict %d: got %s=%s, want %s=%s", i, got[i].Module, got[i].State, want.Module, want.State)
				}
				if !reflect.DeepEqual(got[i].Disagreement, want.Disagreement) {
					t.Errorf("verdict %d disagreement: got %v, want %v", i, got[i].Disagreement, want.Disagreement)
				}
			}
		})
	}
}

func TestNodeIsLiveNeedsBothHealthAndRecency(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-NodeLiveWindow / 2)
	stale := now.Add(-NodeLiveWindow - time.Second)
	cases := []struct {
		n    NodeLiveness
		want bool
	}{
		{NodeLiveness{NodeId: "a", Health: "healthy", LastSeen: fresh}, true},
		{NodeLiveness{NodeId: "a", Health: "draining", LastSeen: fresh}, true},
		{NodeLiveness{NodeId: "a", Health: "stopped", LastSeen: fresh}, false},
		{NodeLiveness{NodeId: "a", Health: "offline", LastSeen: fresh}, false},
		{NodeLiveness{NodeId: "a", Health: "healthy", LastSeen: stale}, false},
		{NodeLiveness{NodeId: "a", Health: "healthy"}, false},
	}
	for _, c := range cases {
		if got := NodeIsLive(c.n, now); got != c.want {
			t.Errorf("%+v: got %v want %v", c.n, got, c.want)
		}
	}
}
