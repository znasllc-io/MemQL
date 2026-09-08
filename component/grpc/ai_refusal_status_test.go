package memql

// ai_refusal_status_test.go -- three conditions, three answers, and none of
// them wearing another's clothes (epic memql#5127, design D2).
//
// The AI handlers used to answer every "I could not get a provider" with one
// of three shrugs -- `provider %q not found`, `no non-streaming chat provider
// available`, `no streaming provider available` -- across two status codes
// chosen by which branch happened to run. What replaced them has to keep three
// genuinely different conditions apart, because their fixes live in three
// different places:
//
//   - a shut door is FailedPrecondition, fixed by whoever owns the laptop, the
//     app sign-in or the ceiling;
//   - an unwired resolver is Internal, fixed in app/ by whoever deploys;
//   - anything else is Internal with an error id, fixed by reading the log.
//
// Collapsing the second into the first is the tempting bug and the expensive
// one: it tells a person their fleet is asleep about a cluster that never
// installed a router, and no amount of waking machines will change the answer.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	memqlengine "github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/component/work"
	"github.com/znasllc-io/memql/core/airoute"
)

// doorRefusal stands in for component/router's InferenceUnavailable, which
// this module does not import -- the same stand-in component/work's own tests
// use, and for the same reason: what crosses the boundary is the
// work.DoorReporter interface, so that is what the test implements.
type doorRefusal struct {
	code  string
	doors []work.DoorReport
}

func (d *doorRefusal) Error() string {
	// The router's Error() leads with the code; so does this, because that
	// contract is precisely what work.InferenceRefusalCode reads back.
	var b strings.Builder
	b.WriteString(d.code)
	b.WriteString(": no door to a model is open for this call")
	for _, door := range d.doors {
		fmt.Fprintf(&b, "; %s (%s): %s", door.Name, door.Door, door.Reason)
	}
	return b.String()
}

func (d *doorRefusal) RefusalCode() string              { return d.code }
func (d *doorRefusal) ReportedDoors() []work.DoorReport { return d.doors }

func TestARouterRefusalIsClassifiedAsOneAndNamesItsCode(t *testing.T) {
	refusal := &doorRefusal{
		code: work.RefusalEveryDoorShut,
		doors: []work.DoorReport{
			{Door: airoute.DoorLocal, Name: "fleet:*", Reason: "no machine is online"},
			{Door: airoute.DoorFederation, Name: "chat54Mini", Reason: "the cost ceiling is reached"},
			// A second federation line: the SET in the metadata must
			// deduplicate while the message keeps every line.
			{Door: airoute.DoorFederation, Name: "streamClaudeSonnet", Reason: "the cost ceiling is reached"},
		},
	}

	message, metadata, ok := aiRefusalStatus(refusal)
	if !ok {
		t.Fatal("a structured door refusal was not classified as one, so it would arrive as codes.Internal with an error id -- the shrug this epic deleted, wearing a new name")
	}
	if !strings.Contains(message, work.RefusalEveryDoorShut) {
		t.Fatalf("the message %q does not name the refusal code, so a client has nothing stable to match on", message)
	}
	if got := metadata["refusalCode"]; got != work.RefusalEveryDoorShut {
		t.Fatalf("metadata refusalCode is %q, expected %q", got, work.RefusalEveryDoorShut)
	}
	if got := metadata["doorsShut"]; got != airoute.DoorLocal+","+airoute.DoorFederation {
		t.Fatalf("metadata doorsShut is %q, expected %q -- the set is deduplicated in chain order",
			got, airoute.DoorLocal+","+airoute.DoorFederation)
	}
	// The door report is the only part with an action in it: "no provider
	// available" sends a person nowhere.
	for _, want := range []string{"fleet:*", "no machine is online", "the cost ceiling is reached"} {
		if !strings.Contains(message, want) {
			t.Errorf("the message drops %q, so the reader learns which doors were tried but not why any of them was shut", want)
		}
	}
}

// TestAWrappedRefusalIsStillARefusal pins the reason work.DoorsFrom exists at
// all: the refusal travels through wrapping on the way here, and a classifier
// that only recognised a bare one would stop working the first time somebody
// added context to the error.
func TestAWrappedRefusalIsStillARefusal(t *testing.T) {
	wrapped := fmt.Errorf("suggest domain knowledge: %w",
		&doorRefusal{code: work.RefusalCeilingReached})

	message, metadata, ok := aiRefusalStatus(wrapped)
	if !ok {
		t.Fatal("a wrapped refusal was not classified as one")
	}
	if metadata["refusalCode"] != work.RefusalCeilingReached {
		t.Fatalf("metadata refusalCode is %q, expected %q", metadata["refusalCode"], work.RefusalCeilingReached)
	}
	if !strings.Contains(message, "suggest domain knowledge") {
		t.Errorf("the wrapper's context was dropped from %q", message)
	}
	// No doors were reported, and an EMPTY list is the honest answer: a single
	// synthetic entry would claim a door that was never named.
	if _, present := metadata["doorsShut"]; present {
		t.Errorf("metadata claims doorsShut %q for a refusal that reported no doors", metadata["doorsShut"])
	}
}

// TestAnUnwiredResolverIsNotARefusal is the discrimination this file exists
// for. Both conditions read as "no model" to a user, and they have nothing in
// common: one is a laptop, the other is a missing line in app/.
func TestAnUnwiredResolverIsNotARefusal(t *testing.T) {
	if _, _, ok := aiRefusalStatus(memqlengine.ErrAIResolverUnwired); ok {
		t.Fatal("ErrAIResolverUnwired was classified as a door refusal: a cluster that never installed the router " +
			"would tell its operator to wake a machine, which cannot fix it")
	}
	// And through a wrapper, which is how it will actually arrive.
	wrapped := fmt.Errorf("chat turn: %w", memqlengine.ErrAIResolverUnwired)
	if _, _, ok := aiRefusalStatus(wrapped); ok {
		t.Fatal("a wrapped ErrAIResolverUnwired was classified as a door refusal")
	}

	// IDENTITY OUTRANKS THE MESSAGE, and this is the case that proves the
	// order rather than assuming it. The two checks above pass whether or not
	// the errors.Is guard runs first, because ErrAIResolverUnwired's wording
	// happens to contain none of the four refusal codes -- so on their own
	// they assert the outcome and pin nothing.
	//
	// Here the wording DOES contain one, which is the drift the guard exists
	// for: work.DoorsFrom falls back to a CONTAINS match over the message, and
	// without the identity check first, an unwired resolver whose sentence
	// ever mentioned a code would start arriving as a shut door.
	drifted := fmt.Errorf("%s: %w", work.RefusalEveryDoorShut, memqlengine.ErrAIResolverUnwired)
	if _, _, ok := aiRefusalStatus(drifted); ok {
		t.Fatal("an error that IS ErrAIResolverUnwired was classified as a door refusal because its message " +
			"mentioned a refusal code: the message match ran ahead of the identity check")
	}
}

// TestAnOrdinaryErrorIsNotARefusal is the last of the three, and it is the one
// that keeps the classifier from being trivially true: without it, a function
// that answered "refusal" to everything would pass every other test here.
func TestAnOrdinaryErrorIsNotARefusal(t *testing.T) {
	if _, _, ok := aiRefusalStatus(errors.New("connection reset by peer")); ok {
		t.Fatal("an ordinary transport error was classified as a door refusal")
	}
	if _, _, ok := aiRefusalStatus(nil); ok {
		t.Fatal("a nil error was classified as a door refusal")
	}
}
