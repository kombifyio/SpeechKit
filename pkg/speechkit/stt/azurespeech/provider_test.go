package azurespeech

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/sttcontract"
)

func TestHealth(t *testing.T) {
	bearer := func(context.Context) (string, error) { return "entra-token", nil }

	t.Run("ok", func(t *testing.T) {
		server, got := fakeService(t, http.StatusOK, `{"values":[]}`)
		p := newTestProvider(server, Options{APIKey: "k"})
		if err := p.Health(context.Background()); err != nil {
			t.Fatalf("Health: %v", err)
		}
		if got.Method != http.MethodGet || got.Path != "/speechtotext/models/base" {
			t.Errorf("health hit %s %s", got.Method, got.Path)
		}
		if got.Query["api-version"] != APIVersion || got.Query["top"] != "1" {
			t.Errorf("health query = %v", got.Query)
		}
		if got.Header.Get("Ocp-Apim-Subscription-Key") != "k" {
			t.Error("health must use the same credential as transcription")
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		server, _ := fakeService(t, http.StatusUnauthorized, "")
		p := newTestProvider(server, Options{APIKey: "k"})
		err := p.Health(context.Background())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "key") {
			t.Errorf("a 401 on the key path must point at the key: %v", err)
		}
	})

	t.Run("missing role", func(t *testing.T) {
		server, _ := fakeService(t, http.StatusForbidden, "")
		p := newTestProvider(server, Options{BearerToken: bearer})
		err := p.Health(context.Background())
		if err == nil || !strings.Contains(err.Error(), "Cognitive Services User") {
			t.Errorf("a 403 on the bearer path must point at the roles: %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server, _ := fakeService(t, http.StatusInternalServerError, "")
		p := newTestProvider(server, Options{APIKey: "k"})
		if err := p.Health(context.Background()); err == nil {
			t.Error("expected an error for HTTP 500")
		}
	})
}

func TestEndpoint_BareHostBecomesHTTPS(t *testing.T) {
	// The colon in "transcriptions:transcribe" must survive URL building
	// unescaped; the service does not accept %3A.
	p := New(Options{Host: "myresource.cognitiveservices.azure.com"})
	got, err := p.endpoint(transcribePath, "api-version="+APIVersion)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	want := "https://myresource.cognitiveservices.azure.com/speechtotext/transcriptions:transcribe?api-version=" + APIVersion
	if got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}
	if _, err := New(Options{}).endpoint(transcribePath, ""); err == nil {
		t.Error("an empty host must be rejected before any request")
	}
}

func TestCapabilitiesIncludeDiarization(t *testing.T) {
	caps := New(Options{}).Capabilities()
	found := false
	for _, c := range caps {
		if c == speechkit.CapabilitySpeakerDiarization {
			found = true
		}
	}
	if !found {
		t.Errorf("capabilities = %v, want speaker diarization", caps)
	}
}

func TestShortLocale(t *testing.T) {
	cases := map[string]string{
		"de-DE": "de",
		"en-US": "en",
		"pt-BR": "pt",
		"zh-CN": "zh",
		"nb-NO": "nb",
		"yue":   "yue",
		"fil":   "fil",
		"DE":    "de",
		"de_AT": "de",
		" en ":  "en",
		"":      "",
	}
	for in, want := range cases {
		if got := ShortLocale(in); got != want {
			t.Errorf("ShortLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsMAITranscribeModel(t *testing.T) {
	cases := map[string]bool{
		"MAI-Transcribe-2":        true,
		"mai-transcribe-1.5":      true,
		" MAI-Transcribe-Medical": true,
		"gpt-4o-mini-transcribe":  false,
		"whisper-1":               false,
		"":                        false,
	}
	for in, want := range cases {
		if got := IsMAITranscribeModel(in); got != want {
			t.Errorf("IsMAITranscribeModel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestProviderContract(t *testing.T) {
	sttcontract.RunContract(t, sttcontract.Case{
		Name:         "azurespeech",
		ExpectedName: "foundry",
		WantText:     "Hallo Welt. Guten Tag.",
		NewProvider: func(baseURL string) stt.STTProvider {
			p := New(Options{Host: baseURL, APIKey: "contract-key"})
			p.Validation = testValidation
			return p
		},
		Success: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, successBody)
		},
	})
}
