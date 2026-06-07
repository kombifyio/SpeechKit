package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

const (
	deepgramBaseURL          = "https://api.deepgram.com"
	deepgramMaxResponseBytes = 16 << 20
)

type DeepgramProvider struct {
	APIKey           string
	Model            string
	DiarizationModel string
	BaseURL          string
	Validation       netsec.ValidationOptions
	client           *http.Client
}

func NewDeepgramProvider(apiKey, model string) *DeepgramProvider {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "nova-3"
	}
	p := &DeepgramProvider{
		APIKey:           apiKey,
		Model:            model,
		DiarizationModel: "latest",
		BaseURL:          deepgramBaseURL,
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 90 * time.Second, DialValidation: &p.Validation})
	return p
}

func (p *DeepgramProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	model := firstNonEmptyTrimmed(opts.Model, p.Model, "nova-3")
	speakerOpts := opts.Speaker.Normalized()

	endpoint, err := p.deepgramListenEndpoint(model, opts.Language, speakerOpts)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(ensureTranscriptionWAV(audio)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+p.APIKey)
	req.Header.Set("Content-Type", "audio/wav")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepgram request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	body, err := io.ReadAll(io.LimitReader(resp.Body, deepgramMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, netsec.ProviderStatusError("deepgram", resp.StatusCode, body)
	}

	var parsed deepgramResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	text, confidence := parsed.bestTranscript()
	lang := firstNonEmptyTrimmed(parsed.detectedLanguage(), opts.Language, "de")

	result := &Result{
		Text:       text,
		Language:   lang,
		Duration:   duration,
		Provider:   p.Name(),
		Model:      model,
		Confidence: confidence,
	}
	if speakerOpts.WantsDiarization() {
		result.Speakers = parsed.diarizationResult(p.Name(), model, text, lang)
	}
	return result, nil
}

func (p *DeepgramProvider) Name() string {
	return "deepgram"
}

func (p *DeepgramProvider) Health(ctx context.Context) error {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, deepgramBaseURL), "v1/projects", p.Validation)
	if err != nil {
		return fmt.Errorf("deepgram endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+p.APIKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("deepgram health: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return netsec.ProviderStatusError("deepgram health", resp.StatusCode, body)
	}
	return nil
}

func (p *DeepgramProvider) deepgramListenEndpoint(model, language string, speakerOpts speaker.Options) (string, error) {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, deepgramBaseURL), "v1/listen", p.Validation)
	if err != nil {
		return "", fmt.Errorf("deepgram endpoint: %w", err)
	}
	q := url.Values{}
	q.Set("model", model)
	q.Set("punctuate", "true")
	language = strings.TrimSpace(language)
	if language != "" && language != "auto" {
		q.Set("language", language)
	}
	if speakerOpts.WantsDiarization() {
		q.Set("utterances", "true")
		diarizationModel := firstNonEmptyTrimmed(speakerOpts.DiarizationModel, p.DiarizationModel, "latest")
		if strings.EqualFold(diarizationModel, "legacy") || strings.EqualFold(diarizationModel, "v1_legacy") {
			q.Set("diarize", "true")
		} else {
			q.Set("diarize_model", diarizationModel)
		}
	}
	return endpoint + "?" + q.Encode(), nil
}

type deepgramResponse struct {
	Results deepgramResults `json:"results"`
}

type deepgramResults struct {
	Channels   []deepgramChannel   `json:"channels"`
	Utterances []deepgramUtterance `json:"utterances"`
}

type deepgramChannel struct {
	Alternatives     []deepgramAlternative `json:"alternatives"`
	DetectedLanguage string                `json:"detected_language,omitempty"`
}

type deepgramAlternative struct {
	Transcript string         `json:"transcript"`
	Confidence float64        `json:"confidence"`
	Words      []deepgramWord `json:"words"`
}

type deepgramWord struct {
	Word              string  `json:"word"`
	PunctuatedWord    string  `json:"punctuated_word"`
	Start             float64 `json:"start"`
	End               float64 `json:"end"`
	Confidence        float64 `json:"confidence"`
	Speaker           *int    `json:"speaker"`
	SpeakerConfidence float64 `json:"speaker_confidence"`
}

type deepgramUtterance struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Transcript string  `json:"transcript"`
	Speaker    *int    `json:"speaker"`
	Confidence float64 `json:"confidence"`
}

func (r deepgramResponse) bestTranscript() (string, float64) {
	for _, channel := range r.Results.Channels {
		if len(channel.Alternatives) == 0 {
			continue
		}
		return strings.TrimSpace(channel.Alternatives[0].Transcript), channel.Alternatives[0].Confidence
	}
	return "", 0
}

func (r deepgramResponse) detectedLanguage() string {
	for _, channel := range r.Results.Channels {
		if strings.TrimSpace(channel.DetectedLanguage) != "" {
			return channel.DetectedLanguage
		}
	}
	return ""
}

func (r deepgramResponse) diarizationResult(provider, model, text, language string) *speaker.DiarizationResult {
	words := r.speakerWords()
	segments := speaker.BuildSegmentsFromWords(words)
	if len(segments) == 0 {
		segments = r.speakerSegments()
	}
	return &speaker.DiarizationResult{
		Provider: provider,
		Model:    model,
		Level:    speaker.IdentificationDiarization,
		Text:     text,
		Language: language,
		Speakers: speaker.SpeakersFromSegments(segments),
		Segments: segments,
		Words:    words,
	}
}

func (r deepgramResponse) speakerWords() []speaker.SpeakerWord {
	var out []speaker.SpeakerWord
	for _, channel := range r.Results.Channels {
		if len(channel.Alternatives) == 0 {
			continue
		}
		for _, word := range channel.Alternatives[0].Words {
			label := ""
			if word.Speaker != nil {
				label = speaker.NormalizeSpeakerLabel(*word.Speaker)
			}
			out = append(out, speaker.SpeakerWord{
				Text:              firstNonEmptyTrimmed(word.PunctuatedWord, word.Word),
				StartMs:           secondsToMillis(word.Start),
				EndMs:             secondsToMillis(word.End),
				Confidence:        word.Confidence,
				SpeakerLabel:      label,
				SpeakerConfidence: word.SpeakerConfidence,
			})
		}
	}
	return out
}

func (r deepgramResponse) speakerSegments() []speaker.SpeakerSegment {
	out := make([]speaker.SpeakerSegment, 0, len(r.Results.Utterances))
	for _, utterance := range r.Results.Utterances {
		label := ""
		if utterance.Speaker != nil {
			label = speaker.NormalizeSpeakerLabel(*utterance.Speaker)
		}
		out = append(out, speaker.SpeakerSegment{
			Text:              strings.TrimSpace(utterance.Transcript),
			StartMs:           secondsToMillis(utterance.Start),
			EndMs:             secondsToMillis(utterance.End),
			SpeakerLabel:      label,
			SpeakerConfidence: utterance.Confidence,
		})
	}
	return out
}

func secondsToMillis(value float64) int64 {
	return int64(value * 1000)
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
