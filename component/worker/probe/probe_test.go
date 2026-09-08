package probe

import (
	"strings"
	"testing"
)

func passed(id string, kind Kind) CaseResult {
	return CaseResult{Id: id, Kind: kind, Outcome: OutcomePassed}
}

func failed(id string, kind Kind) CaseResult {
	return CaseResult{Id: id, Kind: kind, Outcome: OutcomeFailed}
}

// --- the suite ---------------------------------------------------------------

func TestSuiteIsPinnedAndAnUnknownVersionIsAnError(t *testing.T) {
	// An unknown version must not fall back to the pinned suite. Falling back
	// hands a cockpit that asked for suite 2 the cases of suite 1 and lets it
	// report the results as suite 2 -- the exact confusion the version exists
	// to prevent, arriving through the code that implements it.
	if _, err := Suite("2"); err == nil {
		t.Fatal("an unknown suite version must be an error, never a fallback")
	} else if !strings.Contains(err.Error(), SuiteVersion) {
		t.Fatalf("the error must name what this engine pins, got %q", err)
	}
	got, err := Suite(SuiteVersion)
	if err != nil {
		t.Fatalf("the pinned version must resolve: %v", err)
	}
	if len(got) != len(Cases()) {
		t.Fatalf("suite length %d, want %d", len(got), len(Cases()))
	}
}

func TestTheSuiteMeasuresThisClustersOwnQuestion(t *testing.T) {
	// Five schemas drawn from the platform's own prompts, three tool
	// definitions, two prompt sizes. The counts are asserted because the
	// argument for having a probe at all is that it checks OUR schemas -- a
	// suite that quietly lost its structured cases would keep passing while
	// measuring nothing anybody decided on.
	counts := CountByKind()
	if counts[KindStructured] != 5 {
		t.Fatalf("structured cases: %d, want 5", counts[KindStructured])
	}
	if counts[KindToolCall] != 3 {
		t.Fatalf("tool cases: %d, want 3", counts[KindToolCall])
	}
	if counts[KindThroughput] != 2 {
		t.Fatalf("throughput cases: %d, want 2", counts[KindThroughput])
	}
	sizes := map[int]bool{}
	for _, c := range Cases() {
		if c.Kind == KindThroughput {
			sizes[c.PromptTokens] = true
		}
	}
	if !sizes[8192] || !sizes[32768] {
		t.Fatalf("the throughput cases must be 8K and 32K, got %v", sizes)
	}
}

func TestEveryCaseIdIsUniqueAndNonEmpty(t *testing.T) {
	// The id is the join key between a progress message and a case, and it is
	// compared across releases in a measurement's history. A duplicate would
	// make one case's result overwrite another's silently.
	seen := map[string]bool{}
	for _, c := range Cases() {
		if strings.TrimSpace(c.Id) == "" {
			t.Fatal("a case with no id cannot be joined to its progress message")
		}
		if seen[c.Id] {
			t.Fatalf("duplicate case id %q", c.Id)
		}
		seen[c.Id] = true
	}
}

func TestStructuredCasesNameASchemaAndToolCasesNameATool(t *testing.T) {
	// A case whose kind and payload disagree is one the cockpit cannot run,
	// and it would fail per-machine at probe time rather than here.
	for _, c := range Cases() {
		switch c.Kind {
		case KindStructured:
			if c.Schema == "" {
				t.Fatalf("%s is structured and names no schema", c.Id)
			}
		case KindToolCall:
			if c.Tool == "" {
				t.Fatalf("%s is a tool case and names no tool", c.Id)
			}
		case KindThroughput:
			if c.PromptTokens <= 0 {
				t.Fatalf("%s is a throughput case and names no prompt size", c.Id)
			}
		default:
			t.Fatalf("%s has an unknown kind %q", c.Id, c.Kind)
		}
	}
}

// --- the figure discipline ---------------------------------------------------

func TestAFigureIsNeverBothAndNeverNeither(t *testing.T) {
	// The zero value is "neither", and it is reachable by anyone who declares a
	// var or leaves a struct field unset. Validate is what turns that into an
	// error at the seam rather than a blank on a page.
	var unset Figure
	if err := unset.Validate(); err == nil {
		t.Fatal("the zero figure is neither measured nor absent and must be refused")
	}
	f, err := Measured([]float64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("a measured figure must validate: %v", err)
	}
	if err := Absent(AbsentFailed, "the runtime crashed").Validate(); err != nil {
		t.Fatalf("an absent figure with a reason must validate: %v", err)
	}
}

