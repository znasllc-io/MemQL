package routingrules

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/memql"
)

// gate_test.go -- the precondition behind this package's internal-origin
// allowlist entry.
//
// The allowlist in call_origin_conformance_test.go admits
// `component/routingrules` on three claims: the package is small, it exists for
// one operation family, and every call site in it is downstream of ONE gate. The
// entry is not the safety property. This is.
//
// It drives both operations with every role against an engine that refuses
// EVERYTHING and counts the attempts, so "the gate ran first" is measured as a
// count of writes rather than inferred from reading the code. A gate moved
// below the first write, or a third operation added without one, shows up here
// as an engine call from a role that should never have reached it.

// countingEngine refuses every call and records that it was reached. Refusing
// is what makes the count meaningful: a permissive fake would let a
// gate-bypassing path proceed and the test would measure the wrong step.
type countingEngine struct {
	calls    int
	activate int
	retire   int
}

func (e *countingEngine) Execute(context.Context, string) (*memql.ExecuteResult, error) {
	e.calls++
	return nil, fmt.Errorf("countingEngine refuses every write")
}

func (e *countingEngine) ActivateApprovedBundle(context.Context, string, string, memql.AuthoredRuntimeDeps) (memql.ActivationResult, error) {
	e.activate++
	return memql.ActivationResult{}, fmt.Errorf("countingEngine refuses every activation")
}

func (e *countingEngine) RetireActiveBundle(context.Context, string, string, memql.AuthoredRuntimeDeps) error {
	e.retire++
	return fmt.Errorf("countingEngine refuses every retirement")
}

func ctxAs(role auth.Role) context.Context {
	return auth.ContextWithAccess(context.Background(), &auth.AccessContext{
		UserId: "u-1",
		Role:   role,
	})
}

func activatorWith(e *countingEngine) *Activator {
	return NewActivator(e, func() memql.AuthoredRuntimeDeps { return memql.AuthoredRuntimeDeps{} }, shipped)
}

// TestOnlyAnAuthorReachesTheEngine is the precondition itself.
//
// owner and developer hold the create-on-construct capability and reach the
// engine; admin, writer and reader do not and must be refused BEFORE anything
// is written. An admin is the interesting row: on this ladder developer
// outranks admin, so "the most privileged role short of owner" is exactly the
// case a reader of the code is most likely to assume is allowed.
func TestOnlyAnAuthorReachesTheEngine(t *testing.T) {
	// THE SPLIT MUST BE NON-DEGENERATE, or the loop below passes vacuously. If
	// CanAuthor answered false for every role, "reached == mayAuthor" would
	// hold with the engine never reached at all -- a green test over a feature
	// that does not work.
	authors, others := 0, 0
	for _, role := range auth.ValidRoles() {
		if auth.CanAuthor(auth.UserContext{Role: role}) {
			authors++
		} else {
			others++
		}
	}
	if authors == 0 || others == 0 {
		t.Fatalf("CanAuthor splits the %d roles %d/%d; a gate with everyone or nobody on one side "+
			"makes the assertions below vacuous", len(auth.ValidRoles()), authors, others)
	}

	for _, role := range auth.ValidRoles() {
		t.Run(string(role), func(t *testing.T) {
			mayAuthor := auth.CanAuthor(auth.UserContext{Role: role})

			e := &countingEngine{}
			_, err := activatorWith(e).Activate(ctxAs(role), "u-1", validForm())
			if err == nil {
				t.Fatal("the refusing engine reported success, so this test measured nothing")
			}
			reached := e.calls+e.activate > 0
			if reached != mayAuthor {
				t.Fatalf("Activate as %q reached the engine=%v, and CanAuthor=%v.\n"+
					"Every write in this package must sit behind one auth.CanAuthor gate; that is the "+
					"claim its internal-origin allowlist entry rests on.", role, reached, mayAuthor)
			}
			if !mayAuthor && !strings.Contains(err.Error(), "owner or developer") {
				t.Fatalf("the refusal for %q is %q, which does not say who may do this", role, err)
			}

			r := &countingEngine{}
			_, rerr := activatorWith(r).Retire(ctxAs(role), "u-1", "somethingCustom")
			if rerr == nil {
				t.Fatal("the refusing engine reported a successful retirement")
			}
			if got := r.retire > 0; got != mayAuthor {
				t.Fatalf("Retire as %q reached the engine=%v, and CanAuthor=%v", role, got, mayAuthor)
			}
		})
	}
}

// TestAContextWithNoActorReachesNothing. An unauthenticated call is not a
// low-privilege call: the generated construct runs under its author's
// envelope, so a rule armed under no actor is one whose owned reads return
// nothing, correctly, forever.
func TestAContextWithNoActorReachesNothing(t *testing.T) {
	e := &countingEngine{}
	if _, err := activatorWith(e).Activate(context.Background(), "u-1", validForm()); err == nil {
		t.Fatal("a call with no access context was accepted")
	}
	if e.calls+e.activate > 0 {
		t.Fatalf("a call with no access context reached the engine %d time(s)", e.calls+e.activate)
	}
}

// TestARefusedFormReachesTheEngineOnlyToRecordWhy. A form the validator
// refuses must not be armed, and must not be written either -- the bundle rows
// exist to explain a GATE 1 failure, which is a different thing from a form
// that never got that far.
func TestARefusedFormReachesTheEngineOnlyToRecordWhy(t *testing.T) {
	f := validForm()
	f.Policy = ""
	e := &countingEngine{}
	if _, err := activatorWith(e).Activate(ctxAs(auth.RoleOwner), "u-1", f); err == nil {
		t.Fatal("a form with no policy was accepted")
	}
	if e.activate > 0 {
		t.Fatal("a form the validator refused was armed anyway")
	}
	if e.calls > 0 {
		t.Fatalf("a form the validator refused wrote %d row(s); the bundle rows explain a Gate 1 "+
			"failure, and this form never reached Gate 1", e.calls)
	}
}

// TestAShippedRuleCannotBeRetired. The shipped set is re-read from the embedded
// tree on every boot, so retiring one would appear to work and silently revert.
func TestAShippedRuleCannotBeRetired(t *testing.T) {
	e := &countingEngine{}
	_, err := activatorWith(e).Retire(ctxAs(auth.RoleOwner), "u-1", "reasoningParks")
	if err == nil {
		t.Fatal("a shipped rule was retired")
	}
	if e.retire > 0 {
		t.Fatal("retiring a shipped rule reached the engine")
	}
	if !strings.Contains(err.Error(), "precedence") {
		t.Fatalf("the refusal %q does not say what to do instead", err)
	}
}

// TestAnUnboundNodeRefusesRatherThanReportingSuccess. A node with no authored
// runtime cannot arm anything, and a success it could not deliver is worse
// than a refusal naming the missing half.
func TestAnUnboundNodeRefusesRatherThanReportingSuccess(t *testing.T) {
	if NewActivator(nil, nil, nil) != nil {
		t.Fatal("an Activator was built with no engine and no deps")
	}
	var nilActivator *Activator
	if _, err := nilActivator.Activate(ctxAs(auth.RoleOwner), "u-1", validForm()); err == nil {
		t.Fatal("a nil Activator reported success")
	}
}
