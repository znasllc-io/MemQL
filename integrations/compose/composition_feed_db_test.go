package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/znasllc-io/memql/component/auth"
)

// Materializer opens records from its retained compositions feed. A seed
// containing only cards makes a reload erase source and model provenance.
func TestCompositionFeedDB_PreservesOpenRecordProvenance(t *testing.T) {
	e := materializeDBEngine(t)
	s := &store{engine: e}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ctx := auth.ContextWithUserActor(context.Background(), "compose-feed-owner-"+suffix)
	id := "compose-feed-" + suffix
	if err := s.createComposition(ctx, map[string]any{
		"compositionId": id, "name": "Inventory", "format": "csv",
		"sources": []map[string]any{{"kind": "library_file", "ref": "input-file", "label": "Inventory source", "capturedAt": "2026-09-09T16:00:00Z"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.updateCompositionState(ctx, map[string]any{
		"compositionId": id, "status": "ready", "outputFileId": "output-file",
		"modelsUsed": []map[string]any{{"provider": "fleet", "model": "local-inventory", "calls": 1, "tokens": 17}},
		"sha256":     strings.Repeat("a", 64),
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.query(ctx, "query compositions()")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("composition feed rows = %d; want one owner record", len(rows))
	}
	encoded, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"sources":[`, `"label":"Inventory source"`, `"modelsUsed":[`, `"model":"local-inventory"`, `"calls":1`, `"tokens":17`, `"sha256":"` + strings.Repeat("a", 64) + `"`} {
		if !strings.Contains(string(encoded), required) {
			t.Errorf("reloaded open record is missing %s: %s", required, encoded)
		}
	}
}