func TestAnEmptySampleIsRefusedRatherThanScoredAsZero(t *testing.T) {
	if _, err := Measured(nil); err == nil {
		t.Fatal("nothing measured is an absence and the caller must say which one")
	}
}

func TestANonFiniteSampleNeverReachesAFigure(t *testing.T) {
	// NaN is what arithmetic over a missing field produces, and letting one
	// through puts it on a pixel.
	nan := 0.0
	nan = nan / nan
	if _, err := Measured([]float64{1, nan}); err == nil {
		t.Fatal("a non-finite sample must be refused")
	}
}

func TestASampleOfOneHasNoSpreadRatherThanAnAbsentOne(t *testing.T) {
	// One observation genuinely has no spread. Equal bounds say that honestly;
	// an absent spread beside a present median would be a third state nothing
	// downstream knows how to render.
	f, err := Measured([]float64{42})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := f.Stat()
	if s.Median != 42 || s.SpreadLow != 42 || s.SpreadHigh != 42 || s.N != 1 {
		t.Fatalf("got %+v", s)
	}
}

func TestARowCarriesOnlyOneHalf(t *testing.T) {
	// A `median` of 0 sitting beside `measured: false` is the familiar bug:
	// something forgets to check the flag. Neither half is written when the
	// other one is.
	m, _ := Measured([]float64{5})
	row := m.Row()
	if _, present := row["absentReason"]; present {
		t.Fatalf("a measured figure must not carry an absent reason: %#v", row)
	}
	a := Absent(AbsentRefused, "over the context window").Row()
	if _, present := a["median"]; present {
		t.Fatalf("an absent figure must not carry a median: %#v", a)
	}
}

func TestAFigureRoundTripsThroughTheStoredShape(t *testing.T) {
	m, _ := Measured([]float64{10, 20, 30})
	back := FigureFromRow(m.Row())
	got, ok := back.Stat()
	if !ok {
		t.Fatal("a measured figure must read back as measured")
	}
	want, _ := m.Stat()
	if got != want {
		t.Fatalf("%+v vs %+v", got, want)
	}

	a := Absent(AbsentFailed, "the runtime crashed")
	reason, detail, absent := FigureFromRow(a.Row()).Reason()
	if !absent || reason != AbsentFailed || detail != "the runtime crashed" {
		t.Fatalf("absent figure did not round-trip: %v %q %v", reason, detail, absent)
	}
}

func TestACorruptRowReadsAsAbsentRatherThanAsANumber(t *testing.T) {
	// The fail-safe direction. A number is what gets ranked on, so a figure
	// nobody can read must not become one.
	for _, row := range []any{
		nil,
		map[string]any{},
		map[string]any{"measured": true}, // no median
		map[string]any{"measured": true, "median": "not a num"}, // wrong type
	} {
		if FigureFromRow(row).IsMeasured() {
			t.Fatalf("a corrupt figure must read as absent, got measured from %#v", row)
		}
	}
}

// --- scoring -----------------------------------------------------------------

func TestScoreOfNothingIsAbsentNotZero(t *testing.T) {
	got := Score(nil)
	if err := got.Validate(); err != nil {
		t.Fatalf("Score must be total: %v", err)
	}
	for name, f := range map[string]Figure{
		"structuredValidity":  got.StructuredValidity,
		"toolCallCorrectness": got.ToolCallCorrectness,
		"throughputTps":       got.ThroughputTps,
		"timeToFirstTokenMs":  got.TimeToFirstTokenMs,
	} {
		if f.IsMeasured() {
			t.Fatalf("%s must be absent when nothing ran", name)
		}
		reason, _, _ := f.Reason()
		if reason != AbsentUnmeasured {
			t.Fatalf("%s: absence must be `unmeasured` when nothing ran, got %q", name, reason)
		}
	}
}

func TestScoreOfAllFailedStructuredCasesIsAMeasuredZero(t *testing.T) {
	// THE OTHER HALF OF THE PAIR. Five cases ran and five failed is a real and
	// useful 0.0 -- it says do not route structured work to this model. It must
	// not read as a model nobody probed, which is the opposite instruction.
	var results []CaseResult
	for _, c := range Cases() {
		if c.Kind == KindStructured {
			results = append(results, failed(c.Id, c.Kind))
		}
	}
	f := Score(results).StructuredValidity
	s, ok := f.Stat()
	if !ok {
		t.Fatal("five cases that ran and failed is a MEASURED zero, not an absence")
	}
	if s.Median != 0 {
		t.Fatalf("validity %v, want 0", s.Median)
	}
	if s.N != 5 {
		t.Fatalf("the sample size must be the five cases that ran, got %d", s.N)
	}
}

