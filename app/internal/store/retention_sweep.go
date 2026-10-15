package store

// retention_sweep.go enforces the [privacy] retention scope and the retention
// TTL (see docs/privacy/retention-scope.md).
//
// Under the ephemeral scope a meeting's raw material — its transcript
// segments, rolling digests, imported Teams transcript and screen captures —
// is written while the meeting runs and swept once the work that needs it is
// over: the meeting has ended, no write-up is queued or running, no Teams
// transcript import is still waiting and no notes export is pending or
// failed. The session row, the user's notes and
// the write-ups stay. The host calls SweepEndedSession whenever one of those
// pieces of work completes; whichever finishes last performs the sweep. The
// maintenance pass (store open, then daily) sweeps whatever the events
// missed, and after ephemeralSweepBackstop it sweeps regardless of pending
// work, so a write-up or import that never finishes cannot keep a recording
// alive.
//
// The TTL is a hard upper bound by age, independent of the scope and of the
// retention-days settings: pinning exempts a meeting from meeting retention,
// never from the TTL.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// ErrRetentionSweepDeferred reports that a session still has work that needs
// its raw material — the meeting is still recording, a write-up is queued or
// running, or a Teams transcript import is waiting. Nothing was deleted; the
// completion of that work sweeps the session.
var ErrRetentionSweepDeferred = errors.New("store: retention sweep deferred until the session's work completes")

// ephemeralSweepBackstop bounds how long pending work can hold a session's raw
// material under the ephemeral scope. A Teams transcript import gives up after
// 45 minutes and a write-up after its own deadline, so a day only ever matters
// for work a crash or a stuck provider left behind.
const ephemeralSweepBackstop = 24 * time.Hour

// ephemeralSweepGrace keeps the maintenance pass away from a session that
// ended moments ago: between the end of a meeting and the queueing of its
// write-up or Teams import nothing is pending yet, and that sweep belongs to
// the event that completes the work.
const ephemeralSweepGrace = 10 * time.Minute

// RetentionSweepReason says which rule removed the content.
type RetentionSweepReason string

const (
	// RetentionSweepReasonScope: the ephemeral retention scope.
	RetentionSweepReasonScope RetentionSweepReason = "scope"
	// RetentionSweepReasonTTL: the retention TTL.
	RetentionSweepReasonTTL RetentionSweepReason = "ttl"
)

// RetentionSweepTrigger says what ran the sweep.
type RetentionSweepTrigger string

const (
	// RetentionSweepTriggerSessionEnd: the last piece of work of one session
	// (meeting end, write-up, Teams import) completed.
	RetentionSweepTriggerSessionEnd RetentionSweepTrigger = "session_end"
	// RetentionSweepTriggerMaintenance: the pass at store open and daily.
	RetentionSweepTriggerMaintenance RetentionSweepTrigger = "maintenance"
)

// RetentionSweep counts what one sweep removed. It carries counts only, never
// content, so it can go to the audit log as it is.
type RetentionSweep struct {
	Reason  RetentionSweepReason  `json:"reason"`
	Trigger RetentionSweepTrigger `json:"trigger"`
	// SessionID is the swept session for a session-end sweep, 0 otherwise.
	SessionID      int64 `json:"sessionId,omitempty"`
	Segments       int64 `json:"segments"`
	SummaryBatches int64 `json:"summaryBatches"`
	Imports        int64 `json:"imports"`
	Snapshots      int64 `json:"snapshots"`
	// Sessions are whole recording sessions removed, with their notes and
	// write-ups (TTL under the ephemeral scope only).
	Sessions           int64 `json:"sessions"`
	Transcriptions     int64 `json:"transcriptions"`
	QuickNotes         int64 `json:"quickNotes"`
	VoiceAgentSessions int64 `json:"voiceAgentSessions"`
	AudioFiles         int64 `json:"audioFiles"`
	SnapshotFiles      int64 `json:"snapshotFiles"`
}

// Empty reports whether the sweep removed nothing.
func (r RetentionSweep) Empty() bool {
	return r.Segments+r.SummaryBatches+r.Imports+r.Snapshots+r.Sessions+
		r.Transcriptions+r.QuickNotes+r.VoiceAgentSessions+r.AudioFiles+r.SnapshotFiles == 0
}

