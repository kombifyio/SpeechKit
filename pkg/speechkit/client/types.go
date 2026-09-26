package client

import (
	"fmt"
	"time"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// HTTPError is the error every request method returns when the server answers
// with a status outside 2xx. Body is the raw response body, normally the
// server's JSON error envelope, so callers can branch on StatusCode with
// errors.As instead of parsing message text.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("speechkit: HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("speechkit: HTTP %d: %s", e.StatusCode, e.Body)
}

// Status is the GET /readyz body. Status is the overall readiness across
// blocking components ("ok", "starting", "degraded" or "unavailable"),
// Components maps each component name (for example "mode.dictation") to its
// status entry, UptimeSeconds counts seconds since the server started and
// Version is the server build.
type Status struct {
	Status        string         `json:"status"`
	Components    map[string]any `json:"components"`
	UptimeSeconds int64          `json:"uptime_seconds"`
	Version       string         `json:"version"`
}

// TranscribeOptions holds the optional form fields of a [Client.TranscribeFile]
// upload. Language is a BCP-47 hint ("de", "en" or "auto"), Model an optional
// provider model override, Prompt a provider-specific recognition hint and
// Speaker the diarization or identification add-ons to request; the zero
// value transcribes only.
type TranscribeOptions struct {
	Language string
	Model    string
	Prompt   string
	Speaker  speaker.Options
}

// TranscribeResponse is the POST /v1/dictation/transcribe body. DurationMs is
// the decoded audio length and LatencyMs the server-side STT time. Confidence
// is the provider's overall score in [0,1], 0 when not reported; Speakers is
// set only when diarization ran; CustomizationActions lists the Replacement
// actions the server applied to Text.
type TranscribeResponse struct {
	Text                 string                          `json:"text"`
	Language             string                          `json:"language,omitempty"`
	DurationMs           int64                           `json:"duration_ms"`
	LatencyMs            int64                           `json:"latency_ms"`
	Provider             string                          `json:"provider,omitempty"`
	Model                string                          `json:"model,omitempty"`
	Confidence           float64                         `json:"confidence,omitempty"`
	Speakers             *speaker.DiarizationResult      `json:"speakers,omitempty"`
	CustomizationActions []framework.CustomizationAction `json:"customization_actions,omitempty"`
}

// ConfigSummary names the generic JSON object returned by GET /v1/config
// (the map [Client.Config] returns) for hosts that want a typed alias for it.
type ConfigSummary map[string]any

