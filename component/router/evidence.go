package router

// THE EVIDENCE FOLD'S DECISIONS (epic memql#5146, design D5).
//
// ===========================================================================
// IT PROPOSES AND NEVER APPLIES
// ===========================================================================
// A week of real calls says more about a model than any probe can, and the
// obvious thing to do with that is demote automatically. This does not, for two
// reasons that are separate and both sufficient.
//
// The first is the never-a-silent-edit rule: routing is a thing a person is
// accountable for, and a rule that appeared overnight because a threshold was
// crossed is an edit nobody made. The second is smaller and more practical -- a
// week of bad luck on one schema is not a reason to route a whole level
// elsewhere, and the evidence cannot tell the two apart.
//
// So the fold opens an approval carrying a rule, hashed over its exact text,
// and a person decides. Declining is RECORDED, which is the difference between
// a proposal and a nag.
//
// ===========================================================================
// THERE IS EXACTLY ONE RENDERER OF THE RULE GRAMMAR, AND IT IS NOT HERE
// ===========================================================================
// The approval's guarantee is that an approved decision cannot carry to a
// different rule, and it rests on hashing the rendered SOURCE. Two renderers of
// one grammar drift, and with a hash the drift is silent: a person approves
// text A and text B is armed.
//
// So the renderer is INJECTED. This package produces a Form and hashes whatever
// the renderer returns; the tree's one renderer fills it in at the call site.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// MinimumCalls is the floor below which a week's evidence proposes nothing.
//
// TWENTY, and the row records the count even when it is under -- so an operator
// asking why nothing was proposed reads the answer rather than inferring it.
// Nineteen calls all failing is not evidence; it is a bad afternoon.
const MinimumCalls = 20

// FailureThreshold is the structured-failure rate above which a demotion is
// proposed. EXCEEDS, not meets: a boundary decided by nothing is a boundary two
// replicas can disagree about.
const FailureThreshold = 0.3

// PromotionThreshold is the measured structured validity at or above which an
// EXCLUDED model is proposed for promotion back.
//
// It is deliberately not `1 - FailureThreshold`. The two numbers measure
// different things -- one is a failure rate in service, the other a validity
// rate on the probe suite -- and tying them together would make a change to one
// silently move the other.
const PromotionThreshold = 0.9

// The four levels, mirroring the closed set the catalog and the rule grammar
// share. A fifth here that the grammar does not know would render a rule that
// refuses to load.
var evidenceLevels = []string{"fast", "strong", "reasoning", "embeddings"}

// Window is one model's week at one level, as the fold counted it.
type Window struct {
	ModelId string
	Level   string
	// Week is the ISO week as YYYY-Www. It is a KEY: one row per model per
	// level per week, so two folds of the same week land on one row.
	Week string

	Calls              int
	StructuredFailures int
	Repairs            int
	Retries            int
	Parks              int
}

// FailureRate is the structured-failure rate, or zero when nothing was counted.
//
// ZERO FOR AN EMPTY WINDOW, and it is safe here only because MinimumCalls gates
// every caller: a rate over no calls is not a rate, and the floor is what stops
// this being read as "this model never fails".
func (w Window) FailureRate() float64 {
	if w.Calls <= 0 {
		return 0
	}
	return float64(w.StructuredFailures) / float64(w.Calls)
}

// Valid reports whether a window is complete enough to reason about.
//
// A window with no level is the one this returns false for most often, and it
// is the case that matters: a call that did not say which level it was serving
// cannot support a demotion AT a level, and demoting at every level instead
// would be a far heavier act than the evidence supports.
func (w Window) Valid() bool {
	return strings.TrimSpace(w.ModelId) != "" &&
		validEvidenceLevel(w.Level) &&
		strings.TrimSpace(w.Week) != ""
}

func validEvidenceLevel(level string) bool {
	for _, l := range evidenceLevels {
		if level == l {
			return true
		}
	}
	return false
}

