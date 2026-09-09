package app

import "github.com/znasllc-io/memql/component/worker/fleetcatalog"

// Every engine can read persisted registration reports. Worker stream ownership
// is needed only for calls, which cluster_worker.go installs on agent builds.
func (a *App) wireFleetCatalog() {
	if a == nil || a.engine == nil {
		return
	}
	if providers := a.engine.Providers(); providers != nil {
		providers.SetFleetCatalog(&fleetcatalog.Reader{Store: &fleetcatalog.EngineStore{Engine: a.engine}})
	}
}
