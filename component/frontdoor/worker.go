package frontdoor

// WorkerServicePath is the ONE gRPC rule on the api host that does not reach
// the bff: the path prefix every method of the worker stream shares, routed to
// the agent node.
//
// # Why the api host needs a second gRPC rule at all
//
// `WorkerService.Stream` is served by the AGENT node and by nothing else
// (app/transport_agent.go registers it; no other build tag does), while the
// api host's gRPC half is a `/` catch-all to the bff's h2c edge. gRPC puts the
// fully qualified service name in the request path -- every call to this
// service arrives as `/znasllc.memql.worker.v1.WorkerService/<Method>` -- so a
// cockpit dialling the documented `https://api.<domain>` reached a server that
// had never heard of the service and was answered, forever,
// `Unimplemented: unknown service znasllc.memql.worker.v1.WorkerService`. The
// machine never registered and the OS reported "waiting" (epic memql#5218,
// design D10). Local and cloud alike, because both front doors carried the
// same single rule.
//
// So both front doors carry one more rule on the api host, ABOVE the
// catch-all: this prefix to `svc/agent:50051`. It is the shape of the system
// rather than a value -- the same rule in the hand-authored local overlay, in
// cmd/frontdoorhosts' generated gRPC Ingress, and in every account's reserved
// `api.` host -- and render gates in each place refuse a front door without
// it.
//
// # Why it is named here and nowhere else
//
// The generated descriptor already spells the service name
// (memqlv1.WorkerService_ServiceDesc.ServiceName), and this package cannot
// import it: frontdoor is a leaf that every domain-deriving module drags in,
// and component/grpc is most of the engine. So the prefix is spelled ONCE
// here, and deploy/k8s/overlays/frontdoor_worker_test.go -- in a module that
// already depends on component/grpc -- holds the two equal. A second spelling
// in a manifest is the kind that drifts when a proto package is renamed, and
// the drift presents exactly as the bug this rule fixes.
//
// The TRAILING SLASH is load-bearing. A gRPC path is `/<service>/<method>`,
// and a prefix rule without the slash would also match a service whose name
// merely begins with this one.
const WorkerServicePath = "/znasllc.memql.worker.v1.WorkerService/"
