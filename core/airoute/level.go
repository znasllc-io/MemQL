// Package airoute is the shared vocabulary of MemQL's AI routing seam: the
// LEVEL a call declares, the MODALITY derived from it, the NEEDS it must be
// served with, the REQUEST that carries them, and the DECISION that comes back.
//
// It lives in core rather than beside the router because the seam's two halves
// are separate Go modules pointing one way: component/router imports
// component/memql, and the call sites that build a request live inside
// component/memql. Declaring the request beside the router would make the
// re-pointing an import cycle. Separately, component/safety,
// component/fileprocessor, component/healing and component/work depend on
// neither and take an injected provider from their caller, so a home in
// component/memql would add five module edges to name a four-value enum.
//
// It imports nothing outside the standard library, asserted by a test, which
// is what keeps it nameable from every module in the workspace.
package airoute

import (
	"fmt"
	"strings"
)

// Level is how much intelligence a call needs. It is a CLOSED set of four:
// an abstraction a person cannot hold in their head is not one (design D1).
//
// A call declares a level rather than a model, because a model name at a call
// site is a release every time the fleet changes.
type Level string

const (
	// LevelFast is triage, intake, classification and suggestion work.
	LevelFast Level = "fast"
	// LevelStrong is an agent's reply, a conductor turn, an authoring design.
	LevelStrong Level = "strong"
	// LevelReasoning is emitting or repairing a construct, and re-planning.
	LevelReasoning Level = "reasoning"
	// LevelEmbeddings is every embedding call. It is a LEVEL rather than a
	// modality flag so a rule can park it without naming a model.
	LevelEmbeddings Level = "embeddings"
)

// Levels is the closed set, in the order a person reads them.
func Levels() []Level {
	return []Level{LevelFast, LevelStrong, LevelReasoning, LevelEmbeddings}
}

// LevelNames is the four values comma-joined, for an error message.
func LevelNames() string {
	out := make([]string, 0, 4)
	for _, l := range Levels() {
		out = append(out, string(l))
	}
	return strings.Join(out, ", ")
}

// ParseLevel accepts exactly the four values, exactly as spelled. The error
// names all four, because it is what a DSL author and an operator both read:
// "unknown level" alone sends them to the source.
func ParseLevel(s string) (Level, error) {
	switch Level(s) {
	case LevelFast, LevelStrong, LevelReasoning, LevelEmbeddings:
		return Level(s), nil
	}
	return "", fmt.Errorf("unknown level %q: a level is one of %s", s, LevelNames())
}

// Valid reports whether l is one of the four.
func (l Level) Valid() bool {
	_, err := ParseLevel(string(l))
	return err == nil
}

func (l Level) String() string { return string(l) }

// Degrade returns the next level down and whether there is one. The walk is
// reasoning -> strong -> fast, and it stops there.
//
// LevelEmbeddings NEVER degrades. A degraded embedder answers in a DIFFERENT
// VECTOR SPACE, so the result is not a worse vector -- it is one that does not
// belong in the index it is about to be written to, and nothing downstream can
// tell the difference.
func (l Level) Degrade() (Level, bool) {
	switch l {
	case LevelReasoning:
		return LevelStrong, true
	case LevelStrong:
		return LevelFast, true
	}
	return "", false
}
