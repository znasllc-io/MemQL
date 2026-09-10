package steps

import (
	"testing"

	"github.com/znasllc-io/memql/component/automations"
	langparser "github.com/znasllc-io/memql/component/language/parser"
)

func TestExecutorStringLiteralsUseTheDSLLexer(t *testing.T) {
	eval := automations.NewEvaluator()
	mutation := &MutationExecutor{}
	for _, tc := range []struct{ name, source, want string }{
		{"escaped statement", langparser.QuoteString("Write \"Marvel\".\nKeep C:\\notes\\hero.txt and literal \\n.\tEnd."), "Write \"Marvel\".\nKeep C:\\notes\\hero.txt and literal \\n.\tEnd."},
		{"raw multiline DSL literal", "\"first\nsecond\"", "first\nsecond"},
		{"unicode escape", `"\uD83D\uDE80"`, "🚀"},
		{"literal method spelling", `"keep agent.first() unchanged"`, "keep agent.first() unchanged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, evaluate := range map[string]func() (any, error){
				"function concat":  func() (any, error) { return evaluateArgConcat(eval, "concat("+tc.source+")") },
				"mutation concat":  func() (any, error) { return mutation.evaluateConcat(eval, "concat("+tc.source+")") },
				"mutation literal": func() (any, error) { return mutation.evaluateValue(eval, tc.source) },
			} {
				t.Run(name, func(t *testing.T) {
					got, err := evaluate()
					if err != nil || got != tc.want {
						t.Fatalf("literal changed in executor: got=%q want=%q err=%v", got, tc.want, err)
					}
				})
			}
		})
	}
}

func TestExecutorStringLiteralsRefuseInvalidDSL(t *testing.T) {
	for _, source := range []string{`"\x41"`, `"\uD800"`, `"one" "two"`} {
		if _, err := evaluateArgConcat(automations.NewEvaluator(), "concat("+source+")"); err == nil {
			t.Fatalf("invalid DSL string was accepted: %s", source)
		}
	}
}
