package worker

import "testing"

// The four new model-call kinds (epic memql#5137, D4).
//
// These are wire-contract tests, and the contract has two halves that fail in
// completely different ways if they disagree with the cockpit:
//
//   - a KIND STRING the cockpit does not recognise is a silent no-route. The
//     cockpit refuses the call, the engine sees a refusal that names nothing
//     about spelling, and the modality simply never works.
//   - a FLAG SPELLING that disagrees is worse: the machine advertises a door
//     the engine never sees, so selection skips a machine that could have
//     served the call and the fleet looks less capable than it is.
//
// Both were fixed with the cockpit session on 2026-09-07 and are asserted here
// as literals rather than as constants compared to themselves.

func TestTheFourNewKindsAreValid(t *testing.T) {
	for _, kind := range []string{"vision", "transcribe", "speak", "image"} {
		if !IsValidModelCallKind(kind) {
			t.Errorf("kind %q is not accepted by IsValidModelCallKind", kind)
		}
	}
	// The originals must be unaffected by the addition.
	for _, kind := range []string{"chat", "embedding"} {
		if !IsValidModelCallKind(kind) {
			t.Errorf("kind %q stopped being valid", kind)
		}
	}
}

// TestKindConstantsAreTheAgreedStrings pins the spelling.
//
// Asserted against LITERALS on purpose. `ModelCallKindVision == ModelCallKindVision`
// is a tautology; what needs guarding is that the constant still holds the
// string the cockpit implemented against, so a rename fails here rather than
// becoming a modality that quietly never routes.
func TestKindConstantsAreTheAgreedStrings(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{ModelCallKindChat, "chat"},
		{ModelCallKindEmbedding, "embedding"},
		{ModelCallKindVision, "vision"},
		{ModelCallKindTranscribe, "transcribe"},
		{ModelCallKindSpeak, "speak"},
		{ModelCallKindImage, "image"},
	} {
		if tc.got != tc.want {
			t.Errorf("kind constant = %q, want %q -- this string is the wire contract with the cockpit", tc.got, tc.want)
		}
	}
}

// TestUnknownKindIsRefused is the fail-closed direction, and it is the property
// that matters most: a kind nothing recognises must be refused at the boundary
// rather than reaching a worker that would read it as a chat turn and answer
// prose to a transcription request.
func TestUnknownKindIsRefused(t *testing.T) {
	for _, kind := range []string{"", "Chat", "CHAT", "vision ", "audio", "tts", "speech", "imagegen"} {
		if IsValidModelCallKind(kind) {
			t.Errorf("kind %q was accepted; an unrecognised kind must be refused", kind)
		}
	}
}

// TestEachNewKindNamesItsFlag pins the label spellings, which are the other
// half of the contract. Lowercase, no separator: `audioin`, never `audioIn` and
// never `audio_in`.
func TestEachNewKindNamesItsFlag(t *testing.T) {
	for _, tc := range []struct{ kind, flag string }{
		{ModelCallKindVision, "vision"},
		{ModelCallKindTranscribe, "audioin"},
		{ModelCallKindSpeak, "audioout"},
		{ModelCallKindImage, "imagegen"},
	} {
		flag, needs := ModelCallKindNeedsFlag(tc.kind)
		if !needs {
			t.Errorf("kind %q reports needing no capability flag; a machine that never advertised it would be selected and then fail", tc.kind)
			continue
		}
		if flag != tc.flag {
			t.Errorf("kind %q wants flag %q, want %q -- the spelling is the contract with the cockpit", tc.kind, flag, tc.flag)
		}
	}
}

// TestChatAndEmbeddingNeedNoModalityFlag is the negative control for the test
// above. Without it, a ModelCallKindNeedsFlag that returned true for everything
// would pass -- and every chat call in the cluster would then require a flag no
// machine emits.
func TestChatAndEmbeddingNeedNoModalityFlag(t *testing.T) {
	for _, kind := range []string{ModelCallKindChat, ModelCallKindEmbedding, "nonsense"} {
		if flag, needs := ModelCallKindNeedsFlag(kind); needs {
			t.Errorf("kind %q reported needing flag %q; only the four new modality kinds gate on one", kind, flag)
		}
	}
}
