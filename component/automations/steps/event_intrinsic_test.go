package steps

import (
	"reflect"
	"testing"

	"github.com/znasllc-io/memql/component/automations"
)

func TestNestedBuiltinReadsTheEventObject(t *testing.T) {
	input := map[string]any{"filename": "fork.md", "region": "EMEA"}
	evaluator := automations.NewEvaluator()
	evaluator.SetCustom("event", map[string]any{"payload": input})
	for _, tc := range []struct {
		expression string
		want       any
	}{
		{`field(event, "payload")`, input},
		{`field("event", "payload")`, nil},
	} {
		got, err := (&MutationExecutor{}).evaluateValue(evaluator, tc.expression)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s = %#v, %v; want %#v", tc.expression, got, err, tc.want)
		}
	}
}
