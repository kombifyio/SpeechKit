// Package xai adapts xAI's Grok Voice speech-to-text endpoint
// (https://api.x.ai/v1/stt, model grok-voice-transcribe-2.0) to
// [stt.STTProvider]. It is an opt-in bring-your-own-key provider: nothing is
// sent to xAI unless a host builds it with the user's own XAI_API_KEY. Until
// the host marks it [Options.Selected] it is also explicit-only: a router uses
// it for a request that names xAI and never as a fallback for other providers
// (see [stt.ExplicitOnlyProvider]). It needs public https egress.
//
// Only batch transcription is wired. The documented streaming socket
// (wss://api.x.ai/v1/stt) is not, so the provider reports no native
// dictation stream and the router transcribes whole utterances.
//
// Stability: Beta — API-checked; may change with a changelog callout.
package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

const (
	// ProviderName is the provider identifier reported by Name() and stamped
	// on every Result.
	ProviderName = "xai"
	// DefaultModel is xAI's only documented transcription model.
	DefaultModel = "grok-voice-transcribe-2.0"
	// ProfileID is the catalog profile this adapter serves.
	ProfileID = "stt.xai.grok-transcribe"

	defaultBaseURL = "https://api.x.ai"
	// sttMaxResponse bounds the JSON body, which lists every word with its
	// timing; 8 MiB covers hours of speech.
	sttMaxResponse = 8 << 20
	// keytermLimit and keytermMaxChars are xAI's documented bounds for the
	// repeated keyterm field.
	keytermLimit    = 100
	keytermMaxChars = 50
)

// Provider implements stt.STTProvider for xAI's batch transcription API.
//
// BaseURL is validated against Validation on every request; the default
// Validation is strict (public https only).
type Provider struct {
	BaseURL    string
	APIKey     string
	Model      string
	Validation netsec.ValidationOptions
	// Selected reports that the user chose xAI for dictation. Until then the
	// provider is explicit-only (see [Provider.ExplicitOnly]).
	Selected bool
	client   *http.Client
}

// Options configures [New].
type Options struct {
	// APIKey is the user's own xAI API key, sent as a bearer token.
	APIKey string
	// Model is the transcription model id. Empty selects [DefaultModel].
	Model string
	// Selected marks xAI as chosen by the user (model_selection names it).
	// The zero value keeps the provider explicit-only: a router uses it only
	// for a request that names xAI and never as a generic fallback, so a
	// stored key or an enabled switch alone sends no audio to xAI.
	Selected bool
}

// New creates an xAI transcription provider. Zero Options values select the
// provider defaults.
func New(opts Options) *Provider {
	p := &Provider{
		BaseURL:  defaultBaseURL,
		APIKey:   strings.TrimSpace(opts.APIKey),
		Model:    stt.FirstNonEmptyTrimmed(opts.Model, DefaultModel),
		Selected: opts.Selected,
	}
	// Whole recordings (meetings) upload in one request, so the client waits
	// longer than the interactive-dictation providers do.
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 120 * time.Second, DialValidation: &p.Validation})
	return p
}

// transcriptionResponse is the part of the xAI answer SpeechKit reads. The
// answer also lists words with timings, a duration and a language field; the
// language field is deliberately not read (see Transcribe).
type transcriptionResponse struct {
	Text string `json:"text"`
}

func (p *Provider) endpoint(path string) (string, error) {
	base := stt.FirstNonEmptyTrimmed(p.BaseURL, defaultBaseURL)
	return netsec.BuildEndpoint(base, path, p.Validation)
}

func (p *Provider) requireKey() error {
	if p.APIKey == "" {
		return &stt.ProviderError{Provider: ProviderName, Kind: stt.ErrorKindAuth, Message: "no API key configured"}
	}
	return nil
}

