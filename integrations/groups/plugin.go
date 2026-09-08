package groups

// The groups plug-in (epic memql#5165, D12).
//
// Registered on EVERY node type, and the reasoning is customdomain's: the
// concepts and their queries load everywhere anyway, the five caller-facing
// verbs are reached from whichever node serves the OS shell's connection, and
// `groupEnsureForAccount` fires from an automation the cron leader elects one
// runner for across the mesh. Gating the registration by build tag would make
// a verb's availability depend on which replica a browser's stream happened to
// land on -- a coin flip at two replicas, which is the class of bug memql#4352
// closed for workers.

import (
	"context"

	"github.com/znasllc-io/memql/component/memql"
)

func init() {
	memql.RegisterPlugin("groups", func(pctx memql.PluginContext) (memql.IntegrationProvider, error) {
		warn := func(msg string, args ...any) {}
		if pctx.Logger != nil {
			logger := pctx.Logger
			warn = func(msg string, args ...any) { logger.Warn(msg, args...) }
		}
		return New(engineAdapter{pctx.Engine}, warn), nil
	})
}

// engineAdapter narrows IntegrationEngineAccess to the one method this package
// uses. Narrow deliberately: nothing here should be able to reach the AI
// provider registry or the tool surface.
type engineAdapter struct{ engine memql.IntegrationEngineAccess }

func (a engineAdapter) Execute(ctx context.Context, query string) (any, error) {
	return a.engine.Execute(ctx, query)
}
