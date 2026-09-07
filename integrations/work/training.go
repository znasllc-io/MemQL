package work

// training.go -- approval-driven specialist training on the work spine
// (memql#5063).
//
// ===========================================================================
// WHAT LAPSED
// ===========================================================================
// `mintApprovedTrainingPlan` was the gate that turned an APPROVED
// `spawnTrainingPlan` decision into real training: it resolved the
// specialist's primary knowledge domain, opened the training work, and
// escalated for feedback when no domain-bearing skill resolved.
//
// Its only caller was `dispatchDecision` in the planner's decision loop, which
// memql#5052 deleted -- so the gate went with its trigger, under the repo's
// pre-release rule. The MECHANISM survived intact and is not the gap:
// memql#5051 moved the Trainer's bounded tool loop into
// `integrations/knowledge` as the `trainSpecialist` builtin, and
// `dsl/knowledge/automations.memql` carries the `trainSpecialist` work
// template. The refresh CADENCE still opens training per due domain
// (`integrations/planner/refresh_cron.go`).
//
// What was gone is the APPROVAL path: a person approving "train a specialist
// on X" no longer caused training. Only the clock did.
//
// ===========================================================================
// WHERE IT GOES NOW, AND WHY IT IS NOT A NEW LOOP
// ===========================================================================
// Design record 2026-09-05-work-spine-design.md section F:
// `createSpecialist`, `extendSpecialist` and `spawnTrainingPlan` "become skill
// mint and skill training under the same gates". `v1:work:approval` is the one
// concept for every human gate (D6) and already carries the `skillMint` kind,
// so the successor is not a second approval mechanism -- it is a subject on
// the existing one, acted on where every other decision is acted on.
//
// ===========================================================================
// THE TWO GATES THE REPLACEMENT HAD TO KEEP
// ===========================================================================
// The issue is explicit that a replacement without these is worse than the
// gap, because both are cost-control properties
// (docs/public/ai/llm-cost-control.md):
//
//  1. THE SPECIALIST MUST HAVE A DOMAIN-BEARING SKILL, or the request
//     ESCALATES for feedback rather than starting work against no domain.
//     Training with no domain is a bounded tool loop with nowhere to write, so
//     it spends model calls and produces nothing.
//  2. THE APPROVAL IS THE IDEMPOTENCY: one approved decision starts exactly
//     one training run. That is not re-implemented here and must not be --
//     handleDecideApproval resolves the approval through the caller's PENDING
//     list (`decision==""`), so a second decide finds no row and refuses. This
//     runs after that resolution and therefore inherits it. A deterministic
//     goal id would be a SECOND idempotency that could disagree with the
//     first.
//
// The escalation is a `feedback` approval on the same run rather than a log
// line, because the person who approved training is the person who has to
// attach a domain, and an approval is the one surface they are already
// watching.

import (
	"context"
	"fmt"
	"strings"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	langparser "github.com/znasllc-io/memql/component/language/parser"
	work "github.com/znasllc-io/memql/component/work"
)

// TrainingSubjectAction is the value an approval's subject carries to mean
// "this decision is about training a specialist". A field rather than a
// dedicated approval KIND, because a kind is a GATE (which question is being
// asked of a person) and `skillMint` already asks it; a new kind would need a
// case in every switch over kinds and buy nothing.
const TrainingSubjectAction = "trainSpecialist"

// trainingTemplate is the @template automation the run executes. It is the
// same one the refresh cadence opens, which is the point: approval and cadence
// must not drift into two trainers.
const trainingTemplate = "trainSpecialist"

// trainingRequest is an approval subject read back into the four values the
// template's args need.
type trainingRequest struct {
	SpecialistId string
	Topic        string
	Mode         string
}

// trainingRequestFrom reads a decided approval's subject, and answers false
// for every approval that is not a training request.
//
// It requires BOTH the kind and the action. The kind alone would make every
// future skillMint subject start a training run; the action alone would let a
// subject field on an unrelated approval do it.
func trainingRequestFrom(kind string, subject map[string]any) (trainingRequest, bool) {
	if kind != work.ApprovalKindSkillMint {
		return trainingRequest{}, false
	}
	if rowString(subject, "action") != TrainingSubjectAction {
		return trainingRequest{}, false
	}
	req := trainingRequest{
		SpecialistId: strings.TrimSpace(rowString(subject, "specialistId")),
		Topic:        strings.TrimSpace(rowString(subject, "topic")),
		Mode:         strings.TrimSpace(rowString(subject, "mode")),
	}
	if req.Mode == "" {
		req.Mode = "initial"
	}
	return req, true
}