// Form mirrors component/routingrules.Form field for field.
//
// It is a MIRROR rather than an import because that package is epic
// memql#5127's and is not on this branch's base; the rebase deletes this type
// and imports the real one. Until then TestEvidenceFormMatchesTheRuleGrammar
// pins the field list, so the swap is a deletion rather than a redesign.
type Form struct {
	Name        string
	Description string
	// When carries ONLY the condition keys the proposal actually sets.
	//
	// A key present with an EMPTY value is a condition matching only an empty
	// value; a key ABSENT is no condition at all. Emitting every key with "" for
	// the blanks produces a rule that silently never fires -- which on this path
	// would be a demotion a person approved and that did nothing.
	When          map[string]string
	Policy        string
	Level         string
	Precedence    int
	OnUnavailable string
	Excludes      []string
}

// Renderer turns a Form into the rule source that will be armed.
//
// Injected; see the file header. The tree's one renderer is
// component/routingrules.GenerateRule, which is stable across map iteration
// precisely so a re-render of an unchanged rule is byte-identical -- which is
// what makes hashing its output safe.
type Renderer func(Form) (string, error)

// Direction is what a proposal asks for.
const (
	// DirectionDemotion excludes a model at a level.
	DirectionDemotion = "demotion"
	// DirectionPromotion removes an exclusion.
	DirectionPromotion = "promotion"
)

// Proposal is one generated rule, ready to be put to a person.
type Proposal struct {
	ModelId   string
	Level     string
	Week      string
	Direction string
	RuleName  string
	// RuleSource is what will actually be armed.
	RuleSource string
	// Hash is over RuleSource, not over the Form. An approval is a decision
	// about one specific rule TEXT, and hashing the inputs would let a renderer
	// change move the text under an approval that still verified.
	Hash string
	// Reason is the sentence the approval puts to the person. It carries the
	// numbers, because "this model is failing" is not a decidable claim and
	// "17 of 41 structured calls failed last week" is.
	Reason string
}

// ProposeExclusion decides whether a week's evidence warrants a demotion, and
// renders it when it does.
//
// The three refusals in order: an incomplete window (usually one with no
// level), a window under the call floor, and a window at or below the
// threshold. Each returns ok=false with no error -- a fold that proposes
// nothing is the ordinary case, and reporting it as a failure would make the
// nightly log unreadable and the real failures invisible in it.
func ProposeExclusion(w Window, render Renderer) (Proposal, bool, error) {
	if render == nil {
		return Proposal{}, false, fmt.Errorf("router: an evidence proposal needs a rule renderer; there must be exactly one in the tree and this call did not supply it")
	}
	if !w.Valid() || w.Calls < MinimumCalls || w.FailureRate() <= FailureThreshold {
		return Proposal{}, false, nil
	}

	form := exclusionForm(w)
	source, err := render(form)
	if err != nil {
		return Proposal{}, false, fmt.Errorf("router: render the proposed rule: %w", err)
	}
	return Proposal{
		ModelId:    w.ModelId,
		Level:      w.Level,
		Week:       w.Week,
		Direction:  DirectionDemotion,
		RuleName:   form.Name,
		RuleSource: source,
		Hash:       ProposalHash(source),
		Reason: fmt.Sprintf(
			"%d of %d structured calls to %s at level %s failed in %s (%.0f%%), over the %d-call floor and above the %.0f%% threshold. Approving adds a rule excluding it at that level; declining records the decision so this week's evidence does not ask again.",
			w.StructuredFailures, w.Calls, w.ModelId, w.Level, w.Week,
			w.FailureRate()*100, MinimumCalls, FailureThreshold*100),
	}, true, nil
}

// ProposePromotion is the symmetric case: an EXCLUDED model whose newer
// measurement passes.
//
// It reads a MEASUREMENT rather than a week of calls, and that asymmetry is
// forced rather than chosen: an excluded model serves no calls, so there is no
// service evidence to fold. The probe is the only thing that can speak for it,
// which is also why the threshold is a validity rate and not the complement of
// the failure rate.
func ProposePromotion(w Window, measuredValidity float64, hasMeasurement bool, render Renderer) (Proposal, bool, error) {
	if render == nil {
		return Proposal{}, false, fmt.Errorf("router: an evidence proposal needs a rule renderer; there must be exactly one in the tree and this call did not supply it")
	}
	if !w.Valid() || !hasMeasurement || measuredValidity < PromotionThreshold {
		return Proposal{}, false, nil
	}

	form := promotionForm(w)
	source, err := render(form)
	if err != nil {
		return Proposal{}, false, fmt.Errorf("router: render the proposed rule: %w", err)
	}
	return Proposal{
		ModelId:    w.ModelId,
		Level:      w.Level,
		Week:       w.Week,
		Direction:  DirectionPromotion,
		RuleName:   form.Name,
		RuleSource: source,
		Hash:       ProposalHash(source),
		Reason: fmt.Sprintf(
			"%s is excluded at level %s, and a probe since then measured %.0f%% structured validity -- at or above the %.0f%% bar. Approving retires the exclusion; declining records the decision so this week's evidence does not ask again.",
			w.ModelId, w.Level, measuredValidity*100, PromotionThreshold*100),
	}, true, nil
}

