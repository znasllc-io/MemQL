package node

import (
	"context"
	"strings"
	"testing"
	"time"

	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestReconcilerMeshAbsenceRequiresDirectDiscoveryAuthority(t *testing.T) {
	for _, role := range []string{"bff", "agent", "planner", "identity", "workbench", "edge", "mcp", " AGENT ", "unknown"} {
		for _, superseded := range []bool{false, true} {
			name := role
			if superseded {
				name += "/superseded"
			}
			t.Run(name, func(t *testing.T) {
				now := time.Date(2026, 9, 9, 6, 0, 0, 0, time.UTC)
				row := nodeRow(t, "other-pod", "healthy", "", now.Add(-time.Hour).Format(time.RFC3339))
				row.Payload.Fields["nodeType"] = structpb.NewStringValue(role)
				engine := &fakeReconcileEngine{nodes: []*memqlv1.MemoryNode{row}}
				if superseded {
					row.Payload.Fields["deploymentId"] = structpb.NewStringValue("old-deployment")
					engine.deps = []*memqlv1.MemoryNode{deploymentRow(t, "old-deployment", "superseded")}
				}
				reconciler := newTestReconciler(engine, fakeMesh{}, &now)
				reconciler.absentSince["other-pod"] = now.Add(-time.Hour)
				reconciler.reconcile(context.Background())
				wantRetired := superseded || isDialableType(NodeType(strings.ToLower(strings.TrimSpace(role))))
				got := engine.retiredIDs(t)
				if wantRetired && (len(got) != 1 || got[0] != "other-pod") {
					t.Fatalf("explicit retirement authority must retire node: got%v", got)
				}
				if !wantRetired && len(got) != 0 {
					t.Fatalf("local gossip expiry cannot prove %s globally absent: retired%v", role, got)
				}
				if !wantRetired {
					if _, ok := reconciler.absentSince["other-pod"]; ok {
						t.Fatal("non-authoritative absence tracking must be cleared")
					}
				}
			})
		}
	}
}
