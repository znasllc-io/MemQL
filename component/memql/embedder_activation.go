package memql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Activating an embedder binding (epic memql#5137, D6).
//
// FOUR STEPS, IN THIS ORDER, AND THE ORDER IS THE WHOLE SAFETY PROPERTY:
//
//	1. create the vector table for the new width,
//	2. record the binding as a PLAN (no activatedAt -- it has not happened),
//	3. re-embed every row into the new table under the new binding,
//	4. compare the counts and only then flip `active`.
//
// Until step four, every read follows the binding that is still active and the
// table it names. So a switch interrupted at any point leaves a cluster that
// still works: the new table is partly filled and nothing reads it.
//
// The alternative -- re-embedding in place -- fails in a way nothing detects.
// Half the rows answer in the old model's geometry and half in the new one's,
// and cosine distance between them is a number with no meaning. It does not
// error. It ranks.

// EmbedderActivation is one attempt to make a provider the cluster's embedder.
type EmbedderActivation struct {
	// ProviderRef is the provider to bind: a record name, or fleet:<modelId>.
	ProviderRef string
	// DeclaredDimensions is the width from a provider RECORD, or 0 for a fleet
	// model whose width comes from the catalog.
	DeclaredDimensions int
	// ReembedRunId is the work run that will fill the new table. Empty when the
	// cluster has no vectors to carry over.
	ReembedRunId string
}

// ActivationPlan is what an activation decided before doing anything.
//
// SEPARATED FROM THE DOING so the decisions are testable without a database.
// Every property that matters here -- refusing an unknown width, noticing that
// nothing changed, knowing whether a re-embed is needed at all -- is a function
// of values, and a db-gated test for them would skip silently on every machine
// without Postgres.
type ActivationPlan struct {
	Binding EmbedderBinding
	// Previous is the binding being replaced, empty on a cluster's first.
	Previous EmbedderBinding
	// SameWidth is true when the new binding's vectors fit the table the old
	// one already uses.
	//
	// IT DOES NOT MEAN "SKIP THE RE-EMBED". Two embedders of the same width
	// produce vectors in DIFFERENT geometries -- bge-m3 and qwen3-embedding:0.6b
	// are both 1024 and share nothing else -- so the corpus still has to be
	// rebuilt. What it means is that the two bindings share a table NAME, which
	// is why the table is keyed by binding as well as width when they collide.
	SameWidth bool
	// NeedsReembed is false only when the cluster has no vectors at all.
	NeedsReembed bool
	// NoChange is true when the requested binding is already active. The
	// activation then does nothing rather than re-embedding the whole corpus
	// into a table it is already in.
	NoChange bool
}

// PlanActivation decides what an activation would do, without doing it.
func PlanActivation(req EmbedderActivation, current EmbedderBinding, vectorCount int64) (ActivationPlan, error) {
	next, err := BindingFor(req.ProviderRef, req.DeclaredDimensions)
	if err != nil {
		return ActivationPlan{}, err
	}
	next.ReembedRunId = req.ReembedRunId
	next.PreviousRef = current.ProviderRef

	plan := ActivationPlan{
		Binding:  next,
		Previous: current,
		// A width comparison, not an identity one: same width means same table
		// name, which is a fact about storage rather than about meaning.
		SameWidth: current.Valid() && current.Dimensions == next.Dimensions,
		// Nothing to carry over is not a shortcut -- it is the honest state of a
		// cluster that has embedded nothing, and making it wait for a re-embed
		// that has no rows to move would leave the first binding permanently in
		// `plan`.
		NeedsReembed: vectorCount > 0,
		NoChange:     current.Valid() && strings.TrimSpace(current.ProviderRef) == strings.TrimSpace(next.ProviderRef),
	}
	return plan, nil
}

// EmbedderActivator runs an activation against a database.
type EmbedderActivator struct {
	DB *sql.DB
	// Now is injectable so a test can assert the activation timestamp without
	// racing a clock.
	Now func() time.Time
}

// Prepare performs steps one and two: create the new width's table, and record
// the binding as a plan.
//
// IT DOES NOT FLIP `active`, and calling it twice is harmless -- the DDL is
// idempotent and the plan row is keyed by binding id. That matters because a
// node restarting mid-activation re-runs this.
func (a *EmbedderActivator) Prepare(ctx context.Context, plan ActivationPlan) error {
	if a == nil || a.DB == nil {
		return fmt.Errorf("activate embedder: no database")
	}
	if !plan.Binding.Valid() {
		return fmt.Errorf("activate embedder: refusing to prepare an incomplete binding")
	}
	return EnsureVectorTable(ctx, a.DB, plan.Binding.Dimensions)
}

// Complete performs step four: compare the counts, and flip only on a match.
//
// THE COMPARISON IS AGAINST THE OLD TABLE'S COUNT, read at the same moment as
// the new one's, rather than against a target computed when the run started.
// A target captured earlier goes stale the moment anything is embedded during
// the re-embed, and the flip would then happen with the corpus short by
// however many rows arrived in between.
//
// Returns whether it flipped. Not flipping is an ordinary outcome -- the run is
// still filling the table -- so it is a bool rather than an error.
func (a *EmbedderActivator) Complete(ctx context.Context, plan ActivationPlan) (bool, error) {
	if a == nil || a.DB == nil {
		return false, fmt.Errorf("complete embedder activation: no database")
	}
	if !plan.NeedsReembed {
		// A cluster with no vectors has nothing to wait for.
		return true, nil
	}
	oldCount, err := CountVectors(ctx, a.DB, plan.Previous.Dimensions)
	if err != nil {
		return false, err
	}
	newCount, err := CountVectors(ctx, a.DB, plan.Binding.Dimensions)
	if err != nil {
		return false, err
	}
	return ReembedComplete(oldCount, newCount), nil
}

// ActivatedAt is the timestamp a completed activation stamps.
func (a *EmbedderActivator) ActivatedAt() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now().UTC()
}