// ProposalHash is the artifact hash an approval carries.
//
// OVER THE RENDERED SOURCE, so it covers what will actually be armed. Hashing
// the Form would let a renderer change move the text under an approval that
// still verified -- which is the one failure the hash exists to prevent, and
// the one nobody would notice.
func ProposalHash(ruleSource string) string {
	sum := sha256.Sum256([]byte(ruleSource))
	return hex.EncodeToString(sum[:])
}

// RuleName is the generated rule's name, derived from the model and level.
//
// DERIVED rather than random, so a second fold of the same evidence produces
// the same name and the authoring pipeline treats it as the same rule rather
// than as a second one beside it.
func RuleName(direction, modelId, level string) string {
	// The direction is capitalised by hand rather than through strings.Title,
	// which is deprecated and, more to the point, would apply Unicode word
	// rules to a value from a closed two-word set.
	word := direction
	if word != "" {
		word = strings.ToUpper(word[:1]) + word[1:]
	}
	return "evidence" + word + "_" + slugForRule(modelId) + "_" + level
}

func exclusionForm(w Window) Form {
	return Form{
		Name: RuleName(DirectionDemotion, w.ModelId, w.Level),
		Description: fmt.Sprintf(
			"Generated from %s evidence: %d of %d structured calls to %s at level %s failed.",
			w.Week, w.StructuredFailures, w.Calls, w.ModelId, w.Level),
		// ONLY the key that is set. `level` is the whole of the condition: the
		// evidence is per level, so a rule that also pinned a prompt or a role
		// would exclude the model somewhere narrower than the evidence covers.
		When:          map[string]string{"level": w.Level},
		Policy:        "localFirst",
		Level:         w.Level,
		Precedence:    50,
		OnUnavailable: "degrade",
		Excludes:      []string{"fleet:" + w.ModelId},
	}
}

func promotionForm(w Window) Form {
	return Form{
		Name: RuleName(DirectionPromotion, w.ModelId, w.Level),
		Description: fmt.Sprintf(
			"Generated from a probe: %s measured well enough at level %s to be routed again.",
			w.ModelId, w.Level),
		When:          map[string]string{"level": w.Level},
		Policy:        "localFirst",
		Level:         w.Level,
		Precedence:    50,
		OnUnavailable: "degrade",
		// EMPTY, not absent-of-a-field. A promotion is the same rule with
		// nothing excluded, which is what retires the exclusion when the
		// authoring pipeline re-arms it under the same name.
		Excludes: nil,
	}
}

// slugForRule reduces a model tag to something a rule name may carry.
//
// A tag holds ':' and '/' and a rule name is an identifier, so the translation
// is unavoidable -- and it is one-way BY DESIGN: nothing reads the model back
// out of a rule name. The model id travels on the evidence row, where it stays
// byte-identical to what the router selects on.
func slugForRule(modelId string) string {
	var b strings.Builder
	for _, r := range modelId {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// SortWindows orders windows so a fold writes them in a stable order.
//
// Two replicas will not both run the fold -- it is on the cron leader -- but a
// re-run after a failure should produce the same sequence of writes as the run
// it is replacing, or the rows carry an ordering that means nothing.
func SortWindows(ws []Window) {
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].ModelId != ws[j].ModelId {
			return ws[i].ModelId < ws[j].ModelId
		}
		return levelRank(ws[i].Level) < levelRank(ws[j].Level)
	})
}

func levelRank(level string) int {
	for i, l := range evidenceLevels {
		if l == level {
			return i
		}
	}
	return len(evidenceLevels)
}
