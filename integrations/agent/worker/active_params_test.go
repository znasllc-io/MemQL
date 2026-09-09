//go:build agent

package worker

import (
	"encoding/json"
	memqlengine "github.com/znasllc-io/memql/component/memql"
	"testing"
)

// A replica reading persisted registration labels has no discovery-side state.
// Both counts must survive that boundary and reach the routing catalog.
func TestActiveParametersSurviveRegistrationLabelsAndCatalogProjection(t *testing.T) {
	labels := map[string]string{"model:mixture": "ctx=131072,params=35000000000,activeparams=3000000000,tools=1"}
	raw, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	var reread map[string]string
	if err := json.Unmarshal(raw, &reread); err != nil {
		t.Fatal(err)
	}
	c := modelMachine("remote", nil)
	c.Labels = reread
	catalog := catalogOf(t, []Candidate{c})
	if len(catalog) != 1 || catalog[0].Params != 35000000000 || catalog[0].ActiveParams != 3000000000 || catalog[0].EffectiveParams() != 3000000000 {
		t.Fatalf("catalog dropped active or total: %+v", catalog)
	}
	attrs := ParseModelAttributes(labels["model:mixture"])
	if got := ParseModelAttributes(attrs.String()); got.ActiveParams != 3000000000 {
		t.Fatalf("label re-emission dropped active params: %+v", got)
	}
}

func TestContextFloorReachesWorkerEnvelope(t *testing.T) {
	req := memqlengine.FleetCallRequest{Kind: memqlengine.FleetKindChat, ContextTokens: 32768}
	if req.Needs().MinContextWindow != 32768 {
		t.Fatal("context floor lost before machine eligibility")
	}
	start := (&FleetInference{}).buildStart(req)
	if start.GetParams().GetContextTokens() != 32768 {
		t.Fatalf("wire context=%d", start.GetParams().GetContextTokens())
	}
}

func TestCatalogValidatesActiveParametersBeforeMergingMachines(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		total, active, wantActive int64
	}{
		{"invalid", 2_000_000_000, 9_000_000_000, 3_000_000_000},
		{"unknown total", 0, 9_000_000_000, 9_000_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machines := []Candidate{
				modelMachine("first", map[string]ModelAttributes{"same": {Params: tc.total, ActiveParams: tc.active}}),
				modelMachine("second", map[string]ModelAttributes{"same": {Params: 35_000_000_000, ActiveParams: 3_000_000_000}}),
			}
			for _, reverse := range []bool{false, true} {
				if reverse {
					machines[0], machines[1] = machines[1], machines[0]
				}
				got := catalogOf(t, machines)[0]
				if got.Params != 35_000_000_000 || got.ActiveParams != tc.wantActive || got.EffectiveParams() != tc.wantActive {
					t.Errorf("reverse=%v: params=%d active=%d effective=%d; want total35B active%d", reverse, got.Params, got.ActiveParams, got.EffectiveParams(), tc.wantActive)
				}
			}
		})
	}
}
