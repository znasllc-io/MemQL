package router

// routing_rules.go -- the two capabilities behind the routingRuleActivate and
// routingRuleRetire builtins (epic memql#5127, design D7).
//
// They are thin on purpose. Everything that decides anything -- the authoring
// floor, the shipped-name refusal, the closed condition keys, Gate 1, the
// bundle rows, the arming -- lives in component/routingrules, which is
// testable without a database. This file is the shape conversion between a
// builtin's `map[string]any` and that package's Form, plus the actor the
// generated construct will run under.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/auth"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/component/routingrules"
	"github.com/znasllc-io/memql/core/num"
)

func (i *Integration) handleActivateRoutingRule(ctx context.Context, args map[string]any, _ int) ([]memorynodes.MemoryNode, error) {
	owner, err := actingOwner(ctx)
	if err != nil {
		return nil, err
	}
	form, err := formFromArgs(args)
	if err != nil {
		return nil, err
	}
	res, err := routingrules.Activate(ctx, owner, form)
	if err != nil {
		// The RESULT is returned alongside the error where there is one: it
		// carries the rendered source and the Gate 1 diagnostics, which is what
		// an operator needs to fix the form. An error with no result sends them
		// to the logs for text the call already had.
		return nil, fmt.Errorf("%w", err)
	}
	return resultNode("v1:authoring:bundle", res.BundleId, map[string]any{
		"name":          res.Name,
		"bundleId":      res.BundleId,
		"status":        res.Status,
		"constructName": res.ConstructName,
		"source":        res.Source,
	}), nil
}

func (i *Integration) handleRetireRoutingRule(ctx context.Context, args map[string]any, _ int) ([]memorynodes.MemoryNode, error) {
	owner, err := actingOwner(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		return nil, fmt.Errorf("routingRuleRetire: name is required")
	}
	res, err := routingrules.Retire(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	return resultNode("v1:authoring:bundle", res.BundleId, map[string]any{
		"name":     res.Name,
		"bundleId": res.BundleId,
		"status":   res.Status,
	}), nil
}

// actingOwner is the authenticated caller. The generated construct runs under
// their envelope, so a call with no resolvable actor is refused rather than
// armed under a blank one -- a construct authored by nobody is one whose owned
// reads return nothing, correctly, forever.
func actingOwner(ctx context.Context) (string, error) {
	ac, ok := auth.AccessFromContext(ctx)
	if !ok || ac == nil || strings.TrimSpace(ac.UserId) == "" {
		return "", fmt.Errorf("routingRuleActivate: an authenticated caller is required; the generated rule runs under their envelope")
	}
	return memql.BareShortId(ac.UserId), nil
}

// formFromArgs converts the builtin's arguments into the Form.
//
// The `when` argument is an OBJECT rather than a fixed set of string
// arguments, and that is the distinction the whole condition model rests on: a
// key PRESENT with an empty value is a condition matching only an empty value,
// while a key ABSENT is no condition at all. Six optional string arguments
// could not tell those apart -- every unset one would arrive as "" and read as
// a condition.
func formFromArgs(args map[string]any) (routingrules.Form, error) {
	form := routingrules.Form{
		Name:          strings.TrimSpace(stringArg(args, "name")),
		Description:   stringArg(args, "description"),
		Policy:        strings.TrimSpace(stringArg(args, "policy")),
		Level:         strings.TrimSpace(stringArg(args, "level")),
		OnUnavailable: strings.TrimSpace(stringArg(args, "onUnavailable")),
		Precedence:    intArg(args, "precedence"),
	}
	if raw, ok := args["when"]; ok && raw != nil {
		obj, isObj := raw.(map[string]any)
		if !isObj {
			return routingrules.Form{}, fmt.Errorf("routingRuleActivate: `when` must be an object of condition keys")
		}
		form.When = make(map[string]string, len(obj))
		for k, v := range obj {
			s, isStr := v.(string)
			if !isStr {
				return routingrules.Form{}, fmt.Errorf("routingRuleActivate: the value for condition %q must be a string", k)
			}
			form.When[k] = s
		}
	}
	for _, v := range stringSliceArg(args, "excludes") {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			form.Excludes = append(form.Excludes, trimmed)
		}
	}
	return form, nil
}

// intArg reads a precedence off a decoded payload.
//
// It answers ZERO for anything it cannot read, which is the safe direction
// here and not merely the easy one: zero is the shipped floor rule's
// precedence, and the floor always sorts last, so an unreadable precedence
// produces the LEAST authoritative rule rather than one that quietly outranks
// the shipped set. See core/num for why a bare int() conversion is not used.
func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return num.Int64OrZero(v)
	case float64:
		return num.Float64OrZero(v)
	}
	return 0
}

// resultNode wraps a handler's answer as the single node a builtin returns.
func resultNode(concept, id string, payload map[string]any) []memorynodes.MemoryNode {
	raw, _ := json.Marshal(payload)
	return []memorynodes.MemoryNode{{ID: id, Concept: concept, Payload: raw}}
}

func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		if direct, isSlice := args[key].([]string); isSlice {
			return direct
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, isStr := v.(string); isStr {
			out = append(out, s)
		}
	}
	return out
}
