package store

import (
	"context"
	"database/sql"
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
func (s *sqlStore) ExportScope(ctx context.Context, scope speechstorage.Scope) (*ScopeExport, error) {
	scope = speechstorage.NormalizeScope(scope)
	scopeID, err := s.scopeIDForScope(ctx, scope)
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
func (s *sqlStore) DeleteScope(ctx context.Context, scope speechstorage.Scope) (DeleteResult, error) {
	scope = speechstorage.NormalizeScope(scope)
	scopeID, err := s.scopeIDForScope(ctx, scope)
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
		res, err := tx.ExecContext(ctx, s.dialect.rebind(q), scopeID)
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
	if _, err := tx.ExecContext(ctx, s.dialect.rebind(
		`UPDATE store_stats SET
			transcriptions_count    = 0,
			quick_notes_count       = 0,
			total_words             = 0,
			total_audio_duration_ms = 0,
			total_latency_ms        = 0,
			latency_count           = 0
		 WHERE scope_id = ?`), scopeID); err != nil {
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
func (s *sqlStore) collectAudioPathsForScopeInTx(ctx context.Context, tx *sql.Tx, scopeID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, s.dialect.rebind(
		`SELECT path FROM audio_assets
		 WHERE scope_id = ? AND path != ''
		 GROUP BY path
		 ORDER BY MIN(created_at) ASC, path ASC`), scopeID)
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

func (s *sqlStore) listTranscriptionsByScopeID(ctx context.Context, scopeID int64) ([]Transcription, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT t.id, t.text, t.language, t.provider, COALESCE(t.model, ''), COALESCE(t.duration_ms, 0), t.latency_ms,
			COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
			t.created_at, COALESCE(t.owner_user_id, ''), COALESCE(t.owner_org_id, ''), COALESCE(t.owner_source, ''), COALESCE(t.speaker_json, '')
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
		 ORDER BY t.created_at DESC, t.id DESC`), scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var results []Transcription
	for rows.Next() {
		var t Transcription
		var audioStorageKind, audioMimeType string
		var audioSizeBytes, audioDurationMs int64
		var speakerJSON string
		if err := rows.Scan(&t.ID, &t.Text, &t.Language, &t.Provider, &t.Model, &t.DurationMs, &t.LatencyMs,
			&t.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &t.CreatedAt,
			&t.OwnerUserID, &t.OwnerOrgID, &t.OwnerSource, &speakerJSON); err != nil {
			return nil, err
		}
		if strings.TrimSpace(t.Model) == "" {
			t.Model = s.transcriptionModelHint(t.Provider)
		}
		t.Audio = buildAudioAsset(audioStorageKind, t.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
		t.Speakers = unmarshalSpeakerJSON(speakerJSON)
		results = append(results, t)
	}
	if results == nil {
		results = []Transcription{}
	}
	return results, rows.Err()
}

func (s *sqlStore) listQuickNotesByScopeID(ctx context.Context, scopeID int64) ([]QuickNote, error) {
	query := fmt.Sprintf(quickNoteSelectSQL, s.dialect.boolLit(false)) + ` WHERE q.scope_id = ? ORDER BY q.created_at DESC, q.id DESC`
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var results []QuickNote
	for rows.Next() {
		n, err := s.scanQuickNote(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, n)
	}
	if results == nil {
		results = []QuickNote{}
	}
	return results, rows.Err()
}

func (s *sqlStore) listVoiceAgentSessionsByScopeID(ctx context.Context, scopeID int64) ([]VoiceAgentSession, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind( //nolint:rowserrcheck // rows.Err() is checked inside scanVoiceAgentSessionList
		`SELECT id, title, summary, transcript, language, provider_profile_id, runtime_kind, started_at, ended_at, created_at,
			COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, '')
		 FROM voice_agent_sessions
		 WHERE scope_id = ?
		 ORDER BY created_at DESC, id DESC`), scopeID)
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

func (s *sqlStore) listUserDictionaryEntriesByScopeID(ctx context.Context, scopeID int64) ([]UserDictionaryEntry, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		 FROM user_dictionary_entries
		 WHERE scope_id = ?
		 ORDER BY id ASC`), scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	var entries []UserDictionaryEntry
	for rows.Next() {
		var entry UserDictionaryEntry
		var enabled boolValue
		if err := rows.Scan(&entry.ID, &entry.Spoken, &entry.Canonical, &entry.Language, &entry.Source,
			&enabled, &entry.UsageCount, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return nil, err
		}
		entry.Enabled = bool(enabled)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *sqlStore) listAudioAssetPathsByScopeID(ctx context.Context, scopeID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT path FROM audio_assets
		 WHERE scope_id = ? AND path != ''
		 GROUP BY path
		 ORDER BY MIN(created_at) ASC, path ASC`), scopeID)
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
