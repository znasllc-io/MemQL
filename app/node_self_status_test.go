package app

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/component/node"
)

func TestNodeSelfStatusWriterIsWiredIntoProductionLifecycle(t *testing.T) {
	source, err := os.ReadFile("cluster.go")
	require.NoError(t, err)
	require.Contains(t, string(source), "a.wireNodeSelfStatus(nodeIdentity, a.nodeLifecycle)")
	require.Contains(t, string(source), "selfStatus.SyncLifecycle()")
	require.Equal(t, 1, strings.Count(string(source), "a.wireNodeSelfStatus(nodeIdentity, a.nodeLifecycle)"), "one self writer per process")
}

func TestNodeSelfStatusWriterWiringIncludesAllSevenRoles(t *testing.T) {
	engine, err := memql.New(nil)
	require.NoError(t, err)
	for _, role := range []node.NodeType{node.NodeTypeBFF, node.NodeTypeAgent, node.NodeTypePlanner, node.NodeTypeIdentity, node.NodeTypeWorkbench, node.NodeTypeEdge, node.NodeTypeMCP} {
		t.Run(string(role), func(t *testing.T) {
			identity := &node.Identity{ID: "pod", Type: role, Address: "self:50051"}
			app := &App{engine: engine}
			deps, err := node.BootstrapFor(role).NodeDependencies(node.BootstrapContext{Identity: identity, Logger: engine.Logger})
			require.NoError(t, err)
			var lifecycle *node.NodeLifecycle
			for _, dep := range deps {
				if pm, ok := dep.(*node.PeerManager); ok {
					lifecycle = pm.Lifecycle()
				}
			}
			require.NotNil(t, lifecycle, "including fallback bootstrap for identity and edge")
			writer := app.wireNodeSelfStatus(identity, lifecycle)
			require.NotNil(t, writer)
			require.Len(t, app.Dependencies, 1)
			require.Same(t, writer, app.Dependencies[0])
		})
	}
}
