package packages

// builder.go records why Deps.Builder is nil in production, because a nil
// field with no explanation reads as an oversight and this one is a boundary.
//
// # What ships
//
// The D4 FAST PATH ships and is complete: a package whose deployable already
// carries its built output (dist/index.html in the snapshot) deploys with no
// build at all -- no workbench, no network, no restart. That covers every tree
// whose CI already builds it, which is what the memql-project template
// produces, and it is the path the epic's own fixtures exercise end to end.
//
// A package that needs a build gets a TYPED REFUSAL naming the command it
// would have run and the two ways forward (commit the output, or configure a
// build surface). It is refused at the build stage, after the analysis has
// already reported the build plan at the confirm gate -- so nobody discovers
// this by watching a deploy hang.
//
// # What does not, and why it is not a shortcut
//
// D4 puts builds on the WORKBENCH: sandboxed, resource-capped, no cluster
// credentials in the environment. That is not decoration -- a package's build
// script is somebody else's code running inside this cluster, and `npm ci`
// executes whatever the tree's dependencies put in a postinstall hook.
//
// The workbench reaches that isolation through a PER-RUN workspace, and its
// own gate refuses a call whose runId does not resolve to a readable run
// (workspace_owner_unresolved, memql#4354) -- deliberately, because a workspace
// written under a blank actor is readable by nobody, including the operator
// answering "where did my file go". A package deploy has no run, and giving it
// one is a decision this epic's spec does not make.
//
// THE TWO OPTIONS THIS PARAGRAPH USED TO WEIGH ARE GONE, and their reasoning
// did not survive with them (memql#5053). They were `createPlan`, rejected
// because the planner agent CLAIMED such a row off its node-created event and
// decomposed it with an LLM -- a deploy that silently spends model budget is a
// worse defect than a build that refuses -- and `createAdHocPlan`, rejected
// because it required an agentId a deploy does not have.
//
// The successor question is about `v1:work:goal`, and only the FIRST objection
// carries over: opening a goal runs compile, which reaches a model. The second
// does not -- a goal has no agentId field to falsify. So this is still open,
// and it is open for one reason now rather than two.
//
// The third option -- running the build in this process with os/exec -- is the
// one that must not be taken. It would deliver the feature by deleting the
// property D4 named: untrusted code would run in the engine pod, with the
// engine's environment, which includes its credentials.
//
// So the SEAM is here, complete and tested (the pipeline suite drives a fake
// Builder through success, failure and the bounded log tail), and binding it
// wants one small decision on the workbench side: either a plan kind the
// planner does not claim, or a workspace owner that is a user rather than a
// plan. Filed rather than guessed.
