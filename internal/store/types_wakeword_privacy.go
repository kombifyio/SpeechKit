package store

import (
	"context"
	"time"
)

// WakewordActivationStore is the v0.37.5+ extension for backends that
// persist wake-word activation training-data uploads. Audio bytes
// live on the filesystem; this interface manages only metadata +
// relative paths. Every method is scoped to an Identity{User,Org} so
// multi-tenant installs cannot cross-share recordings.
//
// SaveWakewordActivation expects the caller to have already written
// the audio file to disk under audio_path. Implementations refuse
// inserts where audio_path is empty.
type WakewordActivationStore interface {
	// SaveWakewordActivation inserts a new activation row. Returns
	// the activation as stored (with uploaded_at populated by the
	// store). The ID field must be set by the caller (client-
	// generated ULIDs / sortable timestamps); empty ID is rejected.
	SaveWakewordActivation(ctx context.Context, a WakewordActivation) (*WakewordActivation, error)

	// GetWakewordActivation returns one activation by ID, scoped to
	// the supplied owner. Returns sql.ErrNoRows when the ID belongs
	// to a different scope so callers cannot leak existence info
	// across tenants.
	GetWakewordActivation(ctx context.Context, id, ownerUserID, ownerOrgID string) (*WakewordActivation, error)

	// ListWakewordActivations returns the caller's activations
	// (paginated newest-first). Use ListOpts.Limit + .Cursor for
	// pagination — the cursor is the captured_at timestamp of the
	// last item from the previous page.
	ListWakewordActivations(ctx context.Context, ownerUserID, ownerOrgID string, opts ListOpts) ([]WakewordActivation, error)

	// UpdateWakewordActivationLabel sets the label field (empty
	// string clears the label). Scoped to owner — returns
	// sql.ErrNoRows when the ID is in a different scope.
	UpdateWakewordActivationLabel(ctx context.Context, id, ownerUserID, ownerOrgID, label string) error

	// DeleteWakewordActivation removes the row + returns the
	// audio_path so the caller can unlink the file from disk. The
	// store itself does not touch the filesystem.
	DeleteWakewordActivation(ctx context.Context, id, ownerUserID, ownerOrgID string) (audioPath string, err error)

	// CountWakewordActivationsForUser returns the row count for one
	// owner. Used by quota enforcement (per_user_quota_bytes) and
	// the admin dashboard.
	CountWakewordActivationsForUser(ctx context.Context, ownerUserID, ownerOrgID string) (int64, error)

	// SumWakewordActivationBytesForUser returns the total audio_bytes
	// of all activations one owner has stored. Used by the quota
	// gate in the upload endpoint.
	SumWakewordActivationBytesForUser(ctx context.Context, ownerUserID, ownerOrgID string) (int64, error)
}

// WakewordActivation is the metadata row for one captured + uploaded
// wake-word activation. Audio bytes live on the filesystem under
// AudioPath (relative to <server.training_data.audio_dir>).
type WakewordActivation struct {
	ID           string    `json:"id"`
	OwnerUserID  string    `json:"owner_user_id"`
	OwnerOrgID   string    `json:"owner_org_id"`
	PhraseID     string    `json:"phrase_id"`
	Phrase       string    `json:"phrase"`
	Backend      string    `json:"backend"`
	Score        float64   `json:"score"`
	CapturedAt   time.Time `json:"captured_at"`
	UploadedAt   time.Time `json:"uploaded_at"`
	Label        string    `json:"label,omitempty"`
	AudioPath    string    `json:"audio_path"`
	AudioBytes   int64     `json:"audio_bytes"`
	SampleRate   int       `json:"sample_rate"`
	PreRollMs    int       `json:"pre_roll_ms"`
	PostRollMs   int       `json:"post_roll_ms"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
}

// Valid wake-word activation label values. Empty string means
// "unlabeled" — the user has not yet reviewed the clip. Any other
// value is rejected by UpdateWakewordActivationLabel.
const (
	WakewordLabelCorrect       = "correct"
	WakewordLabelFalsePositive = "false_positive"
	WakewordLabelUnknown       = ""
)

// ValidWakewordLabel reports whether s is one of the three canonical
// label values. Exported so the REST handler can reject bad PATCH
// payloads with a clear 400 before reaching the store.
func ValidWakewordLabel(s string) bool {
	switch s {
	case WakewordLabelCorrect, WakewordLabelFalsePositive, WakewordLabelUnknown:
		return true
	default:
		return false
	}
}

// DeleteResult is returned by DeleteScope. The caller is responsible for
// unlinking AudioFilePaths and SnapshotFilePaths from disk; the store only
// removes DB rows.
type DeleteResult struct {
	RowsDeleted    int      `json:"rows_deleted"`
	AudioFilePaths []string `json:"audio_file_paths"`
	// SnapshotFilePaths are the meeting screen captures stored under the
	// scope. They are pictures of the user's desktop, so an erasure that
	// dropped only the rows would leave the most sensitive artifact on disk.
	SnapshotFilePaths []string `json:"snapshot_file_paths"`
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
	RecordingSessions  []RecordingSession    `json:"recording_sessions,omitempty"`
	DictionaryEntries  []UserDictionaryEntry `json:"dictionary_entries,omitempty"`
	AudioAssetPaths    []string              `json:"audio_asset_paths"`
	// SnapshotFilePaths are the on-disk image files of the exported meetings'
	// screen captures, listed like AudioAssetPaths so the caller can bundle
	// them; the JSON never embeds the pixels.
	SnapshotFilePaths []string `json:"snapshot_file_paths"`
}

// SemanticCapabilityProvider is an optional extension for stores that can
// advertise indexing/vector capabilities without forcing every backend to
// implement semantic features immediately.
type SemanticCapabilityProvider interface {
	SemanticCapabilities(ctx context.Context) SemanticCapabilities
}
