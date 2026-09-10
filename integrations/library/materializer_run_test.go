package library

import (
	"context"
	"testing"
)

func TestFileArtifactRestampPreservesProducingRun(t *testing.T) {
	eng := newStubEngine()
	i := NewIntegration(eng)
	ref := "v1:library:file:materialized"
	file := map[string]any{"ownerUserId": "user-a", "name": "Report", "source": "agent_generated", "format": "markdown", "producedByRunId": "v1:work:run:materialize"}
	if err := i.writeFileArtifact(context.Background(), ref, file, artifactCarryForward{}); err != nil {
		t.Fatal(err)
	}
	row := eng.artifacts[stubArtifactId(ref)]
	if row["producedByRunId"] != "v1:work:run:materialize" {
		t.Fatalf("analysis disconnected output from Nexus run: %v", row)
	}
}
