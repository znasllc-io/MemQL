package automations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/component/work"
	"github.com/znasllc-io/memql/core/airoute"
)

// countingClassifier is the whole point of the SymptomClassifier interface:
// the design's headline claim is about how many provider calls a failure
// costs, and a claim about a COUNT is only checkable if the seam can be
// counted. A classifier reached through the engine directly could not be.
type countingClassifier struct {
	calls   int
	symptom work.Symptom
	err     error
}

func (c *countingClassifier) Level() airoute.Level { return airoute.LevelFast }

func (c *countingClassifier) Classify(context.Context, ClassifySymptomInput) (work.Symptom, work.Evidence, error) {
	c.calls++
	if c.err != nil {
		return work.SymptomNone, work.Evidence{}, c.err
	}
	return c.symptom, work.Evidence{
		Tier:   "classified",
		Reason: "the model read the trace",
		Source: work.EvidenceSourceModel,
	}, nil
}

// failedRun builds an execution that failed at one step, the way the executor
// leaves it: the message on the run, and the step facts recorded at the point
// of failure.
func failedRun(id, message string, attempt, retries int) *AutomationExecution {
	run := &AutomationExecution{ID: id}
	run.RecordFailedStep(&Step{ID: "run", Type: StepType("function"), RetryCount: retries}, attempt)
	run.Fail(errors.New(message))
	return run
}

func closeWith(t *testing.T, c SymptomClassifier, run *AutomationExecution) []string {
	t.Helper()
	exec := &recordingJournalExecutor{}
	j := newWorkJournal(exec, nil)
	j.classifier = c
	j.closeRun(context.Background(), run, "")
	return exec.calls
}

// TestATransientFailureCostsZeroProviderCalls is the design's headline
// property (work-spine record section E, epic memql#5127 D12): the
// deterministic table runs FIRST, and a failure it recognises never reaches a
// model at all.
//
// Its negative control is the test below: a novel error must reach the
// classifier exactly once. Without that pair, a classifier that was never
// invoked on ANY path would satisfy this test forever.
func TestATransientFailureCostsZeroProviderCalls(t *testing.T) {
	c := &countingClassifier{symptom: work.SymptomHuman}
	calls := closeWith(t, c, failedRun("v1:work:run:t1", "dial tcp: connection refused", 1, 3))

	if c.calls != 0 {
		t.Fatalf("the classifier was called %d time(s) for a failure the rules table recognises; "+
			"a rules-classified symptom must cost zero provider calls", c.calls)
	}
	name, args := argsOf(t, lastCallNamed(t, calls, "updateWorkRun"))
	if name != "updateWorkRun" || args["status"] != "waiting" {
		t.Fatalf("status = %v, want the run to wait on its retry", args["status"])
	}
	waiting, _ := args["waitingOn"].(map[string]any)
	if waiting["kind"] != WaitKindRetry {
		t.Fatalf("waitingOn.kind = %v, want %q", waiting["kind"], WaitKindRetry)
	}
	if waiting["resumeAt"] == nil || waiting["resumeAt"] == "" {
		t.Error("a retry wait with no resumeAt is never due, so the run would never be handed back")
	}
	if waiting["ruleId"] == nil || waiting["ruleId"] == "" {
		t.Error("the wait must name the rule that fired; without it nobody can tell a rules verdict from a model's")
	}
}

// TestANovelFailureCostsExactlyOneProviderCall is the negative control for the
// test above AND the claim in its own right. A counter that never rises on any
// path reads as zero forever.
func TestANovelFailureCostsExactlyOneProviderCall(t *testing.T) {
	c := &countingClassifier{symptom: work.SymptomPlan}
	calls := closeWith(t, c, failedRun("v1:work:run:t2", "the vendor said something nobody has a rule for", 1, 3))

	if c.calls != 1 {
		t.Fatalf("the classifier was called %d time(s); a failure the rules cannot classify costs exactly one", c.calls)
	}
	_, args := argsOf(t, lastCallNamed(t, calls, "updateWorkRun"))
	waiting, _ := args["waitingOn"].(map[string]any)
	if waiting["kind"] != WaitKindReplan {
		t.Fatalf("waitingOn.kind = %v, want %q -- a plan symptom re-plans the gap", waiting["kind"], WaitKindReplan)
	}
}

