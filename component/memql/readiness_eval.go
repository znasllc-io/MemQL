package memql

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/znasllc-io/memql/component/envregistry"
	"github.com/znasllc-io/memql/component/memql/readiness"
)

// Slot sources, spelled the way email's ConfigResolver spells them so the
// Set up group reads one vocabulary.
const (
	readinessSourceEnv      = "env"
	readinessSourceVariable = "globalVariable"
	readinessSourceSecret   = "globalSecret"
	readinessSourceUnset    = "unset"
)

// readinessResolvers is everything the evaluator needs from the node, as
// functions, so the decision is testable with no engine.
type readinessResolvers struct {
	Env      func(name string) (string, bool)
	Variable func(ctx context.Context, name string) (string, error)
	Secret   func(ctx context.Context, name string) (string, error)
	IsSecret func(name string) bool
	// Hosted reports whether this node hosts the module.
	Hosted func(mod envregistry.Module) bool
	// InferenceOpen reports whether any inference door is open here.
	InferenceOpen func(ctx context.Context) bool
	// IntegrationState asks integration.<name>.status in-process:
	// state is the report's own word, touched is "any slot present",
	// registered=false means the integration is not on this node.
	IntegrationState func(ctx context.Context, name string) (state string, touched bool, registered bool, err error)
}

// resolveSlot walks the ladder -- environment, then the row tier the slot's
// own kind names -- and answers presence and source, NEVER the value.
//
// A secret slot stops at the secret tier rather than falling through to
// globalVariable. The two tiers are different stores with different readers:
// a plaintext row written under a secret's name would satisfy a
// falling-through check while the decrypting reader still finds nothing, so
// the report would say configured about a lane that cannot work.
func resolveSlot(ctx context.Context, r readinessResolvers, name string, optional bool) readiness.SlotReport {
	out := readiness.SlotReport{Name: name, Optional: optional, Source: readinessSourceUnset}
	if r.Env != nil {
		if v, ok := r.Env(name); ok && strings.TrimSpace(v) != "" {
			out.Present, out.Source = true, readinessSourceEnv
			return out
		}
	}
	if r.IsSecret != nil && r.IsSecret(name) {
		if r.Secret != nil {
			if v, err := r.Secret(ctx, name); err == nil && strings.TrimSpace(v) != "" {
				out.Present, out.Source = true, readinessSourceSecret
			}
		}
		return out
	}
	if r.Variable != nil {
		if v, err := r.Variable(ctx, name); err == nil && strings.TrimSpace(v) != "" {
			out.Present, out.Source = true, readinessSourceVariable
		}
	}
	return out
}

func evaluateLane(ctx context.Context, r readinessResolvers, lane envregistry.Lane) readiness.LaneReport {
	out := readiness.LaneReport{Name: lane.Name, ConfigurableFrom: lane.ConfigurableFrom, Complete: true, Slots: []readiness.SlotReport{}}
	for _, name := range lane.Slots {
		s := resolveSlot(ctx, r, name, false)
		if !s.Present {
			out.Complete = false
		}
		out.Slots = append(out.Slots, s)
	}
	// An optional slot is reported but never decides completeness: it is
	// there so a person can see the whole lane, not so a lane can be
	// half-satisfied by the part that did not matter.
	for _, name := range lane.OptionalSlots {
		out.Slots = append(out.Slots, resolveSlot(ctx, r, name, true))
	}
	return out
}

