package stt

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
	googleSTTBaseURL       = "https://speech.googleapis.com"
	googleMaxResponseBytes = 1 << 20
)

// GoogleSTTProvider implements STTProvider for Google Cloud Speech-to-Text v1 REST API.
//
// BaseURL is user-configurable (for testing or regional endpoints). It is
// validated against Validation on every request. Default Validation is strict
// (public https only).
type GoogleSTTProvider struct {
	APIKey     string
	Model      string // "chirp_3", "chirp_2", "latest_long"
	BaseURL    string // Override for testing; defaults to googleSTTBaseURL
	Validation netsec.ValidationOptions
	client     *http.Client
}

// NewGoogleSTTProvider creates a provider for Google Cloud Speech-to-Text.
// Model defaults to "chirp_3" if empty.
func NewGoogleSTTProvider(apiKey, model string) *GoogleSTTProvider {
	if model == "" {
		model = "chirp_3"
	}
	return &GoogleSTTProvider{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: googleSTTBaseURL,
		// Validation zero-value = strict: public https only.
		client: netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second}),
	}
}

// googleRecognizeRequest is the request body for the v1 speech:recognize endpoint.
type googleRecognizeRequest struct {
	Config googleRecognitionConfig `json:"config"`
	Audio  googleRecognitionAudio  `json:"audio"`
}

type googleRecognitionConfig struct {
	Encoding        string `json:"encoding"`
	SampleRateHertz int    `json:"sampleRateHertz"`
	LanguageCode    string `json:"languageCode,omitempty"`
	Model           string `json:"model,omitempty"`
}

type googleRecognitionAudio struct {
	Content string `json:"content"` // base64-encoded audio
}

// googleRecognizeResponse is the response from the v1 speech:recognize endpoint.
type googleRecognizeResponse struct {
	Results []struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"results"`
}

// mapLanguageCode maps short language codes to BCP-47 codes expected by Google.
func mapLanguageCode(lang string) string {
	switch lang {
	case "de":
		return "de-DE"
	case "en":
		return "en-US"
	case "fr":
		return "fr-FR"
	case "es":
		return "es-ES"
	case "it":
		return "it-IT"
	case "auto", "":
		return ""
	default:
		return lang
	}
}

// googleEndpoint builds a validated Google STT URL with an api-key query param.
func (p *GoogleSTTProvider) googleEndpoint(path string) (string, error) {
	base := p.BaseURL
	if base == "" {
		base = googleSTTBaseURL
	}
	validated, err := netsec.BuildEndpoint(base, path, p.Validation)
	if err != nil {
		return "", fmt.Errorf("google endpoint: %w", err)
	}
	q := url.Values{}
	q.Set("key", p.APIKey)
	return validated + "?" + q.Encode(), nil
}

// Transcribe sends audio to Google Cloud Speech-to-Text v1 REST API.
func (p *GoogleSTTProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	endpoint, err := p.googleEndpoint("v1/speech:recognize")
	if err != nil {
		return nil, err
	}

	model := p.Model
	if opts.Model != "" {
		model = opts.Model
	}

	langCode := mapLanguageCode(opts.Language)

	reqBody := googleRecognizeRequest{
		Config: googleRecognitionConfig{
			Encoding:        "LINEAR16",
			SampleRateHertz: 16000,
			LanguageCode:    langCode,
			Model:           model,
		},
		Audio: googleRecognitionAudio{
			Content: base64.StdEncoding.EncodeToString(audio),
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, googleMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google error (%d): %s", resp.StatusCode, string(respBody))
	}

	var gResp googleRecognizeResponse
	if err := json.Unmarshal(respBody, &gResp); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
	}

	// Concatenate all result transcripts.
	var text string
	var confidence float64
	for _, r := range gResp.Results {
		if len(r.Alternatives) > 0 {
			text += r.Alternatives[0].Transcript
			confidence = r.Alternatives[0].Confidence
		}
	}

	lang := opts.Language
	if lang == "" {
		lang = "de"
	}

	return &Result{
		Text:       text,
		Language:   lang,
		Duration:   duration,
		Provider:   p.Name(),
		Model:      model,
		Confidence: confidence,
	}, nil
}

// Name returns the provider identifier.
func (p *GoogleSTTProvider) Name() string {
	return "google"
}

// Health checks if the Google Speech API is reachable.
func (p *GoogleSTTProvider) Health(ctx context.Context) error {
	endpoint, err := p.googleEndpoint("v1/operations")
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, http.NoBody)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("google health: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google health: status %d", resp.StatusCode)
	}
	return nil
}
