package google

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/auth"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// ModelChirp3 is Google's recommended Speech-to-Text model. It runs on the v2
// API only, in the "us" and "eu" multi-regions — never on v1 and never in the
// "global" location — so the provider routes it to a regional v2 recognizer.
const ModelChirp3 = "chirp_3"

// defaultChirp3Region is the multi-region used when Provider.Region is empty.
const defaultChirp3Region = "us"

// isChirp3 reports whether model selects Chirp 3 (case-insensitive).
func isChirp3(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), ModelChirp3)
}

// chirp3Region resolves the v2 multi-region for Chirp 3. The value becomes
// part of the endpoint hostname, so only the regions Chirp 3 is offered in are
// accepted.
func (p *Provider) chirp3Region() (string, error) {
	region := strings.ToLower(strings.TrimSpace(p.Region))
	if region == "" {
		return defaultChirp3Region, nil
	}
	switch region {
	case "us", "eu":
		return region, nil
	default:
		return "", fmt.Errorf("google stt v2: region %q is not supported for %s; use \"us\" or \"eu\"", p.Region, ModelChirp3)
	}
}

// chirp3Host is the regional v2 service host for region.
func chirp3Host(region string) string {
	return region + "-speech.googleapis.com"
}

// resolveProjectID picks the Google Cloud project for v2 requests: the
// explicit ProjectID, then the project embedded in creds (when non-nil), then
// GOOGLE_CLOUD_PROJECT from the environment or the secret resolver.
func (p *Provider) resolveProjectID(ctx context.Context, creds *auth.Credentials) (string, error) {
	projectID := strings.TrimSpace(p.ProjectID)
	var projectIDErr error
	if projectID == "" && creds != nil {
		projectID, projectIDErr = creds.ProjectID(ctx)
		projectID = strings.TrimSpace(projectID)
	}
	if projectID == "" {
		projectID = stt.FirstNonEmptyTrimmed(os.Getenv("GOOGLE_CLOUD_PROJECT"), p.SecretResolver.Resolve("GOOGLE_CLOUD_PROJECT"))
	}
	if projectID == "" {
		if projectIDErr != nil {
			return "", fmt.Errorf("google stt v2: project id unavailable from credentials: %w; set GOOGLE_CLOUD_PROJECT", projectIDErr)
		}
		return "", fmt.Errorf("google stt v2: project id not found in credentials; set GOOGLE_CLOUD_PROJECT")
	}
	// The project id is interpolated into the request path and recognizer name.
	for _, r := range projectID {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '.' && r != ':' {
			return "", fmt.Errorf("google stt v2: invalid project id %q", projectID)
		}
	}
	return projectID, nil
}

// chirp3RecognizeRequest is the protojson body of the v2 Recognize RPC
// (google.cloud.speech.v2.RecognizeRequest) sent to the implicit "_" recognizer.
type chirp3RecognizeRequest struct {
	Config  chirp3RecognitionConfig `json:"config"`
	Content string                  `json:"content"` // base64-encoded audio
}

type chirp3RecognitionConfig struct {
	AutoDecodingConfig     *struct{}                     `json:"autoDecodingConfig,omitempty"`
	ExplicitDecodingConfig *chirp3ExplicitDecodingConfig `json:"explicitDecodingConfig,omitempty"`
	Model                  string                        `json:"model"`
	LanguageCodes          []string                      `json:"languageCodes"`
	Features               *chirp3RecognitionFeatures    `json:"features,omitempty"`
	Adaptation             *chirp3SpeechAdaptation       `json:"adaptation,omitempty"`
}

type chirp3ExplicitDecodingConfig struct {
	Encoding          string `json:"encoding"`
	SampleRateHertz   int    `json:"sampleRateHertz"`
	AudioChannelCount int    `json:"audioChannelCount"`
}

type chirp3RecognitionFeatures struct {
	EnableWordTimeOffsets      bool                     `json:"enableWordTimeOffsets,omitempty"`
	EnableAutomaticPunctuation bool                     `json:"enableAutomaticPunctuation,omitempty"`
	DiarizationConfig          *chirp3DiarizationConfig `json:"diarizationConfig,omitempty"`
}

type chirp3DiarizationConfig struct {
	MinSpeakerCount int `json:"minSpeakerCount,omitempty"`
	MaxSpeakerCount int `json:"maxSpeakerCount,omitempty"`
}

type chirp3SpeechAdaptation struct {
	PhraseSets []chirp3AdaptationPhraseSet `json:"phraseSets"`
}

type chirp3AdaptationPhraseSet struct {
	InlinePhraseSet chirp3PhraseSet `json:"inlinePhraseSet"`
}

type chirp3PhraseSet struct {
	Phrases []chirp3Phrase `json:"phrases"`
}

type chirp3Phrase struct {
	Value string  `json:"value"`
	Boost float32 `json:"boost,omitempty"`
}

// chirp3PhraseBoost is the per-phrase boost applied to vocabulary keyterms.
// Google recommends starting low and caps boost at 20.
const chirp3PhraseBoost = 10

// chirp3Credentials loads the service-account/ADC credentials the v2 REST
// request is authorised with. Speech-to-Text v2 rejects API keys outright
// ("API keys are not supported by this API", checked 2026-09-30), so Chirp 3
// needs credentials even when an API key serves the v1 models.
func (p *Provider) chirp3Credentials() (*auth.Credentials, error) {
	if p.v2Credentials != nil {
		return p.v2Credentials, nil
	}
	creds, err := p.resolveGoogleStreamingCredentials()
	if err != nil {
		return nil, fmt.Errorf("google stt v2: %s needs service-account or ADC credentials (API keys are not accepted by Speech-to-Text v2): %w", ModelChirp3, err)
	}
	return creds, nil
}

