package memql

import (
	"context"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/auth"
)

// conceptModelsModelProfile is the canonical concept id of the curated model
// catalog. Mirrors the concept declared at dsl/models/concepts.memql:modelProfile.
const conceptModelsModelProfile = "v1:models:modelProfile"

// validateModelProfileCuratedImmutable is the write-side guard for CURATED
// catalog entries (epic memql#5137, D1).
//
// WHY A REFUSAL AND NOT A SUCCESSFUL WRITE. dsl/models/seeds.memql is
// re-materialized by the SeedMaterializer on every boot. Without this guard,
// `modelProfileRemove` against a curated row succeeds, the entry disappears
// from the Fleet page, and the next pod restart brings it back -- with nothing
// anywhere saying why. An operator watching their edit undo itself at a
// restart they did not cause has no thread to pull; a refusal naming the reason
// is the only honest answer, and the way to retire a curated entry is a release.
//
// Same shape as validateRbacBaseRoleImmutable and
// validateAgentRolePredefinedLock, deliberately: a whole-row lock with a
// system-actor carve-out so the SeedMaterializer's idempotent re-seed is
// unaffected. It keys on `curated` where those key on `predefined`, because a
// catalog entry's opposite of curated is "an operator added it", not "somebody
// defined it".
//
// The PRIOR-row flag is what closes the obvious bypass. executeWrite
// read-merges the delta on top of the persisted row before this runs, so a
// caller who sets `curated: false` in the same write still fails: the row WAS
// curated, and that is the value this guard reads.
func (e *MemQLEngine) validateModelProfileCuratedImmutable(ctx context.Context, payload map[string]any, priorCurated bool, actor string) error {
	if payload == nil {
		return nil
	}

	merged := boolFromAny(payload["curated"])
	if !merged && !priorCurated {
		// An operator's own entry. Theirs to edit and theirs to remove.
		return nil
	}

	identity, _ := auth.UserIdentityFromContext(ctx)
	if isSystemActor(identity, actor) {
		return nil
	}

	modelID := strings.TrimSpace(stringFromAny(payload["modelId"]))
	return fmt.Errorf(
		"v1:models:modelProfile: catalog entry %q is curated and cannot be changed at runtime -- curated entries are authored in "+
			"dsl/models/seeds.memql and re-materialized by the SeedMaterializer on every boot, so a runtime edit would be undone "+
			"by the next restart with nothing to explain it. modelProfileAdd and modelProfileRemove operate on operator entries "+
			"(curated=false); retiring a curated entry is a release.",
		modelID,
	)
}
