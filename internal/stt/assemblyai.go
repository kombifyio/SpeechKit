package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

const (
	assemblyAIBaseURL          = "https://api.assemblyai.com"
	assemblyAIStreamingBaseURL = "wss://streaming.assemblyai.com"
	assemblyAIStreamingModel   = "u3-rt-pro"
	assemblyAIMaxResponseBytes = 16 << 20
)

type AssemblyAIProvider struct {
	APIKey           string
	Models           []string
	StreamingModel   string
	BaseURL          string
	StreamingBaseURL string
	Validation       netsec.ValidationOptions
	PollInterval     time.Duration
	PollTimeout      time.Duration
	client           *http.Client
}

func NewAssemblyAIProvider(apiKey, models string) *AssemblyAIProvider {
	p := &AssemblyAIProvider{
		APIKey:           apiKey,
		Models:           parseAssemblyAIModels(models),
		StreamingModel:   assemblyAIStreamingModel,
		BaseURL:          assemblyAIBaseURL,
		StreamingBaseURL: assemblyAIStreamingBaseURL,
		PollInterval:     3 * time.Second,
		PollTimeout:      5 * time.Minute,
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 90 * time.Second, DialValidation: &p.Validation})
	return p
}

func (p *AssemblyAIProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	start := time.Now()
	uploadURL, err := p.upload(ctx, ensureTranscriptionWAV(audio))
	if err != nil {
		return nil, err
	}
	speakerOpts := opts.Speaker.Normalized()
	transcriptID, err := p.createTranscript(ctx, uploadURL, opts, speakerOpts)
	if err != nil {
		return nil, err
	}
	transcript, err := p.pollTranscript(ctx, transcriptID)
	if err != nil {
		return nil, err
	}

	lang := firstNonEmptyTrimmed(transcript.LanguageCode, opts.Language, "de")
	model := strings.Join(p.modelsForRequest(opts.Model), ",")
	result := &Result{
		Text:       strings.TrimSpace(transcript.Text),
		Language:   lang,
		Duration:   time.Since(start),
		Provider:   p.Name(),
		Model:      model,
		Confidence: transcript.Confidence,
	}
	if speakerOpts.WantsDiarization() {
		result.Speakers = transcript.diarizationResult(p.Name(), model, lang, speakerOpts)
	}
	return result, nil
}

func (p *AssemblyAIProvider) Name() string {
	return "assemblyai"
}

func (p *AssemblyAIProvider) Health(ctx context.Context) error {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, assemblyAIBaseURL), "v2/transcript", p.Validation)
	if err != nil {
		return fmt.Errorf("assemblyai endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	p.authorize(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("assemblyai health: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return netsec.ProviderStatusError("assemblyai health", resp.StatusCode, body)
	}
	return nil
}

func (p *AssemblyAIProvider) upload(ctx context.Context, audio []byte) (string, error) {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, assemblyAIBaseURL), "v2/upload", p.Validation)
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
	if err := p.doJSON(req, http.StatusOK, &response); err != nil {
		return "", fmt.Errorf("assemblyai upload: %w", err)
	}
	if strings.TrimSpace(response.UploadURL) == "" {
		return "", fmt.Errorf("assemblyai upload: missing upload_url")
	}
	return response.UploadURL, nil
}

func (p *AssemblyAIProvider) createTranscript(ctx context.Context, audioURL string, opts TranscribeOpts, speakerOpts speaker.Options) (string, error) {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, assemblyAIBaseURL), "v2/transcript", p.Validation)
	if err != nil {
		return "", fmt.Errorf("assemblyai endpoint: %w", err)
	}

	body := assemblyAITranscriptRequest{
		AudioURL:          audioURL,
		SpeechModels:      p.modelsForRequest(opts.Model),
		LanguageDetection: opts.Language == "" || opts.Language == "auto",
	}
	if opts.Language != "" && opts.Language != "auto" {
		body.LanguageCode = opts.Language
	}
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
	if err := p.doJSON(req, http.StatusOK, &response); err != nil {
		return "", fmt.Errorf("assemblyai transcript: %w", err)
	}
	if strings.TrimSpace(response.ID) == "" {
		return "", fmt.Errorf("assemblyai transcript: missing id")
	}
	return response.ID, nil
}

