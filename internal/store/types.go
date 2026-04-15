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

// SemanticCapabilityProvider is an optional extension for stores that can
// advertise indexing/vector capabilities without forcing every backend to
// implement semantic features immediately.
type SemanticCapabilityProvider interface {
	SemanticCapabilities(ctx context.Context) SemanticCapabilities
}

// ListOpts controls pagination and filtering for list queries.
type ListOpts struct {
	Limit    int
	Offset   int
	Language string
	After    time.Time
}

type AudioAsset struct {
	StorageKind AudioStorageKind `json:"storageKind"`
	Path        string           `json:"path,omitempty"`
	MimeType    string           `json:"mimeType"`
	SizeBytes   int64            `json:"sizeBytes"`
	DurationMs  int64            `json:"durationMs"`
}

type SemanticCapabilities struct {
	Provider     SemanticProvider `json:"provider"`
	FullText     bool             `json:"fullText"`
	Embeddings   bool             `json:"embeddings"`
	VectorSearch bool             `json:"vectorSearch"`
}

// Transcription represents a saved transcription record.
type Transcription struct {
	ID         int64
	Text       string
	Language   string
	Provider   string
	Model      string
	DurationMs int64
	LatencyMs  int64
	AudioPath  string
	Audio      *AudioAsset
	CreatedAt  time.Time
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

type Stats struct {
	Transcriptions        int
	QuickNotes            int
	TotalWords            int
	TotalAudioDurationMs  int64
	AverageWordsPerMinute float64
	AverageLatencyMs      int64
}
