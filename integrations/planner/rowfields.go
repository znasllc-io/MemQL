package planner

import (
	"github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/core/num"
)

// rowfields.go -- the small row-reading helpers the surviving files share.
//
// They lived in train_specialist_dispatch.go and
// embed_domain_items_dispatch.go, which memql#5051 deletes -- and
// responsibility_intake.go and the authoring transcript, which both STAY, both
// use them. A helper whose only home is a file being deleted is the shape that
// turns a clean deletion into a broken build, so they get a home of their own.

// mapField pulls a nested object field off a row map, tolerating both
// map[string]any and the absence of the field.
func mapField(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

// intFromAny reads an int-valued field, tolerating the float64 a decoded JSON
// number arrives as.
//
// narrowing: SATURATE -- the one caller reads `data.seq`, the position of a
// tool call within its run, and the transcript reader SORTS on it. An
// out-of-range value clamped to the extreme keeps that ordering monotonic;
// zeroing it would move the call to the front, and reproducing a run's calls
// in the wrong order is a transcript that compiles and lies.
func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return num.ClampInt64(n)
	case float64:
		return num.ClampFloat64(n)
	}
	return 0
}

// extractAgentFactoryResult plucks (agentId, action) off the agent factory's
// reply.
//
// The factory answers in a `{bundle: {nodes: [...]}}` envelope, so this
// normalizes to a flat row slice and reads the fields off the first row's top
// level OR its payload -- both shapes occur depending on how the reply was
// projected.
//
// It lived in agent_loop.go; its remaining caller is the REACTIVE LOOP
// (reactive_loop.go), which memql#5052 keeps.
func extractAgentFactoryResult(res any) (agentId, action string) {
	rows := memql.MaterializeRows(res)
	if len(rows) == 0 {
		return "", ""
	}
	row := rows[0]
	if id, ok := row["agentId"].(string); ok {
		agentId = id
	}
	if a, ok := row["action"].(string); ok {
		action = a
	}
	if p, ok := row["payload"].(map[string]any); ok {
		if id, ok := p["agentId"].(string); ok && agentId == "" {
			agentId = id
		}
		if a, ok := p["action"].(string); ok && action == "" {
			action = a
		}
	}
	return agentId, action
}
