package azurespeech

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

func TestTranscribe_RequestShape(t *testing.T) {
	server, got := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "resource-key", Style: "verbatim", Timestamps: "word"})

	_, err := p.Transcribe(context.Background(), []byte("raw-pcm-bytes"), stt.TranscribeOpts{
		Language: "de-DE",
		Keyterms: []string{" kombify ", "SpeechKit", "kombify", "  "},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if got.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", got.Method)
	}
	if got.Path != "/speechtotext/transcriptions:transcribe" {
		t.Errorf("path = %q", got.Path)
	}
	if got.Query["api-version"] != APIVersion {
		t.Errorf("api-version = %q, want %q", got.Query["api-version"], APIVersion)
	}
	if got.Header.Get("Ocp-Apim-Subscription-Key") != "resource-key" {
		t.Errorf("key header = %q", got.Header.Get("Ocp-Apim-Subscription-Key"))
	}
	if got.Header.Get("Authorization") != "" {
		t.Errorf("unexpected Authorization header %q on the key path", got.Header.Get("Authorization"))
	}
	if !stt.IsWAV(got.Audio) {
		t.Errorf("audio part is not a WAV container (raw PCM must be wrapped)")
	}

	if locales := stringList(mustLookup(t, got.Definition, "locales")); !equalStrings(locales, []string{"de"}) {
		t.Errorf("locales = %v, want [de]", locales)
	}
	if enabled, _ := mustLookup(t, got.Definition, "enhancedMode", "enabled").(bool); !enabled {
		t.Error("enhancedMode.enabled must be true")
	}
	if model := mustLookup(t, got.Definition, "enhancedMode", "model"); model != DefaultModel {
		t.Errorf("enhancedMode.model = %v, want %s", model, DefaultModel)
	}
	if style := mustLookup(t, got.Definition, "enhancedMode", "modelOptions", "transcribeStyle"); style != "verbatim" {
		t.Errorf("transcribeStyle = %v, want verbatim", style)
	}
	if ts := mustLookup(t, got.Definition, "enhancedMode", "modelOptions", "timestamps"); ts != "word" {
		t.Errorf("timestamps = %v, want word", ts)
	}
	if phrases := stringList(mustLookup(t, got.Definition, "phraseList", "phrases")); !equalStrings(phrases, []string{"kombify", "SpeechKit"}) {
		t.Errorf("phrases = %v, want trimmed and de-duplicated [kombify SpeechKit]", phrases)
	}
	if _, present := lookup(got.Definition, "diarization"); present {
		t.Error("diarization must be omitted when nobody asked for it")
	}
}

func TestTranscribe_LocalesOmittedForAutoDetect(t *testing.T) {
	for _, language := range []string{"", "auto", "multi"} {
		t.Run("lang="+language, func(t *testing.T) {
			server, got := fakeService(t, http.StatusOK, successBody)
			p := newTestProvider(server, Options{APIKey: "k"})
			if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Language: language}); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if _, present := lookup(got.Definition, "locales"); present {
				t.Errorf("locales must be omitted for %q so the model auto-detects: %v", language, got.Definition["locales"])
			}
		})
	}
}

func TestTranscribe_LocaleFromGlobalOptions(t *testing.T) {
	// A language configured in Settings arrives through Options rather than
	// the per-request override and still has to narrow the request.
	server, got := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k"})
	opts := stt.TranscribeOpts{}
	opts.Options = provideropts.Values{provideropts.OptionLanguage: "pt-BR"}
	if _, err := p.Transcribe(context.Background(), []byte("pcm"), opts); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if locales := stringList(mustLookup(t, got.Definition, "locales")); !equalStrings(locales, []string{"pt"}) {
		t.Errorf("locales = %v, want [pt]", locales)
	}
}

func TestTranscribe_PhraseListCapped(t *testing.T) {
	server, got := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k", MaxPhrases: 2})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Keyterms: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if phrases := stringList(mustLookup(t, got.Definition, "phraseList", "phrases")); !equalStrings(phrases, []string{"a", "b"}) {
		t.Errorf("phrases = %v, want the first two", phrases)
	}
}

func TestTranscribe_DiarizationFlag(t *testing.T) {
	t.Run("per request enables segments", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{APIKey: "k"})
		_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Speaker: speaker.Options{Diarization: true}})
		if err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if enabled, _ := mustLookup(t, got.Definition, "diarization", "enabled").(bool); !enabled {
			t.Error("diarization.enabled must be true when the request asks for speakers")
		}
		if ts := mustLookup(t, got.Definition, "enhancedMode", "modelOptions", "timestamps"); ts != "segment" {
			t.Errorf("timestamps = %v, want segment (speaker labels ride on segments)", ts)
		}
	})

	t.Run("provider default keeps word timestamps", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{APIKey: "k", Diarization: true, Timestamps: "word"})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if enabled, _ := mustLookup(t, got.Definition, "diarization", "enabled").(bool); !enabled {
			t.Error("diarization.enabled must be true from the provider default")
		}
		if ts := mustLookup(t, got.Definition, "enhancedMode", "modelOptions", "timestamps"); ts != "word" {
			t.Errorf("timestamps = %v, want the configured word granularity", ts)
		}
	})

	t.Run("none omits the field", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{APIKey: "k"})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if _, present := lookup(got.Definition, "enhancedMode", "modelOptions", "timestamps"); present {
			t.Error("timestamps must be omitted for the default none")
		}
	})
}

func TestTranscribe_ModelOverride(t *testing.T) {
	server, got := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k"})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Model: "MAI-Transcribe-1.5"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if model := mustLookup(t, got.Definition, "enhancedMode", "model"); model != "MAI-Transcribe-1.5" {
		t.Errorf("request model = %v", model)
	}
	if res.Model != "MAI-Transcribe-1.5" {
		t.Errorf("Result.Model = %q", res.Model)
	}
}

func TestTranscribe_Credentials(t *testing.T) {
	bearer := func(context.Context) (string, error) { return "entra-token", nil }

	t.Run("bearer only", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{BearerToken: bearer})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if got.Header.Get("Authorization") != "Bearer entra-token" {
			t.Errorf("Authorization = %q", got.Header.Get("Authorization"))
		}
		if got.Header.Get("Ocp-Apim-Subscription-Key") != "" {
			t.Error("no key header expected on the bearer path")
		}
	})

	t.Run("bearer wins over key", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{APIKey: "resource-key", BearerToken: bearer})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if got.Header.Get("Authorization") != "Bearer entra-token" {
			t.Errorf("Authorization = %q", got.Header.Get("Authorization"))
		}
		if got.Header.Get("Ocp-Apim-Subscription-Key") != "" {
			t.Error("the key must not be sent when a bearer token is available")
		}
	})

	t.Run("bearer error surfaces and nothing is sent", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		tokenErr := errors.New("az login required")
		p := newTestProvider(server, Options{APIKey: "resource-key", BearerToken: func(context.Context) (string, error) { return "", tokenErr }})
		_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{})
		if !errors.Is(err, tokenErr) {
			t.Fatalf("error = %v, want the token error wrapped", err)
		}
		if got.Calls.Load() != 0 {
			t.Error("no request must leave the process when the token cannot be minted")
		}
	})

	t.Run("no credential", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, successBody)
		p := newTestProvider(server, Options{})
		if _, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{}); err == nil {
			t.Fatal("expected an error without key or bearer token")
		}
		if got.Calls.Load() != 0 {
			t.Error("no request expected without a credential")
		}
	})
}
