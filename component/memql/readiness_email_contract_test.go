package memql

import (
	"context"
	"errors"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/envregistry"
	"github.com/znasllc-io/memql/component/memql/readiness"
	"testing"
	"time"
)

func evaluateIntegrationReadinessForTest(name string, capability IntegrationCapability) readiness.NodeReport {
	e := &MemQLEngine{builtinExecutorHandlers: map[string]builtinExecutorHandler{}}
	if capability.Handler != nil {
		e.builtinExecutorHandlers["integration."+name+".status"] = capability.Handler
	}
	mod := envregistry.Module{Name: name, Evaluator: envregistry.EvaluatorIntegrationPrefix + name}
	return evaluateModule(context.Background(), e.readinessResolvers(), mod, "test-node", "bff", time.Now())
}

func TestIntegrationReadinessEnvelopeSelection(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		want          readiness.State
	}{
		{"named report", `{"integrations":[{"name":"other","state":"configured"},{"name":"email","state":"needs_configuration"}]}`, readiness.Unconfigured},
		{"configured", `{"integrations":[{"name":"email","state":"configured"}]}`, readiness.Configured},
		{"unhealthy remains configured", `{"integrations":[{"name":"email","state":"unhealthy"}]}`, readiness.Configured},
		{"credential touched", `{"integrations":[{"name":"email","state":"needs_configuration","credentials":[{"present":true}]}]}`, readiness.Partial},
		{"unknown state", `{"integrations":[{"name":"email","state":"future_state","settings":[{"source":"env"}]}]}`, readiness.NotApplicable},
		{"empty state", `{"integrations":[{"name":"email","state":""}]}`, readiness.NotApplicable},
		{"missing named report", `{"integrations":[{"name":"other","state":"configured"}]}`, readiness.NotApplicable},
		{"malformed", `{"integrations":`, readiness.NotApplicable},
		{"wrong shape", `{"integrations":{}}`, readiness.NotApplicable},
		{"null", `null`, readiness.NotApplicable},
		{"root state is not a report", `{"state":"configured"}`, readiness.NotApplicable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cap := IntegrationCapability{Handler: func(ctx context.Context, args map[string]any, _ int) ([]memorynodes.MemoryNode, error) {
				if args["probe"] != false {
					t.Fatal("readiness must not probe")
				}
				if err := statusAuthorizedRule(ctx); err != nil {
					t.Fatal(err)
				}
				return []memorynodes.MemoryNode{{Payload: []byte(tc.payload)}}, nil
			}}
			if got := evaluateIntegrationReadinessForTest("email", cap).State; got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	t.Run("absent capability", func(t *testing.T) {
		if got := evaluateIntegrationReadinessForTest("email", IntegrationCapability{}).State; got != readiness.NotApplicable {
			t.Fatal(got)
		}
	})
	t.Run("handler failure", func(t *testing.T) {
		cap := IntegrationCapability{Handler: func(context.Context, map[string]any, int) ([]memorynodes.MemoryNode, error) {
			return nil, errors.New("fixture failure")
		}}
		if got := evaluateIntegrationReadinessForTest("email", cap).State; got != readiness.NotApplicable {
			t.Fatal(got)
		}
	})
}
