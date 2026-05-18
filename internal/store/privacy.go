package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// Compile-time interface guards.
var (
	_ ScopePrivacyStore = (*SQLiteStore)(nil)
	_ ScopePrivacyStore = (*PostgresStore)(nil)
)

// ExportScope returns all user-owned records for the given scope (GDPR Art. 15).
//
// Implementation notes:
//   - Fetches all rows for the scope without pagination (no LIMIT imposed).
//   - Audio file paths are collected from audio_assets so the caller can
//     stream raw bytes alongside the exported JSON if needed.
//   - Read-only; never modifies data.
func (s *SQLiteStore) ExportScope(ctx context.Context, scope speechstorage.Scope) (*ScopeExport, error) {
	scope = speechstorage.NormalizeScope(scope)
	scopeID, err := ensureSQLiteScopeID(ctx, s.db, scope)
	if err != nil {
		return nil, err
	}

	export := &ScopeExport{Scope: scope}

	if export.Transcriptions, err = s.listTranscriptionsByScopeID(ctx, scopeID); err != nil {
		return nil, err
	}
	if export.QuickNotes, err = s.listQuickNotesByScopeID(ctx, scopeID); err != nil {
		return nil, err
	}
	if export.VoiceAgentSessions, err = s.listVoiceAgentSessionsByScopeID(ctx, scopeID); err != nil {
		return nil, err
	}
	if export.DictionaryEntries, err = s.listUserDictionaryEntriesByScopeID(ctx, scopeID); err != nil {
		return nil, err
	}
	if export.AudioAssetPaths, err = s.listAudioAssetPathsByScopeID(ctx, scopeID); err != nil {
		return nil, err
	}
	return export, nil
}

