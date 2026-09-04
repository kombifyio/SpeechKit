// Package azurespeech implements stt.STTProvider on the Azure Speech
// fast-transcription surface of a Microsoft Foundry resource. That surface
// is where Microsoft's own MAI-Transcribe models live: they are not
// deployments, never show up on the OpenAI-compatible /openai/v1 route, and
// are addressed on the resource's custom domain instead.
package azurespeech

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
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

const (
	// APIVersion is the fast-transcription API version this adapter speaks
	// (verified live 2026-09-04).
	APIVersion = "2025-10-15"
	// DefaultModel is the MAI transcription model used when none is configured.
	DefaultModel = "MAI-Transcribe-2"

	// providerName is the id the router, preference pinning and option
	// manifests key on; every Foundry adapter reports it regardless of the
	// surface it talks to.
	providerName = "foundry"

	transcribePath    = "speechtotext/transcriptions:transcribe"
	modelsPath        = "speechtotext/models/base"
	defaultStyle      = "clean"
	defaultTimestamps = "none"
	defaultMaxPhrases = 100
	userAgent         = "kombify-SpeechKit"

	// Transcription responses are JSON; word timestamps on a long recording
	// can grow past the 1 MB the OpenAI-compatible adapter allows.
	maxResponseBytes = 4 << 20
	// Files can be long and the service transcribes them synchronously.
	requestTimeout = 60 * time.Second
)

// Options configures the MAI fast-transcription provider.
type Options struct {
	// Host is the resource's custom domain, e.g.
	// "myresource.cognitiveservices.azure.com" (no scheme). Required.
	Host string
	// APIKey is the resource key, sent as Ocp-Apim-Subscription-Key.
	APIKey string
	// BearerToken, when set, wins over APIKey: it is called per request and
	// the token rides "Authorization: Bearer". Hosts set it for Entra sign-in.
	BearerToken speechkit.BearerTokenFunc
	// Model is "MAI-Transcribe-2" (default) or "MAI-Transcribe-1.5".
	Model string
	// Style is "clean" (default; dictation wants readable text) or "verbatim".
	Style string
	// Timestamps is "none" (default), "segment" or "word".
	Timestamps string
	// Diarization enables speaker labels for every request. A request can
	// also ask for them through TranscribeOpts.Speaker.
	Diarization bool
	// MaxPhrases caps the phraseList built from keyterms (default 100).
	MaxPhrases int
}

// Provider implements stt.STTProvider for MAI-Transcribe on Azure Speech.
//
// Host is user-supplied configuration and is validated against Validation on
// every request. The default Validation is strict: only public https hosts
// are accepted. Tests and hosts pointing at loopback servers relax it.
type Provider struct {
	Host        string
	APIKey      string
	BearerToken speechkit.BearerTokenFunc
	Model       string
	Style       string
	Timestamps  string
	Diarization bool
	MaxPhrases  int
	Validation  netsec.ValidationOptions
	client      *http.Client
}

var (
	_ stt.STTProvider        = (*Provider)(nil)
	_ stt.CapabilityReporter = (*Provider)(nil)
)

// New creates a provider. Default Validation is strict (public https only).
func New(opts Options) *Provider {
	p := &Provider{
		Host:        strings.TrimSpace(opts.Host),
		APIKey:      opts.APIKey,
		BearerToken: opts.BearerToken,
		Model:       stt.FirstNonEmptyTrimmed(opts.Model, DefaultModel),
		Style:       stt.FirstNonEmptyTrimmed(opts.Style, defaultStyle),
		Timestamps:  stt.FirstNonEmptyTrimmed(opts.Timestamps, defaultTimestamps),
		Diarization: opts.Diarization,
		MaxPhrases:  opts.MaxPhrases,
		// Validation zero-value = strict: public https only.
	}
	if p.MaxPhrases <= 0 {
		p.MaxPhrases = defaultMaxPhrases
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: requestTimeout, DialValidation: &p.Validation})
	return p
}

// Name returns the shared Foundry provider id.
func (*Provider) Name() string { return providerName }

