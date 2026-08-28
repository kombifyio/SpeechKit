package allproviders_test

import (
	"encoding/json"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/huggingface"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openaicompat"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openrouter"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/vps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/sttcontract"
)

// loopbackValidation accepts the httptest loopback server used by the
// conformance suite.
var loopbackValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true, AllowPrivate: true}

// jsonTextSuccess serves the OpenAI-/HF-/OpenRouter-compatible `{"text": ...}`
// shape that those providers parse a transcription out of.
func jsonTextSuccess(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": text})
	}
}

// deepgramSuccess serves Deepgram's nested `results.channels[].alternatives[]`
// envelope; the provider reads the first alternative's transcript.
func deepgramSuccess(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{
				"channels": []any{
					map[string]any{
						"alternatives": []any{
							map[string]any{"transcript": text, "confidence": 0.9},
						},
					},
				},
			},
		})
	}
}

// assemblyAISuccess fakes AssemblyAI's three-step transcription flow
// (upload -> create -> poll) with a single stateless handler: the poll GET
// returns "completed" immediately so the provider's poll loop exits on its
// first tick.
func assemblyAISuccess(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v2/upload"):
			_ = json.NewEncoder(w).Encode(map[string]string{"upload_url": "https://example.invalid/audio"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v2/transcript/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed", "text": text})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v2/transcript"):
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "contract-t1", "status": "queued"})
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}
}

// TestSTTProviderContract runs the shared conformance suite against the
// HTTP-backed STT providers that expose an overridable base URL.
func TestSTTProviderContract(t *testing.T) {
	cases := []sttcontract.Case{
		{
			Name:         "openai_compatible",
			ExpectedName: "vps",
			WantText:     "the quick brown fox",
			NewProvider: func(baseURL string) stt.STTProvider {
				return vps.New(baseURL, "contract-key")
			},
			Success: jsonTextSuccess("the quick brown fox"),
		},
		{
			Name:         "huggingface",
			ExpectedName: "huggingface",
			WantText:     "hallo welt",
			NewProvider: func(baseURL string) stt.STTProvider {
				p := huggingface.New("test-model", "test-token")
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				return p
			},
			Success: jsonTextSuccess("hallo welt"),
		},
		{
			Name:         "openrouter",
			ExpectedName: "openrouter",
			WantText:     "transcribed via contract",
			NewProvider: func(baseURL string) stt.STTProvider {
				p := openrouter.New("contract-key", "openai/whisper-1")
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				return p
			},
			Success: jsonTextSuccess("transcribed via contract"),
		},
		{
			Name:         "deepgram",
			ExpectedName: "deepgram",
			WantText:     "guten morgen",
			NewProvider: func(baseURL string) stt.STTProvider {
				p := deepgram.New("contract-key", "nova-3")
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				return p
			},
			Success: deepgramSuccess("guten morgen"),
		},
		{
			Name:         "assemblyai",
			ExpectedName: "assemblyai",
			WantText:     "transkribiert per assemblyai",
			NewProvider: func(baseURL string) stt.STTProvider {
				p := assemblyai.New("contract-key", "")
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				p.PollInterval = time.Millisecond // poll returns "completed" on first tick
				return p
			},
			Success: assemblyAISuccess("transkribiert per assemblyai"),
		},
		{
			Name:         "openai",
			ExpectedName: "openai",
			WantText:     "transcribed via openai",
			NewProvider: func(baseURL string) stt.STTProvider {
				p := openaicompat.NewOpenAI("contract-key")
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				return p
			},
			Success: jsonTextSuccess("transcribed via openai"),
		},
		{
			Name:         "groq",
			ExpectedName: "groq",
			WantText:     "transcribed via groq",
			NewProvider: func(baseURL string) stt.STTProvider {
				p := openaicompat.NewGroq("contract-key")
				p.BaseURL = baseURL
				p.Validation = loopbackValidation
				return p
			},
			Success: jsonTextSuccess("transcribed via groq"),
		},
		{
			Name:         "ollama",
			ExpectedName: "ollama",
			WantText:     "transcribed via ollama",
			NewProvider: func(baseURL string) stt.STTProvider {
				p := openaicompat.NewOllama(baseURL, "contract-model")
				p.Validation = loopbackValidation
				return p
			},
			Success: jsonTextSuccess("transcribed via ollama"),
		},
	}

	for _, c := range cases {
		sttcontract.RunContract(t, c)
	}
}