// Transcribe implements [stt.STTProvider]. The audio is wrapped as WAV and
// posted as multipart form data. Recognition options go in before the file,
// which xAI requires to be the last field: the pinned language (omitted for
// multilanguage so the model detects it), keyterms as repeated keyterm fields,
// and the format and filler_words switches when the option is on. Word
// timings are not carried because xAI reports no per-word confidence, and the
// result's Words field means confidence.
func (p *Provider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	if err := p.requireKey(); err != nil {
		return nil, err
	}
	endpoint, err := p.endpoint("v1/stt")
	if err != nil {
		return nil, fmt.Errorf("xai endpoint: %w", err)
	}

	model := stt.FirstNonEmptyTrimmed(opts.Model, p.Model)
	resolved := stt.ResolveTranscribeOptions(ProviderName, ProfileID, opts, nil, nil)
	language := resolved.APILanguage()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fields := [][2]string{{"model", model}}
	if language != "" {
		fields = append(fields, [2]string{"language", language})
		// Inverse text normalization is language-specific; xAI rejects
		// format=true without a language.
		if resolved.SmartFormat {
			fields = append(fields, [2]string{"format", "true"})
		}
	}
	if resolved.FillerWords {
		fields = append(fields, [2]string{"filler_words", "true"})
	}
	for _, term := range keyterms(resolved.Keyterms) {
		fields = append(fields, [2]string{"keyterm", term})
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, fmt.Errorf("write %s field: %w", field[0], err)
		}
	}
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(stt.EnsureTranscriptionWAV(audio)); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create xai request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xai request: %w", stt.ClassifyTransportError(ProviderName, err))
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	elapsed := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, sttMaxResponse))
	if err != nil {
		return nil, fmt.Errorf("read xai response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(ProviderName, resp, respBody)
	}

	var parsed transcriptionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse xai response: %w", err)
	}

	// The answer's language field only echoes a pinned language: German audio
	// sent without one comes back labelled "en" (verified live 2026-10-16).
	// The label therefore names the request's language, or the multilanguage
	// sentinel when none was pinned, and never an inferred locale.
	lang := stt.FirstNonEmptyTrimmed(language, stt.LanguageMulti)

	return &stt.Result{
		Text:     strings.TrimSpace(parsed.Text),
		Language: lang,
		Duration: elapsed,
		Provider: ProviderName,
		Model:    model,
	}, nil
}

// keyterms keeps the terms xAI accepts: non-blank, at most keytermMaxChars
// characters (a longer term is dropped, never truncated into a different
// word), and at most keytermLimit terms.
func keyterms(terms []string) []string {
	var out []string
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || len([]rune(term)) > keytermMaxChars {
			continue
		}
		out = append(out, term)
		if len(out) == keytermLimit {
			break
		}
	}
	return out
}

// Name returns "xai".
func (p *Provider) Name() string { return ProviderName }

// ExplicitOnly implements [stt.ExplicitOnlyProvider]: xAI is opt-in, so until
// the user selected it a router offers it only to a request that names xAI and
// never tries it as a fallback for other providers.
func (p *Provider) ExplicitOnly() bool { return !p.Selected }

// Health issues an authenticated GET /v1/models, which is free; a missing
// key, transport failure or non-200 status is returned as the error.
func (p *Provider) Health(ctx context.Context) error {
	if err := p.requireKey(); err != nil {
		return err
	}
	endpoint, err := p.endpoint("v1/models")
	if err != nil {
		return fmt.Errorf("xai endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("xai health: %w", stt.ClassifyTransportError(ProviderName, err))
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return httpError("xai health", resp, body)
	}
	return nil
}

// httpError classifies a non-200 answer. xAI reports an invalid API key as
// HTTP 400 (verified live 2026-10-16), which would otherwise read as a bad
// request that retrying or re-authenticating cannot tell apart; a body that
// complains about the API key is an authentication failure.
func httpError(provider string, resp *http.Response, body []byte) *stt.ProviderError {
	e := stt.HTTPError(provider, resp, body)
	if e.Kind == stt.ErrorKindInvalidRequest && strings.Contains(strings.ToLower(string(body)), "api key") {
		e.Kind = stt.ErrorKindAuth
	}
	return e
}

// SetHTTPClient replaces the provider's HTTP client.
func (p *Provider) SetHTTPClient(client *http.Client) {
	if client != nil {
		p.client = client
	}
}

// Capabilities reports the speech-to-text baseline every provider satisfies.
func (*Provider) Capabilities() []speechkit.Capability { return stt.BaseCapabilities() }
