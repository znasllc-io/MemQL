package app

// routing_rule_activator.go -- the adapter that joins integrations/router's
// RuleActivator seam to component/routingrules (epic memql#5127, design D7).
//
// It exists because `integrations` is its own Go module and requires the root
// module at a PUBLISHED version, while component/routingrules lives in the root
// module and is newer than any published one. An import from the integration
// therefore builds under the workspace and fails `GOWORK=off` -- which the
// module-boundaries lane checks, and caught.
//
// app/ is where both sides are visible, so app/ is where the shapes meet. The
// two structs are field-for-field copies across the boundary, and that cost is
// the smaller one: giving component/routingrules its own go.mod would trip
// twelve gates, three of which no local `go test` can see.

import (
	"context"

	"github.com/znasllc-io/memql/component/routingrules"
	integrationsrouter "github.com/znasllc-io/memql/integrations/router"
)

type routingRuleActivatorAdapter struct{}

func (routingRuleActivatorAdapter) ActivateRule(ctx context.Context, owner string, form integrationsrouter.RuleForm) (integrationsrouter.RuleResult, error) {
	res, err := routingrules.Activate(ctx, owner, routingrules.Form{
		Name:          form.Name,
		Description:   form.Description,
		When:          form.When,
		Policy:        form.Policy,
		Level:         form.Level,
		Precedence:    form.Precedence,
		OnUnavailable: form.OnUnavailable,
		Excludes:      form.Excludes,
	})
	// The RESULT travels even when there is an error: it carries the rendered
	// source and the Gate 1 diagnostics, which is what an operator needs to fix
	// the form. An error with no result sends them to the logs for text the
	// call already had.
	return integrationsrouter.RuleResult{
		Name:          res.Name,
		BundleId:      res.BundleId,
		Status:        res.Status,
		ConstructName: res.ConstructName,
		Source:        res.Source,
	}, err
}

func (routingRuleActivatorAdapter) RetireRule(ctx context.Context, owner, name string) (integrationsrouter.RuleResult, error) {
	res, err := routingrules.Retire(ctx, owner, name)
	return integrationsrouter.RuleResult{
		Name:     res.Name,
		BundleId: res.BundleId,
		Status:   res.Status,
	}, err
}

var _ integrationsrouter.RuleActivator = routingRuleActivatorAdapter{}
