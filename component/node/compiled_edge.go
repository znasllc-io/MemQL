//go:build edge

package node

// compiledNodeType is the node type this binary was compiled for, and
// compiledNodeTypeTagged records that it came from a BUILD TAG rather than
// from the untagged default. That flag is what makes it beat MEMQL_NODE_TYPE:
// an untagged build also compiles as bff, so the type alone cannot tell a
// binary that chose bff from one that merely defaulted to it (memql#5115).
//
// edge had no file here until memql#5115, for the same reason and with the same failure: an edge binary whose MEMQL_NODE_TYPE was unset or wrong reported bff and started the worker mesh's dialer. Nothing dials an edge, so it stays out of ValidNodeTypes.
var (
	compiledNodeType       = NodeTypeEdge
	compiledNodeTypeTagged = true
)
