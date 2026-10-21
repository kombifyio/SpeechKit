package assemblyai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

func (p *Provider) upload(ctx context.Context, audio []byte) (string, error) {
	endpoint, err := netsec.BuildEndpoint(stt.FirstNonEmptyTrimmed(p.BaseURL, assemblyAIBaseURL), "v2/upload", p.Validation)
	if err != nil {
		return "", fmt.Errorf("assemblyai endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(audio))
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	p.authorize(req)
	req.Header.Set("Content-Type", "application/octet-stream")

	var response assemblyAIUploadResponse
	if err := p.doJSON(req, &response); err != nil {
		return "", fmt.Errorf("assemblyai upload: %w", err)
	}
	if strings.TrimSpace(response.UploadURL) == "" {
		return "", fmt.Errorf("assemblyai upload: missing upload_url")
	}
	return response.UploadURL, nil
}

func (p *Provider) createTranscript(ctx context.Context, audioURL string, opts stt.TranscribeOpts, resolved stt.ResolvedTranscribeOptions) (string, error) {
	endpoint, err := netsec.BuildEndpoint(stt.FirstNonEmptyTrimmed(p.BaseURL, assemblyAIBaseURL), "v2/transcript", p.Validation)
	if err != nil {
		return "", fmt.Errorf("assemblyai endpoint: %w", err)
	}

	body := assemblyAITranscriptRequest{
		AudioURL:          audioURL,
		SpeechModels:      p.modelsForRequest(opts.Model),
		LanguageDetection: resolved.DetectLanguage || strings.EqualFold(resolved.APILanguage(), "auto") || strings.TrimSpace(resolved.APILanguage()) == "",
	}
	if language := resolved.APILanguage(); language != "" {
		body.LanguageCode = language
	}
	if resolved.Punctuation {
		body.Punctuate = true
	}
	if resolved.SmartFormat {
		body.FormatText = true
	}
	if resolved.UseVocabularyKeyterms && len(resolved.Keyterms) > 0 {
		body.KeytermsPrompt = append([]string(nil), resolved.Keyterms...)
	}
	if prompt := stt.FirstNonEmptyTrimmed(resolved.ContextPrompt, resolved.Prompt); prompt != "" {
		body.Prompt = prompt
	}
	if resolved.PrivacyRedaction {
		body.RedactPII = true
	}
	if resolved.VoiceFocus {
		body.VoiceFocus = true
	}
	if resolved.MedicalDomain {
		body.Domain = "medical-v1"
	}
	speakerOpts := resolved.Speaker
	if speakerOpts.WantsDiarization() {
		body.SpeakerLabels = true
		// AssemblyAI rejects requests that send both speakers_expected and
		// speaker_options ("can not be used in the same request"). Prefer the
		// min/max range when provided, otherwise fall back to the exact count.
		switch {
		case speakerOpts.MinSpeakersExpected > 0 || speakerOpts.MaxSpeakersExpected > 0:
			body.SpeakerOptions = &assemblyAISpeakerOptions{
				MinSpeakersExpected: speakerOpts.MinSpeakersExpected,
				MaxSpeakersExpected: speakerOpts.MaxSpeakersExpected,
			}
		case speakerOpts.SpeakersExpected > 0:
			body.SpeakersExpected = speakerOpts.SpeakersExpected
		}
	}
	if speakerOpts.WantsIdentification() {
		knownValues := assemblyAIKnownValues(speakerOpts)
		knownSpeakers := assemblyAIKnownSpeakers(speakerOpts)
		if len(knownValues) > 0 || len(knownSpeakers) > 0 {
			speakerIdentification := assemblyAISpeakerIdentification{
				SpeakerType: speakerOpts.SpeakerType,
				KnownValues: knownValues,
				Speakers:    knownSpeakers,
			}
			if len(knownSpeakers) > 0 {
				// AssemblyAI requires callers to use either known_values or speakers, not both.
				speakerIdentification.KnownValues = nil
			}
			body.SpeechUnderstanding = &assemblyAISpeechUnderstanding{
				Request: assemblyAISpeechUnderstandingRequest{
					SpeakerIdentification: speakerIdentification,
				},
			}
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal transcript request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create transcript request: %w", err)
	}
	p.authorize(req)
	req.Header.Set("Content-Type", "application/json")

	var response assemblyAITranscriptResponse
	if err := p.doJSON(req, &response); err != nil {
		return "", fmt.Errorf("assemblyai transcript: %w", err)
	}
	if strings.TrimSpace(response.ID) == "" {
		return "", fmt.Errorf("assemblyai transcript: missing id")
	}
	return response.ID, nil
}

func (p *Provider) pollTranscript(ctx context.Context, transcriptID string) (assemblyAITranscriptResponse, error) {
	endpoint, err := netsec.BuildEndpoint(stt.FirstNonEmptyTrimmed(p.BaseURL, assemblyAIBaseURL), "v2/transcript/"+strings.TrimSpace(transcriptID), p.Validation)
	if err != nil {
		return assemblyAITranscriptResponse{}, fmt.Errorf("assemblyai endpoint: %w", err)
	}
	pollCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok && p.PollTimeout > 0 {
		pollCtx, cancel = context.WithTimeout(ctx, p.PollTimeout)
	}
	defer cancel()

	interval := p.PollInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-pollCtx.Done():
			return assemblyAITranscriptResponse{}, pollCtx.Err()
		case <-timer.C:
			req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, endpoint, http.NoBody)
			if err != nil {
				return assemblyAITranscriptResponse{}, err
			}
			p.authorize(req)
			var response assemblyAITranscriptResponse
			if err := p.doJSON(req, &response); err != nil {
				return assemblyAITranscriptResponse{}, fmt.Errorf("assemblyai poll: %w", err)
			}
			switch strings.ToLower(strings.TrimSpace(response.Status)) {
			case "completed":
				return response, nil
			case "error":
				return assemblyAITranscriptResponse{}, fmt.Errorf("assemblyai transcript failed: %s", response.Error)
			default:
				timer.Reset(interval)
			}
		}
	}
}

