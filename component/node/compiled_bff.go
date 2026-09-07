//go:build bff

package node

// compiledNodeType is the node type this binary was compiled for, and
// compiledNodeTypeTagged records that it came from a BUILD TAG rather than
// from the untagged default. That flag is what makes it beat MEMQL_NODE_TYPE:
// an untagged build also compiles as bff, so the type alone cannot tell a
// binary that chose bff from one that merely defaulted to it (memql#5115).
//
// The bff is also the untagged default, which is exactly why this file has to exist: -tags bff is a CHOICE and compiled_default.go is an absence, and only the flag below tells them apart.
var (
	compiledNodeType       = NodeTypeBFF
	compiledNodeTypeTagged = true
)