// TestAnUnclassifiableFailureStillFailsTheRun pins the direction that makes
// this wiring safe to land. A node with no classifier must behave exactly as
// the tree did before this epic; the tempting alternative -- calling an
// unclassified failure "ask a person" -- parks every ordinary failure on a
// question nobody was told about.
func TestAnUnclassifiableFailureStillFailsTheRun(t *testing.T) {
	calls := closeWith(t, nil, failedRun("v1:work:run:t3", "something nothing has a rule for", 1, 0))
	if len(calls) != 1 {
		t.Fatalf("want one close call on a node with no classifier, got %v", calls)
	}
	name, args := argsOf(t, calls[0])
	if name != "updateWorkRun" || args["status"] != "failed" {
		t.Fatalf("call = %q status = %v, want the run to fail exactly as it did before", name, args["status"])
	}
	if args["finishedAt"] == nil {
		t.Error("a failed run finishes")
	}
}

// TestAClassifierThatCannotAnswerIsNotAVerdict covers the two ways a wired
// classifier produces nothing usable. Both must leave the run failing rather
// than acting on a value nobody defined.
func TestAClassifierThatCannotAnswerIsNotAVerdict(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    *countingClassifier
	}{
		{"the call failed", &countingClassifier{err: errors.New("provider down")}},
		{"the answer was outside the enum", &countingClassifier{symptom: work.Symptom("probably fine")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := closeWith(t, tc.c, failedRun("v1:work:run:t4", "novel", 1, 0))
			if tc.c.calls != 1 {
				t.Fatalf("classifier calls = %d, want 1", tc.c.calls)
			}
			_, args := argsOf(t, calls[len(calls)-1])
			if args["status"] != "failed" {
				t.Fatalf("status = %v, want failed", args["status"])
			}
		})
	}
}

// TestARepeatedActionEscalatesRatherThanRetryingForever pins the ORDER inside
// the rules table through this path. The message here also matches a transient
// matcher; the stall rule sits above them all, so a step that spent its whole
// retry budget on the same failure asks a person instead of asking for another
// retry it has already proved does not help.
func TestARepeatedActionEscalatesRatherThanRetryingForever(t *testing.T) {
	c := &countingClassifier{symptom: work.SymptomTransient}
	calls := closeWith(t, c, failedRun("v1:work:run:t5", "connection refused", 4, 3))

	if c.calls != 0 {
		t.Fatalf("the classifier was called %d time(s); the stall rule classifies this", c.calls)
	}
	if !anyCallNamed(calls, "createWorkApproval") {
		t.Fatalf("a stalled run must raise an approval, got %v", calls)
	}
	_, args := argsOf(t, lastCallNamed(t, calls, "createWorkApproval"))
	if args["kind"] != work.ApprovalKindFeedback {
		t.Fatalf("approval kind = %v, want %q", args["kind"], work.ApprovalKindFeedback)
	}
	_, runArgs := argsOf(t, lastCallNamed(t, calls, "updateWorkRun"))
	waiting, _ := runArgs["waitingOn"].(map[string]any)
	if waiting["kind"] != WaitKindApproval {
		t.Fatalf("waitingOn.kind = %v, want %q", waiting["kind"], WaitKindApproval)
	}
	// THE ORDER IS LOAD-BEARING: the approval is written before the run parks
	// on it. A run parked on an approval id that does not exist waits on
	// nothing, which no person can decide and no sweep can resolve.
	if indexOfCall(calls, "createWorkApproval") > indexOfCall(calls, "updateWorkRun") {
		t.Fatalf("the run parked before the approval existed: %v", calls)
	}
}

