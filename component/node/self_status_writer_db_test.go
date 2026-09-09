package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/znasllc-io/memql/component/database/dbtest"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	memqlengine "github.com/znasllc-io/memql/component/memql"
)

// Drive actual updates and the actual stale-node query: startup-only or
// transition-only persistence leaves the healthy self among prune candidates.
func TestSelfStatusWriterProtectsSteadyHealthyNodeFromThirtyMinutePrune(t *testing.T) {
	ctx := context.Background()
	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN()))), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		dbtest.Unreachable(t, "self liveness", dbtest.DSN(), err)
		return
	}
	_, err := memqlengine.LoadUnifiedConcepts(nil)
	require.NoError(t, err)
	engine, err := memqlengine.New(db)
	require.NoError(t, err)
	engine.Logger = testLogger()
	require.NoError(t, engine.Init(memorynodes.DefaultRegistry()))
	prefix := fmt.Sprintf("self-liveness-%d", time.Now().UnixNano())
	selfID := "v1:cluster:node:" + prefix + "-self"
	oldID := "v1:cluster:node:" + prefix + "-old"
	t.Cleanup(func() {
		_, _ = db.NewDelete().Model((*memorynodes.MemoryNode)(nil)).Where("concept = ?", "v1:cluster:node").Where("id IN (?)", bun.In([]string{selfID, oldID})).Exec(ctx)
	})
	now := time.Now().UTC().Truncate(time.Second)
	oldAt := now.Add(-35 * time.Minute)
	payload, err := json.Marshal(map[string]any{"nodeType": "edge", "address": "self:50057", "health": "healthy", "lastSeen": oldAt.Format(time.RFC3339)})
	require.NoError(t, err)
	rows := []memorynodes.MemoryNode{}
	for _, id := range []string{selfID, oldID} {
		rows = append(rows, memorynodes.MemoryNode{ID: id, CreatedAt: oldAt, CreatedBy: "system:test", Concept: "v1:cluster:node", Type: memorynodes.NodeTypeObject, Schema: json.RawMessage(`{}`), Payload: payload, Metadata: json.RawMessage(`{}`), Provenance: json.RawMessage(`{}`)})
	}
	_, err = db.NewInsert().Model(&rows).Exec(ctx)
	require.NoError(t, err)
	lifecycle := NewNodeLifecycle()
	require.NoError(t, lifecycle.MarkReady())
	writer := NewSelfStatusWriter(&Identity{ID: prefix + "-self", Type: NodeTypeEdge, Address: "self:50057"}, lifecycle, engine, testLogger())
	at := oldAt
	writer.now = func() time.Time { return at }
	for i := 0; i < 36; i++ {
		require.NoError(t, writer.refresh(ctx))
		at = at.Add(time.Minute)
	}
	result, err := engine.Execute(ctx, fmt.Sprintf(`query staleClusterNodes(olderThan: %q)`, now.Add(-30*time.Minute).Format(time.RFC3339)))
	require.NoError(t, err)
	require.NotNil(t, result.Bundle)
	foundOld := false
	for _, row := range result.Bundle.Nodes {
		require.NotEqual(t, selfID, row.Id, "healthy self was selected after thirty minutes despite periodic refresh")
		if row.Id == oldID {
			foundOld = true
		}
	}
	require.True(t, foundOld, "old positive control must still be eligible for pruning")
	var latest memorynodes.MemoryNode
	err = db.NewSelect().Model(&latest).Where("id = ?", selfID).OrderExpr(`"createdAt" DESC`).Limit(1).Scan(ctx)
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal(latest.Payload, &stored))
	require.Equal(t, now.Format(time.RFC3339), stored["lastSeen"])
	require.Equal(t, "healthy", stored["health"])
}
