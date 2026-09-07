package planner

import (
	"context"
	"strings"
	"testing"
)

type stubExecutor struct{ name string }

func (s stubExecutor) Backend() string { return s.name }
func (s stubExecutor) Run(context.Context, ExecutorRequest, ProgressCallback) (ExecutorResult, error) {
	return ExecutorResult{}, nil
}

// withRegistered installs a backend for the duration of a test and
// removes it afterwards, so the package-level registry does not leak
// between cases.
func withRegistered(t *testing.T, name string) {
	t.Helper()
	RegisterContainerExecutor(name, stubExecutor{name: name})
	t.Cleanup(func() {
		defaultExecutorRegistry.mu.Lock()
		delete(defaultExecutorRegistry.backends, name)
		defaultExecutorRegistry.mu.Unlock()
	})
}

// TestValidateExecutorBackendRefusesUnregistered is memql#4361's
// creation-time gate. Before it, a Task could name any backend at
// all: the registry is queried only at DISPATCH, so a typo produced a
// Task that looked queued, sat there, and failed much later with an
// error naming a lookup rather than the typo.
func TestValidateExecutorBackendRefusesUnregistered(t *testing.T) {
	// An empty name is valid -- it means "the workspace default".
	if err := ValidateExecutorBackend(""); err != nil {
		t.Fatalf("empty backend must be allowed: %v", err)
	}

	err := ValidateExecutorBackend("cockpit-app:claude-code")
	if err == nil {
		t.Fatal("an unregistered backend must be refused at task creation")
	}
	// With nothing registered, the message must say so: this seam
	// spent its whole life empty (memql#4120), and "no container
	// executor is registered in this binary" is a far more useful
	// thing to read than a list of one.
	if !strings.Contains(err.Error(), "no container executor is registered") {
		t.Fatalf("an empty registry must say so, got %v", err)
	}

	withRegistered(t, "cockpit-app")
	if err := ValidateExecutorBackend("cockpit-app:claude-code"); err != nil {
		t.Fatalf("a registered backend with an app suffix must validate: %v", err)
	}
	if err := ValidateExecutorBackend("cockpit-app"); err != nil {
		t.Fatalf("a registered backend with no suffix must validate: %v", err)
	}

	err = ValidateExecutorBackend("nemoclaw")
	if err == nil {
		t.Fatal("a backend nobody registered must still be refused")
	}
	if !strings.Contains(err.Error(), "cockpit-app") {
		t.Fatalf("the refusal must name what IS registered, got %v", err)
	}
}

// TestLookupResolvesTheBaseName: a Task naming cockpit-app:codex must
// reach the one registered cockpit-app backend, which reads the app
// off the suffix. Registering per-app would mean a release every time
// the app list grew.
func TestLookupResolvesTheBaseName(t *testing.T) {
	withRegistered(t, "cockpit-app")
	exec, err := LookupContainerExecutor("cockpit-app:codex")
	if err != nil {
		t.Fatalf("LookupContainerExecutor: %v", err)
	}
	if exec.Backend() != "cockpit-app" {
		t.Fatalf("resolved to %q, want cockpit-app", exec.Backend())
	}
	if got := BackendArg("cockpit-app:codex"); got != "codex" {
		t.Fatalf("BackendArg = %q, want codex", got)
	}
	if got := BackendArg("cockpit-app"); got != "" {
		t.Fatalf("BackendArg with no suffix = %q, want empty", got)
	}
}

// TestSplitSpendKeepsUnbilledTokensOffTheDollarCeiling is memql#4362's
// accounting rule, extended to local models by memql#4681. The two caps
// want opposite answers about tokens MemQL was not billed for, so they
// are counted in different places.
func TestSplitSpendKeepsUnbilledTokensOffTheDollarCeiling(t *testing.T) {
	got := SplitSpend(ExecutorResult{TokensSpent: 100, Billing: BillingSubscription})
	if (got != Spend{Subscription: 100}) {
		t.Fatalf("subscription spend = %+v, want it entirely on the subscription counter", got)
	}

	got = SplitSpend(ExecutorResult{TokensSpent: 100, Billing: BillingMetered})
	if (got != Spend{Metered: 100}) {
		t.Fatalf("metered spend = %+v, want it entirely on the metered counter", got)
	}

	// A local model runs on hardware the user already owns. The tokens are
	// real; the bill is not.
	got = SplitSpend(ExecutorResult{TokensSpent: 100, Billing: BillingLocal})
	if (got != Spend{Local: 100}) {
		t.Fatalf("local spend = %+v, want it entirely on the local counter -- charging it to a "+
			"dollar budget would mean the more someone used their own machine, the sooner "+
			"their plans stopped", got)
	}

	// Unknown is not metered for the ceiling -- MemQL was not billed -- and
	// it must NOT land on the local counter either, which would claim the
	// work ran on the user's hardware.
	got = SplitSpend(ExecutorResult{TokensSpent: 100, Billing: BillingUnknown})
	if got.Local != 0 {
		t.Fatalf("unknown spend = %+v; recording it as local would claim it ran on the user's "+
			"machine, which is a fact nobody established", got)
	}
	if got.Metered != 0 || got.Subscription != 100 {
		t.Fatalf("unknown spend = %+v, want it off the dollar ceiling", got)
	}

	// An executor that says NOTHING is metered: unattributed spend counts
	// against the ceiling rather than vanishing into a covered bucket,
	// where it would be invisible to the one control that stops runaway
	// cost.
	got = SplitSpend(ExecutorResult{TokensSpent: 100})
	if (got != Spend{Metered: 100}) {
		t.Fatalf("unreported billing = %+v, want it counted against the ceiling", got)
	}
}

// The two dollar-ceiling tests that stood here drove EngineTokenBudget -- the
// per-PLAN cumulative ceiling, deleted with budget.go in memql#5052. The
// invariant they pinned is not lost: component/work/budget_test.go's
// TestCheckCeilings_TokenBudgetExcludesSubscriptionAndLocal asserts exactly it
// against the successor, which reads the RUN's ceilings.
//
// TestSplitSpendKeepsUnbilledTokensOffTheDollarCeiling above stays, because
// SplitSpend stays: it is a fact about an EXECUTOR's billing, and the
// cockpit-app path uses it.