// Capabilities reports the STT baseline plus speaker diarization, which the
// fast-transcription API does natively.
func (*Provider) Capabilities() []speechkit.Capability {
	return append(stt.BaseCapabilities(), speechkit.CapabilitySpeakerDiarization)
}

// transcribeDefinition is the JSON "definition" multipart field.
type transcribeDefinition struct {
	Locales      []string        `json:"locales,omitempty"`
	EnhancedMode enhancedMode    `json:"enhancedMode"`
	Diarization  *diarizationDef `json:"diarization,omitempty"`
	PhraseList   *phraseListDef  `json:"phraseList,omitempty"`
}

type enhancedMode struct {
	Enabled      bool         `json:"enabled"`
	Model        string       `json:"model"`
	ModelOptions modelOptions `json:"modelOptions"`
}

type modelOptions struct {
	TranscribeStyle string `json:"transcribeStyle"`
	Timestamps      string `json:"timestamps,omitempty"`
}

type diarizationDef struct {
	Enabled bool `json:"enabled"`
}

type phraseListDef struct {
	Phrases []string `json:"phrases"`
}

type transcribeResponse struct {
	DurationMilliseconds int64 `json:"durationMilliseconds"`
	CombinedPhrases      []struct {
		Channel int    `json:"channel"`
		Text    string `json:"text"`
	} `json:"combinedPhrases"`
	Phrases []transcribePhrase `json:"phrases"`
}

type transcribePhrase struct {
	Channel              int              `json:"channel"`
	Speaker              *int             `json:"speaker"`
	OffsetMilliseconds   int64            `json:"offsetMilliseconds"`
	DurationMilliseconds int64            `json:"durationMilliseconds"`
	Text                 string           `json:"text"`
	Locale               string           `json:"locale"`
	Confidence           *float64         `json:"confidence"`
	Words                []transcribeWord `json:"words"`
}

type transcribeWord struct {
	Text                 string `json:"text"`
	OffsetMilliseconds   int64  `json:"offsetMilliseconds"`
	DurationMilliseconds int64  `json:"durationMilliseconds"`
}

// Transcribe posts the audio to transcriptions:transcribe and maps the
// combined text, phrase locale and (when asked for) speaker segments.
func (p *Provider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	endpoint, err := p.endpoint(transcribePath, "api-version="+APIVersion)
	if err != nil {
		return nil, err
	}
	resolved := stt.ResolveTranscribeOptions(providerName, "", opts, provideropts.Values{
		provideropts.OptionLanguage: "de",
	}, nil)

	model := stt.FirstNonEmptyTrimmed(resolved.Model, p.Model, DefaultModel)
	diarize := p.Diarization || resolved.Speaker.WantsDiarization()

	// APILanguage is empty for the provider default and for auto/multi, and
	// the model auto-detects when locales is absent — so only a language the
	// user actually pinned narrows the request.
	requestLocale := ""
	if lang := resolved.APILanguage(); lang != "" {
		requestLocale = ShortLocale(lang)
	}

	definition := transcribeDefinition{
		EnhancedMode: enhancedMode{
			Enabled: true,
			Model:   model,
			ModelOptions: modelOptions{
				TranscribeStyle: normalizeStyle(p.Style),
				Timestamps:      requestTimestamps(p.Timestamps, diarize),
			},
		},
	}
	if requestLocale != "" {
		definition.Locales = []string{requestLocale}
	}
	if diarize {
		definition.Diarization = &diarizationDef{Enabled: true}
	}
	if phrases := phraseList(resolved.Keyterms, p.MaxPhrases); len(phrases) > 0 {
		definition.PhraseList = &phraseListDef{Phrases: phrases}
	}
	definitionJSON, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal definition: %w", providerName, err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("audio", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(stt.EnsureTranscriptionWAV(audio)); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}
	if err := writer.WriteField("definition", string(definitionJSON)); err != nil {
		return nil, fmt.Errorf("write definition field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if _, err := p.authorize(ctx, req); err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", providerName, err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(providerName, resp.StatusCode, respBody)
	}

	var parsed transcribeResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	text := parsed.text()
	pinned := ""
	if !stt.IsMultilanguage(resolved.Language) {
		pinned = resolved.Language
	}
	language := stt.FirstNonEmptyTrimmed(parsed.primaryLocale(), requestLocale, pinned)

	result := &stt.Result{
		Text:       text,
		Language:   language,
		Duration:   duration,
		Provider:   providerName,
		Model:      model,
		Confidence: parsed.meanConfidence(),
	}
	if diarize {
		result.Speakers = parsed.diarization(model, text, language)
	}
	return result, nil
}

// Health lists one base model with the configured credential. It is free,
// read-only, and exercises exactly the auth path Transcribe uses.
func (p *Provider) Health(ctx context.Context) error {
	endpoint, err := p.endpoint(modelsPath, "api-version="+APIVersion+"&top=1")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	usedBearer, err := p.authorize(ctx, req)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s health: %w", providerName, err)
	}
	_ = resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return authFailure(providerName+" health", resp.StatusCode, usedBearer)
	default:
		return fmt.Errorf("%s health: status %d", providerName, resp.StatusCode)
	}
}

