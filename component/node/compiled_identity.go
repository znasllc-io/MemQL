//go:build identity

package node

// compiledNodeType is the node type this binary was compiled for, and
// compiledNodeTypeTagged records that it came from a BUILD TAG rather than
// from the untagged default. That flag is what makes it beat MEMQL_NODE_TYPE:
// an untagged build also compiles as bff, so the type alone cannot tell a
// binary that chose bff from one that merely defaulted to it (memql#5115).
//
// identity had no file here until memql#5115, so the auth service compiled as the untagged BFF default and depended entirely on its Deployment setting MEMQL_NODE_TYPE=identity. Unset or wrong, it reported bff, passed the Type == NodeTypeBFF gate in app/cluster.go and started the worker-mesh WorkerDialer -- tokenless, because the identity service has no node token. The tag now answers for it.
var (
	compiledNodeType       = NodeTypeIdentity
	compiledNodeTypeTagged = true
)
