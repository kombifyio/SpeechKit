package google

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

const (
	googleSTTBaseURL       = "https://speech.googleapis.com"
	googleMaxResponseBytes = 1 << 20
)

// Provider implements stt.STTProvider for Google Cloud Speech-to-Text v1 REST API.
//
// BaseURL is user-configurable (for testing or regional endpoints). It is
// validated against Validation on every request. Default Validation is strict
// (public https only).
type Provider struct {
	APIKey                    string
	Model                     string // "latest_long", "latest_short", or another Google STT v1 model tag
	STTCredentialsJSONEnv     string
	ApplicationCredentialsEnv string
	BaseURL                   string // Override for testing; defaults to googleSTTBaseURL
	Validation                netsec.ValidationOptions
	// SecretResolver resolves the streaming credential env names
	// (STTCredentialsJSONEnv, ApplicationCredentialsEnv, GOOGLE_CLOUD_PROJECT)
	// when they are not present in the process environment. Nil falls back to
	// the process-wide stt.SetSecretResolver, then to the environment.
	SecretResolver stt.SecretResolver
	client         *http.Client
}

// New creates a provider for Google Cloud Speech-to-Text.
// Model defaults to "latest_long" if empty.
func New(apiKey, model string) *Provider {
	if model == "" {
		model = "latest_long"
	}
	p := &Provider{
		APIKey:                    apiKey,
		Model:                     model,
		STTCredentialsJSONEnv:     "SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON",
		ApplicationCredentialsEnv: "GOOGLE_APPLICATION_CREDENTIALS",
		BaseURL:                   googleSTTBaseURL,
		// Validation zero-value = strict: public https only.
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

func (p *Provider) SetStreamingCredentialEnvs(credentialsJSONEnv, applicationCredentialsEnv string) {
	if strings.TrimSpace(credentialsJSONEnv) != "" {
		p.STTCredentialsJSONEnv = strings.TrimSpace(credentialsJSONEnv)
	}
	if strings.TrimSpace(applicationCredentialsEnv) != "" {
		p.ApplicationCredentialsEnv = strings.TrimSpace(applicationCredentialsEnv)
	}
}

// googleRecognizeRequest is the request body for the v1 speech:recognize endpoint.
type googleRecognizeRequest struct {
	Config googleRecognitionConfig `json:"config"`
	Audio  googleRecognitionAudio  `json:"audio"`
}

type googleRecognitionConfig struct {
	Encoding                   string                   `json:"encoding"`
	SampleRateHertz            int                      `json:"sampleRateHertz"`
	LanguageCode               string                   `json:"languageCode,omitempty"`
	AlternativeLanguageCodes   []string                 `json:"alternativeLanguageCodes,omitempty"`
	Model                      string                   `json:"model,omitempty"`
	EnableAutomaticPunctuation bool                     `json:"enableAutomaticPunctuation,omitempty"`
	EnableWordTimeOffsets      bool                     `json:"enableWordTimeOffsets,omitempty"`
	SpeechContexts             []googleSpeechContext    `json:"speechContexts,omitempty"`
	DiarizationConfig          *googleDiarizationConfig `json:"diarizationConfig,omitempty"`
}

type googleRecognitionAudio struct {
	Content string `json:"content"` // base64-encoded audio
}

type googleSpeechContext struct {
	Phrases []string `json:"phrases,omitempty"`
}

type googleDiarizationConfig struct {
	EnableSpeakerDiarization bool `json:"enableSpeakerDiarization"`
	MinSpeakerCount          int  `json:"minSpeakerCount,omitempty"`
	MaxSpeakerCount          int  `json:"maxSpeakerCount,omitempty"`
}

// googleRecognizeResponse is the response from the v1 speech:recognize endpoint.
type googleRecognizeResponse struct {
	Results []googleSpeechRecognitionResult `json:"results"`
}

type googleSpeechRecognitionResult struct {
	Alternatives []googleSpeechAlternative `json:"alternatives"`
	LanguageCode string                    `json:"languageCode,omitempty"`
}

type googleSpeechAlternative struct {
	Transcript string           `json:"transcript"`
	Confidence float64          `json:"confidence"`
	Words      []googleWordInfo `json:"words,omitempty"`
}

type googleWordInfo struct {
	StartTime    string `json:"startTime,omitempty"`
	EndTime      string `json:"endTime,omitempty"`
	StartOffset  string `json:"startOffset,omitempty"`
	EndOffset    string `json:"endOffset,omitempty"`
	Word         string `json:"word"`
	SpeakerTag   int    `json:"speakerTag,omitempty"`
	SpeakerLabel string `json:"speakerLabel,omitempty"`
}

// googleEnglishPrimary is the language Google always receives as its primary
// languageCode. RecognitionConfig.languageCode is REQUIRED, and Google cannot
// express unconstrained multilanguage — the docs cap automatic recognition at
// a short candidate list and offer no auto value. Owner decision 2026-08-11:
// English is the standing default and a configured user language rides along
// as an alternative, so Google still recognises it.
const googleEnglishPrimary = "en-US"

// googleLanguageCodes resolves the primary language and its alternatives.
//
// Google is the one provider that cannot simply be told "no language". v1
// requires languageCode and accepts up to three additional BCP-47 tags in
// alternativeLanguageCodes, with the result reporting whichever was detected.
// Omitting the field — which is what the multilanguage sentinel used to do
// here — sent an invalid request.
func googleLanguageCodes(resolved stt.ResolvedTranscribeOptions) (string, []string) {
	requested := mapLanguageCode(resolved.APILanguage())
	if requested == "" {
		// Multilanguage: English carries the request. A language the user
		// configured is still honoured, as an alternative rather than as the
		// primary, so mixed speech resolves to whichever fits better.
		//
		// resolved.Language is checked through stt.IsMultilanguage first: unlike
		// APILanguage it does not suppress the provider default, so without
		// the guard a leftover default would masquerade as a user choice, and
		// the sentinel itself would be mapped as if it were a language tag.
		if !stt.IsMultilanguage(resolved.Language) {
			if configured := mapLanguageCode(strings.TrimSpace(resolved.Language)); configured != "" &&
				!strings.EqualFold(configured, googleEnglishPrimary) {
				return googleEnglishPrimary, []string{configured}
			}
		}
		return googleEnglishPrimary, nil
	}
	if strings.EqualFold(requested, googleEnglishPrimary) {
		return requested, nil
	}
	// An explicit pin stays the primary language, with English alongside it —
	// the product is multilanguage even when the user names a preference.
	return requested, []string{googleEnglishPrimary}
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

func googleProfileID(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	return "stt.google.latest-long"
}

// googleEndpoint builds a validated Google STT URL with an api-key query param.
func (p *Provider) googleEndpoint(path string) (string, error) {
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
func (p *Provider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	endpoint, err := p.googleEndpoint("v1/speech:recognize")
	if err != nil {
		return nil, err
	}

	model := p.Model
	if opts.Model != "" {
		model = opts.Model
	}

	resolved := stt.ResolveTranscribeOptions("google", googleProfileID(model), opts, provideropts.Values{
		provideropts.OptionLanguage:       stt.LanguageMulti,
		provideropts.OptionVocabularyBias: true,
	}, nil)
	langCode, altLangCodes := googleLanguageCodes(resolved)
	speakerOpts := resolved.Speaker

	// Google STT v1 rejects a sample-rate mismatch with HTTP 400. Derive the
	// real rate from the WAV header and send raw PCM; fall back to 16 kHz for
	// already-raw PCM input (the SpeechKit device-capture rate).
	content := audio
	sampleRate := 16000
	if pcm, rate, _, ok := stt.PCM16FromWAV(audio); ok {
		content = pcm
		sampleRate = rate
	}

	reqBody := googleRecognizeRequest{
		Config: googleRecognitionConfig{
			Encoding:                 "LINEAR16",
			SampleRateHertz:          sampleRate,
			LanguageCode:             langCode,
			AlternativeLanguageCodes: altLangCodes,
			Model:                    model,
		},
		Audio: googleRecognitionAudio{
			Content: base64.StdEncoding.EncodeToString(content),
		},
	}
	if speakerOpts.WantsDiarization() {
		reqBody.Config.EnableWordTimeOffsets = true
		reqBody.Config.DiarizationConfig = &googleDiarizationConfig{
			EnableSpeakerDiarization: true,
			MinSpeakerCount:          stt.MinSpeakers(speakerOpts),
			MaxSpeakerCount:          stt.MaxSpeakers(speakerOpts),
		}
	}
	if resolved.Punctuation {
		reqBody.Config.EnableAutomaticPunctuation = true
	}
	if resolved.Timestamps {
		reqBody.Config.EnableWordTimeOffsets = true
	}
	if resolved.UseVocabularyKeyterms && len(resolved.Keyterms) > 0 {
		reqBody.Config.SpeechContexts = []googleSpeechContext{{Phrases: resolved.Keyterms}}
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
		return nil, netsec.ProviderStatusError("google", resp.StatusCode, respBody)
	}

	var gResp googleRecognizeResponse
	if err := json.Unmarshal(respBody, &gResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Concatenate all result transcripts.
	var text string
	var confidence float64
	var diarization *speaker.DiarizationResult
	for _, r := range gResp.Results {
		if len(r.Alternatives) > 0 {
			text += r.Alternatives[0].Transcript
			confidence = r.Alternatives[0].Confidence
		}
	}

	// Report what Google actually detected. With alternativeLanguageCodes the
	// response labels each result with the winning language, which is the only
	// honest answer under multilanguage — the previous fallback reported "de"
	// for audio that was never German.
	detected := ""
	for _, r := range gResp.Results {
		if code := strings.TrimSpace(r.LanguageCode); code != "" {
			detected = code
			break
		}
	}
	lang := stt.FirstNonEmptyTrimmed(detected, resolved.APILanguage(), langCode, stt.LanguageMulti)
	if speakerOpts.WantsDiarization() {
		diarization = googleDiarizationFromResponse(gResp, p.Name(), model, text, lang)
	}

	return &stt.Result{
		Text:       text,
		Language:   lang,
		Duration:   duration,
		Provider:   p.Name(),
		Model:      model,
		Confidence: confidence,
		Speakers:   diarization,
	}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "google"
}

func googleDiarizationFromResponse(resp googleRecognizeResponse, provider, model, text, language string) *speaker.DiarizationResult {
	googleWords := googleDiarizedWords(resp)
	if len(googleWords) == 0 {
		return nil
	}
	words := make([]speaker.SpeakerWord, 0, len(googleWords))
	for _, word := range googleWords {
		label := strings.TrimSpace(word.SpeakerLabel)
		if label == "" {
			// Google v1 diarization assigns a speakerTag per word. Some models
			// (e.g. latest_long) 0-index the tags, so tag 0 is a real speaker,
			// not "unassigned" — dropping it collapsed two speakers into one.
			// This function only runs when diarization was requested.
			label = fmt.Sprint(word.SpeakerTag)
		}
		words = append(words, speaker.SpeakerWord{
			Text:         word.Word,
			StartMs:      parseGoogleDurationMs(firstNonEmptyString(word.StartTime, word.StartOffset)),
			EndMs:        parseGoogleDurationMs(firstNonEmptyString(word.EndTime, word.EndOffset)),
			SpeakerLabel: speaker.NormalizeSpeakerLabel(label),
		})
	}
	// Google v1 can split diarized words across results (one per speaker turn);
	// sort by start time so segments reflect the true conversational order.
	sort.SliceStable(words, func(i, j int) bool { return words[i].StartMs < words[j].StartMs })
	segments := speaker.BuildSegmentsFromWords(words)
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

// googleDiarizedWords aggregates word-level diarization output. Google v1 may
// either split words across results (one per speaker turn) or consolidate them
// in a final result; collecting words from every result/alternative that
// carries them handles both shapes without dropping a speaker.
func googleDiarizedWords(resp googleRecognizeResponse) []googleWordInfo {
	var words []googleWordInfo
	for i := range resp.Results {
		for j := range resp.Results[i].Alternatives {
			if alt := resp.Results[i].Alternatives[j]; len(alt.Words) > 0 {
				words = append(words, alt.Words...)
				break
			}
		}
	}
	return words
}

func parseGoogleDurationMs(raw string) int64 {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "s"))
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return int64(value * 1000)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Health checks if the Google Speech API is reachable.
func (p *Provider) Health(ctx context.Context) error {
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

// Capabilities reports what this provider does beyond plain transcription.
func (*Provider) Capabilities() []speechkit.Capability {
	return append(stt.BaseCapabilities(), speechkit.CapabilitySpeakerDiarization)
}
