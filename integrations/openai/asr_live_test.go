package openai

import (
	"context"
	"github.com/znasllc-io/memql/core/audio"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestASRStreamLiveGA verifies the GA transcription session against the real
// OpenAI Realtime API (#1382): connect without the retired beta header, send
// the GA session.update (session.type: "transcription", audio.input nesting),
// and assert the server accepts the session instead of closing with
// beta_api_shape_disabled or rejecting the config with an error event.
//
// Env-gated: skips unless MEMQL_TEST_OPENAI_BEARER is set (never runs in CI
// lanes). Asserts acceptance by sending a short burst of silence and
// confirming the stream stays open with no error for a grace window -- the
// beta-shape rejection closes the socket within ~1s of connect.
//
// THE VARIABLE IS A TEST INPUT, NOT A PRODUCT CONFIGURATION PATH, and the
// distinction is why it is not named like one. Epic memql#5088 removed every
// manually entered vendor key from the product; nothing outside this file
// reads this name, no manifest registers it, and no code path falls back to
// it. What it is for is an engineer pasting a bearer -- a federated one from a
// pod, or any token their own account can mint -- to answer the one question
// the design record leaves open: whether OpenAI's Realtime WebSocket accepts a
// FEDERATED bearer at all. That answer belongs in
// docs/public/operate/auth/openai-federation.md, and this is how it gets
// checked.
func TestASRStreamLiveGA(t *testing.T) {
	bearer := os.Getenv("MEMQL_TEST_OPENAI_BEARER")
	if bearer == "" {
		t.Skip("MEMQL_TEST_OPENAI_BEARER not set; skipping the live Realtime check")
	}

	model := os.Getenv("MEMQL_OPENAI_REALTIME_MODEL")
	if model == "" {
		model = "whisper-1"
	}

	client, err := NewASRClient(Config{
		Bearer:   func(context.Context) (string, error) { return bearer, nil },
		ASRModel: model,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		t.Fatalf("NewASRClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := client.StartStream(ctx, audio.ASRConfig{Language: "en"})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	defer stream.Close()

	// Feed ~1s of 16kHz PCM16 silence so the session has input; a rejected
	// session surfaces as SendAudio write errors or a closed results channel.
	silence := make([]byte, 3200) // 100ms @ 16kHz mono PCM16
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case r, ok := <-stream.Results():
			if !ok {
				t.Fatal("results channel closed early -- session rejected (check for beta_api_shape_disabled / bad model)")
			}
			// Any result (even a VAD onset on noise) means the session is live.
			t.Logf("got ASR result kind=%v final=%v", r.Kind, r.IsFinal)
			return
		case <-tick.C:
			if err := stream.SendAudio(silence); err != nil {
				t.Fatalf("SendAudio failed -- session was closed by the server: %v", err)
			}
		case <-deadline:
			// No error, no close, audio writes accepted for the full window:
			// the GA session shape was accepted (silence yields no transcript).
			return
		}
	}
}
