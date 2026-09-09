package memql

import (
	"context"
	"time"

	"github.com/znasllc-io/memql/component/envregistry"
	"github.com/znasllc-io/memql/component/memql/readiness"
)

// EvaluateIntegrationReadinessForTest exposes the production resolver and
// evaluator only to external contract tests, avoiding an email import cycle.
func EvaluateIntegrationReadinessForTest(name string, capability IntegrationCapability) readiness.NodeReport {
	e := &MemQLEngine{builtinExecutorHandlers: map[string]builtinExecutorHandler{}}
	if capability.Handler != nil {
		e.builtinExecutorHandlers["integration."+name+".status"] = capability.Handler
	}
	mod := envregistry.Module{Name: name, Evaluator: envregistry.EvaluatorIntegrationPrefix + name}
	return evaluateModule(context.Background(), e.readinessResolvers(), mod, "test-node", "bff", time.Now())
}
