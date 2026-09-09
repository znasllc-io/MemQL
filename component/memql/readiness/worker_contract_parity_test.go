package readiness

import (
	"os"
	"regexp"
	"testing"
)

// The two attribute keys meetsFloor reads out of a `model:<id>` label value,
// pinned to the one place that WRITES and parses them.
//
// The shared fleet catalog depends on the engine, which imports readiness,
// so this gate reads the FILE rather than introducing an import cycle --
// the arrangement os_parity_test.go beside it uses for the shell's fold, and
// component/worker/online_client_parity_test.go uses for the Fleet page. It
// fails naming the file a reader has to go and open.
const modelRoutingPath = "../../worker/fleetcatalog/model.go"

// The declaration this gate reads. `attrContext = "ctx"` inside a const block,
// which is how the shared model parser spells it; the NAME and the LITERAL are what
// this is about, so the pattern is deliberately loose about the whitespace and
// strict about nothing else.
var attrKeyPattern = regexp.MustCompile(`(?m)^\s*attr(Context|Structured)\s*=\s*"([^"]*)"`)

func TestModelAttributeKeysMatchTheRouter(t *testing.T) {
	raw, err := os.ReadFile(modelRoutingPath)
	if err != nil {
		t.Fatalf("the router's attribute keys are unreadable at %s: %v", modelRoutingPath, err)
	}
	found := map[string]string{}
	for _, m := range attrKeyPattern.FindAllStringSubmatch(string(raw), -1) {
		found[m[1]] = m[2]
	}
	// A REACHABLE POSITIVE. Without it an unparseable file makes the assertion
	// below vacuous and this gate reports success over nothing -- which is the
	// exact failure a regexp-based gate is most prone to.
	if len(found) != 2 {
		t.Fatalf("%s no longer declares attrContext and attrStructured as plain string literals "+
			"(found %v). This gate reads them by regexp rather than by import, because that file is "+
			"part of the engine-dependent catalog, so the shape is load-bearing.",
			modelRoutingPath, found)
	}
	if found["Context"] != attrContextKey || found["Structured"] != attrStructuredKey {
		t.Fatalf("the router advertises %q/%q and inference.go's meetsFloor reads %q/%q.\n"+
			"A machine advertising a qualifying model would then be invisible to the readiness feed, "+
			"so MemQL OS would hold its owner on the core gate of a cluster that has inference -- "+
			"with no error anywhere.",
			found["Context"], found["Structured"], attrContextKey, attrStructuredKey)
	}
}
