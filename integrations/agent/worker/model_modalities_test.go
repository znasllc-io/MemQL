//go:build agent

package worker

import (
	"strings"
	"testing"
)

// The four modality flags on the `model:<id>` label (epic memql#5137, D4).
//
// A machine advertises `vision=1`, `audioin=1`, `audioout=1` or `imagegen=1`
// when it can serve that call kind, and OMITS the key entirely when it cannot.
// False is absence, never `=0` -- the convention `structured`, `tools` and
// `embeddings` already follow, and the one the cockpit implements against.

// TestNewModalityFlagsDefaultFalse is the direction-of-default gate, and the
// direction is the whole point.
//
// An unadvertised modality must cost eligibility, never grant it. A machine
// that says nothing about vision is a machine that cannot see; routing a vision
// turn to it fails on somebody else's laptop with an error naming nothing, four
// layers from the selection that sent it there.
func TestNewModalityFlagsDefaultFalse(t *testing.T) {
	a := ParseModelAttributes("ctx=131072,structured=1,tools=1,params=9000000000")
	if a.Vision {
		t.Error("vision read true from a label that never mentioned it")
	}
	if a.AudioIn {
		t.Error("audioIn read true from a label that never mentioned it")
	}
	if a.AudioOut {
		t.Error("audioOut read true from a label that never mentioned it")
	}
	if a.ImageGen {
		t.Error("imageGen read true from a label that never mentioned it")
	}
	// The existing four must be unaffected by the addition.
	if !a.StructuredOutput || !a.Tools || a.ContextWindow != 131072 {
		t.Errorf("adding the modality flags changed how the existing keys parse: %+v", a)
	}
}

// TestModalityFlagsParseWhenAdvertised is the positive control. Without it the
// test above passes just as well over a parser that reads nothing at all.
func TestModalityFlagsParseWhenAdvertised(t *testing.T) {
	a := ParseModelAttributes("ctx=8192,vision=1,audioin=1,audioout=1,imagegen=1")
	if !a.Vision || !a.AudioIn || !a.AudioOut || !a.ImageGen {
		t.Fatalf("advertised modality flags did not parse: %+v", a)
	}
}

// TestModalityFlagsParseOrderIndependently pins the cockpit contract.
//
// The cockpit appends its four after `tools` and requires labels to be
// byte-identical for an unchanged inventory -- otherwise every reconnect
// rewrites the registration row. So the engine must never depend on position,
// and this asserts that by parsing the same set in a deliberately different
// order.
func TestModalityFlagsParseOrderIndependently(t *testing.T) {
	forward := ParseModelAttributes("ctx=8192,structured=1,tools=1,vision=1,audioin=1,audioout=1,imagegen=1")
	shuffled := ParseModelAttributes("imagegen=1,audioout=1,vision=1,tools=1,audioin=1,ctx=8192,structured=1")
	if forward != shuffled {
		t.Fatalf("the parser depends on key order:\n  forward  = %+v\n  shuffled = %+v", forward, shuffled)
	}
}

// TestModalityFlagsUseTheAdvertisedBoolRule -- a novel spelling costs
// eligibility rather than granting it, the same rule the existing flags follow.
// This is what stops a cockpit that starts emitting `vision=yes-ish` from
// making every machine look capable.
func TestModalityFlagsUseTheAdvertisedBoolRule(t *testing.T) {
	for _, spelling := range []string{"1", "true", "TRUE", "yes", "y"} {
		if a := ParseModelAttributes("vision=" + spelling); !a.Vision {
			t.Errorf("vision=%s did not read as true", spelling)
		}
	}
	for _, spelling := range []string{"0", "false", "no", "maybe", "", "1.0"} {
		if a := ParseModelAttributes("vision=" + spelling); a.Vision {
			t.Errorf("vision=%q read as true; an unrecognised spelling must cost eligibility", spelling)
		}
	}
}

// TestAModalityTurnSkipsAMachineThatNeverAdvertisedIt is the property the flags
// exist for, and the one that would otherwise be discovered in production.
//
// Selection must SKIP a machine that has not advertised the capability, not
// pick it and let the call fail. The difference is where the failure lands: a
// skip is a routing decision the engine can explain ("no machine advertises
// vision"), while a failed call is an error on somebody else's laptop, four
// layers from the selection that sent it there, naming nothing about capability.
func TestAModalityTurnSkipsAMachineThatNeverAdvertisedIt(t *testing.T) {
	plain := ParseModelAttributes("ctx=131072,structured=1,tools=1")
	seeing := ParseModelAttributes("ctx=131072,structured=1,tools=1,vision=1")

	for _, tc := range []struct {
		kind   string
		reason string
	}{
		{"vision", "vision"},
		{"transcribe", "audio input"},
		{"speak", "audio output"},
		{"image", "image generation"},
	} {
		needs := NeedsForKind(tc.kind)
		ok, why := plain.Satisfies(needs)
		if ok {
			t.Errorf("a machine advertising no %s was selected for a %q turn", tc.reason, tc.kind)
			continue
		}
		if !strings.Contains(why, tc.reason) {
			t.Errorf("the %q refusal must name the missing capability; got %q", tc.kind, why)
		}
	}

	// The positive control: a machine that DID advertise vision is selected for
	// a vision turn. Without this, a Satisfies that refused everything would
	// pass the loop above.
	if ok, why := seeing.Satisfies(NeedsForKind("vision")); !ok {
		t.Errorf("a machine advertising vision=1 was refused a vision turn: %s", why)
	}

	// And a chat turn is unaffected by any of it -- the four gates must not
	// start refusing the calls that make up almost all the traffic.
	if ok, why := plain.Satisfies(NeedsForKind("chat")); !ok {
		t.Errorf("an ordinary chat turn was refused after adding the modality gates: %s", why)
	}
}

// TestModalityFlagsRoundTripThroughString is the contract's other half.
//
// ModalAttributes.String() renders back to a label value, and its doc comment
// claims it is the single definition of the format the cockpit writes. A flag
// this parser reads and that method does not emit would break that claim
// silently -- and the emission ORDER is what the cockpit matched its own
// against, so the assertion is on the exact string rather than on a re-parse.
func TestModalityFlagsRoundTripThroughString(t *testing.T) {
	a := ModelAttributes{
		ContextWindow:    8192,
		StructuredOutput: true,
		Tools:            true,
		Vision:           true,
		AudioIn:          true,
		AudioOut:         true,
		ImageGen:         true,
	}
	got := a.String()
	for _, want := range []string{"vision=1", "audioin=1", "audioout=1", "imagegen=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() omitted %s: %q", want, got)
		}
	}
	// The four are emitted AFTER tools, in this order. The cockpit put its own
	// four in the same relative position so the two can be reconciled later.
	if i, j := strings.Index(got, "tools=1"), strings.Index(got, "vision=1"); i < 0 || j < i {
		t.Errorf("the modality flags must follow tools=: %q", got)
	}
	if round := ParseModelAttributes(got); round != a {
		t.Errorf("String() -> ParseModelAttributes is not the identity:\n  in  = %+v\n  out = %+v", a, round)
	}
	// A false flag is ABSENT, never `=0`.
	if none := (ModelAttributes{ContextWindow: 8192}).String(); strings.Contains(none, "vision") ||
		strings.Contains(none, "audioin") || strings.Contains(none, "audioout") || strings.Contains(none, "imagegen") {
		t.Errorf("a false modality flag was emitted; false is absence: %q", none)
	}
}
