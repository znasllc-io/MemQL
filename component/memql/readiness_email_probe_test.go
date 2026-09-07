package memql

import (
	"context"
	"errors"
	"testing"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/envregistry"
	"github.com/znasllc-io/memql/component/memql/readiness"
)

func emailModule() envregistry.Module {
	return envregistry.Module{
		Name: "email", Core: true, Description: "d",
		Evaluator: envregistry.EvaluatorIntegrationPrefix + "email",
	}
}

// integrations/email's statusAuthorized, restated over the same context surface
// it reads, so the case below asserts the ACTUAL gate rather than a stand-in
// that would pass whatever the evaluation context happened to carry.
//
// A restatement rather than an import: integrations/email requires
// component/memql, so this package may not import it. That is the same module
// direction component/memql/readiness/inference.go argues at length, and here
// it costs four lines rather than a parity gate -- what is restated is a
// three-role switch that has been stable since the capability was written, and
// the assertion below fails loudly if the real one grows stricter, because the
// probe would start refusing and the verdict would go back to notApplicable.
func statusAuthorizedRule(ctx context.Context) error {
	ac, ok := auth.AccessFromContext(ctx)
	if !ok || ac == nil {
		return errors.New("email.status: no authenticated caller")
	}
	switch ac.Role {
	case auth.RoleOwner, auth.RoleAdmin, auth.RoleDeveloper:
		return nil
	}
	return errors.New("email.status: role may not read integration configuration")
}

// WHY `email` READ notApplicable ON EVERY NODE OF EVERY CLUSTER.
//
// The design record recorded the cause as a lazy-sender timing problem -- "the
// rows are written before the lazy mail sender has resolved". IT IS NOT ONE.
// integrations/email/status.go's describer is deliberately a REPRODUCTION of
// the resolution algorithm ("neither trigger that resolution early nor be
// answered by a cache that predates a credential the operator has since
// seeded"), so it never materializes a sender and timing cannot reach it.
//
// The cause is the ACTOR. app/run.go's boot write passed context.Background(),
// the EVALUATION ran on that caller context, statusAuthorized refuses a context
// with no AccessContext at all, and evaluateModule maps an errored probe to
// notApplicable -- correctly, because a probe that could not run says nothing
// about whether a person did the setup.
//
// Both halves are pinned here: the mapping is right and stays, and the
// evaluation context must not be the one that produces it.
func TestAnUnauthorizedProbeIsWhatMadeEmailNotApplicable(t *testing.T) {
	// (1) The old shape. A bare context reaches the probe, the probe refuses,
	// and the verdict is notApplicable -- on every node, forever.
	refusing := readinessResolvers{
		IntegrationState: func(ctx context.Context, _ string) (string, bool, bool, error) {
			if err := statusAuthorizedRule(ctx); err != nil {
				return "", false, true, err
			}
			return "configured", true, true, nil
		},
	}
	if got := evaluateModule(context.Background(), refusing, emailModule(), "n", "bff", inferenceNow); got.State != readiness.NotApplicable {
		t.Fatalf("a refused probe read %s. The mapping this bug rode in on has changed, so the "+
			"regression pinned below can no longer be reproduced -- read evaluateModule's "+
			"integration arm before touching this test.", got.State)
	}

	// (2) The fix, through the SAME resolver. Only the context differs.
	got := evaluateModule(readinessEvaluateContext(context.Background()), refusing, emailModule(), "n", "bff", inferenceNow)
	if got.State == readiness.NotApplicable {
		t.Fatal("a node carrying the email plug-in still reports notApplicable after boot. " +
			"readinessEvaluateContext must clear integrations/email's statusAuthorized, which " +
			"admits owner, admin or developer and nothing else.")
	}
	if got.State != readiness.Configured {
		t.Fatalf("email reports %s, want configured", got.State)
	}
}

// The gate this leans on, stated as its own case so a reader can see WHICH
// property of the evaluation context does the work. RoleReader -- what
// readinessWriteContext deliberately carries -- would not clear it, which is
// why the two contexts are two.
func TestTheEvaluationActorClearsTheIntegrationStatusFloor(t *testing.T) {
	if err := statusAuthorizedRule(readinessEvaluateContext(context.Background())); err != nil {
		t.Fatalf("the evaluation actor is refused by the integration status floor: %v", err)
	}
	if err := statusAuthorizedRule(readinessWriteContext(context.Background())); err == nil {
		t.Fatal("the WRITE context clears the integration status floor. It carries RoleReader on " +
			"purpose -- it writes a public concept behind @serverOnly plus internal origin and needs " +
			"nothing more -- so if it now clears this, the two contexts have converged and the " +
			"argument for having two has gone with them.")
	}
}
