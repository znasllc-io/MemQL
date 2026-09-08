package probe

import "strings"

// Scoring a run of the suite into the four figures a measurement carries.
//
// ===========================================================================
// THE PAIR THAT MATTERS
// ===========================================================================
// Nothing ran            -> ABSENT, naming why.
// Five ran and all failed -> a MEASURED 0.0.
//
// Those are different answers and collapsing them is the defect this package
// exists to prevent. A machine nobody probed and a model that fails every
// schema look identical on a page that renders both as zero, and they lead to
// opposite actions: one is "go and probe it", the other is "do not route
// structured work here".
//
// ===========================================================================
// A REFUSED CASE IS NOT A FAILURE
// ===========================================================================
// A 32K throughput case put to an 8K model is a question the model never
// claimed to answer. It leaves the denominator entirely rather than counting
// against the score -- counting it would penalise a model for a limit it had
// already declared, and the penalty would follow it into every ranking.

// Outcome is how one case ended.
type Outcome string

const (
	// OutcomePassed is a case that ran and satisfied its check.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed is a case that ran and did not. This COUNTS: it is the
	// measurement.
	OutcomeFailed Outcome = "failed"
	// OutcomeRefused is a case the model declined on a declared limit -- a
	// prompt over its context window. Excluded from the denominator.
	OutcomeRefused Outcome = "refused"
	// OutcomeErrored is a case that could not be scored: the runtime crashed,
	// the stream died, the answer was unreadable in a way that says nothing
	// about the model. Excluded from the denominator, and recorded, because a
	// figure scored over three cases when ten were asked is worth seeing.
	OutcomeErrored Outcome = "errored"
)

// CaseResult is one case as the machine reported it.
type CaseResult struct {
	Id      string
	Kind    Kind
	Outcome Outcome
	// Detail is the runtime's own sentence when a case errored. Never invented.
	Detail string
	// TokensPerSecond and TimeToFirstTokenMs are populated for throughput cases
	// that passed, and ignored otherwise.
	TokensPerSecond    float64
	TimeToFirstTokenMs float64
}

// Figures are the four a v1:platform:modelMeasurement row carries.
type Figures struct {
	StructuredValidity  Figure
	ToolCallCorrectness Figure
	ThroughputTps       Figure
	TimeToFirstTokenMs  Figure
}

// Validate refuses a set where any figure is neither measured nor absent.
func (f Figures) Validate() error {
	for _, fig := range []Figure{f.StructuredValidity, f.ToolCallCorrectness, f.ThroughputTps, f.TimeToFirstTokenMs} {
		if err := fig.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Score folds a run's case results into the four figures.
//
// It is total: every figure it returns is measured or names its absence, so a
// caller cannot produce an unfilled one by forgetting a branch.
func Score(results []CaseResult) Figures {
	structured := ratioOver(results, KindStructured)
	tools := ratioOver(results, KindToolCall)

	var tps, ttft []float64
	throughputSeen, throughputErrored, throughputRefused := 0, 0, 0
	var errDetail string
	for _, r := range results {
		if r.Kind != KindThroughput {
			continue
		}
		throughputSeen++
		switch r.Outcome {
		case OutcomePassed:
			if r.TokensPerSecond > 0 {
				tps = append(tps, r.TokensPerSecond)
			}
			if r.TimeToFirstTokenMs > 0 {
				ttft = append(ttft, r.TimeToFirstTokenMs)
			}
		case OutcomeRefused:
			throughputRefused++
		case OutcomeErrored, OutcomeFailed:
			throughputErrored++
			if errDetail == "" {
				errDetail = strings.TrimSpace(r.Detail)
			}
		}
	}

	return Figures{
		StructuredValidity:  structured,
		ToolCallCorrectness: tools,
		ThroughputTps:       sampleFigure(tps, throughputSeen, throughputErrored, throughputRefused, errDetail),
		TimeToFirstTokenMs:  sampleFigure(ttft, throughputSeen, throughputErrored, throughputRefused, errDetail),
	}
}

// ratioOver scores the pass rate of one kind.
//
// The denominator is passed PLUS failed, and nothing else. A refused case never
// entered the question and an errored one could not be scored, so including
// either would report a number about the harness as though it were a number
// about the model.
func ratioOver(results []CaseResult, kind Kind) Figure {
	hits, trials, errored, refused := 0, 0, 0, 0
	var errDetail string
	for _, r := range results {
		if r.Kind != kind {
			continue
		}
		switch r.Outcome {
		case OutcomePassed:
			hits++
			trials++
		case OutcomeFailed:
			trials++
		case OutcomeRefused:
			refused++
		case OutcomeErrored:
			errored++
			if errDetail == "" {
				errDetail = strings.TrimSpace(r.Detail)
			}
		}
	}
	if trials == 0 {
		return absenceFor(len(results) == 0, errored, refused, errDetail)
	}
	f, err := MeasuredRatio(hits, trials)
	if err != nil {
		return Absent(AbsentUnmeasured, "")
	}
	return f
}

// sampleFigure scores a set of observations, or names the absence.
func sampleFigure(samples []float64, seen, errored, refused int, errDetail string) Figure {
	if len(samples) == 0 {
		return absenceFor(seen == 0, errored, refused, errDetail)
	}
	f, err := Measured(samples)
	if err != nil {
		return Absent(AbsentFailed, err.Error())
	}
	return f
}

// absenceFor picks WHICH absence, and the order of the checks is the order of
// specificity: a probe that never reached these cases is unmeasured, one whose
// cases the model declined is refused, one whose cases blew up is failed.
//
// The default is `unmeasured` rather than `failed`, deliberately. Absence with
// no evidence of a failure is not a failure, and reporting it as one would put
// a model's name beside a crash it never had.
func absenceFor(nothingRan bool, errored, refused int, detail string) Figure {
	switch {
	case nothingRan:
		return Absent(AbsentUnmeasured, "")
	case errored > 0:
		return Absent(AbsentFailed, detail)
	case refused > 0:
		return Absent(AbsentRefused, "the model declined every case at this size, which its declared context window explains")
	default:
		return Absent(AbsentUnmeasured, "")
	}
}
