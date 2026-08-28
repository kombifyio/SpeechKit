package stt

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

const (
	deepgramBaseURL          = "https://api.deepgram.com"
	deepgramMaxResponseBytes = 16 << 20
	deepgramMaxKeyterms      = 100
)

// DeepgramOptions holds provider-specific Deepgram STT controls. These map to
// Deepgram Listen query parameters while keeping SpeechKit's public STT router
// interface provider-neutral.
//
// Deprecated: moved to pkg/speechkit/stt/deepgram.Options. This name is removed in
// v0.65.0; import the provider package instead.
type DeepgramOptions struct {
	Configured            bool
	SmartFormat           bool
	Dictation             bool
	FillerWords           bool
	Numerals              bool
	DetectLanguage        bool
	LanguageOverride      string
	UseVocabularyKeyterms bool
	Keyterms              []string
	EndpointingMs         int
}

// DeepgramProvider transcribes through the Deepgram Listen API.
//
// Deprecated: moved to pkg/speechkit/stt/deepgram.Provider. This name is removed in
// v0.65.0; import the provider package instead.
type DeepgramProvider struct {
	APIKey                string
	Model                 string
	DiarizationModel      string
	BaseURL               string
	Validation            netsec.ValidationOptions
	SmartFormat           bool
	Dictation             bool
	FillerWords           bool
	Numerals              bool
	DetectLanguage        bool
	LanguageOverride      string
	UseVocabularyKeyterms bool
	Keyterms              []string
	EndpointingMs         int
	client                *http.Client
}

// NewDeepgramProvider creates a Deepgram provider. Model defaults to the
// provider default if empty.
//
// Deprecated: moved to pkg/speechkit/stt/deepgram.New. This name is removed in
// v0.65.0; import the provider package instead.
func NewDeepgramProvider(apiKey, model string) *DeepgramProvider {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "nova-3"
	}
	p := &DeepgramProvider{
		APIKey:                apiKey,
		Model:                 model,
		DiarizationModel:      "latest",
		BaseURL:               deepgramBaseURL,
		SmartFormat:           true,
		Numerals:              true,
		UseVocabularyKeyterms: true,
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 90 * time.Second, DialValidation: &p.Validation})
	return p
}

func (p *DeepgramProvider) ApplyOptions(opts DeepgramOptions) {
	if p == nil {
		return
	}
	p.SmartFormat = opts.SmartFormat
	p.Dictation = opts.Dictation
	p.FillerWords = opts.FillerWords
	p.Numerals = opts.Numerals
	p.DetectLanguage = opts.DetectLanguage
	p.LanguageOverride = strings.TrimSpace(opts.LanguageOverride)
	p.UseVocabularyKeyterms = opts.UseVocabularyKeyterms
	p.Keyterms = normalizedDeepgramTerms(opts.Keyterms, deepgramMaxKeyterms)
	if opts.EndpointingMs >= 0 {
		p.EndpointingMs = opts.EndpointingMs
	}
}

