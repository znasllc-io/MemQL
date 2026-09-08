package memql

import "testing"

// TestFleetKindsMatchTheWireKinds is the gate that makes the duplication safe.
//
// The six call-kind strings exist in THREE places by construction, and the
// duplication is not accidental:
//
//   - component/worker's ModelCallKind* -- the wire package, which
//     component/memql cannot import (component/worker's go.mod REQUIRES
//     component/memql, so the edge would be a module cycle and the
//     module-boundaries lane exists to hold that direction).
//   - component/memql's FleetKind* -- here, the provider side.
//   - the cockpit's own, in another repository entirely.
//
// A DISAGREEMENT IS SILENT IN BOTH DIRECTIONS. The cockpit refuses a kind it
// does not recognise, so a rename here becomes "that modality never routes"
// with no error naming a spelling; and a rename THERE looks identical. This
// asserts against LITERALS rather than against the other package's constants
// for exactly that reason -- comparing two constants that were renamed together
// is a tautology, and the third copy is in a repo no Go test here can read.
//
// The literals are the contract agreed with the cockpit session on 2026-09-07.
func TestFleetKindsMatchTheWireKinds(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"chat", FleetKindChat, "chat"},
		{"embedding", FleetKindEmbedding, "embedding"},
		{"vision", FleetKindVision, "vision"},
		{"transcribe", FleetKindTranscribe, "transcribe"},
		{"speak", FleetKindSpeak, "speak"},
		{"image", FleetKindImage, "image"},
	} {
		if tc.got != tc.want {
			t.Errorf("FleetKind%s = %q, want %q -- this string is the wire contract with the cockpit, and a mismatch is a modality that silently never routes",
				tc.name, tc.got, tc.want)
		}
	}
}

// TestNeedsDerivesExactlyOneModalityPerKind pins the derivation.
//
// A kind must set ITS need and no other. Two set at once would require a
// machine to advertise both, which no machine does -- so the call would be
// unroutable rather than wrongly routed, and "no machine available" is a
// failure mode that reads as a fleet problem rather than as a bug here.
func TestNeedsDerivesExactlyOneModalityPerKind(t *testing.T) {
	count := func(n FleetNeeds) int {
		c := 0
		for _, b := range []bool{n.Vision, n.AudioIn, n.AudioOut, n.ImageGen} {
			if b {
				c++
			}
		}
		return c
	}

	for kind, want := range map[string]int{
		FleetKindVision:     1,
		FleetKindTranscribe: 1,
		FleetKindSpeak:      1,
		FleetKindImage:      1,
		// Neither of the original two gates on a modality flag: `embeddings=1`
		// already decides which MODEL answers an embedding call, and every
		// machine serving any model serves chat.
		FleetKindChat:      0,
		FleetKindEmbedding: 0,
	} {
		if got := count(FleetCallRequest{Kind: kind}.Needs()); got != want {
			t.Errorf("kind %q sets %d modality need(s), want %d", kind, got, want)
		}
	}

	// And each kind sets the RIGHT one -- the count above would pass if all
	// four were wired to the same field.
	if n := (FleetCallRequest{Kind: FleetKindVision}).Needs(); !n.Vision {
		t.Error("a vision call does not require a seeing model")
	}
	if n := (FleetCallRequest{Kind: FleetKindTranscribe}).Needs(); !n.AudioIn {
		t.Error("a transcribe call does not require audio input")
	}
	if n := (FleetCallRequest{Kind: FleetKindSpeak}).Needs(); !n.AudioOut {
		t.Error("a speak call does not require audio output")
	}
	if n := (FleetCallRequest{Kind: FleetKindImage}).Needs(); !n.ImageGen {
		t.Error("an image call does not require image generation")
	}
}