func TestARefusedCaseLeavesTheDenominator(t *testing.T) {
	// A 32K case put to an 8K model is a question the model never claimed to
	// answer. Counting it as a failure would penalise a model for a limit it
	// had already declared, and the penalty would follow it into every ranking.
	results := []CaseResult{
		passed("structured.triage", KindStructured),
		passed("structured.intake", KindStructured),
		{Id: "structured.symptom", Kind: KindStructured, Outcome: OutcomeRefused},
	}
	s, ok := Score(results).StructuredValidity.Stat()
	if !ok {
		t.Fatal("two cases ran; that is measured")
	}
	if s.Median != 1 {
		t.Fatalf("validity %v, want 1 -- the refused case must not be in the denominator", s.Median)
	}
	if s.N != 2 {
		t.Fatalf("sample size %d, want 2", s.N)
	}
}

func TestAnErroredCaseIsAbsentWithTheRuntimesOwnSentence(t *testing.T) {
	// A probe whose runtime crashed reports an ABSENT figure naming the crash,
	// and nothing is ranked on it. The sentence is the machine's; the engine
	// never invents one.
	results := []CaseResult{
		{Id: "structured.triage", Kind: KindStructured, Outcome: OutcomeErrored, Detail: "ollama: model runner exited"},
	}
	f := Score(results).StructuredValidity
	if f.IsMeasured() {
		t.Fatal("a case that could not be scored is not a measurement")
	}
	reason, detail, _ := f.Reason()
	if reason != AbsentFailed {
		t.Fatalf("reason %q, want %q", reason, AbsentFailed)
	}
	if detail != "ollama: model runner exited" {
		t.Fatalf("the detail must be the runtime's own sentence, got %q", detail)
	}
}

func TestAnAbsenceWithNoEvidenceOfFailureIsUnmeasuredNotFailed(t *testing.T) {
	// Reporting `failed` by default would put a model's name beside a crash it
	// never had, on a page an operator reads to decide whether to keep using
	// it.
	results := []CaseResult{passed("throughput.8k", KindThroughput)} // no tps reported
	f := Score(results).ThroughputTps
	reason, _, _ := f.Reason()
	if f.IsMeasured() {
		t.Fatal("a throughput case that reported no rate measured nothing")
	}
	if reason != AbsentUnmeasured {
		t.Fatalf("reason %q, want %q", reason, AbsentUnmeasured)
	}
}

func TestThroughputCarriesSpreadAndTheTwoSizesAreOneFigure(t *testing.T) {
	// The 8K and 32K observations are one figure with a spread rather than two
	// numbers, because the question a router asks is "how fast is this model
	// here", and a single best case would answer it with the short prompt.
	results := []CaseResult{
		{Id: "throughput.8k", Kind: KindThroughput, Outcome: OutcomePassed, TokensPerSecond: 60, TimeToFirstTokenMs: 120},
		{Id: "throughput.32k", Kind: KindThroughput, Outcome: OutcomePassed, TokensPerSecond: 20, TimeToFirstTokenMs: 900},
	}
	got := Score(results)
	tps, ok := got.ThroughputTps.Stat()
	if !ok {
		t.Fatal("two passing throughput cases are measured")
	}
	if tps.N != 2 {
		t.Fatalf("sample size %d, want 2", tps.N)
	}
	if tps.SpreadLow == tps.SpreadHigh {
		t.Fatalf("two different observations must produce a spread, got %+v", tps)
	}
	if _, ok := got.TimeToFirstTokenMs.Stat(); !ok {
		t.Fatal("time to first token must be measured alongside")
	}
}

func TestScoreIsTotal(t *testing.T) {
	// Every figure Score returns is measured or names its absence, on every
	// input. A caller cannot produce an unfilled one by forgetting a branch,
	// which is what makes the Validate at the write seam a formality rather
	// than a live hazard.
	inputs := [][]CaseResult{
		nil,
		{},
		{passed("structured.triage", KindStructured)},
		{failed("tool.readFile", KindToolCall)},
		{{Id: "x", Kind: KindThroughput, Outcome: OutcomeRefused}},
		{{Id: "y", Kind: "unknown-kind", Outcome: OutcomePassed}},
	}
	for i, in := range inputs {
		if err := Score(in).Validate(); err != nil {
			t.Fatalf("input %d produced an invalid figure set: %v", i, err)
		}
	}
}