// ShortLocale converts a BCP-47 tag to the primary language subtag MAI
// expects in "locales": de-DE -> de, pt-BR -> pt, zh-CN -> zh. Three-letter
// tags such as yue or fil pass through unchanged.
func ShortLocale(bcp47 string) string {
	tag := strings.TrimSpace(bcp47)
	if tag == "" {
		return ""
	}
	if idx := strings.IndexAny(tag, "-_"); idx >= 0 {
		tag = tag[:idx]
	}
	return strings.ToLower(tag)
}

// IsMAITranscribeModel reports whether model names a Microsoft MAI
// transcription model, which is what decides between this adapter and the
// OpenAI-compatible route.
func IsMAITranscribeModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "mai-transcribe")
}

// authorize attaches the credential and reports which kind was used so
// failures can point at roles versus keys.
func (p *Provider) authorize(ctx context.Context, req *http.Request) (usedBearer bool, err error) {
	if p.BearerToken != nil {
		token, err := p.BearerToken(ctx)
		if err != nil {
			return true, fmt.Errorf("%s bearer token: %w", providerName, err)
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return true, fmt.Errorf("%s bearer token: empty token", providerName)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return true, nil
	}
	if key := strings.TrimSpace(p.APIKey); key != "" {
		req.Header.Set("Ocp-Apim-Subscription-Key", key)
		return false, nil
	}
	return false, fmt.Errorf("%s: no credential configured (API key or bearer token)", providerName)
}

// endpoint validates Host through netsec and appends the query. Host is
// normally a bare custom domain; a full URL is accepted so hosts and tests
// can point the adapter at a non-https loopback server.
func (p *Provider) endpoint(path, query string) (string, error) {
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return "", fmt.Errorf("%s: no host configured (set the project endpoint)", providerName)
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	endpoint, err := netsec.BuildEndpoint(strings.TrimRight(host, "/"), path, p.Validation)
	if err != nil {
		return "", fmt.Errorf("%s endpoint: %w", providerName, err)
	}
	return endpoint + "?" + query, nil
}

func normalizeStyle(style string) string {
	if strings.EqualFold(strings.TrimSpace(style), "verbatim") {
		return "verbatim"
	}
	return defaultStyle
}

// requestTimestamps maps the configured granularity to the wire value. "none"
// is expressed by omitting the field; diarization needs at least segments
// because that is where the speaker labels are carried.
func requestTimestamps(configured string, diarize bool) string {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "word":
		return "word"
	case "segment":
		return "segment"
	}
	if diarize {
		return "segment"
	}
	return ""
}

