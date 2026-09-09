package app

import (
	"os"
	"strings"
	"testing"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/memql"
)

func TestEveryEngineWiresFleetCatalogReader(t *testing.T) {
	source, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "a.wireFleetCatalog()") {
		t.Fatal("BFF engine never installs a fleet catalog reader; graph models disappear on the public query node")
	}
}

func TestProductionFleetCatalogWiringDoesNotInstallDispatcher(t *testing.T) {
	engine, err := memql.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memql.LoadUnifiedConcepts(nil); err != nil {
		t.Fatal(err)
	}
	if err := engine.Init(memorynodes.DefaultRegistry()); err != nil {
		t.Fatal(err)
	}
	app := &App{engine: engine}
	app.wireFleetCatalog()
	if !engine.Providers().FleetCatalogInstalled() {
		t.Fatal("production BFF wiring has no graph catalog reader")
	}
	if engine.Providers().FleetInferenceInstalled() {
		t.Fatal("BFF catalog wiring incorrectly claims worker dispatch")
	}
}