// startApprovedTraining is the gate. It returns the goal and run it opened, or
// empty strings when it escalated or when this was not a training approval.
//
// It never returns an error to its caller. The DECISION has already landed by
// the time this runs, and failing the decide call now would tell a person
// their approval was not recorded when it was -- the reasoning
// handleDecideApproval already applies to a failed resume, for the same
// reason.
func (i *Integration) startApprovedTraining(ctx context.Context, owner, runId, kind string, subject map[string]any) (goalId, trainingRunId string, escalated bool) {
	req, ok := trainingRequestFrom(kind, subject)
	if !ok {
		return "", "", false
	}
	if req.SpecialistId == "" {
		// The approval named no specialist. Escalating rather than guessing:
		// there is no default specialist, and picking one would train the
		// wrong agent on somebody's approval.
		i.escalateTraining(ctx, owner, runId,
			"Training was approved, but the request named no specialist to train. Re-raise it naming one.")
		return "", "", true
	}

	domainId := i.resolveSpecialistPrimaryDomain(ctx, owner, req.SpecialistId)
	if domainId == "" {
		// GATE 1. The template hard-requires a domain, so opening the goal
		// anyway would spend a run to fail inside the Trainer's tool loop with
		// the reason buried in a step result.
		i.escalateTraining(ctx, owner, runId, fmt.Sprintf(
			"Training was approved, but specialist %s has no knowledge domain attached through its skills, "+
				"so there is nothing to train into. Attach a domain-bearing skill and approve it again.",
			req.SpecialistId))
		return "", "", true
	}

	topic := req.Topic
	if topic == "" {
		topic = domainId
	}

	goalId, trainingRunId, err := i.OpenDirectGoal(ctx, DirectGoal{
		OwnerUserId:    owner,
		Statement:      fmt.Sprintf("Train specialist on %q (approved)", topic),
		AutomationName: trainingTemplate,
		Input: map[string]any{
			"domainId":     domainId,
			"specialistId": req.SpecialistId,
			"topic":        topic,
			"mode":         req.Mode,
		},
		RequestedVia: "api",
		TriggeredBy:  "user.approved",
	})
	if err != nil {
		i.log().Warn("work: an approved training request could not be opened",
			"component", "work.training", "owner", owner, "specialist", req.SpecialistId,
			"domain", domainId, "err", err)
		return "", "", false
	}
	i.log().Info("work: approved training opened",
		"component", "work.training", "owner", owner, "goal", goalId, "run", trainingRunId,
		"specialist", req.SpecialistId, "domain", domainId, "mode", req.Mode)
	return goalId, trainingRunId, false
}

// resolveSpecialistPrimaryDomain returns the first knowledge-domain id
// attached to the specialist through its skills, or "" when none resolves.
//
// The agent row carries `capabilities.skillIds` as the capability source of
// truth (#158); effective domains come from unioning those skills' bundles.
// FIRST IN skillIds ORDER, not "any": the order is the specialist's own
// declared priority, and training into an arbitrary member of a set would be
// unreproducible between two runs of the same approval.
//
// Read under the OWNER's borrowed authority, like everything else on this
// path. A read under the deciding caller would answer about their agents; a
// read under a synthetic actor would answer about nobody's.
func (i *Integration) resolveSpecialistPrimaryDomain(ctx context.Context, owner, specialistId string) string {
	as := ownerActor(ctx, owner)
	agents, err := i.store().query(as, "query "+"agentById(agentId: "+langparser.QuoteString(specialistId)+")")
	if err != nil || len(agents) == 0 {
		if err != nil {
			i.log().Warn("work: could not read the specialist named by an approved training request",
				"component", "work.training", "specialist", specialistId, "err", err)
		}
		return ""
	}
	skillIds := rowStringSlice(rowMap(agents[0], "capabilities"), "skillIds")
	if len(skillIds) == 0 {
		return ""
	}

	skills, err := i.store().query(as, "query activeSkillsFull()")
	if err != nil {
		i.log().Warn("work: could not read the skill catalog for an approved training request",
			"component", "work.training", "specialist", specialistId, "err", err)
		return ""
	}
	domainsBySkill := make(map[string][]string, len(skills))
	for _, s := range skills {
		domainsBySkill[rowString(s, "id")] = rowStringSlice(s, "domainIds")
	}
	for _, sid := range skillIds {
		for _, domain := range domainsBySkill[sid] {
			if strings.TrimSpace(domain) != "" {
				return domain
			}
		}
	}
	return ""
}

