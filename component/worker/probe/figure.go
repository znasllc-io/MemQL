// Package probe is the model probe's versioned suite and its scoring
// (epic memql#5146, design D3).
//
// ===========================================================================
// WHAT A PROBE IS FOR
// ===========================================================================
// A pull says a model is on the disk. Whether that model can serve a LEVEL is
// asserted today by a label the cockpit wrote from `ollama show`, and never
// checked. A model's parameter count says nothing about whether its structured
// output validates against the schemas this cluster actually sends, and a
// 4-bit quantization of one model is not the 8-bit of another.
//
// So the suite measures the cluster's OWN question: structured-output validity
// over five schemas drawn from the platform's own prompts, tool-call
// correctness over three tool definitions, and throughput plus time to first
// token at two prompt sizes.
//
// ===========================================================================
// WHY THE FIGURE TYPE IS HERE AND NOT IMPORTED
// ===========================================================================
// component/proving/figure made these decisions first and made them in Go:
// a figure is a Stat or an AbsentReason, never both and never neither; there
// is no mean and no single-number constructor; an absence names its reason.
// This package holds the same discipline in its own type rather than importing
// that one, and the reason is the module graph: component/proving is in the
// ROOT module, which already requires component/worker, so the import would
// close a cycle between two modules.
//
// It is deliberately NOT a copy of that type. The proving figure carries a
// tier, a family, an arm and provenance, because it answers "what did the
// platform measure about itself". This one answers "what did one model do on
// one machine", and the provenance for that is the measurement ROW's key --
// machine, model, suite version, date -- not fields on the number.
//
// What IS shared is the rule, and the rule is what the tests here assert:
// unmeasured and zero are different answers, and a structure that can express
// neither-nor-both is one where some caller eventually renders a zero for a
// probe that never ran. On a page that reads exactly like a slow machine.
package probe

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// AbsentReason is why a figure has no number. Closed: a new reason is a design
// decision, and one mapped onto the nearest existing value is exactly the lie
// this type exists to prevent.
type AbsentReason string

const (
	// AbsentUnmeasured is "the suite did not run this case". Not an error and
	// not a zero.
	AbsentUnmeasured AbsentReason = "unmeasured"
	// AbsentRefused is "the model was asked something it never claimed to
	// answer" -- the 32K case put to an 8K model. It is NOT a failure, and
	// counting it as one would drag a validity score down for a limit the
	// model had already declared.
	AbsentRefused AbsentReason = "refused"
	// AbsentFailed is "the case ran and could not be scored" -- the runtime
	// crashed, the stream died, the output was unparseable in a way that says
	// nothing about the model's validity.
	AbsentFailed AbsentReason = "failed"
)

// Valid reports whether r is one of the three.
func (r AbsentReason) Valid() bool {
	return r == AbsentUnmeasured || r == AbsentRefused || r == AbsentFailed
}

// Stat is a measured figure: a median, the spread around it, and the sample
// size that produced them.
//
// THERE IS NO MEAN AND NO SINGLE-NUMBER CONSTRUCTOR, which is
// component/proving/figure's decision carried here for its reason: a caller who
// wants one number gets the median and is handed the spread with it, because
// "medians and spread, never a best case" is easier to keep in a struct than in
// a review comment.
type Stat struct {
	Median float64
	// SpreadLow and SpreadHigh are the interquartile bounds. Equal to the
	// median for a sample of one, which is honest -- one observation has no
	// spread -- rather than absent.
	SpreadLow  float64
	SpreadHigh float64
	// N is the sample size. On the measured side only: a median with no count
	// behind it is a number a reader cannot weigh.
	N int
}

// Figure is a measured Stat or the reason there is not one. Never both, never
// neither -- enforced by the constructors and asserted by Validate.
type Figure struct {
	stat   *Stat
	reason AbsentReason
	detail string
}

// Measured builds a figure from a sample.
//
// AN EMPTY SAMPLE IS REFUSED rather than producing a zero: nothing measured is
// an absence, and the caller has to say which absence it is.
func Measured(samples []float64) (Figure, error) {
	if len(samples) == 0 {
		return Figure{}, errors.New("probe: a measured figure needs at least one sample; an empty one is an absence and must name its reason")
	}
	sorted := make([]float64, 0, len(samples))
	for _, s := range samples {
		if math.IsNaN(s) || math.IsInf(s, 0) {
			// A non-finite sample is what arithmetic over a missing field
			// produces, and letting one through puts NaN on a pixel.
			return Figure{}, fmt.Errorf("probe: sample %v is not finite", s)
		}
		sorted = append(sorted, s)
	}
	sort.Float64s(sorted)
	return Figure{stat: &Stat{
		Median:     median(sorted),
		SpreadLow:  quantile(sorted, 0.25),
		SpreadHigh: quantile(sorted, 0.75),
		N:          len(sorted),
	}}, nil
}