func (p *DeepgramProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	model := firstNonEmptyTrimmed(opts.Model, p.Model, "nova-3")
	resolved := p.resolveOptions(model, opts)
	apiLanguage := deepgramResolvedLanguage(resolved)
	languageSource := deepgramLanguageSource(resolved)
	speakerOpts := resolved.Speaker

	endpoint, err := p.deepgramListenEndpoint(model, resolved)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(ensureTranscriptionWAV(audio)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+p.APIKey)
	req.Header.Set("Content-Type", "audio/wav")

	// Latency instrumentation: separates connection setup (DNS/TCP/TLS — "how
	// long to reach Deepgram") from time-to-first-byte (Deepgram's own
	// processing), and reports whether the keep-alive connection was reused.
	// Debug-only; effectively free when debug logging is off.
	var (
		reused                  bool
		dnsDur, connDur, tlsDur time.Duration
		dnsAt, connAt, tlsAt    time.Time
		ttfb                    time.Duration
	)
	start := time.Now()
	trace := &httptrace.ClientTrace{
		GotConn:              func(info httptrace.GotConnInfo) { reused = info.Reused },
		DNSStart:             func(httptrace.DNSStartInfo) { dnsAt = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { dnsDur = time.Since(dnsAt) },
		ConnectStart:         func(string, string) { connAt = time.Now() },
		ConnectDone:          func(string, string, error) { connDur = time.Since(connAt) },
		TLSHandshakeStart:    func() { tlsAt = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { tlsDur = time.Since(tlsAt) },
		GotFirstResponseByte: func() { ttfb = time.Since(start) },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace)) //nolint:contextcheck // trace context derives from req.Context(); httptrace requires re-wrapping the request's own context
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepgram request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)
	slog.Debug("deepgram.latency", // #nosec G706 -- slog writes provider telemetry as structured attributes, not interpolated log text.
		"reused", reused,
		"dns_ms", dnsDur.Milliseconds(),
		"connect_ms", connDur.Milliseconds(),
		"tls_ms", tlsDur.Milliseconds(),
		"ttfb_ms", ttfb.Milliseconds(),
		"total_ms", duration.Milliseconds(),
		"audio_bytes", len(audio),
		"provider", p.Name(),
		"model", model,
		"language", apiLanguage,
		"language_source", languageSource,
		"keyterm_count", len(resolved.Keyterms),
	)

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
	lang := firstNonEmptyTrimmed(parsed.detectedLanguage(), apiLanguage, "multi")

	result := &Result{
		Text:       text,
		Language:   lang,
		Duration:   duration,
		Provider:   p.Name(),
		Model:      model,
		Confidence: confidence,
		Words:      parsed.transcriptWords(),
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

func (p *DeepgramProvider) deepgramListenEndpoint(model string, resolved ResolvedTranscribeOptions) (string, error) {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, deepgramBaseURL), "v1/listen", p.Validation)
	if err != nil {
		return "", fmt.Errorf("deepgram endpoint: %w", err)
	}
	q := url.Values{}
	q.Set("model", model)
	if resolved.Punctuation {
		q.Set("punctuate", "true")
	}
	if resolved.SmartFormat {
		q.Set("smart_format", "true")
	}
	if resolved.Dictation {
		q.Set("dictation", "true")
	}
	if resolved.FillerWords {
		q.Set("filler_words", "true")
	}
	if resolved.Numerals {
		q.Set("numerals", "true")
	}
	q.Set("language", deepgramResolvedLanguage(resolved))
	p.applyVocabularyBias(q, model, resolved.Keyterms, resolved.UseVocabularyKeyterms)
	if resolved.Speaker.WantsDiarization() {
		q.Set("utterances", "true")
		diarizationModel := firstNonEmptyTrimmed(resolved.Speaker.DiarizationModel, p.DiarizationModel, "latest")
		if strings.EqualFold(diarizationModel, "legacy") || strings.EqualFold(diarizationModel, "v1_legacy") {
			q.Set("diarize", "true")
		} else {
			q.Set("diarize_model", diarizationModel)
		}
	}
	return endpoint + "?" + q.Encode(), nil
}

func (p *DeepgramProvider) resolveOptions(model string, opts TranscribeOpts) ResolvedTranscribeOptions {
	providerDefaults := provideropts.Values{
		provideropts.OptionLanguage:       deepgramCodeSwitchingLanguage(),
		provideropts.OptionPunctuation:    true,
		provideropts.OptionSmartFormat:    true,
		provideropts.OptionNumerals:       true,
		provideropts.OptionVocabularyBias: true,
	}
	providerOverrides := provideropts.Values{}
	if language := strings.TrimSpace(p.LanguageOverride); language != "" {
		providerOverrides[provideropts.OptionLanguage] = language
	}
	if !p.SmartFormat {
		providerOverrides[provideropts.OptionSmartFormat] = false
	}
	if p.Dictation {
		providerOverrides[provideropts.OptionDictation] = true
	}
	if p.FillerWords {
		providerOverrides[provideropts.OptionFillerWords] = true
	}
	if !p.Numerals {
		providerOverrides[provideropts.OptionNumerals] = false
	}
	if p.DetectLanguage {
		providerOverrides[provideropts.OptionDetectLanguage] = true
	}
	if !p.UseVocabularyKeyterms {
		providerOverrides[provideropts.OptionVocabularyBias] = false
	}
	if p.EndpointingMs > 0 {
		providerOverrides[provideropts.OptionEndpointingMs] = p.EndpointingMs
	}
	if len(p.Keyterms) > 0 {
		providerOverrides[provideropts.OptionKeyterms] = append([]string(nil), p.Keyterms...)
	}
	return ResolveTranscribeOptions("deepgram", deepgramProfileID(model), opts, providerDefaults, providerOverrides)
}