// DeleteScope removes all user-owned DB rows for the given scope across every
// scoped table and returns a DeleteResult with the total count of deleted rows
// and the distinct audio file paths that were stored under that scope
// (GDPR Art. 17).
//
// Deletion order respects foreign-key constraints:
//  1. Collect audio file paths (before rows are gone)
//  2. Link tables (transcription_audio_assets, quick_note_audio_assets)
//  3. audio_assets
//  4. voice_agent_session_turns, voice_agent_session_summary_items
//  5. voice_agent_sessions
//  6. transcriptions
//  7. quick_notes
//  8. user_dictionary_entries
//  9. store_stats row (reset to zero counts)
//
// NOTE: Audio files on disk are NOT deleted here; only DB rows are removed.
// The caller receives AudioFilePaths in the returned DeleteResult and is
// responsible for unlinking them.
func (s *SQLiteStore) DeleteScope(ctx context.Context, scope speechstorage.Scope) (DeleteResult, error) {
	scope = speechstorage.NormalizeScope(scope)
	scopeID, err := ensureSQLiteScopeID(ctx, s.db, scope)
	if err != nil {
		return DeleteResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DeleteResult{}, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	// 1. Capture audio paths BEFORE deletion so the rows still exist to query.
	audioPaths, err := s.collectAudioPathsForScopeInTx(ctx, tx, scopeID)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("collect audio paths: %w", err)
	}

	total := 0

	deletes := []string{
		// 2. Link tables for audio assets
		`DELETE FROM transcription_audio_assets
		 WHERE transcription_id IN (SELECT id FROM transcriptions WHERE scope_id = ?)`,
		`DELETE FROM quick_note_audio_assets
		 WHERE quick_note_id IN (SELECT id FROM quick_notes WHERE scope_id = ?)`,
		// 3. Audio asset rows
		`DELETE FROM audio_assets WHERE scope_id = ?`,
		// 4. Voice agent children
		`DELETE FROM voice_agent_session_turns
		 WHERE session_id IN (SELECT id FROM voice_agent_sessions WHERE scope_id = ?)`,
		`DELETE FROM voice_agent_session_summary_items
		 WHERE session_id IN (SELECT id FROM voice_agent_sessions WHERE scope_id = ?)`,
		// 5–8. Owner tables
		`DELETE FROM voice_agent_sessions WHERE scope_id = ?`,
		`DELETE FROM transcriptions WHERE scope_id = ?`,
		`DELETE FROM quick_notes WHERE scope_id = ?`,
		`DELETE FROM user_dictionary_entries WHERE scope_id = ?`,
	}

	for _, q := range deletes {
		res, err := tx.ExecContext(ctx, q, scopeID)
		if err != nil {
			return DeleteResult{}, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return DeleteResult{}, err
		}
		total += int(n)
	}

	// 9. Reset stats row to zero (keep row so scope re-use is clean)
	if _, err := tx.ExecContext(ctx,
		`UPDATE store_stats SET
			transcriptions_count    = 0,
			quick_notes_count       = 0,
			total_words             = 0,
			total_audio_duration_ms = 0,
			total_latency_ms        = 0,
			latency_count           = 0
		 WHERE scope_id = ?`, scopeID); err != nil {
		return DeleteResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{
		RowsDeleted:    total,
		AudioFilePaths: audioPaths,
	}, nil
}

// collectAudioPathsForScopeInTx returns the distinct non-empty file paths from
// audio_assets rows belonging to the given scopeID. Must be called before the
// audio_assets rows are deleted (within the same transaction so the read sees
// the pre-delete snapshot under SQLite's default serializable isolation).
func (s *SQLiteStore) collectAudioPathsForScopeInTx(ctx context.Context, tx *sql.Tx, scopeID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT path FROM audio_assets
		 WHERE scope_id = ? AND path != ''
		 ORDER BY created_at ASC`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p) != "" {
			paths = append(paths, p)
		}
	}
	return paths, rows.Err()
}

// -- SQLiteStore scoped list helpers --

func (s *SQLiteStore) listTranscriptionsByScopeID(ctx context.Context, scopeID int64) ([]Transcription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.text, t.language, t.provider, COALESCE(t.model, ''), COALESCE(t.duration_ms, 0), t.latency_ms,
			COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
			t.created_at, COALESCE(t.owner_user_id, ''), COALESCE(t.owner_org_id, ''), COALESCE(t.owner_source, '')
		 FROM transcriptions t
		 LEFT JOIN audio_assets a ON a.id = (
			SELECT link.audio_asset_id
			FROM transcription_audio_assets link
			JOIN audio_assets asset ON asset.id = link.audio_asset_id
			WHERE link.transcription_id = t.id AND link.role = 'source'
			ORDER BY link.created_at DESC, link.audio_asset_id DESC
			LIMIT 1
		 )
		 WHERE t.scope_id = ?
		 ORDER BY t.created_at DESC, t.id DESC`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var results []Transcription
	for rows.Next() {
		var t Transcription
		var audioStorageKind, audioMimeType string
		var audioSizeBytes, audioDurationMs int64
		if err := rows.Scan(&t.ID, &t.Text, &t.Language, &t.Provider, &t.Model, &t.DurationMs, &t.LatencyMs,
			&t.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &t.CreatedAt,
			&t.OwnerUserID, &t.OwnerOrgID, &t.OwnerSource); err != nil {
			return nil, err
		}
		if strings.TrimSpace(t.Model) == "" {
			t.Model = s.transcriptionModelHint(t.Provider)
		}
		t.Audio = buildAudioAsset(audioStorageKind, t.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
		results = append(results, t)
	}
	if results == nil {
		results = []Transcription{}
	}
	return results, rows.Err()
}

func (s *SQLiteStore) listQuickNotesByScopeID(ctx context.Context, scopeID int64) ([]QuickNote, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT q.id, q.text, q.language, q.provider, COALESCE(q.duration_ms, 0), q.latency_ms,
			COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
			COALESCE(q.pinned, 0), q.created_at, q.updated_at
		 FROM quick_notes q
		 LEFT JOIN audio_assets a ON a.id = (
			SELECT link.audio_asset_id
			FROM quick_note_audio_assets link
			JOIN audio_assets asset ON asset.id = link.audio_asset_id
			WHERE link.quick_note_id = q.id AND link.role = 'source'
			ORDER BY link.created_at DESC, link.audio_asset_id DESC
			LIMIT 1
		 )
		 WHERE q.scope_id = ?
		 ORDER BY q.created_at DESC, q.id DESC`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var results []QuickNote
	for rows.Next() {
		var n QuickNote
		var pinned int
		var audioStorageKind, audioMimeType string
		var audioSizeBytes, audioDurationMs int64
		if err := rows.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs,
			&n.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Pinned = pinned != 0
		n.Audio = buildAudioAsset(audioStorageKind, n.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
		results = append(results, n)
	}
	if results == nil {
		results = []QuickNote{}
	}
	return results, rows.Err()
}

func (s *SQLiteStore) listVoiceAgentSessionsByScopeID(ctx context.Context, scopeID int64) ([]VoiceAgentSession, error) {
	rows, err := s.db.QueryContext(ctx, //nolint:rowserrcheck // rows.Err() is checked inside scanVoiceAgentSessionList
		`SELECT id, title, summary, transcript, language, provider_profile_id, runtime_kind, started_at, ended_at, created_at,
			COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, '')
		 FROM voice_agent_sessions
		 WHERE scope_id = ?
		 ORDER BY created_at DESC, id DESC`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	sessions, err := scanVoiceAgentSessionList(rows)
	if sessions == nil {
		sessions = []VoiceAgentSession{}
	}
	return sessions, err
}

func (s *SQLiteStore) listUserDictionaryEntriesByScopeID(ctx context.Context, scopeID int64) ([]UserDictionaryEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		 FROM user_dictionary_entries
		 WHERE scope_id = ?
		 ORDER BY id ASC`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var entries []UserDictionaryEntry
	for rows.Next() {
		var entry UserDictionaryEntry
		var enabledInt int
		if err := rows.Scan(&entry.ID, &entry.Spoken, &entry.Canonical, &entry.Language, &entry.Source,
			&enabledInt, &entry.UsageCount, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return nil, err
		}
		entry.Enabled = enabledInt != 0
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *SQLiteStore) listAudioAssetPathsByScopeID(ctx context.Context, scopeID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT path FROM audio_assets
		 WHERE scope_id = ? AND path != ''
		 ORDER BY created_at ASC`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p) != "" {
			paths = append(paths, p)
		}
	}
	return paths, rows.Err()
}

// -- PostgresStore stubs (Phase-2 follow-up: cpv.2.4) --

// ErrPrivacyNotImplementedForBackend is returned by the Postgres stubs for
// ExportScope and DeleteScope. Customer install path defaults to SQLite;
// Postgres subject-rights support is planned for Phase 2.
var ErrPrivacyNotImplementedForBackend = errors.New(
	"privacy: not implemented for postgres backend (Phase-2 follow-up, cpv.2.4)")

func (s *PostgresStore) ExportScope(_ context.Context, _ speechstorage.Scope) (*ScopeExport, error) {
	return nil, ErrPrivacyNotImplementedForBackend
}

func (s *PostgresStore) DeleteScope(_ context.Context, _ speechstorage.Scope) (DeleteResult, error) {
	return DeleteResult{}, ErrPrivacyNotImplementedForBackend
}
