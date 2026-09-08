package router

// routing_rules.go -- the two capabilities behind the routingRuleActivate and
// routingRuleRetire builtins (epic memql#5127, design D7).
//
// They are thin on purpose. Everything that decides anything -- the authoring
// floor, the shipped-name refusal, the closed condition keys, Gate 1, the
// bundle rows, the arming -- lives in component/routingrules, which is
// testable without a database. This file is the shape conversion between a
// builtin's `map[string]any` and that package's form, plus the actor the
// generated construct will run under.
//
// IT DOES NOT IMPORT component/routingrules, and that is a MODULE fact rather
// than a taste one. `integrations` is its own Go module and requires the root
// module at a published version; component/routingrules lives in the root
// module and is newer than any published version, so an import here builds
// under the workspace and fails `GOWORK=off` -- which is what the
// module-boundaries lane checks and what it caught.
//
// So the seam is an INTERFACE declared here and satisfied there, installed
// from app/ where both are visible: the same shape work.SetCompiler and
// work.SetRemedy already use, for the same reason.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/auth"
	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/core/num"
)

// RuleForm is the shape a routing rule arrives in. It mirrors
// component/routingrules.Form field for field; the conversion lives in app/,
// where both types are visible.
//
// Duplicating a struct across a module boundary is a cost, and the alternative
// is worse: a new nested Go module for component/routingrules trips twelve
// gates, three of which no local `go test` can see.
type RuleForm struct {
	Name          string
	Description   string
	When          map[string]string
	Policy        string
	Level         string
	Precedence    int
	OnUnavailable string
	Excludes      []string
}

// RuleResult is what activation reports back.
type RuleResult struct {
	Name          string
	BundleId      string
	Status        string
	ConstructName string
	Source        string
}

// RuleActivator arms and retires runtime-authored routing rules. Satisfied by
// an adapter in app/ over component/routingrules.
type RuleActivator interface {
	ActivateRule(ctx context.Context, owner string, form RuleForm) (RuleResult, error)
	RetireRule(ctx context.Context, owner, name string) (RuleResult, error)
}

// SetRuleActivator installs the seam. A nil activator is a working state: the
// two capabilities refuse with a sentence naming the missing half rather than
// reporting a success nobody can act on.
func (i *Integration) SetRuleActivator(a RuleActivator) {
	if i == nil {
		return
	}
	i.ruleActivator = a
}

func (i *Integration) handleActivateRoutingRule(ctx context.Context, args map[string]any, _ int) ([]memorynodes.MemoryNode, error) {
	owner, err := actingOwner(ctx)
	if err != nil {
		return nil, err
	}
	form, err := formFromArgs(args)
	if err != nil {
		return nil, err
	}
	if i.ruleActivator == nil {
		return nil, fmt.Errorf("routingRuleActivate: the authored runtime is not wired on this node, so a routing rule cannot be armed here. Arming happens on a node that runs the authored-construct scheduler")
	}
	res, err := i.ruleActivator.ActivateRule(ctx, owner, form)
	if err != nil {
		return nil, err
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
	if i.ruleActivator == nil {
		return nil, fmt.Errorf("routingRuleRetire: the authored runtime is not wired on this node")
	}
	res, err := i.ruleActivator.RetireRule(ctx, owner, name)
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
func formFromArgs(args map[string]any) (RuleForm, error) {
	form := RuleForm{
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
			return RuleForm{}, fmt.Errorf("routingRuleActivate: `when` must be an object of condition keys")
		}
		form.When = make(map[string]string, len(obj))
		for k, v := range obj {
			s, isStr := v.(string)
			if !isStr {
				return RuleForm{}, fmt.Errorf("routingRuleActivate: the value for condition %q must be a string", k)
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
