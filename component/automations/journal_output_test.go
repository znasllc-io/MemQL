package automations

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/memql"
)

func TestJournalPersistsFlatEngineOutputForResume(t *testing.T) {
	want := map[string]any{"reply": "Complete section content", "outputFileId": "saved-file"}
	rec := &recordingJournalExecutor{}
	j := newWorkJournal(rec, nil)
	j.stepFinished(context.Background(), &AutomationExecution{ID: "run"}, &Step{ID: "reason"}, &StepResult{
		StepId: "reason", Status: "success", Result: memql.NewResultWithOutput(want), CompletedAt: time.Now(),
	}, "chain")
	_, args := argsOf(t, rec.calls[0])
	loaded, err := runJournalFromRows(map[string]any{"id": "run"}, []map[string]any{{"key": "reason", "status": "done", "result": args["result"]}})
	if err != nil {
		t.Fatal(err)
	}
	got := minimalToStepResult(loaded.Steps["reason"])
	if got == nil || !reflect.DeepEqual(got.Result, want) {
		t.Fatalf("journal lost engine output before resume: got %+v, want %+v", got, want)
	}
}
