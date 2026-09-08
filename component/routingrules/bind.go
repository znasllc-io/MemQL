package routingrules

// bind.go -- the handoff from app wiring to the integration that serves the
// builtin, mirroring component/emailrules' (epic memql#5127, design D7).
//
// The handoff is a package-level bind rather than a PluginContext field for the
// reason emailrules' is: neither half fits. ActivateApprovedBundle is a method
// on the CONCRETE engine, and the authored-runtime deps are assembled from the
// App's registry, scheduler hooks, catalog promoter and audit sink. A
// PluginContext carrying either would carry the whole authoring runtime to
// every plug-in that never touches it.
//
// UNBOUND IS A WORKING STATE and it refuses. A node with no authored runtime --
// a bff replica, anything built without the scheduler -- cannot arm a rule, and
// the builtin says so rather than returning a success nobody can act on.

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/znasllc-io/memql/component/memql"
)

var (
	bindMu     sync.RWMutex
	boundEng   Engine
	boundDeps  func() memql.AuthoredRuntimeDeps
	boundNames ShippedNames
	boundSince time.Time
)

// Bind installs the activation seam. Called once from app wiring, after the
// authored registry and scheduler exist.
func Bind(engine Engine, deps func() memql.AuthoredRuntimeDeps, shipped ShippedNames) {
	bindMu.Lock()
	defer bindMu.Unlock()
	boundEng, boundDeps, boundNames, boundSince = engine, deps, shipped, time.Now().UTC()
}

// Bound returns an Activator, or nil on a node where nothing was bound.
func Bound() *Activator {
	bindMu.RLock()
	defer bindMu.RUnlock()
	return NewActivator(boundEng, boundDeps, boundNames)
}

// BoundSince reports when the seam was wired, for a status surface. The zero
// time means never.
func BoundSince() time.Time {
	bindMu.RLock()
	defer bindMu.RUnlock()
	return boundSince
}

// ErrUnbound is what a caller gets on a node that cannot arm anything. It is
// its own value so a handler can tell "this node cannot" apart from "this form
// is wrong", which are read by different people.
var ErrUnbound = fmt.Errorf("routingrules: the authored runtime is not wired on this node, so a routing rule cannot be armed here. " +
	"Arming happens on a node that runs the authored-construct scheduler")

// Activate is the package-level entry point the integration calls.
func Activate(ctx context.Context, owner string, f Form) (Result, error) {
	a := Bound()
	if a == nil {
		return Result{}, ErrUnbound
	}
	return a.Activate(ctx, owner, f)
}

// Retire is the package-level counterpart.
func Retire(ctx context.Context, owner, name string) (Result, error) {
	a := Bound()
	if a == nil {
		return Result{}, ErrUnbound
	}
	return a.Retire(ctx, owner, name)
}
