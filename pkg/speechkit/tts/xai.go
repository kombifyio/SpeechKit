package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

const (
	xaiTTSBaseURL      = "https://api.x.ai"
	xaiTTSPath         = "v1/tts"
	xaiTTSDefaultVoice = "eve"
	xaiTTSSampleRate   = 24000
	xaiTTSMP3BitRate   = 128000
	xaiTTSMaxChars     = 60000
	xaiTTSMaxAudio     = 64 << 20
	xaiTTSMinSpeed     = 0.7
	xaiTTSMaxSpeed     = 1.5
)

// XAI implements Provider using xAI's Grok Voice text-to-speech REST API
// (POST /v1/tts). It is an opt-in bring-your-own-key provider that reuses the
// XAI_API_KEY shared with the STT, Assist and Voice Agent adapters. A voice is
// a voice_id from GET /v1/tts/voices (every voice speaks every supported
// language) or a custom voice id; the API has no separate model parameter.
//
// BaseURL is configurable for testing and validated against Validation on
// every request. Default Validation is strict (public https only).
type XAI struct {
	apiKey     string
	voice      string
	BaseURL    string
	Validation netsec.ValidationOptions
	client     *http.Client
}

// XAIOpts configures the xAI TTS provider.
type XAIOpts struct {
	APIKey string
	// Voice is a Grok voice_id such as "eve" (the default), "ara" or "rex".
	Voice string
}

// NewXAI creates an xAI TTS provider.
func NewXAI(opts XAIOpts) *XAI {
	voice := strings.TrimSpace(opts.Voice)
	if voice == "" {
		voice = xaiTTSDefaultVoice
	}
	p := &XAI{
		apiKey:  strings.TrimSpace(opts.APIKey),
		voice:   voice,
		BaseURL: xaiTTSBaseURL,
		// Validation zero-value = strict: public https only.
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 60 * time.Second, DialValidation: &p.Validation})
	return p
}

type xaiTTSRequest struct {
	Text         string             `json:"text"`
	VoiceID      string             `json:"voice_id"`
	Language     string             `json:"language"`
	OutputFormat xaiTTSOutputFormat `json:"output_format"`
	Speed        float64            `json:"speed,omitempty"`
}

type xaiTTSOutputFormat struct {
	Codec      string `json:"codec"`
	SampleRate int    `json:"sample_rate"`
	BitRate    int    `json:"bit_rate,omitempty"`
}

// Name returns "xai".
func (x *XAI) Name() string { return "xai" }

// Kind reports [ProviderKindDirectProvider]: xAI's own API.
func (x *XAI) Kind() ProviderKind { return ProviderKindDirectProvider }

// Synthesize renders text with Grok Voice. The voice is the resolved Voice
// option, else the configured Voice, else "eve". Format selects mp3 (default),
// wav or pcm (raw signed 16-bit mono), all at 24 kHz; any other format, opus
// included, becomes mp3. The locale maps to the closest xAI language code, or
// "auto" when none fits. Speed is clamped to xAI's 0.7–1.5. It fails on empty
// text, text over xAI's 60,000-character limit, a missing API key, an endpoint
// that fails validation, a transport error, or a non-200 response.
func (x *XAI) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("xai tts: empty text")
	}
	if n := len([]rune(text)); n > xaiTTSMaxChars {
		return nil, fmt.Errorf("xai tts: text has %d characters, the limit is %d", n, xaiTTSMaxChars)
	}
	if x.apiKey == "" {
		return nil, fmt.Errorf("xai tts: no API key configured")
	}

	base := strings.TrimSpace(x.BaseURL)
	if base == "" {
		base = xaiTTSBaseURL
	}
	endpoint, err := netsec.BuildEndpoint(base, xaiTTSPath, x.Validation)
	if err != nil {
		return nil, fmt.Errorf("xai tts: endpoint: %w", err)
	}

	resolved := ResolveSynthesizeOptions("xai", "tts.xai.grok-voice", opts, provideropts.Values{
		provideropts.OptionVoice:       x.voice,
		provideropts.OptionAudioFormat: "mp3",
	}, nil)
	voice := strings.TrimSpace(resolved.Voice)
	if voice == "" {
		voice = x.voice
	}

	codec, format := xaiTTSCodec(resolved.Format)
	output := xaiTTSOutputFormat{Codec: codec, SampleRate: xaiTTSSampleRate}
	if codec == "mp3" {
		output.BitRate = xaiTTSMP3BitRate
	}
	reqBody := xaiTTSRequest{
		Text:         text,
		VoiceID:      voice,
		Language:     xaiTTSLanguage(resolved.Locale),
		OutputFormat: output,
		Speed:        xaiTTSSpeed(resolved.Speed),
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("xai tts: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("xai tts: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+x.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := x.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xai tts: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, netsec.ProviderStatusError("xai tts", resp.StatusCode, errBody)
	}
	// Read one byte past the limit so an oversized answer is an error, never
	// truncated audio returned as success.
	audio, err := io.ReadAll(io.LimitReader(resp.Body, xaiTTSMaxAudio+1))
	if err != nil {
		return nil, fmt.Errorf("xai tts: read response: %w", err)
	}
	if len(audio) > xaiTTSMaxAudio {
		return nil, fmt.Errorf("xai tts: audio response exceeds the %d MiB limit", xaiTTSMaxAudio>>20)
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("xai tts: empty audio response")
	}

	return &Result{
		Audio:      audio,
		Format:     format,
		SampleRate: xaiTTSSampleRate,
		Provider:   "xai",
		Voice:      voice,
	}, nil
}

