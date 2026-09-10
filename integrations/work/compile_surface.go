package work

import "fmt"

// compile_surface.go -- honest createGoal when this replica has no local Compiler.
//
// Folded from memql#5268 onto the claim-based compile path in compile.go:
// planner replicas still claim via HandleRunEvent → dispatchCompile; BFF
// replicas opt into event-forward so createGoal does not silently accept a
// run nobody will compile.

// EnableCompileViaEvent marks this replica as able to hand compile to the
// cluster through the run graph event. Called once from mesh / BFF bootstrap
// on every node that accepts createGoal but does not run compile itself.
//
// First call wins, matching SetCompiler: two halves of one deployment must
// not disagree about whether a nil compiler is a forward or a refuse.
func (i *Integration) EnableCompileViaEvent() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.compileViaEvent {
		i.compileViaEvent = true
	}
}

func (i *Integration) compileViaEventEnabled() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.compileViaEvent
}

// hasCompileSurface reports whether createGoal can honestly say compile will
// run: either this replica compiles, or the run event will reach a replica that does.
func (i *Integration) hasCompileSurface() bool {
	return i.compilerRef() != nil || i.compileViaEventEnabled()
}

// errNoCompileSurface is the loud refuse. Kept as one sentence so every caller
// -- createGoal, fork, responsibility -- says the same thing.
var errNoCompileSurface = fmt.Errorf("work: no compile surface — createGoal needs a planner (local compiler) or mesh event forward; refusing rather than leaving a run stuck in compiling")
