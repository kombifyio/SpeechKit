package tts_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/netsec"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/internal/tts/ttscontract"
)

// loopbackValidation accepts the httptest loopback server used by the
// conformance suite.
var loopbackValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true, AllowPrivate: true}

// rawAudioSuccess serves raw audio bytes with the given content type, the
// shape OpenAI and HuggingFace TTS return.
func rawAudioSuccess(contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte("contract-fake-audio"))
	}
}

// googleAudioSuccess serves the Google base64 `{"audioContent": ...}` envelope.
func googleAudioSuccess() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"audioContent": base64.StdEncoding.EncodeToString([]byte("contract-fake-audio")),
		})
	}
}

// TestTTSProviderContract runs the shared conformance suite against the
// HTTP-backed TTS providers that expose an overridable base URL.
func TestTTSProviderContract(t *testing.T) {
	cases := []ttscontract.Case{
		{
			Name:         "openai",
			ExpectedName: "openai",
			NewProvider: func(baseURL string) tts.Provider {
				p := tts.NewOpenAI(tts.OpenAIOpts{APIKey: "contract-key"})
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				return p
			},
			Success: rawAudioSuccess("audio/mpeg"),
		},
		{
			Name:         "google",
			ExpectedName: "google",
			NewProvider: func(baseURL string) tts.Provider {
				p := tts.NewGoogle(tts.GoogleOpts{APIKey: "contract-key"})
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				return p
			},
			Success: googleAudioSuccess(),
		},
		{
			Name:         "huggingface",
			ExpectedName: "huggingface",
			NewProvider: func(baseURL string) tts.Provider {
				p := tts.NewHuggingFace(tts.HuggingFaceOpts{Token: "contract-token"})
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				return p
			},
			Success: rawAudioSuccess("audio/flac"),
		},
	}

	for _, c := range cases {
		ttscontract.RunContract(t, c)
	}
}
