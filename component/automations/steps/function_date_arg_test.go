package steps

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/znasllc-io/memql/component/automations"
	"github.com/znasllc-io/memql/component/memql"
)

func TestFunctionDateArgumentsResolveBeforeRendering(t *testing.T) {
	evaluator := automations.NewEvaluator()
	evaluator.SetCustom("timestamp", "2026-09-09T06:00:00Z")
	evaluator.SetCustom("args", map[string]any{})
	for _, tc := range []struct {
		name, expr string
		want       any
	}{
		{"prune default", `addDuration(now, concat("PT-", coalesce(args.window, "30"), "M"))`, "2026-09-09T05:30:00Z"},
		{"days between", `daysBetween("2026-09-01", "2026-09-03")`, 2},
		{"nested date", `concat("cutoff=", addDuration(now, "-PT30M"))`, "cutoff=2026-09-09T05:30:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveArgValueRef(tc.expr, evaluator)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestFunctionInvalidDateArgumentFailsBeforeDispatch(t *testing.T) {
	evaluator := automations.NewEvaluator()
	for _, expression := range []any{
		`addDuration("invalid-date", "-PT30M")`,
		`concat("", addDuration("invalid-date", "-PT30M"))`,
		[]any{`addDuration("invalid-date", "-PT30M")`},
	} {
		t.Run(fmt.Sprintf("%v", expression), func(t *testing.T) {
			_, err := resolveArgValueRef(expression, evaluator)
			require.Error(t, err, "an invalid optional cutoff must not become nil or source text")
			result, err := (&FunctionExecutor{}).Execute(context.Background(), &automations.Step{
				ID: "stale", Function: &automations.FunctionStepConfig{Name: "staleClusterNodes", Args: map[string]any{"olderThan": expression}},
			}, &Context{Engine: &memql.MemQLEngine{}, Evaluator: evaluator})
			require.Error(t, err)
			require.Contains(t, err.Error(), "argument")
			require.Equal(t, "failed", result.Status)
		})
	}
}
