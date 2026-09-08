package memql

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// The embedder is a CLUSTER BINDING, and the vector width belongs to the
// provider (epic memql#5137, D6).
//
// WHAT WAS WRONG. The embedding model was the string literal "embedding3Small"
// in five files, and `node_vectors` was declared `vector(1536)` -- that model's
// width, written into the schema. Together those two facts meant the cluster
// could only ever embed with one paid OpenAI model: changing it required
// editing five files AND a migration, and doing either without the other
// produced a table full of vectors of the wrong width, which is not an error
// anywhere. It is a search space that quietly returns wrong neighbours.
//
// WHAT REPLACES IT. One row, `v1:platform:embedderBinding` at the literal id
// `active`, naming a providerRef and the width that provider produces. Vectors
// live in one table per width, `node_vectors_<dims>`, created when a binding of
// a new width is activated. Switching the binding opens the `reembedLibrary`
// work goal, which fills the new table and flips `active` only when the count
// matches -- so a switch part-way through still serves the OLD space, because a
// half-written vector column is a corrupt search space rather than a degraded
// one.
//
// WHY A LITERAL ID. The same reason v1:cluster:database is at `primary`
// (memql#4766): a re-write is then a new VERSION of one logical row rather than
// a second row, and "which binding is active" has exactly one answer to read.

// unnamedFallbackProvider is the journal key for a structured call that ran on
// whatever the registry's unnamed scan picked, rather than on a provider the
// prompt named.
//
// It is deliberately not a provider name and deliberately not empty. The
// journal hashes the provider so a replay cannot serve one provider's answer
// for another's; an empty string would collide with "no provider recorded",
// and a real name would claim a choice nobody made.
const unnamedFallbackProvider = "(unnamed-fallback)"

// EmbedderBindingConceptID is the concept the active binding lives on.
const EmbedderBindingConceptID = "v1:platform:embedderBinding"

// ActiveEmbedderBindingID is the literal row id of the active binding.
const ActiveEmbedderBindingID = "active"

// EmbedderBinding is the cluster's active embedding configuration.
type EmbedderBinding struct {
	// ProviderRef is a provider record name (embedding3Small) or a fleet
	// reference (fleet:qwen3-embedding:0.6b). It is resolved through the same
	// registry path every other provider name takes, so a fleet embedder is
	// resolved against the ACTING USER's machines like any other fleet call.
	ProviderRef string
	// Dimensions is the vector width this provider produces, and therefore
	// which node_vectors_<dims> table its vectors live in. Never zero on a
	// valid binding: a width of zero cannot have a table and cannot have an
	// index, and pgvector refuses both rather than accepting them silently.
	Dimensions int
	// ActivatedAt is when this binding became active, RFC3339.
	ActivatedAt string
	// ReembedRunId names the work run that filled this binding's table, when
	// the binding replaced another. Empty for the first binding a cluster ever
	// activates, which has nothing to re-embed FROM.
	ReembedRunId string
}

// Valid reports whether the binding can actually be used. A binding with no
// provider or no width is a row somebody started writing, and using it would
// create node_vectors_0.
func (b EmbedderBinding) Valid() bool {
	return strings.TrimSpace(b.ProviderRef) != "" && b.Dimensions > 0
}

// VectorTableFor names the table holding vectors of a given width.
//
// ONE TABLE PER WIDTH rather than one table with an untyped column, because a
// pgvector index needs fixed dimensions: an untyped column can be written and
// cannot be searched quickly, which is the failure that looks like the feature
// working until the corpus grows.
func VectorTableFor(dims int) string {
	return fmt.Sprintf("node_vectors_%d", dims)
}

// embedderBindingCache holds the last binding read, so the hot embedding path
// does not re-read a row that changes at most a handful of times in a cluster's
// life. It is invalidated by the same write that activates a new binding.
type embedderBindingCache struct {
	mu     sync.RWMutex
	loaded bool
	value  EmbedderBinding
}

var activeBinding embedderBindingCache

// SetActiveEmbedderBinding records the binding the engine should use. Called by
// the activation path once a new binding's table is filled and its count
// matches, and by boot once the row has been read.
func SetActiveEmbedderBinding(b EmbedderBinding) {
	activeBinding.mu.Lock()
	defer activeBinding.mu.Unlock()
	activeBinding.value = b
	activeBinding.loaded = true
}

// ClearActiveEmbedderBinding forgets the cached binding. Used by tests and by
// the reload path; a cleared cache re-reads rather than serving a stale width.
func ClearActiveEmbedderBinding() {
	activeBinding.mu.Lock()
	defer activeBinding.mu.Unlock()
	activeBinding.value = EmbedderBinding{}
	activeBinding.loaded = false
}

