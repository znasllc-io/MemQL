package proving

// classifier.go -- the symptom classifier the proving suite installs on its
// executor (epic memql#5127, design D12).
//
// # Why it has to be here at all
//
// The recovery family's headline claim is a COUNT: a failure the deterministic
// rules table classifies costs ZERO provider calls, and one it cannot costs
// exactly one. Neither half is measurable unless a classifier is actually
// wired -- with none installed, the failure path never reaches a model on ANY
// path, so `recovery.modelCalls` is zero for every scenario and the zero-claim
// passes for the wrong reason. That is the "a counter that never rises on any
// path reads as zero forever" failure the suite's own design names.
//
// So this serves the classifier from the SAME cassette the reasoning steps
// use, which makes the call show up in `RecordedResponses` exactly as a
// reasoning step's does. The control scenario -- a failure whose message no
// rule matches -- must then report one, and it fails the suite if it reports
// none.
//
// # What it deliberately is not
//
// It is not a model, and it is not a stub that answers cleverly. It plays back
// a recorded verdict, because what is being measured is HOW MANY TIMES the
// path reaches a model, not how good the verdict is. A classifier that decided
// anything here would make the count depend on its decisions.

import (
	"context"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/automations"
	"github.com/znasllc-io/memql/component/proving/cassette"
	"github.com/znasllc-io/memql/component/work"
	"github.com/znasllc-io/memql/core/airoute"
)

// provingClassifier satisfies automations.SymptomClassifier.
type provingClassifier struct{ player *cassette.Player }

// Level mirrors the production classifier's. It is asserted rather than
// assumed: the cheapest tier is a property of the design, and a proving suite
// that measured a classifier running at a different level would be measuring
// something the platform does not do.
func (c *provingClassifier) Level() airoute.Level { return airoute.LevelFast }

func (c *provingClassifier) Classify(_ context.Context, in automations.ClassifySymptomInput) (work.Symptom, work.Evidence, error) {
	if c == nil || c.player == nil {
		// No cassette on this arm. Answering anyway would invent a provider
		// call that never happened, which is the one thing this file exists
		// not to do.
		return work.SymptomNone, work.Evidence{}, fmt.Errorf("proving: the classifier was reached and no cassette is loaded for this arm")
	}
	turn, err := c.player.Serve(c.player.ModelId(), classifierPrompt(in))
	if err != nil {
		return work.SymptomNone, work.Evidence{}, err
	}
	sym := work.Symptom(strings.TrimSpace(turn.Response))
	if !sym.Valid() || sym == work.SymptomNone {
		// A recorded response outside the enum is a cassette problem, and it
		// must not become a verdict: the production classifier refuses the
		// same way, and a suite that was more forgiving would measure a path
		// the platform does not take.
		return work.SymptomNone, work.Evidence{}, fmt.Errorf("proving: the cassette answered %q, which is not one of the five symptoms", turn.Response)
	}
	return sym, work.Evidence{
		Tier:   "classified",
		Reason: "played back from the cassette",
		Source: work.EvidenceSourceModel,
	}, nil
}

// classifierPrompt is the cassette key for one classification. It carries the
// step and the message and nothing else: a key that varied with a timestamp or
// a run id would miss on every replay, which would read as a provider being
// unavailable rather than as a key that cannot match.
func classifierPrompt(in automations.ClassifySymptomInput) string {
	return "classifySymptom:" + in.StepKey + ":" + in.ErrorMessage
}
