package node

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
	memqlengine "github.com/znasllc-io/memql/component/memql"
)

// Real history and the real DSL query: a latest-only fake cannot expose the
// 50-version window that disconnected healthy replicas during a cold load.
func TestWorkerDiscoverySurvivesMoreThanFiftyHistoricalRows(t *testing.T) {
	ctx := context.Background()
	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN()))), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		dbtest.Unreachable(t, "worker discovery", dbtest.DSN(), err)
		return
	}
	if _, err := memqlengine.LoadUnifiedConcepts(nil); err != nil {
		t.Fatal(err)
	}
	engine, err := memqlengine.New(db)
	if err != nil {
		t.Fatal(err)
	}
	engine.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if err = engine.Init(memorynodes.DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("discovery-%d", time.Now().UnixNano())
	var ids []string
	t.Cleanup(func() {
		if len(ids) > 0 {
			_, _ = db.NewDelete().Model((*memorynodes.MemoryNode)(nil)).Where("concept = ?", "v1:cluster:node").Where("id IN (?)", bun.In(ids)).Exec(ctx)
		}
	})
	base := time.Now().UTC().Add(-time.Second)
	var rows []memorynodes.MemoryNode
	add := func(short, health string, at time.Time) {
		id := "v1:cluster:node:" + short
		ids = append(ids, id)
		raw, _ := json.Marshal(map[string]any{"nodeType": "agent", "address": short + ":50055", "health": health, "lastSeen": at.Format(time.RFC3339Nano)})
		rows = append(rows, memorynodes.MemoryNode{ID: id, CreatedAt: at, CreatedBy: "system:test", Concept: "v1:cluster:node", Type: memorynodes.NodeTypeObject, Schema: json.RawMessage(`{}`), Payload: raw, Metadata: json.RawMessage(`{}`), Provenance: json.RawMessage(`{}`)})
	}
	// More than 50 current nodes also requires the query's unbounded contract.
	for i := 0; i < 55; i++ {
		add(fmt.Sprintf("%s-stable-%d", prefix, i), "healthy", base.Add(time.Duration(i)*time.Microsecond))
	}
	for i := 0; i < 60; i++ {
		add(prefix+"-noisy", "degraded", base.Add(time.Duration(i+1)*time.Millisecond))
	}
	add(prefix+"-retired", "healthy", base)
	add(prefix+"-retired", "stopped", base.Add(80*time.Millisecond))
	if _, err = db.NewInsert().Model(&rows).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	identity := &Identity{ID: "discovery-reader", Type: NodeTypeBFF, Address: "self:50058"}
	wd := NewWorkerDialer(identity, NewPeerManager(identity, testLogger()), engine, nil, nil, testLogger())
	targets, err := wd.discoverFromDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, target := range targets {
		found[target.NodeId] = true
	}
	for i := 0; i < 55; i++ {
		if !found[fmt.Sprintf("%s-stable-%d", prefix, i)] {
			t.Fatalf("healthy replica %d vanished behind liveness history (%d targets)", i, len(targets))
		}
	}
	if found[prefix+"-retired"] {
		t.Fatal("historical healthy row revived a stopped replica")
	}
}
