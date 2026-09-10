package work

import (
	"testing"
)

// Intake happens on a BFF. Naming that node as the compiler made a pending
// run look like a lost planner even though no planner ever took ownership.
func TestCompilingRunDoesNotNameItsIntakeNode(t *testing.T) {
	t.Setenv("MEMQL_NODE_ID", "bff-intake")
	i, eng := newTestIntegration(t)
	if _, err := i.handleCreateGoal(callerContext("u-alice"), map[string]any{"statement": "summarize the invoices"}, 0); err != nil {
		t.Fatal(err)
	}
	if got := eng.callTo(t, "createWorkRun").Args(t)["nodeId"]; got != "" && got != nil {
		t.Fatalf("unclaimed compile names intake node %v as its worker", got)
	}
}
