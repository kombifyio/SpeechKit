// Package geminitranscribe adapts Google's Gemini Transcribe model
// (gemini-3.5-transcribe) on the Gemini API Interactions endpoint
// (POST https://generativelanguage.googleapis.com/v1beta/interactions) to
// [stt.STTProvider]. Audio is sent inline as base64 WAV, so short dictation
// clips need no File API upload. Live dictation
// ([speechkit.DictationStreamProvider]) streams on gemini-3.5-transcribe-live
// over the Gemini Live API. It is an opt-in bring-your-own-key provider and
// needs a Gemini API key and public https egress.
//
// Stability: Experimental — may change in any release.
package geminitranscribe

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// Wire shape source: the google-genai Python SDK's generated Interactions
// client (google/genai/_gaos):
//   - interactions.py: POST "/{api_version}/interactions";
//     sdkconfiguration.py and _api_client.py: server
//     https://generativelanguage.googleapis.com with api_version "v1beta"
//     for the Gemini API; _hooks/google_genai_auth.py: x-goog-api-key header.
//   - google_genai.py _normalize_create_body: a content list input is sent
//     as [{"type":"user_input","content":[...]}].
//   - types/interactions/audiocontent.py: {"type":"audio","data":<base64>,
//     "mime_type":"audio/wav"} — inline data is accepted beside "uri".
//   - types/interactions/generationconfig.py and transcriptionconfig.py:
//     generation_config.transcription_config.{language_codes,
//     custom_vocabulary, mode:{"type":"smart"|"verbatim"}}.
//   - types/interactions/interaction.py: output_text is NOT on the wire; the
//     SDK derives it from the trailing "text" content of the last
//     "model_output" step, which transcriptText mirrors.
const (
	defaultBaseURL   = "https://generativelanguage.googleapis.com"
	apiVersion       = "v1beta"
	audioMimeTypeWAV = "audio/wav"

	// DefaultModel is the GA Gemini Transcribe model used when none is set.
	DefaultModel = "gemini-3.5-transcribe"
)

// Mode selects Gemini Transcribe's transcription mode.
type Mode string

const (
	// ModeSmart removes filler words and resolves spoken self-corrections
	// ("Tuesday, actually wait, Wednesday" becomes "Wednesday") and formats
	// numbers and lists. That is what dictation inserts into a document, so
	// it is the default.
	ModeSmart Mode = "smart"
	// ModeVerbatim keeps every spoken word, filler and false start. It is
	// the API's own default and the mode word timestamps and diarization
	// require.
	ModeVerbatim Mode = "verbatim"
)

// Provider implements [stt.STTProvider] for Gemini Transcribe.
//
// BaseURL is validated against Validation on every request. The default
// Validation is strict (public https only).
type Provider struct {
	BaseURL string
	APIKey  string
	Model   string
	// Mode is sent as transcription_config.mode; empty means ModeSmart.
	Mode       Mode
	Validation netsec.ValidationOptions
	client     *http.Client
	// liveConnect replaces the Gemini Live dial in tests.
	liveConnect liveConnectFunc
}

// Options configures [New].
type Options struct {
	// APIKey is the Gemini API key.
	APIKey string
	// Model is the Gemini model. Empty selects [DefaultModel].
	Model string
}