type assemblyAIUploadResponse struct {
	UploadURL string `json:"upload_url"`
}

type assemblyAITranscriptRequest struct {
	AudioURL            string                         `json:"audio_url"`
	SpeechModels        []string                       `json:"speech_models,omitempty"`
	LanguageDetection   bool                           `json:"language_detection,omitempty"`
	LanguageCode        string                         `json:"language_code,omitempty"`
	Punctuate           bool                           `json:"punctuate,omitempty"`
	FormatText          bool                           `json:"format_text,omitempty"`
	KeytermsPrompt      []string                       `json:"keyterms_prompt,omitempty"`
	Prompt              string                         `json:"prompt,omitempty"`
	RedactPII           bool                           `json:"redact_pii,omitempty"`
	VoiceFocus          bool                           `json:"voice_focus,omitempty"`
	Domain              string                         `json:"domain,omitempty"`
	SpeakerLabels       bool                           `json:"speaker_labels,omitempty"`
	SpeakersExpected    int                            `json:"speakers_expected,omitempty"`
	SpeakerOptions      *assemblyAISpeakerOptions      `json:"speaker_options,omitempty"`
	SpeechUnderstanding *assemblyAISpeechUnderstanding `json:"speech_understanding,omitempty"`
}

type assemblyAISpeakerOptions struct {
	MinSpeakersExpected int `json:"min_speakers_expected,omitempty"`
	MaxSpeakersExpected int `json:"max_speakers_expected,omitempty"`
}

type assemblyAISpeechUnderstanding struct {
	Request  assemblyAISpeechUnderstandingRequest  `json:"request,omitempty"`
	Response assemblyAISpeechUnderstandingResponse `json:"response,omitempty"`
}

type assemblyAISpeechUnderstandingRequest struct {
	SpeakerIdentification assemblyAISpeakerIdentification `json:"speaker_identification"`
}

type assemblyAISpeakerIdentification struct {
	SpeakerType string                      `json:"speaker_type,omitempty"`
	KnownValues []string                    `json:"known_values,omitempty"`
	Speakers    []assemblyAISpeakerMetadata `json:"speakers,omitempty"`
}

type assemblyAISpeakerMetadata struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Role        string `json:"role,omitempty"`
	Description string `json:"description,omitempty"`
}

type assemblyAISpeechUnderstandingResponse struct {
	SpeakerIdentification assemblyAISpeakerIdentificationResponse `json:"speaker_identification,omitempty"`
}

type assemblyAISpeakerIdentificationResponse struct {
	Mapping map[string]string `json:"mapping,omitempty"`
	Status  string            `json:"status,omitempty"`
}

type assemblyAITranscriptResponse struct {
	ID                  string                         `json:"id"`
	Status              string                         `json:"status"`
	Text                string                         `json:"text"`
	LanguageCode        string                         `json:"language_code"`
	Confidence          float64                        `json:"confidence"`
	Error               string                         `json:"error"`
	SpeechUnderstanding *assemblyAISpeechUnderstanding `json:"speech_understanding,omitempty"`
	Utterances          []assemblyAIUtterance          `json:"utterances"`
}

type assemblyAIUtterance struct {
	Text       string           `json:"text"`
	Start      int64            `json:"start"`
	End        int64            `json:"end"`
	Speaker    string           `json:"speaker"`
	Confidence float64          `json:"confidence"`
	Words      []assemblyAIWord `json:"words"`
}

type assemblyAIWord struct {
	Text       string  `json:"text"`
	Start      int64   `json:"start"`
	End        int64   `json:"end"`
	Confidence float64 `json:"confidence"`
	Speaker    string  `json:"speaker"`
}
