package rbac

import "github.com/znasllc-io/memql/component/memql"

// init self-registers the rbac integration as an always-on plug-in. Anchored
// from app/plugins_core.go so every node-type binary has the governPrincipal /
// canCreatePrincipal builtins available -- relational RBAC governance (Epic 1,
// memql#2071) is product-agnostic and runs on every node (E1.6 enforces it
// server-side on the request path).
//
// THE FACTORY TAKES THE ENGINE AND THE DATABASE NOW (epic memql#5166). The
// governance half is still pure rank arithmetic and needs neither; the three
// role builtins write rows and count the people a retirement would strand, so
// they do. Both are passed straight through and both may be nil: a node wired
// without a database serves governance and refuses a retirement with a typed
// code, which is the honest answer rather than one that reports zero holders on
// exactly the node that cannot count them.
func init() {
	memql.RegisterPlugin("rbac", func(ctx memql.PluginContext) (memql.IntegrationProvider, error) {
		return New(ctx.Engine, ctx.BunDB), nil
	})
}
