package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// Gemini 3.8 TTS model ids served by the Gemini API Interactions endpoint.
const (
	// GeminiTTSModelFlash is the flagship Gemini 3.8 speech model.
	GeminiTTSModelFlash = "gemini-3.8-flash-tts"
	// GeminiTTSModelFlashLite is the high-throughput Gemini 3.8 speech model.
	GeminiTTSModelFlashLite = "gemini-3.8-flash-lite-tts"
	// GeminiTTSDefaultModel is used when [GeminiOpts.Model] is empty.
	GeminiTTSDefaultModel = GeminiTTSModelFlashLite
	// GeminiTTSDefaultVoice is the prebuilt voice used when none is configured.
	GeminiTTSDefaultVoice = "Kore"
)

// The endpoint mirrors the generated google-genai client: base URL from
// _gaos/sdkconfiguration.py SERVERS, api_version "v1beta" from
// _api_client.py (Gemini API mode), path "/{api_version}/interactions" from
// _gaos/interactions.py, and the API key header from
// _gaos/_hooks/google_genai_auth.py.
const (
	geminiTTSBaseURL    = "https://generativelanguage.googleapis.com"
	geminiTTSPath       = "v1beta/interactions"
	geminiTTSSampleRate = 24000
	geminiTTSMaxAudio   = 64 << 20
	// geminiTTSMaxBody bounds the JSON response: base64 inflates the audio by
	// 4/3, plus headroom for the surrounding interaction fields.
	geminiTTSMaxBody = geminiTTSMaxAudio/3*4 + 1<<20
)

// Gemini implements Provider using the Gemini API text-to-speech models
// (Gemini 3.8 Flash TTS and Flash-Lite TTS) through the unary Interactions
// endpoint. The unary response carries a complete 24 kHz mono 16-bit WAV;
// raw L16 PCM is wrapped in a WAV header so callers always get a playable
// file unless they asked for "pcm".
//
// BaseURL is configurable for testing. It is validated against Validation
// on every request. Default Validation is strict (public https only).
type Gemini struct {
	apiKey     string
	model      string
	voice      string
	BaseURL    string
	Validation netsec.ValidationOptions
	client     *http.Client
}

// GeminiOpts configures the Gemini TTS provider.
type GeminiOpts struct {
	APIKey string
	Model  string // GeminiTTSModelFlash or GeminiTTSModelFlashLite; empty => GeminiTTSDefaultModel
	Voice  string // prebuilt voice such as "Kore", "Puck", "Charon"; empty => GeminiTTSDefaultVoice
}

// NewGemini creates a Gemini API TTS provider.
func NewGemini(opts GeminiOpts) *Gemini {
	p := &Gemini{
		apiKey:  strings.TrimSpace(opts.APIKey),
		model:   firstNonEmptyTTS(strings.TrimSpace(opts.Model), GeminiTTSDefaultModel),
		voice:   firstNonEmptyTTS(strings.TrimSpace(opts.Voice), GeminiTTSDefaultVoice),
		BaseURL: geminiTTSBaseURL,
		// Validation zero-value = strict: public https only.
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 60 * time.Second, DialValidation: &p.Validation})
	return p
}

// geminiInteractionRequest is the CreateModelInteraction body. Field names
// follow _gaos/types/interactions/createmodelinteraction.py,
// generationconfig.py, speechconfig.py and audioresponseformat.py, which
// declare no aliases, so the wire uses the snake_case attribute names.
type geminiInteractionRequest struct {
	Model            string                 `json:"model"`
	Input            string                 `json:"input"`
	ResponseFormat   geminiResponseFormat   `json:"response_format"`
	GenerationConfig geminiGenerationConfig `json:"generation_config"`
}

type geminiResponseFormat struct {
	Type string `json:"type"`
}

type geminiGenerationConfig struct {
	SpeechConfig []geminiSpeechConfig `json:"speech_config"`
}

type geminiSpeechConfig struct {
	Voice    string `json:"voice"`
	Language string `json:"language,omitempty"`
}

// geminiInteraction is the subset of _gaos/types/interactions/interaction.py
// the provider reads. Audio arrives as an "audio" content item
// (audiocontent.py) inside the trailing "model_output" steps
// (modeloutputstep.py); the SDK derives output_audio from those steps
// (_output_properties_from_steps in _gaos/google_genai.py), so the top-level
// field is only a fallback.
type geminiInteraction struct {
	Status      string                   `json:"status"`
	Steps       []geminiStep             `json:"steps"`
	OutputAudio *geminiAudioContent      `json:"output_audio"`
	Errors      []geminiInteractionError `json:"errors"`
}

type geminiStep struct {
	Type    string               `json:"type"`
	Content []geminiAudioContent `json:"content"`
}

type geminiAudioContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

type geminiInteractionError struct {
	Code string `json:"code"`
}

