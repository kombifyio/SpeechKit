package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// TranscriptionDeleteStore is an optional extension for stores that can
// remove a single dictation or Assist transcript together with its audio.
type TranscriptionDeleteStore interface {
	DeleteTranscription(ctx context.Context, id int64) error
}

// VoiceAgentSessionDeleteStore is an optional extension for stores that can
// remove a single Voice Agent conversation with its turns and summary.
type VoiceAgentSessionDeleteStore interface {
	DeleteVoiceAgentSession(ctx context.Context, id int64) error
}

// RecordingSessionRecoveryStore is an optional extension used at startup to
// end the sessions a previous process left running.
type RecordingSessionRecoveryStore interface {
	// CloseStaleRecordingSessions marks every session still "active" whose
	// last activity is older than before as finished, with its last
	// activity as the end time, and returns how many it closed.
	CloseStaleRecordingSessions(ctx context.Context, before time.Time) (int, error)
}

// Compactor is implemented by stores that can drop deleted content from
// their files after an erasure (SQLite: VACUUM plus a WAL truncation).
type Compactor interface {
	Compact(ctx context.Context) error
}

// orphanAudioNamePattern matches the names persistAudio gives audio files:
// "<prefix><unix-nano><ext>" with prefix "" or "qn_". The orphan sweep only
// ever touches files of this shape, so a user file dropped into the folder
// is never deleted.
var orphanAudioNamePattern = regexp.MustCompile(`^(?:qn_)?\d{10,}\.[a-z0-9]{1,10}$`)

// orphanAudioMinAge keeps the sweep away from a file whose transaction may
// still be in flight.
const orphanAudioMinAge = time.Hour

// DeleteTranscription removes one transcript of the current scope and its
// audio. Meeting segments that referenced it keep their own text.
func (s *sqlStore) DeleteTranscription(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	result, err := tx.ExecContext(ctx, s.dialect.rebind(`DELETE FROM transcriptions WHERE id = ? AND scope_id = ?`), id, scopeID)
	if err != nil {
		return fmt.Errorf("delete transcription: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	paths, err := queryStrings(ctx, tx, s.dialect.rebind(
		`SELECT path FROM audio_assets WHERE owner_kind = 'transcription' AND owner_id = ? AND path <> ''`), id)
	if err != nil {
		return fmt.Errorf("collect transcription audio: %w", err)
	}
	if err := deleteAudioAssetsForOwner(ctx, tx, s.dialect.name, "transcription", id); err != nil {
		return fmt.Errorf("delete transcription audio assets: %w", err)
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transcription delete: %w", err)
	}
	s.removeAudioFiles(paths)
	return nil
}

// DeleteVoiceAgentSession removes one Voice Agent conversation of the
// current scope. The children are named explicitly, as in DeleteScope, so
// nothing depends on the cascade being enforced.
func (s *sqlStore) DeleteVoiceAgentSession(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	owned := `SELECT id FROM voice_agent_sessions WHERE id = ? AND scope_id = ?`
	for _, q := range []string{
		`DELETE FROM voice_agent_session_turns WHERE session_id IN (` + owned + `)`,
		`DELETE FROM voice_agent_session_summary_items WHERE session_id IN (` + owned + `)`,
	} {
		if _, err := tx.ExecContext(ctx, s.dialect.rebind(q), id, scopeID); err != nil {
			return fmt.Errorf("delete voice agent session children: %w", err)
		}
	}
	result, err := tx.ExecContext(ctx, s.dialect.rebind(`DELETE FROM voice_agent_sessions WHERE id = ? AND scope_id = ?`), id, scopeID)
	if err != nil {
		return fmt.Errorf("delete voice agent session: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit voice agent session delete: %w", err)
	}
	return nil
}

// CloseStaleRecordingSessions ends sessions a previous process left running.
// A meeting live when the app quit, crashed or restarted for an update kept
// status "active" forever, and meeting retention skips active sessions, so
// it was never deleted. Its end time is its last activity, which keeps the
// retention clock honest.
func (s *sqlStore) CloseStaleRecordingSessions(ctx context.Context, before time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(
		`UPDATE recording_sessions
		 SET status = ?, capture_status = ?,
		     capture_stopped_at = COALESCE(capture_stopped_at, updated_at),
		     ended_at = COALESCE(ended_at, capture_stopped_at, updated_at)
		 WHERE status = ? AND updated_at < ?`),
		string(RecordingSessionStatusFinished),
		string(RecordingSessionCaptureStopped),
		string(RecordingSessionStatusActive),
		s.dialect.timeArg(before),
	)
	if err != nil {
		return 0, fmt.Errorf("close stale recording sessions: %w", err)
	}
	closed, _ := result.RowsAffected()
	return int(closed), nil
}

