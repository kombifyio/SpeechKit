package store

import (
	"context"
	"time"

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

// DeleteResult is returned by DeleteScope. The caller is responsible for
// unlinking AudioFilePaths from disk; the store only removes DB rows.
type DeleteResult struct {
	RowsDeleted    int      `json:"rows_deleted"`
	AudioFilePaths []string `json:"audio_file_paths"`
}

// ScopePrivacyStore is an optional extension for backends that implement
// GDPR Subject-Rights operations scoped to a Storage-3.0 scope.
//
// Both methods operate entirely within the scope boundaries — no cross-scope
// data is ever returned or deleted. Audio files on disk are NOT removed by
// DeleteScope (only the database rows are deleted). The caller receives the
// paths in DeleteResult and is responsible for unlinking them. Disk cleanup
// for ExportScope is the caller's responsibility (stream the paths returned in
// AudioAssetPaths alongside the JSON).
type ScopePrivacyStore interface {
	// ExportScope returns all user-owned records for the given scope. The
	// returned ScopeExport includes audio asset paths; the caller is
	// responsible for streaming the raw audio bytes if needed.
	ExportScope(ctx context.Context, scope Scope) (*ScopeExport, error)

	// DeleteScope removes all user-owned DB rows for the given scope across
	// every scoped table. Returns a DeleteResult with the total row count and
	// the distinct audio file paths that were associated with those rows.
	// The caller must unlink AudioFilePaths from disk; the store does not.
	DeleteScope(ctx context.Context, scope Scope) (DeleteResult, error)
}

// ScopeExport is the structured payload returned by ExportScope (GDPR Art. 15).
type ScopeExport struct {
	Scope              Scope                 `json:"scope"`
	Transcriptions     []Transcription       `json:"transcriptions"`
	QuickNotes         []QuickNote           `json:"quick_notes"`
	VoiceAgentSessions []VoiceAgentSession   `json:"voice_agent_sessions"`
	DictionaryEntries  []UserDictionaryEntry `json:"dictionary_entries,omitempty"`
	AudioAssetPaths    []string              `json:"audio_asset_paths"`
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