// New creates a Gemini Transcribe provider. Zero Options values select the
// provider defaults.
func New(opts Options) *Provider {
	model := opts.Model
	if strings.TrimSpace(model) == "" {
		model = DefaultModel
	}
	p := &Provider{
		BaseURL: defaultBaseURL,
		APIKey:  opts.APIKey,
		Model:   model,
		Mode:    ModeSmart,
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

type interactionRequest struct {
	Model            string           `json:"model"`
	Input            []inputStep      `json:"input"`
	GenerationConfig generationConfig `json:"generation_config"`
	// Store false keeps the audio and transcript out of the interaction
	// history; SpeechKit never reads an interaction back.
	Store bool `json:"store"`
}

type inputStep struct {
	Type    string         `json:"type"`
	Content []audioContent `json:"content"`
}

type audioContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

type generationConfig struct {
	TranscriptionConfig transcriptionConfig `json:"transcription_config"`
}

type transcriptionConfig struct {
	LanguageCodes    []string          `json:"language_codes,omitempty"`
	CustomVocabulary []string          `json:"custom_vocabulary,omitempty"`
	Mode             transcriptionMode `json:"mode"`
}

type transcriptionMode struct {
	Type Mode `json:"type"`
}

type interactionResponse struct {
	Status string `json:"status"`
	Steps  []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"steps"`
}

func (p *Provider) endpoint(path string) (string, error) {
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return netsec.BuildEndpoint(baseURL, apiVersion+"/"+path, p.Validation)
}

// Transcribe implements [stt.STTProvider]: the audio is wrapped as WAV and
// sent inline with opts.Model, else the configured Model. The requested
// language and language hints become language_codes and keyterms become
// custom_vocabulary. The response carries no language, so the result
// Language echoes the requested locale or "multi".
func (p *Provider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	endpoint, err := p.endpoint("interactions")
	if err != nil {
		return nil, fmt.Errorf("gemini endpoint: %w", err)
	}

	model := p.Model
	if opts.Model != "" {
		model = opts.Model
	}
	mode := p.Mode
	if mode == "" {
		mode = ModeSmart
	}
	resolved := stt.ResolveTranscribeOptions("gemini", "stt.gemini.transcribe", opts, nil, nil)
	requestBody := interactionRequest{
		Model: model,
		Input: []inputStep{{
			Type: "user_input",
			Content: []audioContent{{
				Type:     "audio",
				Data:     base64.StdEncoding.EncodeToString(stt.EnsureTranscriptionWAV(audio)),
				MimeType: audioMimeTypeWAV,
			}},
		}},
		GenerationConfig: generationConfig{TranscriptionConfig: transcriptionConfig{
			LanguageCodes:    languageCodes(resolved.APILanguage(), resolved.LanguageHints),
			CustomVocabulary: nonEmpty(resolved.Keyterms),
			Mode:             transcriptionMode{Type: mode},
		}},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create gemini request: %w", err)
	}
	req.Header.Set("x-goog-api-key", p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini request: %w", stt.ClassifyTransportError("gemini", err))
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, stt.MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read gemini response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, stt.HTTPError("gemini", resp, respBody)
	}

	var result interactionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse gemini response: %w", err)
	}
	if result.Status != "" && result.Status != "completed" {
		return nil, fmt.Errorf("gemini interaction status %q", result.Status)
	}

	return &stt.Result{
		Text:     result.transcriptText(),
		Language: stt.FirstNonEmptyTrimmed(resolved.APILanguage(), stt.LanguageMulti),
		Duration: duration,
		Provider: p.Name(),
		Model:    model,
	}, nil
}

// transcriptText mirrors the SDK's output_text: the trailing run of text
// blocks in the last model_output step after the final user_input.
func (r interactionResponse) transcriptText() string {
	var parts []string
	collecting := false
	for i := len(r.Steps) - 1; i >= 0; i-- {
		step := r.Steps[i]
		if step.Type == "user_input" {
			break
		}
		if step.Type != "model_output" || len(step.Content) == 0 {
			if collecting {
				break
			}
			continue
		}
		stop := false
		for j := len(step.Content) - 1; j >= 0; j-- {
			if step.Content[j].Type == "text" {
				collecting = true
				parts = append(parts, step.Content[j].Text)
			} else if collecting {
				stop = true
				break
			}
		}
		if stop {
			break
		}
	}
	var b strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteString(parts[i])
	}
	return b.String()
}

// bareLocales maps a bare language to the regional BCP-47 code Gemini
// Transcribe documents (for example "es-ES"). Unlisted bare codes pass
// through unchanged.
var bareLocales = map[string]string{
	"de": "de-DE",
	"en": "en-US",
	"es": "es-ES",
	"fr": "fr-FR",
	"it": "it-IT",
	"pt": "pt-BR",
	"nl": "nl-NL",
	"ja": "ja-JP",
	"ko": "ko-KR",
	"hi": "hi-IN",
}

// languageCodes joins the pinned language and the language hints into
// language_codes, dropping multilanguage tokens and duplicates. A nil result
// omits the field, which is Gemini's automatic language detection.
func languageCodes(language string, hints []string) []string {
	var codes []string
	seen := map[string]bool{}
	for _, value := range append([]string{language}, hints...) {
		if stt.IsMultilanguage(value) {
			continue
		}
		code := strings.TrimSpace(value)
		if regional, ok := bareLocales[strings.ToLower(code)]; ok {
			code = regional
		}
		if key := strings.ToLower(code); !seen[key] {
			seen[key] = true
			codes = append(codes, code)
		}
	}
	return codes
}

func nonEmpty(values []string) []string {
	var out []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// Name returns "google": Gemini Transcribe takes the Google STT slot, and
// the router matches a selected stt.google.* profile by this name.
func (p *Provider) Name() string {
	return "google"
}

// Health issues an authenticated GET /v1beta/models/{model}; a transport
// failure or non-200 status is returned as the error.
func (p *Provider) Health(ctx context.Context) error {
	endpoint, err := p.endpoint("models/" + url.PathEscape(p.Model))
	if err != nil {
		return fmt.Errorf("gemini endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("x-goog-api-key", p.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("gemini health: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return stt.HTTPError("gemini health", resp, nil)
	}
	return nil
}

// Capabilities reports the speech-to-text baseline every provider satisfies.
func (*Provider) Capabilities() []speechkit.Capability { return stt.BaseCapabilities() }