// ActiveEmbedderBinding returns the cluster's active binding.
//
// THE SECOND RETURN IS "IS THERE ONE", NOT "DID IT WORK", and the difference
// matters at every call site. A cluster with no binding is the ordinary state
// of a fresh install that has not chosen an embedder yet -- it is not an error,
// and it must not be reported as one. What it IS is a cluster where nothing can
// be embedded, so every caller degrades honestly rather than falling back to a
// model nobody chose.
func ActiveEmbedderBinding() (EmbedderBinding, bool) {
	activeBinding.mu.RLock()
	defer activeBinding.mu.RUnlock()
	if !activeBinding.loaded || !activeBinding.value.Valid() {
		return EmbedderBinding{}, false
	}
	return activeBinding.value, true
}

// ErrNoEmbedderBound is what an embedding call gets when the cluster has no
// active binding.
//
// IT NAMES THE FIX, because the alternative reads as a bug. "embedding provider
// unavailable" sends an operator looking at credentials; what is actually true
// is that nobody has chosen an embedder, and the place to choose one is a
// screen they have probably not opened.
var ErrNoEmbedderBound = fmt.Errorf(
	"no embedder is bound: this cluster has no active %s row, so nothing can be embedded. "+
		"Bind one from Fleet -> Models, or pull a catalog embeddings model onto a machine "+
		"(qwen3-embedding:0.6b is the 16 GB default). There is deliberately no fallback: an "+
		"embedder chosen for you would write vectors into a search space you did not pick",
	EmbedderBindingConceptID,
)

// ResolveEmbedderProvider returns the provider name the active binding names.
//
// This is the ONE narrowing from "the cluster's embedder" to "a provider name",
// and every embedding site goes through it -- integrations/embedding, knowledge,
// similarity, harnessRecall and the semantic cache. Before this epic each of
// those carried its own `defaultProvider = "embedding3Small"` constant, which
// is five copies of one decision that could drift, and did not drift only
// because nobody had ever changed it.
func ResolveEmbedderProvider(_ context.Context) (string, error) {
	b, ok := ActiveEmbedderBinding()
	if !ok {
		return "", ErrNoEmbedderBound
	}
	return b.ProviderRef, nil
}

// EmbedderDimensions returns the active binding's vector width, or 0 and false
// when nothing is bound. Callers that need a table name use VectorTableFor on
// the result rather than assuming 1536.
func EmbedderDimensions() (int, bool) {
	b, ok := ActiveEmbedderBinding()
	if !ok {
		return 0, false
	}
	return b.Dimensions, true
}

// catalogWidths holds the vector width the CATALOG records per model id, keyed
// by the runtime's own model id exactly as a machine advertises it.
//
// IT IS A CACHE OF SEEDED DSL DATA, not a second source of truth. The rows live
// in `v1:models:modelProfile` and are re-materialized on every boot; this map is
// filled from them once the engine has loaded, because `fleetProvider.Dimensions`
// is on the embedding hot path and cannot run a query per call.
//
// A model absent from it reports 0, which callers read as "unknown". That is the
// right answer for an operator's own pull that the catalog has never heard of,
// and it is why a binding to such a model is refused rather than guessed: the
// binding's whole job is to know the width before the first vector exists.
var catalogWidths struct {
	mu     sync.RWMutex
	byID   map[string]int
	loaded bool
}

// SetCatalogWidths publishes the catalog's vector widths. Called once the
// modelProfile rows are readable; safe to call again after a re-seed.
func SetCatalogWidths(widths map[string]int) {
	catalogWidths.mu.Lock()
	defer catalogWidths.mu.Unlock()
	next := make(map[string]int, len(widths))
	for id, dims := range widths {
		if id = strings.TrimSpace(id); id != "" && dims > 0 {
			next[id] = dims
		}
	}
	catalogWidths.byID = next
	catalogWidths.loaded = true
}

// catalogDimensionsFor answers the width the catalog records for one model id.
//
// EXACT EQUALITY, never a prefix or a fuzzy match. The model id is byte-identical
// from the cockpit's label to the catalog row to a policy naming
// `fleet:<modelId>` precisely so this comparison can be a string equality --
// `qwen3-embedding:0.6b` and `qwen3-embedding:0.6b-q8` are different models with
// potentially different widths, and matching them to each other would bind a
// table at the wrong size.
func catalogDimensionsFor(modelID string) (int, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return 0, false
	}
	catalogWidths.mu.RLock()
	defer catalogWidths.mu.RUnlock()
	if !catalogWidths.loaded {
		return 0, false
	}
	dims, ok := catalogWidths.byID[modelID]
	return dims, ok && dims > 0
}
