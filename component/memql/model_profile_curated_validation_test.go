package memql

import (
	"strings"
	"testing"
)

// Epic memql#5137, D1: the curated model-catalog guard.
//
// dsl/models/mutations.memql documents modelProfileRemove as refusing a curated
// entry, and a documented refusal with nothing enforcing it is worse than no
// refusal at all -- it reads as a promise on the page and behaves as a silent
// success that the next boot reverts. These cases are what make the promise
// real.
//
// The guard touches no engine field, so it runs through a nil receiver
// (DB-free), the same way TestAgentRolePredefinedLockGuard and
// TestRBACBaseRoleImmutableGuard do.
func TestModelProfileCuratedImmutableGuard(t *testing.T) {
	userCtx, userActor := userActorContext()
	sysCtx, sysActor := systemSeedContext()

	// curatedRow is a persisted seed row: what dsl/models/seeds.memql
	// materializes. Built fresh per subtest so a mutation in one cannot leak
	// into another.
	curatedRow := func() map[string]any {
		return map[string]any{
			"modelId":         "qwen3.5:9b",
			"category":        "text",
			"runtime":         "ollama",
			"minMachineClass": "16",
			"flags":           []any{"structured", "tools", "vision", "streaming"},
			"active":          true,
			"curated":         true,
		}
	}

	// operatorRow is what modelProfileAdd writes: the same shape with curated
	// stamped false. It is the row these mutations exist for.
	operatorRow := func() map[string]any {
		return map[string]any{
			"modelId":         "mistral-small:24b",
			"category":        "text",
			"runtime":         "ollama",
			"minMachineClass": "24",
			"flags":           []any{"tools", "streaming"},
			"active":          true,
			"curated":         false,
		}
	}

	// merged models what executeWrite hands the guard: the prior row with the
	// caller's delta merged on top. Built this way rather than hand-writing the
	// merged map, because the guard only ever sees merged payloads and a
	// hand-written one could omit a field the real merge always inherits --
	// which is exactly the field this guard reads.
	merged := func(prior, delta map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range prior {
			out[k] = v
		}
		for k, v := range delta {
			out[k] = v
		}
		return out
	}

	t.Run("removing a curated entry is rejected", func(t *testing.T) {
		prior := curatedRow()
		payload := merged(prior, map[string]any{"active": false})

		err := (*MemQLEngine)(nil).validateModelProfileCuratedImmutable(userCtx, payload, true, userActor)
		if err == nil {
			t.Fatal("removing a curated catalog entry must be refused: the SeedMaterializer re-materializes it on the next boot, so a success here is undone with nothing to explain it")
		}
		if !strings.Contains(err.Error(), "qwen3.5:9b") {
			t.Errorf("the refusal must name the entry it is about; got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "seeds.memql") {
			t.Errorf("the refusal must say where curated entries are authored, or it tells an operator no way forward; got %q", err.Error())
		}
	})

	t.Run("flipping curated to false in the same write does not bypass it", func(t *testing.T) {
		// The obvious bypass, and the reason the guard takes the PRIOR flag
		// rather than reading the merged payload alone.
		prior := curatedRow()
		payload := merged(prior, map[string]any{"curated": false, "active": false})

		if err := (*MemQLEngine)(nil).validateModelProfileCuratedImmutable(userCtx, payload, true, userActor); err == nil {
			t.Fatal("a delta setting curated=false on a row that WAS curated must still be refused")
		}
	})

	t.Run("an operator's own entry is theirs to remove", func(t *testing.T) {
		prior := operatorRow()
		payload := merged(prior, map[string]any{"active": false})

		if err := (*MemQLEngine)(nil).validateModelProfileCuratedImmutable(userCtx, payload, false, userActor); err != nil {
			t.Fatalf("an operator entry (curated=false) must be removable by its operator: %v", err)
		}
	})

	t.Run("the seed materializer re-seeds without being refused", func(t *testing.T) {
		// The carve-out that keeps boot working. Without it this guard would
		// refuse the SeedMaterializer's own idempotent re-write and every pod
		// would fail to start, which is a considerably louder bug than the one
		// the guard prevents -- but a bug found at boot rather than in review is
		// still one this case exists to prevent.
		prior := curatedRow()
		payload := merged(prior, map[string]any{"notes": "reworded"})

		if err := (*MemQLEngine)(nil).validateModelProfileCuratedImmutable(sysCtx, payload, true, sysActor); err != nil {
			t.Fatalf("a system actor must be able to re-materialize a curated entry: %v", err)
		}
	})

	t.Run("adding a new operator entry is not gated", func(t *testing.T) {
		// A create has no prior row, so priorCurated is false and the merged
		// payload carries curated=false from modelProfileAdd's stamp.
		payload := operatorRow()

		if err := (*MemQLEngine)(nil).validateModelProfileCuratedImmutable(userCtx, payload, false, userActor); err != nil {
			t.Fatalf("modelProfileAdd must not be gated by the curated guard: %v", err)
		}
	})

	t.Run("a caller cannot mint a curated entry", func(t *testing.T) {
		// The other direction, and the reason `curated` is not in
		// modelProfileAdd's accept{} block. If it ever were, this is what would
		// stop a caller minting a row that claims to be curated -- one the
		// SeedMaterializer does not know about and therefore never re-seeds,
		// while every surface treats it as release-time curation.
		payload := merged(operatorRow(), map[string]any{"curated": true})

		if err := (*MemQLEngine)(nil).validateModelProfileCuratedImmutable(userCtx, payload, false, userActor); err == nil {
			t.Fatal("a non-system actor writing curated=true must be refused")
		}
	})
}