// escalateTraining raises a feedback approval on the run the request came
// from, so the person who approved training finds out why nothing started.
//
// A LOG LINE WOULD NOT DO. The approval was a person's decision, and the
// answer to it -- "nothing happened, and here is what to change" -- has to
// reach the same person. The predecessor parked its Plan at awaitingFeedback
// for exactly this; `v1:work:approval` of kind feedback is where that went.
func (i *Integration) escalateTraining(ctx context.Context, owner, runId, question string) {
	if runId == "" {
		// No run to hang it on. Nothing in the product raises a training
		// approval without one; if that changes, the log line is the record.
		i.log().Warn("work: an approved training request could not start and has no run to escalate on",
			"component", "work.training", "owner", owner, "reason", question)
		return
	}
	if _, err := i.RaiseApproval(ctx, owner, ApprovalSeed{
		RunId:    runId,
		StepKey:  trainingTemplate,
		Kind:     work.ApprovalKindFeedback,
		Question: question,
		// The subject IS the artifact here, and the hash is derived from it --
		// so re-raising the same escalation twice produces the same hash and a
		// person is not asked to re-answer a question they already answered.
		Subject: map[string]any{
			"action":   TrainingSubjectAction,
			"question": question,
		},
		Evidence: map[string]any{
			"tier":   "human",
			"reason": "training_target_unresolved",
			"source": "work.training",
		},
	}); err != nil {
		i.log().Warn("work: could not escalate an unresolvable training request",
			"component", "work.training", "owner", owner, "run", runId, "err", err)
		return
	}
	i.log().Info("work: an approved training request escalated for feedback",
		"component", "work.training", "owner", owner, "run", runId)
}

// ---------------------------------------------------------------------------
// The door
// ---------------------------------------------------------------------------

// handleRequestSpecialistTraining raises the approval the gate above acts on.
//
// WITHOUT IT THIS WHOLE FILE IS A GATE NOTHING CAN REACH, which is the failure
// mode memql#5063 is a symptom of: the mechanism survived, the gate is
// restored, and a path with no entry point is still a path nobody walks.
//
// It is the successor to the planner agent emitting `spawnTrainingPlan` from
// inside a running plan, and it keeps that shape: the request hangs on a RUN,
// because an approval with no run is a question nobody can answer
// (RaiseApproval refuses one) and because "who asked for this" is the run's
// own record.
//
// CALLER-SCOPED, and the run is the scope. The caller must be able to read the
// run through `workRunForOwner` -- the owned tier decides that, and there is
// no second check here on purpose. Requesting training against somebody else's
// run would put a decision in their inbox that they did not raise and cannot
// place.
//
// It does NOT check that the specialist has a domain. That check belongs at
// the approval, not the request: a person may well attach a domain-bearing
// skill between asking and deciding, and refusing here would make the fix
// unreachable from the surface showing the problem.
func (i *Integration) handleRequestSpecialistTraining(ctx context.Context, args map[string]any, _ int) ([]memorynodes.MemoryNode, error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return nil, err
	}
	runId := argString(args, "runId")
	specialistId := argString(args, "specialistId")
	if runId == "" || specialistId == "" {
		return nil, fmt.Errorf("work: requestSpecialistTraining needs a runId and a specialistId")
	}
	mode := argString(args, "mode")
	switch mode {
	case "":
		mode = "initial"
	case "initial", "refresh":
	default:
		// The template branches on this, so an unrecognised value would reach
		// the Trainer's prompt and be interpreted rather than refused.
		return nil, fmt.Errorf("work: training mode %q is not initial or refresh", mode)
	}

	run, err := i.store().runForOwner(ctx, runId)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("work: no run %q is readable by this caller", runId)
	}
	owner := rowString(run, "ownerUserId")

	subject := map[string]any{
		"action":       TrainingSubjectAction,
		"specialistId": specialistId,
		"topic":        argString(args, "topic"),
		"mode":         mode,
	}
	approvalId, err := i.RaiseApproval(ctx, owner, ApprovalSeed{
		RunId:   runId,
		StepKey: trainingTemplate,
		Kind:    work.ApprovalKindSkillMint,
		Subject: subject,
		// DERIVED from the subject, so the artifact-hash gate is live on this
		// kind too: a request edited between asking and deciding hashes
		// differently and the approval refuses rather than training on terms
		// nobody agreed to. currentArtifactHash passes skillMint's stored hash
		// through unchanged, which is what makes a derived one meaningful here.
		ArtifactHash: work.ArtifactHash(subject),
		Question: fmt.Sprintf("Train specialist %s on %q?", specialistId,
			firstNonEmpty(argString(args, "topic"), "its attached domain")),
		Evidence: map[string]any{
			"tier":   "human",
			"reason": "specialist_training_requested",
			"source": "work.training",
		},
	})
	if err != nil {
		return nil, err
	}
	return i.resultNode(map[string]any{
		"approvalId":   approvalId,
		"runId":        runId,
		"specialistId": specialistId,
		"mode":         mode,
	}), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