// evaluateModule decides one node's verdict on one module. Presence and
// source only: no branch here ever keeps a resolved value.
func evaluateModule(ctx context.Context, r readinessResolvers, mod envregistry.Module, nodeId, nodeType string, now time.Time) readiness.NodeReport {
	out := readiness.NodeReport{Module: mod.Name, NodeId: nodeId, NodeType: nodeType, Core: mod.Core, Lanes: []readiness.LaneReport{}, ReportedAt: now}
	if r.Hosted != nil && !r.Hosted(mod) {
		out.State = readiness.NotApplicable
		return out
	}
	switch {
	case mod.Evaluator == envregistry.EvaluatorInferenceStatus:
		if r.InferenceOpen != nil && r.InferenceOpen(ctx) {
			out.State = readiness.Configured
		} else {
			out.State = readiness.Unconfigured
		}
	case strings.HasPrefix(mod.Evaluator, envregistry.EvaluatorIntegrationPrefix):
		name := strings.TrimPrefix(mod.Evaluator, envregistry.EvaluatorIntegrationPrefix)
		state, touched, registered, err := "", false, false, error(nil)
		if r.IntegrationState != nil {
			state, touched, registered, err = r.IntegrationState(ctx, name)
		}
		switch {
		// A probe that failed, or an integration this node does not carry,
		// says nothing about whether a person did the setup. Unconfigured
		// would send them to a form they may not need.
		case err != nil || !registered:
			out.State = readiness.NotApplicable
		// unhealthy is CONFIGURED: the setup was done and the send is
		// failing for some other reason, which is a different repair.
		case state == "configured" || state == "unhealthy":
			out.State = readiness.Configured
		case touched:
			out.State = readiness.Partial
		default:
			out.State = readiness.Unconfigured
		}
	default:
		// Lanes are alternatives, not requirements: ONE complete lane
		// configures the module. "Touched" is any required slot present in
		// any lane, which is what separates a half-finished setup from one
		// nobody has started.
		touched, complete := false, false
		for _, lane := range mod.Lanes {
			lr := evaluateLane(ctx, r, lane)
			out.Lanes = append(out.Lanes, lr)
			if lr.Complete {
				complete = true
			}
			for _, s := range lr.Slots {
				if s.Present && !s.Optional {
					touched = true
				}
			}
		}
		switch {
		case complete:
			out.State = readiness.Configured
		case touched:
			out.State = readiness.Partial
		default:
			out.State = readiness.Unconfigured
		}
	}
	return out
}

func evaluateModules(ctx context.Context, r readinessResolvers, mods []envregistry.Module, nodeId, nodeType string, now time.Time) []readiness.NodeReport {
	out := make([]readiness.NodeReport, 0, len(mods))
	for _, mod := range mods {
		out = append(out, evaluateModule(ctx, r, mod, nodeId, nodeType, now))
	}
	return out
}

// integrationReport is the slice of integration.<name>.status's payload the
// evaluator reads. The report's own fields, nothing invented.
type integrationReport struct {
	State    string `json:"state"`
	Settings []struct {
		Source string `json:"source"`
	} `json:"settings"`
	Credentials []struct {
		Present bool   `json:"present"`
		Source  string `json:"source"`
	} `json:"credentials"`
}

// readinessResolvers wires the evaluator to this node: the environment, the
// two row tiers, the plug-in registry, the provider registry and the
// in-process capability map.
func (e *MemQLEngine) readinessResolvers() readinessResolvers {
	manifest, _ := envregistry.LoadManifest("")
	nodeType := envregistry.ResolveNodeType()
	return readinessResolvers{
		Env:      os.LookupEnv,
		Variable: e.ResolveSystemVariable,
		Secret:   e.ResolveSystemSecret,
		IsSecret: func(name string) bool { return manifest != nil && manifest.IsSecret(name) },
		Hosted: func(mod envregistry.Module) bool {
			if mod.HostedBy.Everywhere() {
				return true
			}
			for _, name := range mod.HostedBy.Integrations {
				if e.IntegrationByName(name) != nil {
					return true
				}
			}
			for _, nt := range mod.HostedBy.NodeTypes {
				if nt == nodeType {
					return true
				}
			}
			return false
		},
		InferenceOpen: func(ctx context.Context) bool { return len(e.inferenceDoors(ctx).Doors) > 0 },
		IntegrationState: func(ctx context.Context, name string) (string, bool, bool, error) {
			handler, ok := e.builtinExecutorHandlers["integration."+name+".status"]
			if !ok {
				return "", false, false, nil
			}
			nodes, err := handler(ctx, map[string]any{"probe": false}, 0)
			if err != nil {
				return "", false, true, err
			}
			for _, n := range nodes {
				var rep integrationReport
				if err := json.Unmarshal(n.Payload, &rep); err != nil {
					continue
				}
				if rep.State == "" {
					continue
				}
				touched := false
				for _, s := range rep.Settings {
					if s.Source != "" && s.Source != readinessSourceUnset {
						touched = true
					}
				}
				for _, c := range rep.Credentials {
					if c.Present {
						touched = true
					}
				}
				return rep.State, touched, true, nil
			}
			return "", false, true, nil
		},
	}
}
