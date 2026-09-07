package stt

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// The recorded wire shape of the one transcription call (epic memql#5088,
// Task 1). Its sibling for the chat, streaming, embedding and speech calls is
// component/memql/openai_wire_test.go; the reasoning for recording any of this
// is written down there and not repeated.
//
// Transcription is the one call site whose body is MULTIPART rather than JSON,
// and that is exactly why it gets its own test rather than a fixture file. The
// three things that must survive the move to the official SDK are things a
// JSON fixture could not express:
//
//   - the FILE PART'S FILENAME. The community SDK takes `FilePath` and derives
//     the multipart filename from it; the official SDK takes an io.Reader and
//     asks for the name separately (openai.File(rdr, name, contentType)),
//     defaulting to "anonymous_file" when it is not given. OpenAI documents
//     that it identifies the audio format from an extension-bearing filename,
//     so a dropped name is a transcription that starts failing on format
//     detection -- with a 400 that names the format, not the filename.
//   - the LANGUAGE field, which is optional and must be ABSENT rather than
//     empty when no hint was configured.
//   - the MODEL and RESPONSE FORMAT fields.
func TestWhisperWireTranscription(t *testing.T) {
	srv, captured := newRecordingWhisperServer(t)

	provider := NewOpenAIWhisperProviderWithClient(newWireTestWhisperClient(srv.URL), NewLogger())

	session, err := provider.StartStream(context.Background(), StreamConfig{
		Format:       "pcm16",
		SampleRate:   16000,
		Channels:     1,
		LanguageHint: "en-US",
	})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	if err := session.SendAudio(make([]byte, 3200)); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}

	final, err := session.Finalize(context.Background())
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if final.Text != "a recorded transcript" {
		t.Errorf("transcript is %q, want %q", final.Text, "a recorded transcript")
	}

	if captured.path != "/v1/audio/transcriptions" {
		t.Errorf("path is %q, want /v1/audio/transcriptions", captured.path)
	}
	if captured.fileName != "audio.wav" {
		t.Errorf("multipart file part is named %q, want %q -- OpenAI identifies the audio format "+
			"from an extension-bearing filename, so this name is load-bearing", captured.fileName, "audio.wav")
	}
	if captured.fields["model"] != "whisper-1" {
		t.Errorf("model field is %q, want %q", captured.fields["model"], "whisper-1")
	}
	// "en-US" normalises to ISO-639-1 "en" before it is sent.
	if captured.fields["language"] != "en" {
		t.Errorf("language field is %q, want %q (the hint is normalised to ISO-639-1)",
			captured.fields["language"], "en")
	}
	if captured.fields["response_format"] != "json" {
		t.Errorf("response_format field is %q, want %q", captured.fields["response_format"], "json")
	}
	// OpenAI rejects a file part with no Content-Type, and the official SDK
	// refuses an empty one outright rather than substituting a default -- so an
	// absent value here is a transcription that fails on every call, not a
	// cosmetic difference. The exact value is left to the machine's MIME table
	// (audio/wav or audio/x-wav, or the octet-stream fallback), because that
	// table is read from system files and differs between a laptop and a
	// distroless container; what must hold on every one of them is that the
	// part carries a media type at all.
	if strings.TrimSpace(captured.fileContentType) == "" {
		t.Error("the multipart file part carries no Content-Type; OpenAI rejects the part without one")
	}
	if len(captured.fileBytes) == 0 {
		t.Error("the multipart file part carried no bytes")
	}
	// The WAV wrapper is what makes the PCM the session buffered identifiable
	// to the API at all; a raw PCM body reaches OpenAI as an unknown format.
	if !strings.HasPrefix(string(captured.fileBytes), "RIFF") {
		t.Errorf("the uploaded audio is not a WAV container (first bytes %q)",
			string(captured.fileBytes[:min(4, len(captured.fileBytes))]))
	}
}

// TestWhisperWireOmitsAnAbsentLanguage pins the optional field's absence.
// Sending language="" is not the same as omitting it: OpenAI reads an empty
// string as a language code it cannot parse.
func TestWhisperWireOmitsAnAbsentLanguage(t *testing.T) {
	srv, captured := newRecordingWhisperServer(t)

	provider := NewOpenAIWhisperProviderWithClient(newWireTestWhisperClient(srv.URL), NewLogger())
	session, err := provider.StartStream(context.Background(), StreamConfig{
		Format:     "pcm16",
		SampleRate: 16000,
		Channels:   1,
	})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	if err := session.SendAudio(make([]byte, 3200)); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	if _, err := session.Finalize(context.Background()); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if value, present := captured.fields["language"]; present {
		t.Errorf("language field is present as %q although no hint was configured; it must be omitted", value)
	}
}

// --- the recorder ----------------------------------------------------------

type capturedMultipart struct {
	path      string
	fields    map[string]string
	fileName        string
	fileContentType string
	fileBytes       []byte
	header    http.Header
}

func newRecordingWhisperServer(t *testing.T) (*httptest.Server, *capturedMultipart) {
	t.Helper()
	captured := &capturedMultipart{fields: map[string]string{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		captured.path = req.URL.Path
		captured.header = req.Header.Clone()

		mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("transcription request is not multipart: content-type=%q err=%v",
				req.Header.Get("Content-Type"), err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		reader := multipart.NewReader(req.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			body, _ := io.ReadAll(part)
			if part.FormName() == "file" {
				captured.fileName = part.FileName()
				captured.fileBytes = body
				captured.fileContentType = part.Header.Get("Content-Type")
				continue
			}
			captured.fields[part.FormName()] = string(body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text":"a recorded transcript"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

// newWireTestWhisperClient builds the SDK client the provider will use,
// pointed at the recording server.
//
// It goes through NewOpenAIWhisperProviderWithClient rather than
// NewOpenAIWhisperProvider because the latter has no base-URL seam: it takes a
// key and builds its own client against api.openai.com. That constructor is
// what the engine calls, and after this epic it takes a bearer source instead
// of a key -- the wire shape asserted here is the same either way, which is
// the point.
func newWireTestWhisperClient(baseURL string) *openai.Client {
	client := openai.NewClient(
		option.WithAPIKey("sk-wire-test"),
		option.WithBaseURL(baseURL+"/v1"),
	)
	return &client
}