// enforceTranscriptRetention discards dictation and Assist transcripts,
// Voice Agent conversations and dictation recording sessions older than the
// retention window, with their audio. Pinned transcripts and pinned sessions
// are kept. Meetings have their own retention (enforceMeetingRetention) and
// quick notes are notes the user wrote down on purpose, so neither is
// touched here.
func (s *sqlStore) enforceTranscriptRetention() {
	if s.transcriptRetentionDays <= 0 {
		return
	}
	ctx := context.Background()
	cutoff := s.dialect.timeArg(time.Now().Add(-time.Duration(s.transcriptRetentionDays) * 24 * time.Hour))
	expired := `SELECT id FROM transcriptions WHERE created_at < ? AND pinned = ?`
	dictationSessions := `rs.kind = ? AND rs.retention_pinned = ? AND rs.updated_at < ?`
	snapshotFiles := s.snapshotFilesForSessionDelete(ctx, dictationSessions, string(RecordingSessionKindDictation), false, cutoff)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Warn("store: begin transcript retention tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	scopes, err := queryInt64s(ctx, tx, s.dialect.rebind(`SELECT DISTINCT scope_id FROM transcriptions WHERE created_at < ? AND pinned = ?`), cutoff, false)
	if err != nil {
		slog.Warn("store: transcript retention scopes", "err", err)
		return
	}
	audioPaths, err := queryStrings(ctx, tx, s.dialect.rebind(
		`SELECT path FROM audio_assets WHERE owner_kind = 'transcription' AND path <> '' AND owner_id IN (`+expired+`)`), cutoff, false)
	if err != nil {
		slog.Warn("store: transcript retention audio", "err", err)
		return
	}
	removed := map[string]int64{}
	steps := []struct {
		label string
		query string
		args  []any
	}{
		{"", `DELETE FROM transcription_audio_assets WHERE transcription_id IN (` + expired + `)`, []any{cutoff, false}},
		{"", `DELETE FROM audio_assets WHERE owner_kind = 'transcription' AND owner_id IN (` + expired + `)`, []any{cutoff, false}},
		{"transcripts", `DELETE FROM transcriptions WHERE created_at < ? AND pinned = ?`, []any{cutoff, false}},
		{"", `DELETE FROM voice_agent_session_turns WHERE session_id IN (SELECT id FROM voice_agent_sessions WHERE created_at < ?)`, []any{cutoff}},
		{"", `DELETE FROM voice_agent_session_summary_items WHERE session_id IN (SELECT id FROM voice_agent_sessions WHERE created_at < ?)`, []any{cutoff}},
		{"voice_agent_sessions", `DELETE FROM voice_agent_sessions WHERE created_at < ?`, []any{cutoff}},
		{"dictation_sessions", `DELETE FROM recording_sessions WHERE kind = ? AND retention_pinned = ? AND updated_at < ?`, []any{string(RecordingSessionKindDictation), false, cutoff}},
	}
	for _, step := range steps {
		result, err := tx.ExecContext(ctx, s.dialect.rebind(step.query), step.args...)
		if err != nil {
			slog.Warn("store: transcript retention sweep", "err", err)
			return
		}
		if step.label != "" {
			removed[step.label], _ = result.RowsAffected()
		}
	}
	for _, scopeID := range scopes {
		if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
			slog.Warn("store: transcript retention stats", "err", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit transcript retention", "err", err)
		return
	}
	s.removeAudioFiles(audioPaths)
	if removed["dictation_sessions"] > 0 {
		s.removeSnapshotFiles(snapshotFiles)
	}
	if removed["transcripts"]+removed["voice_agent_sessions"]+removed["dictation_sessions"] > 0 {
		slog.Info("store: history discarded by retention",
			"transcripts", removed["transcripts"],
			"voice_agent_sessions", removed["voice_agent_sessions"],
			"dictation_sessions", removed["dictation_sessions"],
			"days", s.transcriptRetentionDays)
	}
}

// sweepOrphanedAudio deletes audio files no row references any more: files
// written for a transaction that never committed (a crash between the write
// and the commit), or left behind by an older build. Retention, the size cap
// and erasure all find files through their rows, so an unreferenced file
// would otherwise live forever. Only files named the way persistAudio names
// them and older than orphanAudioMinAge are candidates; when the references
// cannot be read, nothing is deleted.
func (s *sqlStore) sweepOrphanedAudio(now time.Time) int {
	entries, err := os.ReadDir(s.audioDir)
	if err != nil {
		return 0
	}
	referencedPaths, err := queryStrings(context.Background(), s.db, `SELECT path FROM audio_assets WHERE path <> ''`)
	if err != nil {
		slog.Warn("store: orphan audio sweep skipped", "err", err)
		return 0
	}
	referenced := make(map[string]bool, len(referencedPaths))
	for _, path := range referencedPaths {
		referenced[filepath.Base(strings.TrimSpace(path))] = true
	}
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !orphanAudioNamePattern.MatchString(name) || referenced[name] {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < orphanAudioMinAge {
			continue
		}
		if err := s.removeManagedFile(filepath.Join(s.audioDir, name)); err != nil {
			slog.Warn("store: orphaned audio not removed", "err", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("store: orphaned audio files removed", "count", removed)
	}
	return removed
}

func (s *sqlStore) removeAudioFiles(paths []string) {
	for _, path := range paths {
		if err := s.removeManagedFile(path); err != nil {
			slog.Warn("store: audio file not removed", "err", err)
		}
	}
}

type queryContexter interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryStrings(ctx context.Context, db queryContexter, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err reports iteration failures
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func queryInt64s(ctx context.Context, db queryContexter, query string, args ...any) ([]int64, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err reports iteration failures
	var out []int64
	for rows.Next() {
		var value int64
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
