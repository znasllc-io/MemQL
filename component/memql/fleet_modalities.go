package memql

import (
	"context"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/core/common"
)

// The fleet's four new modality doors (epic memql#5137, D4).
//
// Before these, `fleet:` served chat and embedding, so vision, transcription,
// speech and image generation had exactly one door each and it was a paid
// vendor's. On a cluster with no federation -- which since epic memql#5088 is
// every local cluster -- that meant those four capabilities did not exist.
//
// EVERY ONE OF THEM IS GATED ON WHAT A MACHINE ADVERTISED, and the gate lives
// in FleetCallRequest.Needs() rather than here: the KIND derives the need, the
// router refuses a machine that never advertised it, and this file only builds
// the request. That ordering is what keeps "which machines can serve this" in
// one place -- a check written here as well would be a second answer, and the
// two would disagree the first time one of them was updated.

// FleetImageRequest asks a machine to generate an image.
type FleetImageRequest struct {
	Prompt string
	Width  int
	Height int
	Count  int
	Format string
}

// FleetImage is one generated image.
type FleetImage struct {
	Data      []byte
	MediaType string
}

// FleetSpeechRequest asks a machine to speak.
type FleetSpeechRequest struct {
	Text  string
	Voice string
	// Format is the requested container ("wav", "mp3", "opus"). A runtime that
	// cannot produce it answers in what it can and says so on the result --
	// the caller wanted speech, and speech in another container is still
	// speech.
	Format   string
	Speed    float64
	SpeedSet bool
}

// FleetAudio is a span of audio, in either direction.
type FleetAudio struct {
	Data         []byte
	MediaType    string
	SampleRateHz int
}

// FleetTranscript is a transcription result.
type FleetTranscript struct {
	Text string
	// Segments are the timed spans. EMPTY IS LEGITIMATE and means the runtime
	// reported no timings, not that the transcript is empty -- whisper.cpp and
	// an OpenAI-compatible endpoint disagree about whether to return them, and
	// a caller that needs timings checks rather than assuming.
	Segments []FleetTranscriptSegment
}

// FleetTranscriptSegment is one timed span.
type FleetTranscriptSegment struct {
	StartSeconds float64
	EndSeconds   float64
	Text         string
}

// CallVision satisfies common.VisionAIProvider.
//
// The images ride the LAST user message as image parts, which is where both
// runtimes want them: Ollama's /api/chat takes `images: []` on a message and
// the OpenAI-compatible shape takes a content array with image_url on one. A
// prompt with no image is deliberately still a vision call -- the caller asked
// for one, and silently downgrading it to chat would route to a machine that
// never advertised vision and answer as though it had looked.
func (p *fleetProvider) CallVision(ctx context.Context, prompt string, images []common.VisionContent) (string, error) {
	parts := make([]FleetImage, 0, len(images))
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		parts = append(parts, FleetImage{Data: img.Data, MediaType: img.MimeType})
	}
	res, err := p.call(ctx, FleetCallRequest{
		Kind:     FleetKindVision,
		Messages: []common.ChatMessage{{Role: "user", Content: prompt}},
		Images:   parts,
	})
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// Transcribe runs a batch transcription on a machine advertising `audioin=1`.
func (p *fleetProvider) Transcribe(ctx context.Context, audio FleetAudio) (FleetTranscript, error) {
	if len(audio.Data) == 0 {
		return FleetTranscript{}, fmt.Errorf("fleet transcribe: no audio")
	}
	res, err := p.call(ctx, FleetCallRequest{Kind: FleetKindTranscribe, Audio: &audio})
	if err != nil {
		return FleetTranscript{}, err
	}
	return FleetTranscript{Text: res.Content, Segments: res.Segments}, nil
}

// Speak runs a text-to-speech call on a machine advertising `audioout=1`.
func (p *fleetProvider) Speak(ctx context.Context, req FleetSpeechRequest) (FleetAudio, error) {
	if strings.TrimSpace(req.Text) == "" {
		return FleetAudio{}, fmt.Errorf("fleet speak: no text")
	}
	res, err := p.call(ctx, FleetCallRequest{
		Kind:     FleetKindSpeak,
		Messages: []common.ChatMessage{{Role: "user", Content: req.Text}},
		Speech:   &req,
	})
	if err != nil {
		return FleetAudio{}, err
	}
	if res.Audio == nil {
		// A machine that advertised audioout and returned no bytes is a bug on
		// that machine, and saying so beats returning an empty sound file that
		// plays as silence.
		return FleetAudio{}, fmt.Errorf("fleet speak: %s returned no audio", res.MachineLabel)
	}
	return *res.Audio, nil
}

// GenerateImage runs an image-generation call on a machine advertising
// `imagegen=1`.
func (p *fleetProvider) GenerateImage(ctx context.Context, req FleetImageRequest) ([]FleetImage, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("fleet image: no prompt")
	}
	res, err := p.call(ctx, FleetCallRequest{
		Kind:     FleetKindImage,
		Messages: []common.ChatMessage{{Role: "user", Content: req.Prompt}},
		Image:    &req,
	})
	if err != nil {
		return nil, err
	}
	if len(res.Images) == 0 {
		return nil, fmt.Errorf("fleet image: %s returned no images", res.MachineLabel)
	}
	return res.Images, nil
}
