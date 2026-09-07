//go:build planner

package node

// compiledNodeType is the node type this binary was compiled for, and
// compiledNodeTypeTagged records that it came from a BUILD TAG rather than
// from the untagged default. That flag is what makes it beat MEMQL_NODE_TYPE:
// an untagged build also compiles as bff, so the type alone cannot tell a
// binary that chose bff from one that merely defaulted to it (memql#5115).
//
// Every node type this repo builds an image for has a file here; scripts/ci/node_type_lists_test.go holds that set against app/build_<type>.go.
var (
	compiledNodeType       = NodeTypePlanner
	compiledNodeTypeTagged = true
)