// chirp3Endpoint builds the validated regional v2 recognize URL. A BaseURL
// overridden away from the v1 default (tests, proxies) is honoured as-is;
// otherwise the regional host for region is used.
func (p *Provider) chirp3Endpoint(region, projectID string) (string, error) {
	base := strings.TrimSpace(p.BaseURL)
	if base == "" || base == googleSTTBaseURL {
		base = "https://" + chirp3Host(region)
	}
	path := fmt.Sprintf("v2/projects/%s/locations/%s/recognizers/_:recognize", projectID, region)
	validated, err := netsec.BuildEndpoint(base, path, p.Validation)
	if err != nil {
		return "", fmt.Errorf("google endpoint: %w", err)
	}
	return validated, nil
}

// transcribeChirp3 runs batch recognition through the v2 Recognize REST
// endpoint of the regional "_" recognizer, which is the only surface Chirp 3
// is served on.
func (p *Provider) transcribeChirp3(ctx context.Context, audio []byte, model string, opts stt.TranscribeOpts) (*stt.Result, error) {
	region, err := p.chirp3Region()
	if err != nil {
		return nil, err
	}
	creds, err := p.chirp3Credentials()
	if err != nil {
		return nil, err
	}
	projectID, err := p.resolveProjectID(ctx, creds)
	if err != nil {
		return nil, err
	}
	endpoint, err := p.chirp3Endpoint(region, projectID)
	if err != nil {
		return nil, err
	}

	resolved := stt.ResolveTranscribeOptions("google", googleProfileID(model), opts, provideropts.Values{
		provideropts.OptionLanguage:       stt.LanguageMulti,
		provideropts.OptionVocabularyBias: true,
	}, nil)
	// Same candidate list as v1: v2 languageCodes takes several tags and
	// reports the detected one per result.
	langCode, altLangCodes := googleLanguageCodes(resolved)
	speakerOpts := resolved.Speaker

	config := chirp3RecognitionConfig{
		Model:         ModelChirp3,
		LanguageCodes: append([]string{langCode}, altLangCodes...),
	}
	// WAV input carries its own header, so Google auto-detects the encoding.
	// Headerless input is SpeechKit's 16 kHz mono PCM16 device capture.
	if _, _, _, ok := stt.PCM16FromWAV(audio); ok {
		config.AutoDecodingConfig = &struct{}{}
	} else {
		config.ExplicitDecodingConfig = &chirp3ExplicitDecodingConfig{Encoding: "LINEAR16", SampleRateHertz: 16000, AudioChannelCount: 1}
	}
	features := chirp3RecognitionFeatures{
		EnableAutomaticPunctuation: resolved.Punctuation,
		EnableWordTimeOffsets:      resolved.Timestamps,
	}
	if speakerOpts.WantsDiarization() {
		features.EnableWordTimeOffsets = true
		features.DiarizationConfig = &chirp3DiarizationConfig{
			MinSpeakerCount: stt.MinSpeakers(speakerOpts),
			MaxSpeakerCount: stt.MaxSpeakers(speakerOpts),
		}
	}
	if features != (chirp3RecognitionFeatures{}) {
		config.Features = &features
	}
	if resolved.UseVocabularyKeyterms && len(resolved.Keyterms) > 0 {
		phrases := make([]chirp3Phrase, 0, len(resolved.Keyterms))
		for _, term := range resolved.Keyterms {
			if term = strings.TrimSpace(term); term != "" {
				phrases = append(phrases, chirp3Phrase{Value: term, Boost: chirp3PhraseBoost})
			}
		}
		if len(phrases) > 0 {
			config.Adaptation = &chirp3SpeechAdaptation{PhraseSets: []chirp3AdaptationPhraseSet{{InlinePhraseSet: chirp3PhraseSet{Phrases: phrases}}}}
		}
	}

	jsonBody, err := json.Marshal(chirp3RecognizeRequest{
		Config:  config,
		Content: base64.StdEncoding.EncodeToString(audio),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	token, err := creds.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("google stt v2: access token: %w", err)
	}
	req.Header.Set("Authorization", stt.FirstNonEmptyTrimmed(token.Type, "Bearer")+" "+token.Value)

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google request: %w", stt.ClassifyTransportError("google", err))
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, googleMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, stt.HTTPError("google", resp, respBody)
	}

	// The v2 RecognizeResponse protojson shape (results[].alternatives[] with
	// words[].startOffset/endOffset/speakerLabel and results[].languageCode)
	// is a subset of what googleRecognizeResponse already decodes.
	var gResp googleRecognizeResponse
	if err := json.Unmarshal(respBody, &gResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var text strings.Builder
	var confidence float64
	detected := ""
	for _, r := range gResp.Results {
		if len(r.Alternatives) > 0 {
			text.WriteString(r.Alternatives[0].Transcript)
			confidence = r.Alternatives[0].Confidence
		}
		if code := strings.TrimSpace(r.LanguageCode); code != "" && detected == "" {
			detected = code
		}
	}
	lang := stt.FirstNonEmptyTrimmed(detected, resolved.APILanguage(), langCode, stt.LanguageMulti)
	var diarization *speaker.DiarizationResult
	if speakerOpts.WantsDiarization() {
		diarization = googleDiarizationFromResponse(gResp, p.Name(), ModelChirp3, text.String(), lang)
	}

	return &stt.Result{
		Text:       text.String(),
		Language:   lang,
		Duration:   duration,
		Provider:   p.Name(),
		Model:      ModelChirp3,
		Confidence: confidence,
		Speakers:   diarization,
	}, nil
}
