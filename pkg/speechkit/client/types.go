package client

import (
	"fmt"
	"time"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

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

type Status struct {
	Status        string         `json:"status"`
	Components    map[string]any `json:"components"`
	UptimeSeconds int64          `json:"uptime_seconds"`
	Version       string         `json:"version"`
}

type TranscribeOptions struct {
	Language string
	Model    string
	Prompt   string
	Speaker  speaker.Options
}

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

type ConfigSummary map[string]any

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

type Word = speechcustomize.Word
type Replacement = speechcustomize.Replacement
type Lexicon = speechcustomize.Lexicon
type Ruleset = speechcustomize.Ruleset
type Pack = speechcustomize.Pack

type AudioAsset struct {
	StorageKind string `json:"storageKind"`
	Path        string `json:"path,omitempty"`
	MimeType    string `json:"mimeType"`
	SizeBytes   int64  `json:"sizeBytes"`
	DurationMs  int64  `json:"durationMs"`
}

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

type VoiceAgentTranscript struct {
	ID         int64            `json:"id"`
	Transcript string           `json:"transcript"`
	Turns      []map[string]any `json:"turns,omitempty"`
	Language   string           `json:"language"`
	CreatedAt  time.Time        `json:"created_at"`
}

type VoiceAgentSummary struct {
	ID        int64          `json:"id"`
	Summary   map[string]any `json:"summary"`
	Language  string         `json:"language"`
	CreatedAt time.Time      `json:"created_at"`
}

type TTSSynthesizeRequest struct {
	Text   string  `json:"text"`
	Locale string  `json:"locale,omitempty"`
	Voice  string  `json:"voice,omitempty"`
	Speed  float64 `json:"speed,omitempty"`
	Format string  `json:"format,omitempty"`
}

type TTSSynthesizeResponse struct {
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`
	SampleRate  int    `json:"sample_rate,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Voice       string `json:"voice,omitempty"`
}

type Voice struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Locale    string `json:"locale"`
	Default   bool   `json:"default"`
	Discovery string `json:"discovery,omitempty"`
}

type PersonaResource map[string]any
type RoleResource map[string]any
type SequenceResource map[string]any

type CatalogReadiness = framework.Readiness
type ProviderDefault = catalog.ProviderDefault
type ProviderMatrixRow = catalog.ProviderMatrixRow
