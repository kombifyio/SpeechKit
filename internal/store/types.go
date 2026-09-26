package store

import (
	"context"
	"time"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// Scope is an alias for the public speechkit storage Scope, re-exported so
// callers within this package (and callers that only import store) do not need
// to reference the pkg/speechkit/storage sub-package directly.
type Scope = speechstorage.Scope

type AudioStorageKind string

const (
	AudioStorageLocalFile AudioStorageKind = "local-file"
)

type SemanticProvider string

const (
	SemanticProviderNone SemanticProvider = "none"
)

// Store is the central storage abstraction.
// Each backend (SQLite, PostgreSQL, kombify Cloud) implements this interface.
type Store interface {
	// Transcriptions
	SaveTranscription(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error
	GetTranscription(ctx context.Context, id int64) (*Transcription, error)
	ListTranscriptions(ctx context.Context, opts ListOpts) ([]Transcription, error)
	TranscriptionCount(ctx context.Context) (int, error)

	// Quick Notes
	SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error)
	GetQuickNote(ctx context.Context, id int64) (*QuickNote, error)
	ListQuickNotes(ctx context.Context, opts ListOpts) ([]QuickNote, error)
	UpdateQuickNote(ctx context.Context, id int64, text string) error
	UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error
	PinQuickNote(ctx context.Context, id int64, pinned bool) error
	DeleteQuickNote(ctx context.Context, id int64) error
	QuickNoteCount(ctx context.Context) (int, error)
	Stats(ctx context.Context) (Stats, error)

	// Lifecycle
	Close() error
}

// TranscriptionPinStore is an optional extension for stores that can mark a
// transcription as kept. It stays out of Store so a backend that only records
// dictation history is not forced to grow a curation surface.
type TranscriptionPinStore interface {
	PinTranscription(ctx context.Context, id int64, pinned bool) error
}

// UserDictionaryStore is an optional extension for stores that persist
// user-specific dictation terms outside config.toml.
type UserDictionaryStore interface {
	ReplaceUserDictionaryEntries(ctx context.Context, language string, entries []UserDictionaryEntry) error
	ListUserDictionaryEntries(ctx context.Context, language string) ([]UserDictionaryEntry, error)
	RecordUserDictionaryUsage(ctx context.Context, canonical, language string) error
}

type CustomizationListOpts = speechcustomize.ListOptions
type CustomizationReplaceOpts = speechcustomize.ReplaceOptions

type WordStore interface {
	ReplaceWords(ctx context.Context, language string, words []speechcustomize.Word) error
	ListWords(ctx context.Context, opts CustomizationListOpts) ([]speechcustomize.Word, error)
	RecordWordUsage(ctx context.Context, term, language string) error
}

type ReplacementStore interface {
	ReplaceReplacements(ctx context.Context, language string, replacements []speechcustomize.Replacement) error
	ListReplacements(ctx context.Context, opts CustomizationListOpts) ([]speechcustomize.Replacement, error)
	RecordReplacementUsage(ctx context.Context, id string) error
}

type LexiconStore interface {
	ReplaceLexicons(ctx context.Context, language string, lexicons []speechcustomize.Lexicon) error
	ListLexicons(ctx context.Context, opts CustomizationListOpts) ([]speechcustomize.Lexicon, error)
}

type RulesetStore interface {
	ReplaceRulesets(ctx context.Context, language string, rulesets []speechcustomize.Ruleset) error
	ListRulesets(ctx context.Context, opts CustomizationListOpts) ([]speechcustomize.Ruleset, error)
}

type CustomizationStore interface {
	WordStore
	ReplacementStore
	LexiconStore
	RulesetStore
}

type CustomizationSourceStore interface {
	ReplaceWordsWithOptions(ctx context.Context, opts CustomizationReplaceOpts, words []speechcustomize.Word) error
	ReplaceReplacementsWithOptions(ctx context.Context, opts CustomizationReplaceOpts, replacements []speechcustomize.Replacement) error
	ReplaceLexiconsWithOptions(ctx context.Context, opts CustomizationReplaceOpts, lexicons []speechcustomize.Lexicon) error
	ReplaceRulesetsWithOptions(ctx context.Context, opts CustomizationReplaceOpts, rulesets []speechcustomize.Ruleset) error
}

type CustomizationVocabularyStore interface {
	ReplaceVocabularyWithOptions(ctx context.Context, opts CustomizationReplaceOpts, words []speechcustomize.Word, extras []speechcustomize.Replacement) error
}

// VoiceAgentSessionStore is an optional extension for backends that persist
// Voice Agent dialogue summaries.
type VoiceAgentSessionStore interface {
	SaveVoiceAgentSession(ctx context.Context, session VoiceAgentSession) (int64, error)
	GetVoiceAgentSession(ctx context.Context, id int64) (*VoiceAgentSession, error)
	ListVoiceAgentSessions(ctx context.Context, opts ListOpts) ([]VoiceAgentSession, error)
}

// AudioAssetStore is an optional extension for backends that persist
// first-class audio asset metadata alongside legacy audio_path columns.
type AudioAssetStore interface {
	GetAudioAsset(ctx context.Context, ownerKind string, ownerID int64) (*AudioAsset, error)
}

// ListOpts controls pagination and filtering for list queries.
type ListOpts struct {
	Limit            int
	Offset           int
	Language         string
	After            time.Time
	OwnerUserID      string
	OwnerOrgID       string
	IncludeOwnerless bool
	IncludeAllOwners bool
	// Kind filters recording sessions by normalized kind ("meeting",
	// "dictation"); empty means all kinds. Only list queries over recording
	// sessions honor it.
	Kind string
}

type AudioAsset struct {
	StorageKind AudioStorageKind `json:"storageKind"`
	Path        string           `json:"-"`
	MimeType    string           `json:"mimeType"`
	SizeBytes   int64            `json:"sizeBytes"`
	DurationMs  int64            `json:"durationMs"`
}

type AudioAssetInput struct {
	Data       []byte
	MimeType   string
	Extension  string
	DurationMs int64
}

// TranscriptionAudioStore is implemented by stores that can persist
// transcription audio with accurate source metadata.
type TranscriptionAudioStore interface {
	SaveTranscriptionWithAudio(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audio AudioAssetInput) error
}

// TranscriptionSpeakerStore is implemented by stores that can persist
// normalized speaker diarization metadata alongside a transcription.
type TranscriptionSpeakerStore interface {
	SaveTranscriptionWithAudioAndSpeakers(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audio AudioAssetInput, speakers *speaker.DiarizationResult) error
}

type SemanticCapabilities struct {
	Provider     SemanticProvider `json:"provider"`
	FullText     bool             `json:"fullText"`
	Embeddings   bool             `json:"embeddings"`
	VectorSearch bool             `json:"vectorSearch"`
}

// Transcription represents a saved transcription record.
type Transcription struct {
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
	Pinned      bool                       `json:"pinned"`
}

type UserDictionaryEntry struct {
	ID         int64
	Spoken     string
	Canonical  string
	Language   string
	Source     string
	Enabled    bool
	UsageCount int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// QuickNote represents a user-created dictation note.
type QuickNote struct {
	ID         int64
	Text       string
	Language   string
	Provider   string
	DurationMs int64
	LatencyMs  int64
	AudioPath  string
	Audio      *AudioAsset
	Pinned     bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type VoiceAgentTurn struct {
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

type VoiceAgentSessionSummary struct {
	Title         string   `json:"title,omitempty"`
	Summary       string   `json:"summary"`
	Ideas         []string `json:"ideas,omitempty"`
	Decisions     []string `json:"decisions,omitempty"`
	OpenQuestions []string `json:"openQuestions,omitempty"`
	NextSteps     []string `json:"nextSteps,omitempty"`
	RawText       string   `json:"rawText,omitempty"`
}

type VoiceAgentSession struct {
	ID                int64                    `json:"id"`
	StartedAt         time.Time                `json:"startedAt"`
	EndedAt           time.Time                `json:"endedAt"`
	Language          string                   `json:"language"`
	ProviderProfileID string                   `json:"providerProfileId,omitempty"`
	RuntimeKind       string                   `json:"runtimeKind,omitempty"`
	Transcript        string                   `json:"transcript,omitempty"`
	Turns             []VoiceAgentTurn         `json:"turns,omitempty"`
	Summary           VoiceAgentSessionSummary `json:"summary"`
	CreatedAt         time.Time                `json:"createdAt"`
	OwnerUserID       string                   `json:"ownerUserId,omitempty"`
	OwnerOrgID        string                   `json:"ownerOrgId,omitempty"`
	OwnerSource       string                   `json:"ownerSource,omitempty"`
}

type Stats struct {
	Transcriptions        int
	QuickNotes            int
	TotalWords            int
	TotalAudioDurationMs  int64
	AverageWordsPerMinute float64
	AverageLatencyMs      int64
}
