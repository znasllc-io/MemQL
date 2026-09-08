package probe

import (
	"fmt"
	"sort"
	"strings"
)

// The SUITE (epic memql#5146, design D3).
//
// ===========================================================================
// PINNED BY VERSION, AND THE VERSION IS PART OF THE MEASUREMENT'S KEY
// ===========================================================================
// Two figures scored by different suites are not comparable. So the version is
// not metadata beside a measurement -- it is one third of the row's key
// (machine, model, suiteVersion), and a cockpit that does not know the version
// the engine asked for ENDS the probe with `unknown_suite_version` rather than
// running its own and reporting numbers under a name that means something else.
//
// ===========================================================================
// THE CASES ARE THIS CLUSTER'S OWN QUESTION
// ===========================================================================
// The five structured schemas are drawn from the platform's own prompts --
// triage, intake, symptom, factory decision, healing patches -- rather than
// from a general benchmark, because the fact the router needs is whether THIS
// model's structured output validates against the schemas THIS cluster sends.
// A model that scores well on somebody else's benchmark and cannot produce a
// valid triage answer is useless here, and the reverse is equally true.
//
// The three tool definitions are the same argument one level along: a runtime
// handed tools it cannot honour answers prose, which surfaces three layers away
// as an agent that stopped using its tools for no reason a reader can see.
//
// The two throughput prompts are 8K and 32K because those are the two sizes
// that separate machines in practice: almost anything serves a short prompt at
// a tolerable rate, and the 32K case is where unified memory and a small card
// stop looking alike.

// SuiteVersion is the pinned version this engine asks for and scores under.
//
// BUMP IT WHEN THE CASES CHANGE, never when only their wording does. A bump
// makes every existing measurement incomparable -- correctly, because it is --
// and the machine page shows them as measured under an older suite rather than
// silently mixing them.
const SuiteVersion = "1"

// Kind is what a case measures. Closed; each kind is scored differently.
type Kind string

const (
	// KindStructured asks for output against one of the platform's schemas and
	// scores VALIDITY -- did it parse and satisfy the schema.
	KindStructured Kind = "structured"
	// KindToolCall offers a tool definition and scores CORRECTNESS -- did the
	// model call the right tool with arguments that satisfy its schema.
	KindToolCall Kind = "toolCall"
	// KindThroughput measures tokens per second and time to first token at a
	// stated prompt size.
	KindThroughput Kind = "throughput"
)

// Case is one unit of the suite.
type Case struct {
	// Id is the join key between a progress message and a case, and it is
	// compared across releases in a measurement's history. Stable: renaming one
	// is a suite version bump, not an edit.
	Id   string
	Kind Kind
	// Schema names the platform prompt whose output schema this case checks.
	// Empty for the other kinds.
	Schema string
	// Tool names the tool definition offered. Empty for the other kinds.
	Tool string
	// PromptTokens is the approximate prompt size a throughput case builds to.
	// Zero for the other kinds.
	//
	// A model whose context window is under this size must REFUSE the case
	// rather than fail it -- see AbsentRefused. Asking an 8K model a 32K
	// question and counting the failure would drag its score down for a limit
	// it had already declared.
	PromptTokens int
}

// cases is the suite, in the order it runs.
//
// Structured first, then tools, then throughput. The order is not arbitrary:
// the first two are the ones a person cares about most and the last is the
// slowest, so a probe that is cancelled or crashes half way through has
// measured the useful half.
var cases = []Case{
	{Id: "structured.triage", Kind: KindStructured, Schema: "triage"},
	{Id: "structured.intake", Kind: KindStructured, Schema: "intake"},
	{Id: "structured.symptom", Kind: KindStructured, Schema: "symptom"},
	{Id: "structured.factoryDecision", Kind: KindStructured, Schema: "factoryDecision"},
	{Id: "structured.healingPatches", Kind: KindStructured, Schema: "healingPatches"},

	{Id: "tool.readFile", Kind: KindToolCall, Tool: "readFile"},
	{Id: "tool.searchGraph", Kind: KindToolCall, Tool: "searchGraph"},
	{Id: "tool.recordDecision", Kind: KindToolCall, Tool: "recordDecision"},

	{Id: "throughput.8k", Kind: KindThroughput, PromptTokens: 8192},
	{Id: "throughput.32k", Kind: KindThroughput, PromptTokens: 32768},
}

// Cases returns the pinned suite's cases, in run order.
func Cases() []Case {
	out := make([]Case, len(cases))
	copy(out, cases)
	return out
}

// Suite returns the cases for a version, or an error naming what is pinned.
//
// An unknown version is an ERROR rather than a fallback to the pinned suite.
// Falling back would let a cockpit that asked for suite 2 be handed suite 1's
// cases and report the results as suite 2 -- the exact confusion the version
// exists to prevent, arriving through the code that implements it.
func Suite(version string) ([]Case, error) {
	if strings.TrimSpace(version) != SuiteVersion {
		return nil, fmt.Errorf("probe: unknown suite version %q (this engine pins %q)", version, SuiteVersion)
	}
	return Cases(), nil
}

// CountByKind returns how many cases of each kind the suite has, which is what
// a progress renderer needs to draw a real fraction.
func CountByKind() map[Kind]int {
	out := map[Kind]int{}
	for _, c := range cases {
		out[c.Kind]++
	}
	return out
}

// CaseIds returns every case id, sorted. Used by the tests that pin stability
// and by a cockpit reconciling what it ran against what it was asked.
func CaseIds() []string {
	out := make([]string, 0, len(cases))
	for _, c := range cases {
		out = append(out, c.Id)
	}
	sort.Strings(out)
	return out
}
