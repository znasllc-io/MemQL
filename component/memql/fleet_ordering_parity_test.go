package memql

// The ordering rule has two implementations, and this is what keeps them in
// step (epic memql#5096, design D5).
//
// `orderModels` here is what the router applies when a policy names
// `fleet:*`. MemQL OS's Models section restates it in
// clients/os/src/apps/fleet/models/ordering.ts, because its whole job is to
// show WHICH MODEL WILL BE USED -- and a list the engine did not order is a
// list the reader has to re-derive the ordering of, which is the question
// they came to the page with. `fleetModels` returns rows sorted by id
// deliberately, so ranking is a second question and that file is its answer.
//
// Neither side can be deleted in favour of the other: the engine cannot ship
// a per-caller ranking on a projection every reader shares, and the page
// cannot ask the engine per render.
//
// So the risk is DRIFT, and drift here is invisible in the worst way -- both
// sides keep working and the page simply names a different model from the one
// the router picks. Both read the SAME fixture, so a change to either rule
// fails on the other's side.
//
// A MISSING FIXTURE IS A FAILURE, NOT A SKIP. A skip makes this gate silently
// vacuous the moment the file is renamed -- which is exactly when the two
// rules are most likely to have diverged. The failure names the path, so the
// fix is obvious in either direction.

import (
	"encoding/json"
	"os"
	"testing"
)

// orderingFixturePath is the shared table, relative to this package. Written
// out rather than derived so a failure names the file to open.
const orderingFixturePath = "../../clients/os/src/apps/fleet/models/ordering.fixture.json"

type orderingFixture struct {
	Cases []struct {
		Name       string   `json:"name"`
		Preference []string `json:"preference"`
		Models     []struct {
			ModelId       string `json:"modelId"`
			Params        int64  `json:"params"`
			ActiveParams  int64  `json:"activeParams"`
			Quant         string `json:"quant"`
			ContextWindow int    `json:"contextWindow"`
		} `json:"models"`
		Want []string `json:"want"`
	} `json:"cases"`
}

func TestModelOrderingMatchesTheClients(t *testing.T) {
	raw, err := os.ReadFile(orderingFixturePath)
	if err != nil {
		t.Fatalf("the shared ordering table is unreadable at %s: %v\n"+
			"It is the only thing keeping component/memql.orderModels and MemQL OS's\n"+
			"copy in step. If it moved, update orderingFixturePath; if it was deleted,\n"+
			"the client is ranking models some other way and that needs a decision,\n"+
			"not a skip.", orderingFixturePath, err)
	}
	var fixture orderingFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("%s does not parse: %v", orderingFixturePath, err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatalf("%s declares no cases -- a gate over nothing passes for the wrong reason",
			orderingFixturePath)
	}

	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			models := make([]FleetModel, 0, len(tc.Models))
			for _, m := range tc.Models {
				models = append(models, FleetModel{
					ModelId:       m.ModelId,
					Params:        m.Params,
					ActiveParams:  m.ActiveParams,
					Quant:         m.Quant,
					ContextWindow: m.ContextWindow,
				})
			}
			got := orderModels(models, tc.Preference)
			if len(got) != len(tc.Want) {
				t.Fatalf("ordered %d models, want %d", len(got), len(tc.Want))
			}
			for i, want := range tc.Want {
				if got[i].ModelId != want {
					ids := make([]string, 0, len(got))
					for _, m := range got {
						ids = append(ids, m.ModelId)
					}
					t.Fatalf("order = %v, want %v\n"+
						"The same table is asserted by MemQL OS's ordering.test.ts, so this\n"+
						"failure means one of the two rules moved and the other did not --\n"+
						"and the symptom in production is a page naming a different model\n"+
						"from the one the router picks.", ids, tc.Want)
				}
			}
		})
	}
}