func (r *RetentionSweep) add(other RetentionSweep) {
	r.Segments += other.Segments
	r.SummaryBatches += other.SummaryBatches
	r.Imports += other.Imports
	r.Snapshots += other.Snapshots
	r.Sessions += other.Sessions
	r.Transcriptions += other.Transcriptions
	r.QuickNotes += other.QuickNotes
	r.VoiceAgentSessions += other.VoiceAgentSessions
	r.AudioFiles += other.AudioFiles
	r.SnapshotFiles += other.SnapshotFiles
}

// RetentionSweepStore is an optional extension for stores that enforce the
// retention scope.
type RetentionSweepStore interface {
	// SweepEndedRecordingSession applies the retention scope to one session
	// of the caller's scope whose work may have ended. Under the retain scope
	// it does nothing. It returns ErrRetentionSweepDeferred while the session
	// still has work that needs its raw material, and sql.ErrNoRows for an
	// unknown session.
	SweepEndedRecordingSession(ctx context.Context, sessionID int64) (RetentionSweep, error)
}

// SweepEndedSession applies the retention scope to a session whose work may
// have ended, when st enforces it. Call it after every piece of work on the
// session completes or is abandoned; the store decides whether this was the
// last one. A deferred sweep is not an error for the caller.
func SweepEndedSession(ctx context.Context, st Store, sessionID int64) {
	sweeper, ok := st.(RetentionSweepStore)
	if !ok || sweeper == nil || sessionID <= 0 {
		return
	}
	if _, err := sweeper.SweepEndedRecordingSession(ctx, sessionID); err != nil &&
		!errors.Is(err, ErrRetentionSweepDeferred) && !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("store: retention sweep failed", "session", sessionID, "err", err)
	}
}

// normalizeStoreRetentionScope resolves the configured scope: empty is
// retain, an unknown value fails closed to ephemeral.
func normalizeStoreRetentionScope(scope framework.RetentionScope) framework.RetentionScope {
	return framework.NormalizeRetentionScope(string(scope))
}

// ephemeral reports whether the store sweeps raw material. The constructors
// normalize the scope, so a zero value means retain.
func (s *sqlStore) ephemeral() bool {
	return s.retentionScope == framework.RetentionScopeEphemeral
}

// SweepEndedRecordingSession implements RetentionSweepStore.
func (s *sqlStore) SweepEndedRecordingSession(ctx context.Context, sessionID int64) (RetentionSweep, error) {
	if !s.ephemeral() {
		return RetentionSweep{}, nil
	}
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return RetentionSweep{}, err
	}
	var kind, status string
	err = s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT kind, status FROM recording_sessions WHERE id = ? AND scope_id = ?`), sessionID, scopeID).Scan(&kind, &status)
	if err != nil {
		return RetentionSweep{}, err
	}
	if status == string(RecordingSessionStatusActive) {
		return RetentionSweep{}, ErrRetentionSweepDeferred
	}
	pending, err := s.sessionWorkPending(ctx, sessionID)
	if err != nil {
		return RetentionSweep{}, err
	}
	if pending {
		return RetentionSweep{}, ErrRetentionSweepDeferred
	}
	sweep, err := s.sweepSessionRawMaterial(ctx, sessionID, RecordingSessionKind(kind))
	if err != nil {
		return RetentionSweep{}, err
	}
	sweep.Reason = RetentionSweepReasonScope
	sweep.Trigger = RetentionSweepTriggerSessionEnd
	sweep.SessionID = sessionID
	s.reportRetentionSweep(sweep)
	return sweep, nil
}

// sessionWorkPending reports whether a write-up is queued or running, a
// Teams transcript import is still waiting, or a notes export is pending or
// failed: all of them read the meeting's raw material. A write-up a restart
// interrupted counts as pending until a newer write-up of the session exists,
// because startup recovery resumes it. A failed export keeps the raw material
// for its retry until the TTL removes it.
func (s *sqlStore) sessionWorkPending(ctx context.Context, sessionID int64) (bool, error) {
	var pending int64
	err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT
			(SELECT COUNT(*) FROM recording_session_enhancements e WHERE e.session_id = ? AND (
				e.status IN (?, ?)
				OR (e.status = ? AND e.stage = ? AND NOT EXISTS (
					SELECT 1 FROM recording_session_enhancements n WHERE n.session_id = e.session_id AND n.id > e.id)))) +
			(SELECT COUNT(*) FROM recording_session_imports WHERE session_id = ? AND kind = ? AND status = ?) +
			(SELECT COUNT(*) FROM recording_session_exports WHERE session_id = ? AND status IN (?, ?))`),
		sessionID, string(RecordingSessionEnhancementPending), string(RecordingSessionEnhancementRunning),
		string(RecordingSessionEnhancementPartial), RecordingSessionEnhancementStageInterrupted,
		sessionID, string(RecordingSessionImportTeamsTranscript), string(RecordingSessionImportWaiting),
		sessionID, string(RecordingSessionExportPending), string(RecordingSessionExportFailed),
	).Scan(&pending)
	if err != nil {
		return false, fmt.Errorf("check pending session work: %w", err)
	}
	return pending > 0, nil
}

