package worker

import (
	"testing"

	"github.com/znasllc-io/memql/component/memql/readiness"
)

// THE THREE CONSTANTS component/memql/readiness RESTATES, asserted by IMPORT.
//
// The `ai` readiness evaluator decides whether this cluster has an inference
// door by reading v1:worker:registration rows: a `model:<id>` label meeting the
// capability floor, an `app:<id>` label, and a heartbeat inside the online
// window. All three of those are THIS package's vocabulary.
//
// It cannot import this package. component/worker REQUIRES component/memql, so
// the edge would be a cycle -- and the module-boundaries lane exists to hold
// that direction. This test runs the other way, which is legal, and it is the
// only thing standing between the two copies and a silent disagreement.
//
// WHAT A DISAGREEMENT WOULD LOOK LIKE, since that is what makes the gate worth
// its lines: a cockpit advertising a model under a renamed prefix would be a
// perfectly working machine that the readiness feed could not see. MemQL OS
// would then hold its owner on the core gate -- of a cluster that HAS
// inference -- with no error anywhere and every other surface working.
func TestReadinessRestatesTheWorkerContractExactly(t *testing.T) {
	if readiness.OnlineWindow != OnlineWindow {
		t.Errorf("readiness.OnlineWindow is %s and component/worker.OnlineWindow is %s.\n"+
			"OnlineWindow here is 2 x HeartbeatBatchInterval. If you changed either, the copies to "+
			"follow are component/memql/readiness/inference.go (this one) and "+
			"clients/os/src/apps/fleet/online.ts (TestFleetOnlineWindowMatchesTheClients).",
			readiness.OnlineWindow, OnlineWindow)
	}
	if readiness.ModelLabelPrefix != ModelLabelPrefix {
		t.Errorf("the model label prefix is %q in component/memql/readiness and %q here. A machine "+
			"advertising a qualifying model would be invisible to the readiness feed, so the core "+
			"gate would hold on a cluster that has inference.",
			readiness.ModelLabelPrefix, ModelLabelPrefix)
	}
	if readiness.AppLabelPrefix != AppLabelPrefix {
		t.Errorf("the app label prefix is %q in component/memql/readiness and %q here",
			readiness.AppLabelPrefix, AppLabelPrefix)
	}
}

// The app door is read off the `app:<id>` LABEL, whose derivation is this
// package's AppLabels -- a known id AND allowed by the machine's own policy AND
// signed in. That is why component/memql/readiness needs no copy of the closed
// runnable set on its primary path, and this pins the property it leans on: a
// label is emitted for exactly the apps Runnable() admits, and for no others.
func TestAppLabelsAreEmittedOnlyForRunnableApps(t *testing.T) {
	apps := []AppInfo{
		{Id: AppIdClaudeCode, Version: "2.1.0", SignedIn: true, Allowed: true},
		{Id: AppIdCodex, Version: "1.0.0", SignedIn: false, Allowed: true},
		{Id: AppIdCodex, Version: "1.0.0", SignedIn: true, Allowed: false},
		{Id: "some-future-app", Version: "9.9", SignedIn: true, Allowed: true},
	}
	labels := AppLabels(apps)
	if _, ok := labels[AppLabelKey(AppIdClaudeCode)]; !ok {
		t.Errorf("a known, allowed, signed-in app produced no label: %v", labels)
	}
	if len(labels) != 1 {
		t.Errorf("AppLabels produced %v. Only a runnable app may produce a label -- that is what "+
			"lets component/memql/readiness read the app door off the label instead of carrying a "+
			"second copy of the engine's closed runnable app set.", labels)
	}
}
