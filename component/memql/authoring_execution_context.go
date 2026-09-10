package memql

import (
	"context"
	"sort"

	"github.com/znasllc-io/memql/core/id"
)

type authoredExecutionKey struct{}
type authoredExecution struct {
	owner    string
	registry *AuthoredRuntimeRegistry
}

// ContextWithAuthoredExecution confines a validated work bundle to this run's
// calls. It registers no shared functions, triggers or schedules.
func ContextWithAuthoredExecution(ctx context.Context, owner string, registry *AuthoredRuntimeRegistry) context.Context {
	return context.WithValue(ctx, authoredExecutionKey{}, authoredExecution{owner, registry})
}

// WorkBundleVersion binds an execution to the complete source closure, including
// function arguments and dependencies absent from the structural run fingerprint.
func WorkBundleVersion(constructs []SandboxConstruct) string {
	rows := make([]map[string]any, 0, len(constructs))
	for _, c := range constructs {
		rows = append(rows, map[string]any{"kind": c.Kind, "name": c.Name, "source": c.Source})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a["kind"] != b["kind"] {
			return a["kind"].(string) < b["kind"].(string)
		}
		if a["name"] != b["name"] {
			return a["name"].(string) < b["name"].(string)
		}
		return a["source"].(string) < b["source"].(string)
	})
	return string(id.NewUntracked().MustFromMap(map[string]any{"constructs": rows}))
}
