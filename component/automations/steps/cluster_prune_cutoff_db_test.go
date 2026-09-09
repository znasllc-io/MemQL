package steps

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/znasllc-io/memql/component/automations"
	"github.com/znasllc-io/memql/component/database/dbtest"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/memql"
)

func TestPruneStaleClusterNodesPreservesFreshHeartbeats(t *testing.T) {
	ctx := context.Background()
	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN()))), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		dbtest.Unreachable(t, "prune cutoff", dbtest.DSN(), err)
		return
	}
	_, err := memql.LoadUnifiedConcepts(nil)
	require.NoError(t, err)
	eng, err := memql.New(db)
	require.NoError(t, err)
	eng.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NoError(t, eng.Init(memorynodes.DefaultRegistry()))
	eng.SetLogicRunner(automations.NewLogicRunner(eng, NewRegistry(), eng.Logger))
	prefix := fmt.Sprintf("prune-cutoff-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	var rows []memorynodes.MemoryNode
	var ids []string
	for _, fixture := range []struct {
		name, health string
		age          time.Duration
	}{
		{"fresh", "healthy", 20 * time.Minute}, {"stale", "healthy", 40 * time.Minute}, {"stopped", "stopped", time.Hour},
	} {
		id := "v1:cluster:node:" + prefix + "-" + fixture.name
		ids = append(ids, id)
		payload, err := json.Marshal(map[string]any{"nodeType": "agent", "address": "test:50055", "health": fixture.health, "lastSeen": now.Add(-fixture.age).Format(time.RFC3339Nano)})
		require.NoError(t, err)
		rows = append(rows, memorynodes.MemoryNode{ID: id, CreatedAt: now.Add(-fixture.age), CreatedBy: "system:test", Concept: "v1:cluster:node", Type: memorynodes.NodeTypeObject, Schema: json.RawMessage(`{}`), Payload: payload, Metadata: json.RawMessage(`{}`), Provenance: json.RawMessage(`{}`)})
	}
	_, err = db.NewInsert().Model(&rows).Exec(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.NewDelete().Model((*memorynodes.MemoryNode)(nil)).Where("concept = ?", "v1:cluster:node").Where("id IN (?)", bun.In(ids)).Exec(ctx)
	})
	// Discovery legitimately uses an unfiltered latest scan before the cron.
	_, err = eng.Execute(ctx, `query staleClusterNodes()`)
	require.NoError(t, err)
	for _, call := range []string{
		fmt.Sprintf(`query staleClusterNodes(olderThan: %q)`, now.Add(-30*time.Minute).Format(time.RFC3339Nano)),
		`logic pruneStaleClusterNodes(event: {})`,
	} {
		t.Run(call, func(t *testing.T) {
			result, err := eng.Execute(ctx, call)
			require.NoError(t, err)
			raw, err := json.Marshal(result.OutputPayload())
			require.NoError(t, err)
			require.True(t, strings.Contains(string(raw), prefix+"-stale"), "old positive control missing")
			require.False(t, strings.Contains(string(raw), prefix+"-fresh"), "fresh node selected for retirement")
			require.False(t, strings.Contains(string(raw), prefix+"-stopped"), "already stopped node selected")
		})
	}
}
