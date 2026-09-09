package memql_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/database/dbtest"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/component/worker/fleetcatalog"
)

// A recording store proves actor plumbing, but cannot prove the DSL actually
// excludes another owner's private machine. Use real persisted registrations
// and the same reader the BFF and agent install, with no worker stream at all.
func TestFleetCatalogGraphReaderEnforcesOwnerAndSharedBoundaries(t *testing.T) {
	ctx := context.Background()
	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN()))), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		dbtest.Unreachable(t, "fleet catalog graph read", dbtest.DSN(), err)
		return
	}
	if _, err := memql.LoadUnifiedConcepts(nil); err != nil {
		t.Fatal(err)
	}
	engine, err := memql.New(db)
	if err != nil {
		t.Fatal(err)
	}
	engine.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := engine.Init(memorynodes.DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("catalog-read-%d", time.Now().UnixNano())
	alice := "v1:identity:user:" + prefix + "-alice"
	bob := "v1:identity:user:" + prefix + "-bob"
	var ids []string
	t.Cleanup(func() {
		if len(ids) > 0 {
			_, _ = db.NewDelete().Model((*memorynodes.MemoryNode)(nil)).Where("concept = ?", "v1:worker:registration").Where("id IN (?)", bun.In(ids)).Exec(ctx)
		}
	})
	for _, spec := range []struct{ name, owner, sharing, serve string }{{"mine", alice, "private", "owner"}, {"private", bob, "private", "owner"}, {"shared", bob, "cluster", "cluster"}, {"owner-consent-only", bob, "cluster", "owner"}} {
		id := "v1:worker:registration:" + prefix + "-" + spec.name
		ids = append(ids, id)
		raw, _ := json.Marshal(map[string]any{"name": spec.name, "ownerUserId": spec.owner, "capabilities": []string{"MODEL"}, "labels": map[string]string{"model:" + prefix + "-" + spec.name: "ctx=32768,structured=1,params=27300000000"}, "sharing": map[string]string{"mode": spec.sharing}, "capabilityDescriptor": map[string]string{"inferenceServe": spec.serve}, "lastSeenAt": time.Now().UTC().Format(time.RFC3339Nano), "connectedNodeId": "another-agent-replica"})
		row := memorynodes.MemoryNode{ID: id, CreatedAt: time.Now().UTC(), CreatedBy: spec.owner, Concept: "v1:worker:registration", Type: memorynodes.NodeTypeObject, Schema: json.RawMessage(`{}`), Payload: raw, Metadata: json.RawMessage(`{}`), Provenance: json.RawMessage(`{}`)}
		if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		reader := &fleetcatalog.Reader{Store: &fleetcatalog.EngineStore{Engine: engine}}
		engine.Providers().SetFleetCatalog(reader)
		owner, err := reader.Catalog(ctx, alice)
		if err != nil {
			t.Fatal(err)
		}
		if len(owner) != 1 || owner[0].ModelId != prefix+"-mine" || !owner[0].Online() {
			t.Fatalf("owner catalog = %+v", owner)
		}
		shared, err := reader.Catalog(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, model := range shared {
			seen[model.ModelId] = true
		}
		if !seen[prefix+"-shared"] || seen[prefix+"-private"] || seen[prefix+"-owner-consent-only"] || seen[prefix+"-mine"] {
			t.Fatalf("shared catalog = %v", seen)
		}
		// Run the actual public builtin on a node with a catalog but no dispatcher.
		result, err := engine.Execute(auth.ContextWithUserActor(ctx, alice), "builtin fleetModels()")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(result.OutputPayload())
		if err != nil {
			t.Fatal(err)
		}
		var nodes map[string]memorynodes.MemoryNode
		if err := json.Unmarshal(raw, &nodes); err != nil {
			t.Fatal(err)
		}
		if _, ok := nodes[prefix+"-mine"]; !ok {
			t.Fatalf("public builtin lost owner model: %s", raw)
		}
		if _, ok := nodes[prefix+"-shared"]; !ok {
			t.Fatalf("public builtin lost shared model: %s", raw)
		}
		for _, name := range []string{"private", "owner-consent-only"} {
			if _, ok := nodes[prefix+"-"+name]; ok {
				t.Fatalf("public builtin leaked %s", name)
			}
		}
		if engine.Providers().FleetInferenceInstalled() {
			t.Fatal("graph reads installed a dispatcher")
		}
	}
}
