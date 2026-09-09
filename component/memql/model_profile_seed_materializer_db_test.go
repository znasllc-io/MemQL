package memql

import (
	"context"
	"encoding/json"
	"maps"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	concept "github.com/znasllc-io/memql/component/database/memory-nodes"
)

// Parsing the catalog does not prove boot can write it: a missing
// createModelProfile left every seed registered and the live catalog empty.
// Exercise the real embedded definitions, materializer, mutation and database.
// Only IDs are isolated; no synthetic mutation or replacement catalog is loaded.
func TestModelProfileSeedsMaterializeThroughRealEngine(t *testing.T) {
	eng, db, _ := sharedReadMergeEngine(t)
	ctx := context.Background()
	const conceptID = "v1:models:modelProfile"
	var definitions []*SeedDefinition
	for _, def := range eng.Seeds().All() {
		if def.UseConcept == "modelProfile" {
			definitions = append(definitions, def)
		}
	}
	require.NotEmpty(t, definitions, "embedded model catalog must be loaded")
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	for _, original := range definitions {
		t.Run(original.Name, func(t *testing.T) {
			require.Equal(t, "global", original.Scope)
			def := *original
			def.Body.fields = maps.Clone(original.Body.fields)
			id := uniqueSuffix("model-seed-" + original.Body.fields["id"].str)
			def.Body.fields["id"] = seedValue{kind: seedString, str: id}
			rowID := conceptID + ":" + id
			t.Cleanup(func() {
				_, err := db.NewDelete().Model((*concept.MemoryNode)(nil)).
					Where("concept = ?", conceptID).Where("id IN (?, ?)", rowID, rowID+"-forged").Exec(ctx)
				require.NoError(t, err)
			})
			assertSeedPayload := func() {
				t.Helper()
				payload := latestPayload(t, ctx, db, conceptID, rowID)
				for field, value := range original.Body.fields {
					if field == "id" {
						continue
					}
					want, err := json.Marshal(seedValueToInterface(value))
					require.NoError(t, err)
					got, err := json.Marshal(payload[field])
					require.NoError(t, err)
					require.JSONEq(t, string(want), string(got), "stored seed field %s", field)
				}
				require.Equal(t, true, payload["curated"])
			}
			sm := eng.SeedMaterializer()
			require.NoError(t, sm.materializeGlobal(ctx, &def), "boot must write the embedded model profile")
			assertSeedPayload()
			require.NoError(t, sm.materializeGlobal(ctx, &def), "a second boot must accept an existing curated row")
			assertSeedPayload()

			if original != definitions[0] {
				return
			}
			// Simulate a catalog revision through the same system seed path,
			// then restore the shipped definition. Both directions must persist.
			updated := def
			updated.Body.fields = maps.Clone(def.Body.fields)
			updated.Body.fields["notes"] = seedValue{kind: seedString, str: "Updated catalog recommendation"}
			updated.Body.fields["minMachineClass"] = seedValue{kind: seedString, str: "32"}
			updated.Body.fields["memoryNeedBytes"] = seedValue{kind: seedInt, intV: 9900000000}
			updated.Body.fields["recommendedFor"] = seedValue{kind: seedStringArray, stringsV: []string{"strong"}}
			require.NoError(t, sm.materializeGlobal(ctx, &updated))
			payload := latestPayload(t, ctx, db, conceptID, rowID)
			require.Equal(t, "Updated catalog recommendation", payload["notes"])
			require.Equal(t, "32", payload["minMachineClass"])
			require.Equal(t, float64(9900000000), payload["memoryNeedBytes"])
			require.Equal(t, []any{"strong"}, payload["recommendedFor"])
			require.Equal(t, true, payload["curated"])
			require.NoError(t, sm.materializeGlobal(ctx, &def))
			assertSeedPayload()

			// Ordinary clients may neither replace a curated row nor mint one
			// under a new ID. Assert the permission failure, not any error.
			userCtx := rowAuthzCallerCtx("model-seed-user-" + id)
			for _, targetID := range []string{id, id + "-forged"} {
				args := buildArgsFromBody(def.Body, def.UseConcept, targetID, "")
				args["notes"] = "Forged recommendation"
				call, err := renderArgsCallList(args)
				require.NoError(t, err)
				_, err = eng.Execute(userCtx, "createModelProfile("+call+")")
				require.ErrorContains(t, err, "server-only")
			}
			assertSeedPayload()
			count, err := db.NewSelect().Model((*concept.MemoryNode)(nil)).
				Where("concept = ?", conceptID).Where("id = ?", rowID+"-forged").Count(ctx)
			require.NoError(t, err)
			require.Zero(t, count, "refused user call must not create a curated row")
		})
	}
}
