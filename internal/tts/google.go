package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

const (
	googleTTSBaseURL      = "https://texttospeech.googleapis.com"
	googleTTSPath         = "v1/text:synthesize"
	googleDefaultVoice    = "en-US-Neural2-J"
	googleDefaultLanguage = "en-US"
	googleSampleRate      = 24000
)

// Google implements Provider using the Google Cloud Text-to-Speech API.
//
// BaseURL is configurable for testing. It is validated against Validation
// on every request. Default Validation is strict (public https only).
type Google struct {
	apiKey     string
	voice      string
	BaseURL    string
	Validation netsec.ValidationOptions
	client     *http.Client
}

// GoogleOpts configures the Google TTS provider.
type GoogleOpts struct {
	APIKey string
	Voice  string // e.g. "de-DE-Neural2-B", "en-US-Neural2-J"
}

// NewGoogle creates a Google Cloud TTS provider.
func NewGoogle(opts GoogleOpts) *Google {
	voice := opts.Voice
	if voice == "" {
		voice = googleDefaultVoice
	}
	return &Google{
		apiKey:  opts.APIKey,
		voice:   voice,
		BaseURL: googleTTSBaseURL,
		// Validation zero-value = strict: public https only.
		client: netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second}),
	}
}

type googleTTSRequest struct {
	Input       googleTTSInput       `json:"input"`
	Voice       googleTTSVoice       `json:"voice"`
	AudioConfig googleTTSAudioConfig `json:"audioConfig"`
}

type googleTTSInput struct {
	Text string `json:"text"`
}

type googleTTSVoice struct {
	LanguageCode string `json:"languageCode"`
	Name         string `json:"name"`
}

type googleTTSAudioConfig struct {
	AudioEncoding   string  `json:"audioEncoding"`
	SpeakingRate    float64 `json:"speakingRate,omitempty"`
	SampleRateHertz int     `json:"sampleRateHertz,omitempty"`
}

type googleTTSResponse struct {
	AudioContent string `json:"audioContent"` // base64 encoded
}

// localeToLanguageCode maps short locale to Google TTS language code.
func localeToLanguageCode(locale string) string {
	switch locale {
	case "de", "de-DE":
		return "de-DE"
	case "en", "en-US":
		return "en-US"
	case "en-GB":
		return "en-GB"
	case "fr", "fr-FR":
		return "fr-FR"
	case "es", "es-ES":
		return "es-ES"
	case "it", "it-IT":
		return "it-IT"
	case "pt", "pt-BR":
		return "pt-BR"
	case "ja", "ja-JP":
		return "ja-JP"
	case "ko", "ko-KR":
		return "ko-KR"
	case "zh", "zh-CN":
		return "zh-CN"
	default:
		return googleDefaultLanguage
	}
}

// voiceForLocale returns a suitable Google voice for the given locale if the
// configured default voice doesn't match the locale's language.
func (g *Google) voiceForLocale(locale string) string {
	langCode := localeToLanguageCode(locale)
	// If the configured voice already matches the locale prefix, use it.
	if len(g.voice) >= 2 && g.voice[:2] == langCode[:2] {
		return g.voice
	}
	// Fallback voices per language.
	switch langCode {
	case "de-DE":
		return "de-DE-Neural2-B"
	case "en-US":
		return "en-US-Neural2-J"
	case "en-GB":
		return "en-GB-Neural2-B"
	case "fr-FR":
		return "fr-FR-Neural2-B"
	case "es-ES":
		return "es-ES-Neural2-B"
	default:
		return g.voice
	}
}

func (g *Google) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	if text == "" {
		return nil, fmt.Errorf("google tts: empty text")
	}

	base := g.BaseURL
	if base == "" {
		base = googleTTSBaseURL
	}
	validated, err := netsec.BuildEndpoint(base, googleTTSPath, g.Validation)
	if err != nil {
		return nil, fmt.Errorf("google tts: endpoint: %w", err)
	}
	q := url.Values{}
	q.Set("key", g.apiKey)
	endpoint := validated + "?" + q.Encode()

	voice := opts.Voice
	locale := opts.Locale
	if locale == "" {
		locale = "en-US"
	}
	langCode := localeToLanguageCode(locale)
	if voice == "" {
		voice = g.voiceForLocale(locale)
	}

	// Google TTS returns LINEAR16 (WAV) or MP3.
	audioEncoding := "MP3"
	format := "mp3"
	if opts.Format == "wav" || opts.Format == "pcm" {
		audioEncoding = "LINEAR16"
		format = "wav"
	}

	speed := opts.Speed
	if speed <= 0 {
		speed = 1.0
	}

	reqBody := googleTTSRequest{
		Input: googleTTSInput{Text: text},
		Voice: googleTTSVoice{
			LanguageCode: langCode,
			Name:         voice,
		},
		AudioConfig: googleTTSAudioConfig{
			AudioEncoding:   audioEncoding,
			SpeakingRate:    speed,
			SampleRateHertz: googleSampleRate,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("google tts: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("google tts: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google tts: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("google tts: HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	var ttsResp googleTTSResponse
	if err := json.NewDecoder(resp.Body).Decode(&ttsResp); err != nil {
		return nil, fmt.Errorf("google tts: decode response: %w", err)
	}

	audio, err := base64.StdEncoding.DecodeString(ttsResp.AudioContent)
	if err != nil {
		return nil, fmt.Errorf("google tts: decode audio: %w", err)
	}

	return &Result{
		Audio:      audio,
		Format:     format,
		SampleRate: googleSampleRate,
		Provider:   "google",
		Voice:      voice,
	}, nil
}

func (g *Google) Name() string { return "google" }

func (g *Google) Health(ctx context.Context) error {
	if g.apiKey == "" {
		return fmt.Errorf("google tts: no API key configured")
	}
	return nil
}