// TestAPreconditionMissIsClassifiedFromTheValueNotTheMessage pins that the
// precondition signal travels as a FACT recorded on the execution rather than
// as words in an error string. The environment.literal rule keys on the flag,
// so a run whose message says nothing about preconditions still classifies.
func TestAPreconditionMissIsClassifiedFromTheValueNotTheMessage(t *testing.T) {
	c := &countingClassifier{symptom: work.SymptomHuman}
	run := failedRun("v1:work:run:t6", "the check did not hold", 1, 0)
	run.PreconditionMissed = true

	calls := closeWith(t, c, run)
	if c.calls != 0 {
		t.Fatalf("the classifier was called %d time(s); a precondition miss is a rules verdict", c.calls)
	}
	_, args := argsOf(t, lastCallNamed(t, calls, "createWorkApproval"))
	// An environment symptom heals, and healing is never a silent edit: it
	// reaches a person as a planReview (work-spine design D5).
	if args["kind"] != work.ApprovalKindPlanReview {
		t.Fatalf("approval kind = %v, want %q", args["kind"], work.ApprovalKindPlanReview)
	}
}

// TestTheSymptomLandsOnTheStepRow pins that a classification is recorded where
// a person reads it, not only in a log line.
func TestTheSymptomLandsOnTheStepRow(t *testing.T) {
	c := &countingClassifier{symptom: work.SymptomPlan}
	calls := closeWith(t, c, failedRun("v1:work:run:t7", "novel", 1, 0))
	if !anyCallNamed(calls, "updateWorkStep") {
		t.Fatalf("no step row was written: %v", calls)
	}
	_, args := argsOf(t, lastCallNamed(t, calls, "updateWorkStep"))
	if args["symptom"] != string(work.SymptomPlan) {
		t.Fatalf("step symptom = %v, want %q", args["symptom"], work.SymptomPlan)
	}
	if args["stepId"] == nil || args["stepId"] == "" {
		t.Error("updateWorkStep takes a stepId; a write with none updates nothing and reports success")
	}
}

// TestAnInferenceRefusalStillParksAndIsNotClassified pins that the two failure
// paths stay in order. A shut door is not a symptom -- nothing about the work
// was wrong -- so it must reach parkOnInference without costing a classifier
// call.
func TestAnInferenceRefusalStillParksAndIsNotClassified(t *testing.T) {
	c := &countingClassifier{symptom: work.SymptomHuman}
	run := &AutomationExecution{ID: "v1:work:run:t8"}
	run.Fail(&stubDoorRefusal{code: work.RefusalEveryDoorShut})

	calls := closeWith(t, c, run)
	if c.calls != 0 {
		t.Fatalf("the classifier was called %d time(s) for a shut door; the door check runs first", c.calls)
	}
	_, args := argsOf(t, lastCallNamed(t, calls, "createWorkApproval"))
	if args["kind"] != work.ApprovalKindInferenceUnavailable {
		t.Fatalf("approval kind = %v, want %q", args["kind"], work.ApprovalKindInferenceUnavailable)
	}
}

// TestTheClassifierRunsAtTheCheapestLevel asserts the tier from OUTSIDE the
// implementation. It is on the interface because the cheapest tier is a
// property of the design -- the answer is one of five words and the acts it
// selects are all bounded -- not an implementation detail a later reader may
// quietly change.
func TestTheClassifierRunsAtTheCheapestLevel(t *testing.T) {
	c := &countingClassifier{}
	if got := c.Level(); got != airoute.LevelFast {
		t.Fatalf("classifier level = %q, want %q", got, airoute.LevelFast)
	}
}

func anyCallNamed(calls []string, name string) bool {
	return indexOfCall(calls, name) >= 0
}

func indexOfCall(calls []string, name string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, name+"(") {
			return i
		}
	}
	return -1
}

func lastCallNamed(t *testing.T, calls []string, name string) string {
	t.Helper()
	for i := len(calls) - 1; i >= 0; i-- {
		if strings.HasPrefix(calls[i], name+"(") {
			return calls[i]
		}
	}
	t.Fatalf("no %s call in %v", name, calls)
	return ""
}
