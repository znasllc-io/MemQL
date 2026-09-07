// Package readiness is the pure decision layer of configuration readiness
// (design record docs/superpowers/specs/2026-09-06-configuration-readiness-design.md,
// section 4.5): values in, verdicts out, no engine, no database, no provider.
//
// It is a package of the ROOT module on purpose. A nested module would trip a
// dozen gates, and the one importer that must reach it -- component/memql,
// where the writer lives because only allowlisted packages may stamp internal
// origin -- already depends on the root.
package readiness

import (
	"sort"
	"time"
)

// State is one node's, or the fold's, verdict on one module.
type State string

const (
	Configured    State = "configured"
	Partial       State = "partial"
	Unconfigured  State = "unconfigured"
	NotApplicable State = "notApplicable"
	// Unreported is the fold's word for "no live node reported this module".
	// It is never spelled Unconfigured: not knowing and not being configured
	// are different answers, and the OS draws nothing for this one.
	Unreported State = "unreported"
)

// SlotReport is one registry entry's presence and source. Never a value.
type SlotReport struct {
	Name     string `json:"name"`
	Present  bool   `json:"present"`
	Source   string `json:"source"`
	Optional bool   `json:"optional,omitempty"`
}

// LaneReport is one lane's completeness on one node.
type LaneReport struct {
	Name             string       `json:"name"`
	ConfigurableFrom string       `json:"configurableFrom"`
	Complete         bool         `json:"complete"`
	Slots            []SlotReport `json:"slots"`
}

// NodeReport is one row of v1:platform:moduleReadiness.
type NodeReport struct {
	Module     string       `json:"module"`
	NodeId     string       `json:"nodeId"`
	NodeType   string       `json:"nodeType"`
	State      State        `json:"state"`
	Core       bool         `json:"core"`
	Lanes      []LaneReport `json:"lanes"`
	ReportedAt time.Time    `json:"reportedAt"`
}

// NodeLiveness is what the fold needs from a v1:cluster:node row.
type NodeLiveness struct {
	NodeId   string    `json:"nodeId"`
	Health   string    `json:"health"`
	LastSeen time.Time `json:"lastSeen"`
}

// NodeVerdict is one live reporter's contribution to a Verdict.
type NodeVerdict struct {
	NodeId     string    `json:"nodeId"`
	NodeType   string    `json:"nodeType"`
	State      State     `json:"state"`
	ReportedAt time.Time `json:"reportedAt"`
}

// Verdict is the cluster-wide answer for one module.
type Verdict struct {
	Module       string        `json:"module"`
	State        State         `json:"state"`
	Core         bool          `json:"core"`
	Disagreement []string      `json:"disagreement"`
	Nodes        []NodeVerdict `json:"nodes"`
}

// NodeLiveWindow is how recently a cluster node must have been seen for its
// report to count. Three times the reconciler's 20s grace, so a slow
// heartbeat does not flicker a verdict. MIRRORED as NODE_LIVE_WINDOW_SECONDS
// in clients/os/src/system/readinessFold.ts and pinned by
// TestNodeLiveWindowMatchesTheClient in this package.
const NodeLiveWindow = 60 * time.Second

var liveHealth = map[string]bool{
	"healthy":    true,
	"connecting": true,
	"degraded":   true,
	"draining":   true,
}

// NodeIsLive reports whether a node's report may count: a live health word
// and a heartbeat inside the window. A zero LastSeen is never live.
func NodeIsLive(n NodeLiveness, now time.Time) bool {
	if !liveHealth[n.Health] || n.LastSeen.IsZero() {
		return false
	}
	return now.Sub(n.LastSeen) <= NodeLiveWindow
}

func rank(s State) int {
	switch s {
	case Unconfigured:
		return 2
	case Partial:
		return 1
	default:
		return 0
	}
}

// Fold turns every node's report into one verdict per module.
//
//  1. Keep reports from live nodes only, so a dead replica's stale row cannot
//     pin a verdict.
//  2. Drop notApplicable.
//  3. Worst state wins.
//  4. If the kept reports disagree, the verdict is partial and Disagreement
//     names every live reporter as nodeId=state, worst state first.
//  5. A module with no kept report is unreported.
//
// Modules appear in name order, so two folds over the same rows are equal.
func Fold(reports []NodeReport, nodes []NodeLiveness, now time.Time) []Verdict {
	live := map[string]bool{}
	for _, n := range nodes {
		if NodeIsLive(n, now) {
			live[n.NodeId] = true
		}
	}
	kept := map[string][]NodeReport{}
	core := map[string]bool{}
	seen := map[string]bool{}
	var order []string
	for _, r := range reports {
		if !seen[r.Module] {
			seen[r.Module] = true
			order = append(order, r.Module)
		}
		core[r.Module] = core[r.Module] || r.Core
		if !live[r.NodeId] || r.State == NotApplicable {
			continue
		}
		kept[r.Module] = append(kept[r.Module], r)
	}
	sort.Strings(order)
	out := make([]Verdict, 0, len(order))
	for _, module := range order {
		v := Verdict{Module: module, Core: core[module], Disagreement: []string{}, Nodes: []NodeVerdict{}}
		rs := kept[module]
		if len(rs) == 0 {
			v.State = Unreported
			out = append(out, v)
			continue
		}
		sort.Slice(rs, func(i, j int) bool {
			if rank(rs[i].State) != rank(rs[j].State) {
				return rank(rs[i].State) > rank(rs[j].State)
			}
			return rs[i].NodeId < rs[j].NodeId
		})
		states := map[State]bool{}
		for _, r := range rs {
			states[r.State] = true
			v.Nodes = append(v.Nodes, NodeVerdict{NodeId: r.NodeId, NodeType: r.NodeType, State: r.State, ReportedAt: r.ReportedAt})
		}
		if len(states) > 1 {
			v.State = Partial
			for _, r := range rs {
				v.Disagreement = append(v.Disagreement, r.NodeId+"="+string(r.State))
			}
		} else {
			v.State = rs[0].State
		}
		out = append(out, v)
	}
	return out
}
