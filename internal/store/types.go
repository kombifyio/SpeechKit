package store

import (
	"context"
	"time"
)

type AudioStorageKind string

const (
	AudioStorageLocalFile AudioStorageKind = "local-file"
)

type SemanticProvider string

const (
	SemanticProviderNone SemanticProvider = "none"
)

// Store is the central storage abstraction.
// Each backend, such as SQLite or future host-provided backends, implements this interface.
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

// UserDictionaryStore is an optional extension for stores that persist
// user-specific dictation terms outside config.toml.
type UserDictionaryStore interface {
	ReplaceUserDictionaryEntries(ctx context.Context, language string, entries []UserDictionaryEntry) error
	ListUserDictionaryEntries(ctx context.Context, language string) ([]UserDictionaryEntry, error)
	RecordUserDictionaryUsage(ctx context.Context, canonical, language string) error
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

// SemanticCapabilityProvider is an optional extension for stores that can
// advertise indexing/vector capabilities without forcing every backend to
// implement semantic features immediately.
type SemanticCapabilityProvider interface {
	SemanticCapabilities(ctx context.Context) SemanticCapabilities
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

type SemanticCapabilities struct {
	Provider     SemanticProvider `json:"provider"`
	FullText     bool             `json:"fullText"`
	Embeddings   bool             `json:"embeddings"`
	VectorSearch bool             `json:"vectorSearch"`
}

// Transcription represents a saved transcription record.
type Transcription struct {
	ID          int64       `json:"id"`
	Text        string      `json:"text"`
	Language    string      `json:"language"`
	Provider    string      `json:"provider"`
	Model       string      `json:"model"`
	DurationMs  int64       `json:"durationMs"`
	LatencyMs   int64       `json:"latencyMs"`
	AudioPath   string      `json:"audioPath,omitempty"`
	Audio       *AudioAsset `json:"audio,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
	OwnerUserID string      `json:"ownerUserId,omitempty"`
	OwnerOrgID  string      `json:"ownerOrgId,omitempty"`
	OwnerSource string      `json:"ownerSource,omitempty"`
}

type UserDictionaryEntry struct {
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