// phraseList trims, de-duplicates and caps the keyterms for phraseList.phrases.
func phraseList(terms []string, limit int) []string {
	if len(terms) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if _, dup := seen[term]; dup {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (r transcribeResponse) text() string {
	for _, combined := range r.CombinedPhrases {
		if text := strings.TrimSpace(combined.Text); text != "" {
			return text
		}
	}
	parts := make([]string, 0, len(r.Phrases))
	for _, phrase := range r.Phrases {
		if text := strings.TrimSpace(phrase.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func (r transcribeResponse) primaryLocale() string {
	for _, phrase := range r.Phrases {
		if locale := strings.TrimSpace(phrase.Locale); locale != "" {
			return locale
		}
	}
	return ""
}

// meanConfidence averages the phrase confidences the service reported; it is
// zero when none were present rather than pretending to know.
func (r transcribeResponse) meanConfidence() float64 {
	var sum float64
	count := 0
	for _, phrase := range r.Phrases {
		if phrase.Confidence == nil {
			continue
		}
		sum += *phrase.Confidence
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// diarization builds the canonical speaker result from the phrases. Nil when
// no phrase carries a speaker, so a request that asked for diarization but
// got none back does not advertise an empty speaker list.
func (r transcribeResponse) diarization(model, text, language string) *speaker.DiarizationResult {
	segments := make([]speaker.SpeakerSegment, 0, len(r.Phrases))
	var words []speaker.SpeakerWord
	labelled := false
	for _, phrase := range r.Phrases {
		label := ""
		if phrase.Speaker != nil {
			label = speaker.NormalizeSpeakerLabel(*phrase.Speaker)
			labelled = true
		}
		segment := speaker.SpeakerSegment{
			Text:         strings.TrimSpace(phrase.Text),
			StartMs:      phrase.OffsetMilliseconds,
			EndMs:        phrase.OffsetMilliseconds + phrase.DurationMilliseconds,
			SpeakerLabel: label,
		}
		if phrase.Confidence != nil {
			segment.SpeakerConfidence = *phrase.Confidence
		}
		for _, w := range phrase.Words {
			word := speaker.SpeakerWord{
				Text:         strings.TrimSpace(w.Text),
				StartMs:      w.OffsetMilliseconds,
				EndMs:        w.OffsetMilliseconds + w.DurationMilliseconds,
				SpeakerLabel: label,
			}
			segment.Words = append(segment.Words, word)
			words = append(words, word)
		}
		segments = append(segments, segment)
	}
	if !labelled {
		return nil
	}
	return &speaker.DiarizationResult{
		Provider: providerName,
		Model:    model,
		Level:    speaker.IdentificationDiarization,
		Text:     text,
		Language: language,
		Speakers: speaker.SpeakersFromSegments(segments),
		Segments: segments,
		Words:    words,
	}
}

// statusError classifies a non-200 answer through netsec, so response bodies
// never reach logs or UI, and appends Azure's structured message for request
// problems: "model 'x' is not supported" or a region gap is exactly what the
// user has to act on. Auth failures stay opaque.
func statusError(scope string, status int, body []byte) error {
	base := netsec.ProviderStatusError(scope, status, body)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return base
	}
	if detail := azureErrorDetail(body); detail != "" {
		return fmt.Errorf("%w: %s", base, detail)
	}
	return base
}

// azureErrorDetail extracts code and message from the two envelope shapes
// Azure uses ({"code","message"} and {"error":{"code","message"}}).
func azureErrorDetail(body []byte) string {
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	code, message := envelope.Code, envelope.Message
	if message == "" && envelope.Error != nil {
		code, message = envelope.Error.Code, envelope.Error.Message
	}
	message = strings.Join(strings.Fields(message), " ")
	if message == "" {
		return ""
	}
	if runes := []rune(message); len(runes) > 240 {
		message = string(runes[:240]) + "..."
	}
	if code = strings.TrimSpace(code); code != "" {
		return code + ": " + message
	}
	return message
}

// authFailure explains a 401/403 in terms of what the user can change: the
// roles on the resource for a signed-in account, the key otherwise.
func authFailure(scope string, status int, usedBearer bool) error {
	if usedBearer {
		return fmt.Errorf("%s: status %d: the signed-in account cannot use this resource; it needs the Cognitive Services User / Foundry User roles and must belong to the resource's tenant", scope, status)
	}
	return fmt.Errorf("%s: status %d: invalid key for this resource (or keys are disabled by policy; sign in with Microsoft instead)", scope, status)
}