// xaiTTSCodec maps a generic output format to the xAI codec and the format
// name the Result reports. xAI offers no Opus, so it falls back to mp3.
func xaiTTSCodec(format string) (codec, reported string) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav", "wave":
		return "wav", "wav"
	case "pcm", "pcm16", "linear16", "s16le":
		return "pcm", "pcm"
	default:
		return "mp3", "mp3"
	}
}

// xaiTTSSpeed clamps speed to xAI's accepted range; zero keeps the default.
func xaiTTSSpeed(speed float64) float64 {
	switch {
	case speed <= 0:
		return 0
	case speed < xaiTTSMinSpeed:
		return xaiTTSMinSpeed
	case speed > xaiTTSMaxSpeed:
		return xaiTTSMaxSpeed
	default:
		return speed
	}
}

// xaiTTSLanguages is xAI's documented language list, keyed by lower-case code.
var xaiTTSLanguages = map[string]string{
	"en": "en", "bn": "bn", "zh": "zh", "fr": "fr", "de": "de", "hi": "hi",
	"id": "id", "it": "it", "ja": "ja", "ko": "ko", "ru": "ru", "tr": "tr",
	"vi": "vi", "ar-eg": "ar-EG", "ar-sa": "ar-SA", "ar-ae": "ar-AE",
	"pt-br": "pt-BR", "pt-pt": "pt-PT", "es-mx": "es-MX", "es-es": "es-ES",
}

// xaiTTSLanguage maps a SpeechKit locale ("de", "en-US", "zh-Hans", "es")
// to the closest language xAI documents, or "auto" so the model detects the
// language when the locale is unset or has no match. xAI requires the field.
func xaiTTSLanguage(locale string) string {
	l := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(locale, "_", "-")))
	if l == "" || l == "auto" || l == "multi" {
		return "auto"
	}
	if code, ok := xaiTTSLanguages[l]; ok {
		return code
	}
	base, region, _ := strings.Cut(l, "-")
	switch base {
	case "pt":
		return "pt-BR"
	case "es":
		if region == "" {
			return "es-ES"
		}
		return "es-MX" // Latin American Spanish
	case "ar":
		return "ar-SA"
	}
	if code, ok := xaiTTSLanguages[base]; ok {
		return code
	}
	return "auto"
}

// CloseIdleConnections drops idle keep-alive connections in the provider's
// HTTP client. It is safe on a nil receiver.
func (x *XAI) CloseIdleConnections() {
	if x != nil && x.client != nil {
		x.client.CloseIdleConnections()
	}
}

// Health reports whether a key is configured and the API accepts it. It calls
// GET /v1/tts/voices, which is free, instead of synthesizing a billable clip.
func (x *XAI) Health(ctx context.Context) error {
	if x.apiKey == "" {
		return fmt.Errorf("xai tts: no API key configured")
	}
	base := strings.TrimSpace(x.BaseURL)
	if base == "" {
		base = xaiTTSBaseURL
	}
	endpoint, err := netsec.BuildEndpoint(base, xaiTTSPath+"/voices", x.Validation)
	if err != nil {
		return fmt.Errorf("xai tts: endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("xai tts: create health request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+x.apiKey)
	resp, err := x.client.Do(req)
	if err != nil {
		return fmt.Errorf("xai tts: health check failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	if resp.StatusCode != http.StatusOK {
		return netsec.ProviderStatusError("xai tts health", resp.StatusCode, nil)
	}
	return nil
}
