package agent

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/core/common"
)

func TestWorkExecutionCreatesFilesWithoutRedelegating(t *testing.T) {
	ctx := auth.ContextWithAccess(context.Background(), &auth.AccessContext{UserId: "owner"})
	ctx = common.ContextWithRun(ctx, common.RunContext{RunId: "run", GoalId: "goal", OwnerUserId: "owner"})
	r := newTestReplier(&registryEngine{registered: map[string]bool{"composeFile": true}})
	data := map[string]any{"assistant": map[string]any{}, "productionDirective": "delegate everything"}
	names := r.scopeWorkExecution(ctx, data, []string{"produceArtifact", "canvasPublish", "recall"})
	if !slices.Contains(names, "composeFile") || slices.Contains(names, "produceArtifact") || slices.Contains(names, "canvasPublish") || !slices.Contains(names, "recall") {
		t.Fatalf("executor tool surface = %v", names)
	}
	if !strings.Contains(data["productionDirective"].(string), "outputFileId") {
		t.Fatalf("missing instruction to finish the actual file: %v", data)
	}
	if refusal := guardProduceArtifactRedelegation("produceArtifact", turnContext{IsWorkExecution: true}); !strings.Contains(refusal, "composeFile") {
		t.Fatalf("executor can open another goal: %q", refusal)
	}
}

func TestWorkExecutionRequiresThePersistedOwnerContext(t *testing.T) {
	r := newTestReplier(&registryEngine{registered: map[string]bool{"composeFile": true}})
	for _, ctx := range []context.Context{
		context.Background(),
		common.ContextWithRun(auth.ContextWithAccess(context.Background(), &auth.AccessContext{UserId: "stranger"}), common.RunContext{RunId: "run", GoalId: "goal", OwnerUserId: "owner"}),
	} {
		data := map[string]any{}
		names := r.scopeWorkExecution(ctx, data, []string{"produceArtifact"})
		if !slices.Equal(names, []string{"produceArtifact"}) || len(data) != 0 {
			t.Fatalf("ordinary or different-owner turn acquired executor tools: %v %v", names, data)
		}
	}
}
