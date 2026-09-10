package fleetcatalog_test

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
	"github.com/znasllc-io/memql/component/database/dbtest"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/memql"
	workerservice "github.com/znasllc-io/memql/component/worker"
	"github.com/znasllc-io/memql/component/worker/fleetcatalog"
)

// The observer has a warm query cache and receives no local invalidation from
// the agent writing the next heartbeat. Both owner and shared routing reads
// must still see the new persisted heartbeat before deriving online status.
func TestFleetCatalogReadsFreshHeartbeatWrittenByAnotherNode(t *testing.T) {
	ctx := context.Background()
	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN()))), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		dbtest.Unreachable(t, "fleet catalog heartbeat freshness", dbtest.DSN(), err)
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
	now := time.Now().UTC()
	prefix := fmt.Sprintf("catalog-heartbeat-%d", now.UnixNano())
	owner := "v1:identity:user:" + prefix
	ids := []string{"v1:worker:registration:" + prefix + "-own", "v1:worker:registration:" + prefix + "-shared"}
	t.Cleanup(func() {
		_, _ = db.NewDelete().Model((*memorynodes.MemoryNode)(nil)).Where("concept = ?", "v1:worker:registration").Where("id IN (?)", bun.In(ids)).Exec(ctx)
	})
	writeHeartbeat := func(index int, lastSeen time.Time) {
		t.Helper()
		sharing, serve := "private", "owner"
		if index == 1 {
			sharing, serve = "cluster", "cluster"
		}
		raw, err := json.Marshal(map[string]any{"name": prefix, "ownerUserId": owner, "capabilities": []string{"MODEL"},
			"labels": map[string]string{"model:" + ids[index]: "ctx=32768,structured=1"}, "sharing": map[string]string{"mode": sharing},
			"capabilityDescriptor": map[string]string{"inferenceServe": serve}, "lastSeenAt": lastSeen.Format(time.RFC3339Nano), "connectedNodeId": "another-agent-replica"})
		if err != nil {
			t.Fatal(err)
		}
		row := memorynodes.MemoryNode{ID: ids[index], CreatedAt: time.Now().UTC(), CreatedBy: owner, Concept: "v1:worker:registration", Type: memorynodes.NodeTypeObject,
			Schema: json.RawMessage(`{}`), Payload: raw, Metadata: json.RawMessage(`{}`), Provenance: json.RawMessage(`{}`)}
		if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	readAt := now
	reader := &fleetcatalog.Reader{Store: &fleetcatalog.EngineStore{Engine: engine}, Now: func() time.Time { return readAt }}
	assertOnline := func(index int, phase string) {
		t.Helper()
		actingOwner := owner
		if index == 1 {
			actingOwner = ""
		}
		models, err := reader.Catalog(ctx, actingOwner)
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range models {
			if model.ModelId == ids[index] {
				if !model.Online() {
					t.Errorf("%s: %s catalog held an old heartbeat and marked a live machine offline", phase, []string{"owner", "shared"}[index])
				}
				return
			}
		}
		t.Errorf("%s: catalog lost model %s", phase, ids[index])
	}
	for index := range ids {
		writeHeartbeat(index, now.Add(-workerservice.OnlineWindow+time.Second))
	}
	for index := range ids {
		assertOnline(index, "initial read warms observer cache")
	}
	for index := range ids {
		writeHeartbeat(index, now.Add(time.Second))
	}
	readAt = now.Add(2 * time.Second)
	for index := range ids {
		assertOnline(index, "new heartbeat committed on another node")
	}
}