// sweepSessionRawMaterial removes the raw material of one session and keeps
// the session row, the notes, the write-ups and the Copilot recap. Screen
// captures go for every session kind; transcript segments, rolling digests
// and the Teams transcript import only for meetings, where they are the raw
// material behind the write-up. A dictation session's transcript is the
// dictation's own result.
func (s *sqlStore) sweepSessionRawMaterial(ctx context.Context, sessionID int64, kind RecordingSessionKind) (RetentionSweep, error) {
	var sweep RetentionSweep
	// Read before the transaction: a SQLite transaction that starts with a
	// read and then writes fails with SQLITE_BUSY at once, without waiting,
	// when another connection wrote in between. The session has ended, so no
	// snapshot is added meanwhile.
	snapshotPaths, err := queryStrings(ctx, s.db, s.dialect.rebind(
		`SELECT path FROM recording_session_snapshots WHERE session_id = ? AND path <> ''`), sessionID)
	if err != nil {
		return sweep, fmt.Errorf("collect snapshot files: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sweep, fmt.Errorf("begin retention sweep: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	steps := []sweepStep{
		{&sweep.Snapshots, `DELETE FROM recording_session_snapshots WHERE session_id = ?`, []any{sessionID}},
	}
	if kind == RecordingSessionKindMeeting {
		steps = append(steps,
			sweepStep{&sweep.SummaryBatches, `DELETE FROM meeting_summary_batches WHERE session_id = ?`, []any{sessionID}},
			sweepStep{&sweep.Imports, `DELETE FROM recording_session_imports WHERE session_id = ? AND kind = ?`, []any{sessionID, string(RecordingSessionImportTeamsTranscript)}},
			sweepStep{&sweep.Segments, `DELETE FROM recording_session_segments WHERE session_id = ?`, []any{sessionID}},
		)
	}
	if err := s.runSweepSteps(ctx, tx, steps); err != nil {
		return RetentionSweep{}, fmt.Errorf("retention sweep: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RetentionSweep{}, fmt.Errorf("commit retention sweep: %w", err)
	}
	sweep.SnapshotFiles = s.removeFilesCounted(snapshotPaths)
	return sweep, nil
}

// sweepStep is one DELETE of a sweep; count, when set, receives the rows it
// removed.
type sweepStep struct {
	count *int64
	query string
	args  []any
}

func (s *sqlStore) runSweepSteps(ctx context.Context, tx execContexter, steps []sweepStep) error {
	for _, step := range steps {
		result, err := tx.ExecContext(ctx, s.dialect.rebind(step.query), step.args...)
		if err != nil {
			return err
		}
		if step.count != nil {
			*step.count, _ = result.RowsAffected()
		}
	}
	return nil
}

func (s *sqlStore) removeFilesCounted(paths []string) int64 {
	var removed int64
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := s.removeManagedFile(path); err != nil {
			slog.Warn("store: file not removed by retention", "err", err)
			continue
		}
		removed++
	}
	return removed
}

func (s *sqlStore) reportRetentionSweep(sweep RetentionSweep) {
	if sweep.Empty() {
		return
	}
	slog.Info("store: retention sweep",
		"reason", string(sweep.Reason), "trigger", string(sweep.Trigger), "session", sweep.SessionID,
		"segments", sweep.Segments, "summary_batches", sweep.SummaryBatches, "imports", sweep.Imports,
		"snapshots", sweep.Snapshots, "sessions", sweep.Sessions, "transcriptions", sweep.Transcriptions,
		"quick_notes", sweep.QuickNotes, "voice_agent_sessions", sweep.VoiceAgentSessions,
		"audio_files", sweep.AudioFiles, "snapshot_files", sweep.SnapshotFiles)
	if s.onRetentionSweep != nil {
		s.onRetentionSweep(sweep)
	}
}

// enforceEphemeralScope is the maintenance side of the ephemeral scope: it
// sweeps every session that ended more than ephemeralSweepGrace ago, still
// holds raw material and has no work pending, every ended session past
// ephemeralSweepBackstop whatever is pending, and every stored audio file (no audio is written under this
// scope, so what is left was kept before the scope was set or by a crash).
func (s *sqlStore) enforceEphemeralScope(now time.Time) {
	if !s.ephemeral() {
		return
	}
	ctx := context.Background()
	total := RetentionSweep{Reason: RetentionSweepReasonScope, Trigger: RetentionSweepTriggerMaintenance}
	candidates, err := s.sessionsWithRawMaterial(ctx, `rs.status <> ?`, string(RecordingSessionStatusActive))
	if err != nil {
		slog.Warn("store: ephemeral sweep candidates", "err", err)
		return
	}
	grace := now.Add(-ephemeralSweepGrace)
	backstop := now.Add(-ephemeralSweepBackstop)
	for _, candidate := range candidates {
		if !candidate.endedAt.Before(grace) {
			continue
		}
		if !candidate.endedAt.Before(backstop) {
			pending, err := s.sessionWorkPending(ctx, candidate.id)
			if err != nil || pending {
				continue
			}
		} else if failed, err := s.sessionExportFailed(ctx, candidate.id); err != nil || failed {
			// A failed export keeps the raw material with a warning until
			// the TTL, past the backstop too.
			continue
		}
		sweep, err := s.sweepSessionRawMaterial(ctx, candidate.id, candidate.kind)
		if err != nil {
			slog.Warn("store: ephemeral sweep", "session", candidate.id, "err", err)
			continue
		}
		total.add(sweep)
	}
	total.AudioFiles += s.removeAudioCreatedBefore(now)
	s.reportRetentionSweep(total)
}

// sessionExportFailed reports whether a notes export of the session failed
// and still waits for a retry.
func (s *sqlStore) sessionExportFailed(ctx context.Context, sessionID int64) (bool, error) {
	var failed int64
	err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT COUNT(*) FROM recording_session_exports WHERE session_id = ? AND status = ?`),
		sessionID, string(RecordingSessionExportFailed)).Scan(&failed)
	if err != nil {
		return false, fmt.Errorf("check failed session export: %w", err)
	}
	return failed > 0, nil
}

// enforceRetentionTTL removes what is older than the TTL. Under either scope
// that is the raw material of every ended session (pinned or not), stored
// audio and Voice Agent conversations. Under the ephemeral scope it is also
// whole sessions with their notes and write-ups, dictation and Assist history
// and quick notes, pinned or not.
func (s *sqlStore) enforceRetentionTTL(now time.Time) {
	if s.retentionTTLDays <= 0 {
		return
	}
	ctx := context.Background()
	cutoff := now.Add(-time.Duration(s.retentionTTLDays) * 24 * time.Hour)
	total := RetentionSweep{Reason: RetentionSweepReasonTTL, Trigger: RetentionSweepTriggerMaintenance}

	candidates, err := s.sessionsWithRawMaterial(ctx, `rs.status <> ?`, string(RecordingSessionStatusActive))
	if err != nil {
		slog.Warn("store: retention ttl candidates", "err", err)
		return
	}
	for _, candidate := range candidates {
		if !candidate.endedAt.Before(cutoff) {
			continue
		}
		sweep, err := s.sweepSessionRawMaterial(ctx, candidate.id, candidate.kind)
		if err != nil {
			slog.Warn("store: retention ttl sweep", "session", candidate.id, "err", err)
			continue
		}
		total.add(sweep)
	}
	total.AudioFiles += s.removeAudioCreatedBefore(cutoff)
	history, err := s.discardHistoryOlderThan(ctx, cutoff, s.ephemeral())
	if err != nil {
		slog.Warn("store: retention ttl history", "err", err)
	}
	total.add(history)
	s.reportRetentionSweep(total)
}

type rawMaterialCandidate struct {
	id      int64
	kind    RecordingSessionKind
	endedAt time.Time
}

// sessionsWithRawMaterial lists the sessions matching where that still hold
// something a sweep removes, so an already swept session is not swept, or
// reported, again.
func (s *sqlStore) sessionsWithRawMaterial(ctx context.Context, where string, args ...any) ([]rawMaterialCandidate, error) {
	meeting := string(RecordingSessionKindMeeting)
	query := `SELECT rs.id, rs.kind, rs.ended_at, rs.updated_at FROM recording_sessions rs
		WHERE ` + where + ` AND (
			EXISTS (SELECT 1 FROM recording_session_snapshots x WHERE x.session_id = rs.id)
			OR (rs.kind = ? AND (
				EXISTS (SELECT 1 FROM recording_session_segments x WHERE x.session_id = rs.id)
				OR EXISTS (SELECT 1 FROM meeting_summary_batches x WHERE x.session_id = rs.id)
				OR EXISTS (SELECT 1 FROM recording_session_imports x WHERE x.session_id = rs.id AND x.kind = ?))))`
	args = append(args, meeting, string(RecordingSessionImportTeamsTranscript))
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err reports iteration failures
	var out []rawMaterialCandidate
	for rows.Next() {
		var candidate rawMaterialCandidate
		var kind string
		var ended sql.NullTime
		var updated time.Time
		if err := rows.Scan(&candidate.id, &kind, &ended, &updated); err != nil {
			return nil, err
		}
		candidate.kind = RecordingSessionKind(kind)
		candidate.endedAt = updated.UTC()
		if ended.Valid {
			candidate.endedAt = ended.Time.UTC()
		}
		out = append(out, candidate)
	}
	return out, rows.Err()
}

// removeAudioCreatedBefore deletes the audio files and their rows created
// before cutoff, whoever owns them, and returns how many files went.
func (s *sqlStore) removeAudioCreatedBefore(cutoff time.Time) int64 {
	ctx := context.Background()
	type asset struct {
		kind, path string
		id         int64
	}
	// Read before the transaction, for the reason given in
	// sweepSessionRawMaterial.
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT owner_kind, owner_id, path FROM audio_assets
		 WHERE path <> '' AND created_at < ?
		 ORDER BY created_at ASC, id ASC`), s.dialect.timeArg(cutoff))
	if err != nil {
		slog.Warn("store: query audio retention rows", "err", err)
		return 0
	}
	var assets []asset
	for rows.Next() {
		var a asset
		if err := rows.Scan(&a.kind, &a.id, &a.path); err != nil {
			slog.Warn("store: scan audio retention row", "err", err)
			continue
		}
		assets = append(assets, a)
	}
	iterErr := rows.Err()
	_ = rows.Close()
	if iterErr != nil {
		slog.Warn("store: audio retention rows", "err", iterErr)
		return 0
	}
	if len(assets) == 0 {
		return 0
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		slog.Warn("store: begin audio retention tx", "err", err)
		return 0
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	var removed int64
	for _, a := range assets {
		if err := s.removeManagedFile(a.path); err != nil {
			slog.Warn("store: remove retained audio", "err", err)
			continue
		}
		removed++
		if err := deleteAudioAsset(ctx, tx, s.dialect.name, a.kind, a.id, a.path); err != nil {
			slog.Warn("store: delete retained audio asset", "kind", a.kind, "id", a.id, "err", err)
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit audio retention", "err", err)
	}
	return removed
}

// discardHistoryOlderThan removes Voice Agent conversations older than cutoff
// and, with everything set (the TTL under the ephemeral scope), also whole
// recording sessions with their notes and write-ups, dictation and Assist
// history and quick notes — pinned ones included, because the TTL is a hard
// bound.
func (s *sqlStore) discardHistoryOlderThan(ctx context.Context, cutoff time.Time, everything bool) (RetentionSweep, error) {
	var sweep RetentionSweep
	at := s.dialect.timeArg(cutoff)
	active := string(RecordingSessionStatusActive)
	var snapshotFiles []string
	if everything {
		snapshotFiles = s.snapshotFilesForSessionDelete(ctx,
			`rs.status <> ? AND COALESCE(rs.ended_at, rs.updated_at) < ?`, active, at)
	}

	// Read before the transaction, for the reason given in
	// sweepSessionRawMaterial.
	var scopes []int64
	var audioPaths []string
	var err error
	if everything {
		scopes, err = queryInt64s(ctx, s.db, s.dialect.rebind(
			`SELECT scope_id FROM transcriptions WHERE created_at < ?
			 UNION SELECT scope_id FROM quick_notes WHERE created_at < ?`), at, at)
		if err != nil {
			return sweep, fmt.Errorf("history ttl scopes: %w", err)
		}
		audioPaths, err = queryStrings(ctx, s.db, s.dialect.rebind(
			`SELECT path FROM audio_assets WHERE path <> '' AND (
				(owner_kind = 'transcription' AND owner_id IN (SELECT id FROM transcriptions WHERE created_at < ?))
				OR (owner_kind = 'quick_note' AND owner_id IN (SELECT id FROM quick_notes WHERE created_at < ?)))`), at, at)
		if err != nil {
			return sweep, fmt.Errorf("history ttl audio: %w", err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return sweep, fmt.Errorf("begin history ttl: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	steps := []sweepStep{
		{nil, `DELETE FROM voice_agent_session_turns WHERE session_id IN (SELECT id FROM voice_agent_sessions WHERE created_at < ?)`, []any{at}},
		{nil, `DELETE FROM voice_agent_session_summary_items WHERE session_id IN (SELECT id FROM voice_agent_sessions WHERE created_at < ?)`, []any{at}},
		{&sweep.VoiceAgentSessions, `DELETE FROM voice_agent_sessions WHERE created_at < ?`, []any{at}},
	}
	if everything {
		steps = append(steps,
			sweepStep{nil, `DELETE FROM transcription_audio_assets WHERE transcription_id IN (SELECT id FROM transcriptions WHERE created_at < ?)`, []any{at}},
			sweepStep{nil, `DELETE FROM quick_note_audio_assets WHERE quick_note_id IN (SELECT id FROM quick_notes WHERE created_at < ?)`, []any{at}},
			sweepStep{nil, `DELETE FROM audio_assets WHERE owner_kind = 'transcription' AND owner_id IN (SELECT id FROM transcriptions WHERE created_at < ?)`, []any{at}},
			sweepStep{nil, `DELETE FROM audio_assets WHERE owner_kind = 'quick_note' AND owner_id IN (SELECT id FROM quick_notes WHERE created_at < ?)`, []any{at}},
			sweepStep{&sweep.Transcriptions, `DELETE FROM transcriptions WHERE created_at < ?`, []any{at}},
			sweepStep{&sweep.QuickNotes, `DELETE FROM quick_notes WHERE created_at < ?`, []any{at}},
			sweepStep{&sweep.Sessions, `DELETE FROM recording_sessions WHERE status <> ? AND COALESCE(ended_at, updated_at) < ?`, []any{active, at}},
		)
	}
	if err := s.runSweepSteps(ctx, tx, steps); err != nil {
		return RetentionSweep{}, fmt.Errorf("history ttl: %w", err)
	}
	for _, scopeID := range scopes {
		if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
			return RetentionSweep{}, fmt.Errorf("history ttl stats: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return RetentionSweep{}, fmt.Errorf("commit history ttl: %w", err)
	}
	sweep.AudioFiles = s.removeFilesCounted(audioPaths)
	if sweep.Sessions > 0 {
		sweep.SnapshotFiles = s.removeFilesCounted(snapshotFiles)
	}
	return sweep, nil
}