// MeasuredRatio builds a figure for a pass rate: hits out of trials.
//
// A ratio has no spread of its own -- it is one number over a whole run -- so
// the bounds equal the median and N is the trial count. Reporting it as a
// sample of trials would invent a distribution nobody measured.
//
// ZERO TRIALS IS AN ABSENCE, and zero hits out of some trials is a MEASURED
// ZERO. Those are the two answers this whole type exists to keep apart: five
// cases that all failed is a real and useful 0.0, and it must not read as a
// model nobody probed.
func MeasuredRatio(hits, trials int) (Figure, error) {
	if trials <= 0 {
		return Figure{}, errors.New("probe: a ratio over zero trials is an absence, not a zero")
	}
	r := float64(hits) / float64(trials)
	return Figure{stat: &Stat{Median: r, SpreadLow: r, SpreadHigh: r, N: trials}}, nil
}

// Absent builds a figure that names why there is no number.
func Absent(reason AbsentReason, detail string) Figure {
	if !reason.Valid() {
		reason = AbsentUnmeasured
	}
	return Figure{reason: reason, detail: detail}
}

// IsMeasured reports whether the figure carries a number.
func (f Figure) IsMeasured() bool { return f.stat != nil }

// Stat returns the measured statistics, and whether there are any.
func (f Figure) Stat() (Stat, bool) {
	if f.stat == nil {
		return Stat{}, false
	}
	return *f.stat, true
}

// Reason returns the absence's reason and detail, and whether the figure is
// absent at all.
func (f Figure) Reason() (AbsentReason, string, bool) {
	if f.stat != nil {
		return "", "", false
	}
	return f.reason, f.detail, true
}

// Validate refuses a figure that is neither measured nor absent.
//
// The zero value of Figure is exactly that -- no stat, no reason -- and it is
// reachable by anyone who declares a `var f Figure` or leaves a struct field
// unset. Every path that writes a figure to a row runs this first, so the
// unfilled case is an error at the seam rather than a blank on a page.
func (f Figure) Validate() error {
	if f.stat != nil {
		if f.reason != "" {
			return errors.New("probe: a figure carries a stat AND a reason; it must carry exactly one")
		}
		return nil
	}
	if !f.reason.Valid() {
		return errors.New("probe: a figure carries neither a stat nor a valid absent reason; unmeasured is a value and must be said")
	}
	return nil
}

// Row renders a figure in the shape v1:platform:modelMeasurement stores and the
// wire carries.
//
// The two halves never both appear. A reader that switches on `measured` sees
// only the fields that mean anything for that branch, which is what stops the
// familiar bug: a `median` of 0 sitting beside `measured: false`, read by
// something that forgot to check the flag.
func (f Figure) Row() map[string]any {
	if s, ok := f.Stat(); ok {
		return map[string]any{
			"measured":   true,
			"median":     s.Median,
			"spreadLow":  s.SpreadLow,
			"spreadHigh": s.SpreadHigh,
			"n":          s.N,
		}
	}
	reason := f.reason
	if !reason.Valid() {
		reason = AbsentUnmeasured
	}
	return map[string]any{
		"measured":     false,
		"absentReason": string(reason),
		"absentDetail": f.detail,
	}
}

// FigureFromRow reads a stored figure back.
//
// A row that is missing, malformed, or claims to be measured without a median
// reads as ABSENT with `unmeasured`. That direction is deliberate: a corrupt
// figure must not become a number, because a number is what gets ranked on.
func FigureFromRow(v any) Figure {
	row, ok := v.(map[string]any)
	if !ok || len(row) == 0 {
		return Absent(AbsentUnmeasured, "")
	}
	measured, _ := row["measured"].(bool)
	if !measured {
		reason := AbsentReason(stringOf(row["absentReason"]))
		if !reason.Valid() {
			reason = AbsentUnmeasured
		}
		return Absent(reason, stringOf(row["absentDetail"]))
	}
	median, ok := floatOf(row["median"])
	if !ok {
		return Absent(AbsentUnmeasured, "")
	}
	low, okLow := floatOf(row["spreadLow"])
	high, okHigh := floatOf(row["spreadHigh"])
	if !okLow {
		low = median
	}
	if !okHigh {
		high = median
	}
	n, _ := floatOf(row["n"])
	return Figure{stat: &Stat{Median: median, SpreadLow: low, SpreadHigh: high, N: int(n)}}
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// quantile is the nearest-rank quantile, which needs no interpolation and
// therefore cannot report a value no observation produced.
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := int(math.Round(q * float64(len(sorted)-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func floatOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
