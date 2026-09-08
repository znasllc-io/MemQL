package memql

import (
	"context"

	"github.com/znasllc-io/memql/core/airoute"
)

// ai_test_resolver_test.go -- the router stand-in these tests resolve through.
//
// It lives in a _test.go file DELIBERATELY. What it does -- take the pinned
// name, or the registry default, and hand back that entry -- is precisely the
// resolution path epic memql#5127 deletes from production, and a helper of this
// shape in a non-test file is one a later caller would find and use. Here it
// cannot be: the package's production code does not compile against it.
//
// It is not a mock of the router's DECISIONS either. A test that needed a rule
// to fire belongs in component/router, where the rules are; these tests are
// about the runtime's caching, its journal seam and its event emission, and
// what they need from a resolver is only that it returns the provider they
// registered.
func testRegistryResolver(providers *ProviderRegistry) func(context.Context, airoute.ResolveRequest) (ResolvedProvider, error) {
	return func(ctx context.Context, req airoute.ResolveRequest) (ResolvedProvider, error) {
		name := req.ExplicitProvider
		if name == "" {
			name = providers.Default()
		}
		entry, ok := providers.EntryForContext(ctx, name)
		if !ok || entry == nil || !entry.Available || entry.Client == nil {
			// A FLEET name that cannot be served yields the TYPED refusal, the
			// way the real chain walk does. It matters here rather than being
			// mock detail: the property these tests assert is that a structured
			// call whose default names a local model PARKS instead of falling
			// through to a paid vendor, and a stand-in that answered with a
			// generic error would let the test pass while saying nothing about
			// it.
			if modelId, isFleet := IsFleetReference(name); isFleet {
				return ResolvedProvider{}, providers.FleetRefusal(ctx, actingUserFromContext(ctx), modelId)
			}
			return ResolvedProvider{}, ErrProviderUnavailable(name)
		}
		return ResolvedProvider{
			Client: entry.Client,
			Entry:  entry,
			Resolution: airoute.Resolution{
				ProviderName: entry.Config.Name,
				Model:        entry.Config.Model,
				Decision: airoute.Decision{
					Level:       req.Level,
					ServedLevel: req.Level,
					Outcome:     airoute.OutcomeOK,
				},
			},
		}, nil
	}
}

// newTestAIRuntime is newAIRuntime with the seam wired, which every test that
// invokes a prompt needs and none of them should have to remember.
func newTestAIRuntime(prompts *PromptRegistry, providers *ProviderRegistry, cfg aiCacheConfig) *aiRuntime {
	r := newAIRuntime(nil, prompts, providers, cfg)
	if r != nil {
		r.resolve = testRegistryResolver(providers)
	}
	return r
}

// testAIResolver adapts the stand-in to the engine's AIResolver seam, for the
// tests that go through InvokeAIStructured rather than through the runtime.
type testAIResolver struct {
	fn func(context.Context, airoute.ResolveRequest) (ResolvedProvider, error)
}

func (t testAIResolver) ResolveFor(ctx context.Context, req airoute.ResolveRequest) (ResolvedProvider, error) {
	return t.fn(ctx, req)
}

// wireTestResolver installs the stand-in on an engine.
func wireTestResolver(e *MemQLEngine, providers *ProviderRegistry) {
	e.SetAIResolver(testAIResolver{fn: testRegistryResolver(providers)})
}
