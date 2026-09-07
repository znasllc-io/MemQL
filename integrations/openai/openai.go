// Package openai implements the OpenAI ASR client behind MemQL's streaming
// transcription path (the AiTranscribeStream* messages). It implements the
// audio.ASRProvider interface, providing cloud-based speech-to-text with no
// GPU requirement.
//
// ASR uses the OpenAI Realtime API in transcription-only mode (type: "transcription"),
// which provides streaming speech-to-text via WebSocket without bundling an LLM.
//
// The provider handles sample rate conversion between MemQL's audio pipeline
// (16kHz) and OpenAI's native format (24kHz) transparently.
//
// The package also shipped a TTS client against /v1/audio/speech, for the
// Polyphon voice transport. Epic memql#4988 retired that transport along with
// the voice and cognition node types, and nothing constructed the client; it
// and its two config knobs (MEMQL_POLYPHON_OPENAI_TTS_MODEL / _TTS_VOICE) are
// gone. The engine's own text-to-speech, AiSpeechMsg, is served by the AI
// provider registry in component/memql/ai_providers.go and never used this.
package openai

import (
	"context"
	"fmt"
	"log/slog"
)

// Config holds the configuration for the OpenAI ASR provider.
type Config struct {
	// Bearer returns the credential to present at dial time (epic memql#5088).
	//
	// A FUNCTION, NOT A STRING, and that is the whole change. The Realtime
	// WebSocket sets Authorization once, at dial, and a long-lived connection
	// outlives the one-hour federated bearer that opened it -- but a
	// reconnection minutes later must NOT reuse the token the first dial
	// captured. Holding the string would have made every reconnect after the
	// first hour fail with a 401 that named nothing, on a path nobody watches.
	// Taking a function means each dial asks the engine's exchanger for
	// whatever is current, and the caching lives in one place.
	//
	// There is no API-key field any more: federation is the only door.
	Bearer func(ctx context.Context) (string, error) `json:"-"`

	// ASRModel is the transcription model used by the Realtime API in
	// transcription-only mode. Defaults to whisper-1 -- the only
	// transcription model OpenAI enables on every project by default.
	// The newer gpt-4o-{transcribe,mini-transcribe,transcribe-diarize}
	// variants are gated behind project-level model access (visible
	// at platform.openai.com/settings/organization/projects/<id>/limits)
	// and the Realtime session silently fails the FIRST committed
	// utterance with "Project ... does not have access to model" until
	// the project owner enables them. whisper-1 emits final transcripts
	// only -- no streaming partials -- so a client's interim text is
	// suppressed; everything else (final transcript, agent reply) works
	// normally.
	//
	// To upgrade to streaming partials: enable gpt-4o-mini-transcribe
	// (or gpt-4o-transcribe for higher accuracy) on the OpenAI project,
	// then set MEMQL_POLYPHON_OPENAI_ASR_MODEL=gpt-4o-mini-transcribe.
	ASRModel string `json:"asrModel"`

	// Logger for debug output.
	Logger *slog.Logger `json:"-"`
}

const defaultASRModel = "whisper-1"

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		ASRModel: defaultASRModel,
	}
}

// validate checks that required fields are set and applies defaults.
func (c *Config) validate() error {
	if c.Bearer == nil {
		return fmt.Errorf("openai: a bearer source is required -- the Realtime WebSocket authenticates with a federated token, and this cluster supplied none")
	}
	if c.ASRModel == "" {
		c.ASRModel = defaultASRModel
	}
	return nil
}

// logger returns the configured logger or a discard logger.
func (c *Config) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.New(slog.DiscardHandler)
}