// DictionaryEntry is one user-dictionary correction: Spoken is the text the
// recognizer tends to produce and Canonical the text to emit instead, scoped
// to Language (BCP-47). Source records where the entry came from and
// defaults to "settings" on write; UsageCount counts how often it matched.
// ID, CreatedAt and UpdatedAt are server-assigned and ignored on write.
type DictionaryEntry struct {
	ID         int64     `json:"id,omitempty"`
	Spoken     string    `json:"spoken"`
	Canonical  string    `json:"canonical"`
	Language   string    `json:"language"`
	Source     string    `json:"source,omitempty"`
	Enabled    bool      `json:"enabled"`
	UsageCount int       `json:"usageCount,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
}

// Word aliases [speechcustomize.Word], one vocabulary bias term.
type Word = speechcustomize.Word

// Replacement aliases [speechcustomize.Replacement], one post-recognition
// rewrite rule (substitution, synonym, snippet, command or template).
type Replacement = speechcustomize.Replacement

// Lexicon aliases [speechcustomize.Lexicon], a named group of Word IDs.
type Lexicon = speechcustomize.Lexicon

// Ruleset aliases [speechcustomize.Ruleset], a named group of Replacement IDs.
type Ruleset = speechcustomize.Ruleset

// Pack aliases [speechcustomize.Pack], the exportable bundle of Words,
// Replacements, Lexicons and Rulesets exchanged by [Client.CustomizationPack]
// and [Client.ImportCustomizationPack].
type Pack = speechcustomize.Pack

// AudioAsset describes the audio retained with a [Transcript]. StorageKind is
// the server's storage backend ("local-file" today), MimeType the stored
// encoding (audio/wav when unknown), SizeBytes the stored size and DurationMs
// the audio length. The current server never serializes Path.
type AudioAsset struct {
	StorageKind string `json:"storageKind"`
	Path        string `json:"path,omitempty"`
	MimeType    string `json:"mimeType"`
	SizeBytes   int64  `json:"sizeBytes"`
	DurationMs  int64  `json:"durationMs"`
}

// Transcript is one persisted dictation result as returned by
// [Client.Transcripts] and [Client.Transcript]. DurationMs is the audio length
// and LatencyMs the STT processing time; Audio is present only when audio was
// retained; the Owner fields carry the identity the server attributed the
// record to; Speakers is set when diarization ran.
type Transcript struct {
	ID          int64                      `json:"id"`
	Text        string                     `json:"text"`
	Language    string                     `json:"language"`
	Provider    string                     `json:"provider"`
	Model       string                     `json:"model"`
	DurationMs  int64                      `json:"durationMs"`
	LatencyMs   int64                      `json:"latencyMs"`
	AudioPath   string                     `json:"audioPath,omitempty"`
	Audio       *AudioAsset                `json:"audio,omitempty"`
	CreatedAt   time.Time                  `json:"createdAt"`
	OwnerUserID string                     `json:"ownerUserId,omitempty"`
	OwnerOrgID  string                     `json:"ownerOrgId,omitempty"`
	OwnerSource string                     `json:"ownerSource,omitempty"`
	Speakers    *speaker.DiarizationResult `json:"speakers,omitempty"`
}

// VoiceAgentTranscript is the GET /v1/voiceagent/sessions/{id}/transcript
// body: the flattened Transcript of a persisted session plus its Turns, each
// a JSON object with "role", "text" and "createdAt" keys.
type VoiceAgentTranscript struct {
	ID         int64            `json:"id"`
	Transcript string           `json:"transcript"`
	Turns      []map[string]any `json:"turns,omitempty"`
	Language   string           `json:"language"`
	CreatedAt  time.Time        `json:"created_at"`
}

// VoiceAgentSummary is the GET /v1/voiceagent/sessions/{id}/summary body.
// Summary is the server's structured summary object with "title", "summary",
// "ideas", "decisions", "openQuestions", "nextSteps" and "rawText" keys.
type VoiceAgentSummary struct {
	ID        int64          `json:"id"`
	Summary   map[string]any `json:"summary"`
	Language  string         `json:"language"`
	CreatedAt time.Time      `json:"created_at"`
}

// TTSSynthesizeRequest is the POST /v1/tts/synthesize body. Text is required.
// Locale (BCP-47), Voice (a provider voice id), Speed (rate multiplier where
// 1 is normal; 0 leaves the provider default and providers clamp to their own
// range) and Format ("mp3", "wav", "opus" or "pcm"; empty means the provider
// default) are optional.
type TTSSynthesizeRequest struct {
	Text   string  `json:"text"`
	Locale string  `json:"locale,omitempty"`
	Voice  string  `json:"voice,omitempty"`
	Speed  float64 `json:"speed,omitempty"`
	Format string  `json:"format,omitempty"`
}

// TTSSynthesizeResponse is the POST /v1/tts/synthesize body. AudioBase64 is
// the audio in Format, standard base64 (decode with base64.StdEncoding);
// SampleRate is in Hz and DurationMs the audio length, each 0 when the
// provider does not report it; Provider and Voice name what actually
// synthesized the text.
type TTSSynthesizeResponse struct {
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`
	SampleRate  int    `json:"sample_rate,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Voice       string `json:"voice,omitempty"`
}

// Voice is one entry of GET /v1/tts/voices: the voice ID (or model) a
// configured TTS Provider will use, its Locale ("auto" when the provider
// chooses per request), whether it is the server's Default and Discovery,
// always "configured" because the list is a configuration snapshot rather
// than live provider discovery.
type Voice struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Locale    string `json:"locale"`
	Default   bool   `json:"default"`
	Discovery string `json:"discovery,omitempty"`
}

// PersonaResource is the untyped JSON object shape of one persona as served by
// the persona endpoints; decode [Client.Persona] output into it when a host
// has no persona type of its own.
type PersonaResource map[string]any

// RoleResource is the untyped JSON object shape of one role; see
// [Client.Role].
type RoleResource map[string]any

// SequenceResource is the untyped JSON object shape of one sequence; see
// [Client.Sequence].
type SequenceResource map[string]any

// CatalogReadiness aliases [framework.Readiness], the per-profile readiness
// report returned by [Client.CatalogReadiness] and [Client.ProviderReadiness].
type CatalogReadiness = framework.Readiness

// ProviderDefault aliases [catalog.ProviderDefault], one default profile of a
// provider as listed by [Client.CatalogProviders].
type ProviderDefault = catalog.ProviderDefault

// ProviderMatrixRow aliases [catalog.ProviderMatrixRow], one provider's
// profiles and per-feature support as listed by [Client.CatalogProviders].
type ProviderMatrixRow = catalog.ProviderMatrixRow