// Synthesize renders text with the configured Gemini TTS model. The voice is
// the resolved Voice option, else the configured voice; a request locale is
// forwarded as the speech language. The result is WAV at 24 kHz unless
// Format is "pcm", in which case the WAV header is stripped. It fails on
// empty text, a missing API key, an endpoint that fails validation, a
// transport error, a non-200 response, or a response without inline audio.
func (g *Gemini) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("gemini tts: empty text")
	}
	if g.apiKey == "" {
		return nil, fmt.Errorf("gemini tts: no API key configured")
	}

	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTTS(g.BaseURL, geminiTTSBaseURL), geminiTTSPath, g.Validation)
	if err != nil {
		return nil, fmt.Errorf("gemini tts: endpoint: %w", err)
	}

	resolved := ResolveSynthesizeOptions("gemini", "tts.gemini.gemini-3.8-tts", opts, provideropts.Values{
		provideropts.OptionVoice:       g.voice,
		provideropts.OptionAudioFormat: "wav",
	}, nil)
	voice := firstNonEmptyTTS(resolved.Voice, g.voice)

	body, err := json.Marshal(geminiInteractionRequest{
		Model:            g.model,
		Input:            text,
		ResponseFormat:   geminiResponseFormat{Type: "audio"},
		GenerationConfig: geminiGenerationConfig{SpeechConfig: []geminiSpeechConfig{{Voice: voice, Language: resolved.Locale}}},
	})
	if err != nil {
		return nil, fmt.Errorf("gemini tts: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini tts: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini tts: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, netsec.ProviderStatusError("gemini tts", resp.StatusCode, errBody)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, geminiTTSMaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("gemini tts: read response: %w", err)
	}
	if len(raw) > geminiTTSMaxBody {
		return nil, fmt.Errorf("gemini tts: response exceeds %d bytes", geminiTTSMaxBody)
	}
	var interaction geminiInteraction
	if err := json.Unmarshal(raw, &interaction); err != nil {
		return nil, fmt.Errorf("gemini tts: decode response: %w", err)
	}
	content := interaction.audio()
	if content == nil || content.Data == "" {
		if len(interaction.Errors) > 0 {
			return nil, fmt.Errorf("gemini tts: interaction %s without audio (error code %q)", interaction.Status, interaction.Errors[0].Code)
		}
		return nil, fmt.Errorf("gemini tts: interaction %s without inline audio", interaction.Status)
	}
	audio, err := base64.StdEncoding.DecodeString(content.Data)
	if err != nil {
		return nil, fmt.Errorf("gemini tts: decode audio: %w", err)
	}

	result := &Result{Format: "wav", SampleRate: geminiTTSSampleRate, Provider: "gemini", Voice: voice}
	if bytes.HasPrefix(audio, []byte("RIFF")) {
		result.Audio = audio
		result.SampleRate = readWAVSampleRate(audio)
	} else {
		// Raw L16 PCM (the streaming shape) is 24 kHz mono 16-bit.
		result.Audio = encodeAuraWAV(audio)
	}
	if strings.EqualFold(resolved.Format, "pcm") {
		pcm, ok := wavPCMData(result.Audio)
		if !ok {
			return nil, fmt.Errorf("gemini tts: malformed WAV response")
		}
		result.Audio, result.Format = pcm, "pcm"
	}
	return result, nil
}

// audio returns the last audio content of the trailing model_output steps,
// mirroring the SDK's output_audio derivation, else the top-level
// output_audio.
func (i geminiInteraction) audio() *geminiAudioContent {
	for s := len(i.Steps) - 1; s >= 0; s-- {
		step := i.Steps[s]
		if step.Type == "user_input" {
			break
		}
		if step.Type != "model_output" {
			continue
		}
		for c := len(step.Content) - 1; c >= 0; c-- {
			if step.Content[c].Type == "audio" {
				return &step.Content[c]
			}
		}
	}
	return i.OutputAudio
}

// wavPCMData returns the payload of the "data" chunk of a RIFF/WAVE file,
// walking chunks so headers with extra chunks (LIST, fact) are handled.
func wavPCMData(wav []byte) ([]byte, bool) {
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, false
	}
	for off := 12; off+8 <= len(wav); {
		id := string(wav[off : off+4])
		size := int(binary.LittleEndian.Uint32(wav[off+4 : off+8])) // #nosec G115 -- a negative size on 32-bit hosts is rejected below.
		start := off + 8
		if id == "data" {
			// Streamed WAVs may carry a placeholder size; clamp to what arrived.
			end := start + size
			if size < 0 || end > len(wav) || end < start {
				end = len(wav)
			}
			return wav[start:end], true
		}
		if size < 0 || start+size > len(wav) {
			return nil, false
		}
		off = start + size + size%2 // chunks are word-aligned
	}
	return nil, false
}

// Name implements [Provider]; it returns "gemini".
func (g *Gemini) Name() string { return "gemini" }

// Kind implements [Provider]; the Gemini API is a direct cloud provider.
func (g *Gemini) Kind() ProviderKind { return ProviderKindDirectProvider }

// CloseIdleConnections releases idle keep-alive connections of the
// provider's HTTP client.
func (g *Gemini) CloseIdleConnections() {
	if g != nil && g.client != nil {
		g.client.CloseIdleConnections()
	}
}

// Health reports an error when no API key is configured; it makes no
// network call.
func (g *Gemini) Health(ctx context.Context) error {
	if g.apiKey == "" {
		return fmt.Errorf("gemini tts: no API key configured")
	}
	return nil
}
