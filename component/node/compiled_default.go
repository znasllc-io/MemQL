//go:build !agent && !planner && !bff && !identity && !workbench && !mcp && !edge

package node

// compiledNodeType is the node type an UNTAGGED build runs as, and
// compiledNodeTypeTagged says that it is a DEFAULT rather than a decision --
// which is what lets MEMQL_NODE_TYPE select the type here and nowhere else
// (memql#5115). Mirrors app/build_default.go, whose deny-list decides the same
// thing for the wiring.
//
// The constraint is a DENY list of live node types, and its staleness is
// SILENT in the direction that matters. Retiring a node type without editing
// this line does not make `-tags <retired>` an error; it makes the retired name
// a spelling of this file. That is not hypothetical here: `identity` and `edge`
// were never ADDED to it, so both compiled as bff for their whole life
// (memql#5057, memql#5115). scripts/ci/node_type_lists_test.go now holds this
// list against app/build_<type>.go so it cannot drift again.
var (
	compiledNodeType       = NodeTypeBFF
	compiledNodeTypeTagged = false
)