// deepgramCodeSwitchingLanguage is the language SpeechKit falls back to when
// the user has not picked one: Deepgram's multilingual code-switching mode,
// which transcribes speech that mixes languages without being told which.
//
// It is a default, not a mandate. Code-switching trades per-language
// accuracy for the ability to follow a switch mid-sentence, and which side
// of that trade wins depends on the speaker — so a configured language must
// reach the API unchanged. `multi` is the only multilingual value Deepgram
// accepts; there is no "German plus English" subset.
func deepgramCodeSwitchingLanguage() string {
	return "multi"
}

// deepgramResolvedLanguage returns the language to send for this request:
// whatever the option resolution produced, falling back to code-switching
// when nothing (or "auto") is configured.
func deepgramResolvedLanguage(resolved ResolvedTranscribeOptions) string {
	if language := normalizedDeepgramLanguage(resolved.Language); language != "" {
		return language
	}
	return deepgramCodeSwitchingLanguage()
}

func deepgramLanguageSource(resolved ResolvedTranscribeOptions) string {
	option, ok := resolved.Effective.Options[provideropts.OptionLanguage]
	if !ok {
		return string(provideropts.SourceProviderDefault)
	}
	return string(option.Source)
}

func (p *DeepgramProvider) applyVocabularyBias(q url.Values, model string, requestTerms []string, useVocabulary bool) {
	if p == nil {
		return
	}
	termInput := append([]string(nil), p.Keyterms...)
	if useVocabulary {
		termInput = append(termInput, requestTerms...)
	}
	terms := normalizedDeepgramTerms(termInput, deepgramMaxKeyterms)
	if len(terms) == 0 {
		return
	}
	param := deepgramVocabularyParam(model)
	for _, term := range terms {
		q.Add(param, term)
	}
}

func deepgramProfileID(model string) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(model)), "nova-3") {
		return "stt.deepgram.nova-3"
	}
	return ""
}

func deepgramVocabularyParam(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(model, "nova-3") || strings.HasPrefix(model, "flux") {
		return "keyterm"
	}
	return "keywords"
}

// normalizedDeepgramLanguage canonicalises a SpeechKit locale for Deepgram
// Listen. Blank and "auto" mean "no explicit choice" and resolve to the
// multilingual default further up.
//
// The region subtag is deliberately preserved: Listen accepts hyphenated
// BCP-47 codes and treats them as distinct models (verified against the live
// API — "de-DE", "en-US", "en-GB" and "zh-Hans" all transcribe). Stripping
// the region, as the realtime agent path has to do for agent.language, would
// silently discard a regional choice the user made.
//
// Only the separator is normalised: "de_DE" is rejected with
// "No such model/language/tier combination found", and underscored locales
// do occur in host configurations.
func normalizedDeepgramLanguage(language string) string {
	language = strings.TrimSpace(language)
	if language == "" || strings.EqualFold(language, "auto") {
		return ""
	}
	return strings.ReplaceAll(language, "_", "-")
}

func normalizedDeepgramTerms(terms []string, limit int) []string {
	if len(terms) == 0 || limit == 0 {
		return nil
	}
	out := make([]string, 0, min(len(terms), limit))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, term)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func ParseDeepgramKeyterms(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	terms := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ',' || r == ';'
	})
	return normalizedDeepgramTerms(terms, deepgramMaxKeyterms)
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

// transcriptWords returns per-word acoustic confidence for the best alternative.
// It uses the same words Deepgram already returns in alternatives[0].words (the
// source speakerWords() reads for diarization), so plain dictation gets the
// confidence data that bestTranscript() otherwise discards. Returns nil when no
// words are present.
func (r deepgramResponse) transcriptWords() []WordConfidence {
	for _, channel := range r.Results.Channels {
		if len(channel.Alternatives) == 0 {
			continue
		}
		words := channel.Alternatives[0].Words
		if len(words) == 0 {
			return nil
		}
		out := make([]WordConfidence, 0, len(words))
		for _, word := range words {
			out = append(out, WordConfidence{
				Text:       firstNonEmptyTrimmed(word.PunctuatedWord, word.Word),
				Confidence: word.Confidence,
				StartMs:    secondsToMillis(word.Start),
				EndMs:      secondsToMillis(word.End),
			})
		}
		return out
	}
	return nil
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