func (p *AssemblyAIProvider) pollTranscript(ctx context.Context, transcriptID string) (assemblyAITranscriptResponse, error) {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, assemblyAIBaseURL), "v2/transcript/"+strings.TrimSpace(transcriptID), p.Validation)
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
			if err := p.doJSON(req, http.StatusOK, &response); err != nil {
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

func (p *AssemblyAIProvider) doJSON(req *http.Request, wantStatus int, target any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	body, err := io.ReadAll(io.LimitReader(resp.Body, assemblyAIMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != wantStatus {
		return netsec.ProviderStatusError("assemblyai", resp.StatusCode, body)
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

func (p *AssemblyAIProvider) authorize(req *http.Request) {
	req.Header.Set("Authorization", p.APIKey)
}

func (p *AssemblyAIProvider) modelsForRequest(override string) []string {
	models := parseAssemblyAIModels(override)
	if len(models) == 0 {
		models = p.Models
	}
	if len(models) == 0 {
		models = []string{"universal-3-pro", "universal-2"}
	}
	return append([]string(nil), models...)
}

type assemblyAIUploadResponse struct {
	UploadURL string `json:"upload_url"`
}

type assemblyAITranscriptRequest struct {
	AudioURL            string                         `json:"audio_url"`
	SpeechModels        []string                       `json:"speech_models,omitempty"`
	LanguageDetection   bool                           `json:"language_detection,omitempty"`
	LanguageCode        string                         `json:"language_code,omitempty"`
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

func (r assemblyAITranscriptResponse) diarizationResult(provider, model, language string, opts speaker.Options) *speaker.DiarizationResult {
	segments := make([]speaker.SpeakerSegment, 0, len(r.Utterances))
	var words []speaker.SpeakerWord
	mapping := r.speakerIdentificationMapping()
	for _, utterance := range r.Utterances {
		label, personID, displayName, role := assemblyAISpeakerIdentity(utterance.Speaker, opts, mapping)
		segment := speaker.SpeakerSegment{
			Text:                  strings.TrimSpace(utterance.Text),
			StartMs:               utterance.Start,
			EndMs:                 utterance.End,
			SpeakerLabel:          label,
			SpeakerConfidence:     utterance.Confidence,
			PersonID:              personID,
			DisplayName:           displayName,
			Role:                  role,
			AttributionConfidence: assemblyAIAttributionConfidence(displayName, role, utterance.Confidence),
		}
		for _, word := range utterance.Words {
			wordLabel, wordPersonID, wordDisplayName, wordRole := assemblyAISpeakerIdentity(firstNonEmptyTrimmed(word.Speaker, utterance.Speaker), opts, mapping)
			sw := speaker.SpeakerWord{
				Text:                  word.Text,
				StartMs:               word.Start,
				EndMs:                 word.End,
				Confidence:            word.Confidence,
				SpeakerLabel:          wordLabel,
				SpeakerConfidence:     utterance.Confidence,
				PersonID:              wordPersonID,
				DisplayName:           wordDisplayName,
				Role:                  wordRole,
				AttributionConfidence: assemblyAIAttributionConfidence(wordDisplayName, wordRole, utterance.Confidence),
			}
			segment.Words = append(segment.Words, sw)
			words = append(words, sw)
		}
		segments = append(segments, segment)
	}
	level := speaker.IdentificationDiarization
	if opts.WantsIdentification() {
		level = speaker.IdentificationProviderID
	}
	return &speaker.DiarizationResult{
		Provider: provider,
		Model:    model,
		Level:    level,
		Text:     strings.TrimSpace(r.Text),
		Language: language,
		Speakers: speaker.SpeakersFromSegments(segments),
		Segments: segments,
		Words:    words,
	}
}

func (r assemblyAITranscriptResponse) speakerIdentificationMapping() map[string]string {
	if r.SpeechUnderstanding == nil {
		return nil
	}
	return r.SpeechUnderstanding.Response.SpeakerIdentification.Mapping
}

func assemblyAISpeakerIdentity(raw string, opts speaker.Options, mapping map[string]string) (label, personID, displayName, role string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", ""
	}
	label = speaker.NormalizeSpeakerLabel(raw)
	if mapped := assemblyAIMappedSpeaker(raw, label, mapping); mapped != "" {
		if opts.SpeakerType == speaker.SpeakerTypeRole {
			role = mapped
		} else {
			displayName = mapped
		}
		personID = assemblyAIKnownSpeakerID(opts, displayName, role)
		return label, personID, displayName, role
	}
	if opts.WantsIdentification() && knownValueContains(opts, raw) {
		if opts.SpeakerType == speaker.SpeakerTypeRole {
			role = raw
		} else {
			displayName = raw
		}
		personID = assemblyAIKnownSpeakerID(opts, displayName, role)
		return label, personID, displayName, role
	}
	return label, "", "", ""
}

func assemblyAIAttributionConfidence(displayName, role string, confidence float64) float64 {
	if displayName == "" && role == "" {
		return 0
	}
	return confidence
}

func assemblyAIKnownValues(opts speaker.Options) []string {
	values := append([]string(nil), opts.KnownValues...)
	for _, known := range opts.KnownSpeakers {
		switch opts.SpeakerType {
		case speaker.SpeakerTypeRole:
			values = append(values, known.Role)
		default:
			values = append(values, known.DisplayName)
		}
	}
	return speaker.Options{KnownValues: values}.Normalized().KnownValues
}

func assemblyAIKnownSpeakers(opts speaker.Options) []assemblyAISpeakerMetadata {
	opts = opts.Normalized()
	out := make([]assemblyAISpeakerMetadata, 0, len(opts.KnownSpeakers))
	for _, known := range opts.KnownSpeakers {
		item := assemblyAISpeakerMetadata{
			ID:          known.ID,
			Description: known.Description,
		}
		if opts.SpeakerType == speaker.SpeakerTypeRole {
			item.Role = known.Role
		} else {
			item.Name = known.DisplayName
		}
		if item.ID == "" && item.Name == "" && item.Role == "" && item.Description == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func assemblyAIMappedSpeaker(raw, label string, mapping map[string]string) string {
	if len(mapping) == 0 {
		return ""
	}
	candidates := []string{raw, label}
	if strings.HasPrefix(label, "speaker_") {
		candidates = append(candidates, strings.TrimPrefix(label, "speaker_"))
	}
	for _, candidate := range candidates {
		if value := strings.TrimSpace(mapping[candidate]); value != "" {
			return value
		}
	}
	return ""
}

func assemblyAIKnownSpeakerID(opts speaker.Options, displayName, role string) string {
	displayName = strings.TrimSpace(displayName)
	role = strings.TrimSpace(role)
	if displayName == "" && role == "" {
		return ""
	}
	for _, known := range opts.Normalized().KnownSpeakers {
		if displayName != "" && strings.EqualFold(known.DisplayName, displayName) {
			return known.ID
		}
		if role != "" && strings.EqualFold(known.Role, role) {
			return known.ID
		}
	}
	return ""
}

func knownValueContains(opts speaker.Options, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, known := range assemblyAIKnownValues(opts) {
		if strings.EqualFold(known, value) {
			return true
		}
	}
	return false
}

func parseAssemblyAIModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
