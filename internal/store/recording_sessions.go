package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

var _ RecordingSessionStore = (*SQLiteStore)(nil)
var _ RecordingSessionStore = (*PostgresStore)(nil)

func (s *sqlStore) SaveRecordingSession(ctx context.Context, session RecordingSession) (int64, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	owner, _ := RecordOwnerFromContext(ctx)
	session = normalizeRecordingSession(session)

	return s.dialect.insertReturningID(ctx, s.db,
		`INSERT INTO recording_sessions (
			scope_id, external_id, kind, status, capture_status, summary_status, summary_error,
			title, language, language_base, provider, model, input_source, processing_mode,
			summary, started_at, ended_at, capture_started_at, capture_paused_at,
			capture_stopped_at, summary_updated_at, owner_user_id, owner_org_id, owner_source
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID,
		session.ExternalID,
		string(session.Kind),
		string(session.Status),
		string(session.CaptureStatus),
		string(session.SummaryStatus),
		session.SummaryError,
		session.Title,
		session.Language,
		normalizeDictionaryLanguage(session.Language),
		session.Provider,
		session.Model,
		session.InputSource,
		session.ProcessingMode,
		session.Summary,
		session.StartedAt,
		nullableRecordingTime(session.EndedAt),
		nullableRecordingTime(session.CaptureStartedAt),
		nullableRecordingTime(session.CapturePausedAt),
		nullableRecordingTime(session.CaptureStoppedAt),
		nullableRecordingTime(session.SummaryUpdatedAt),
		owner.UserID,
		owner.OrgID,
		owner.Source,
	)
}

func (s *sqlStore) UpdateRecordingSessionSummary(ctx context.Context, id int64, summary string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE recording_sessions
		 SET summary = ?, summary_status = ?, summary_error = '', summary_updated_at = ?, updated_at = %s
		 WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		strings.TrimSpace(summary),
		string(RecordingSessionSummaryReady),
		now,
		id,
		scopeID,
	)
	if err != nil {
		return fmt.Errorf("update recording session summary: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqlStore) UpdateRecordingSessionCaptureStatus(ctx context.Context, id int64, status RecordingSessionCaptureStatus, at time.Time) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	status = normalizeRecordingSessionCaptureStatus(status)
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	setParts := []string{"capture_status = ?"}
	args := []any{string(status)}
	switch status {
	case RecordingSessionCaptureRecording:
		setParts = append(setParts,
			"capture_started_at = COALESCE(capture_started_at, ?)",
			"capture_stopped_at = NULL",
		)
		args = append(args, at)
	case RecordingSessionCapturePaused:
		setParts = append(setParts, "capture_paused_at = ?")
		args = append(args, at)
	case RecordingSessionCaptureStopped:
		setParts = append(setParts, "capture_stopped_at = ?")
		args = append(args, at)
	case RecordingSessionCaptureIdle:
		setParts = append(setParts,
			"capture_started_at = NULL",
			"capture_paused_at = NULL",
			"capture_stopped_at = NULL",
		)
	}
	query := fmt.Sprintf(
		`UPDATE recording_sessions SET %s, updated_at = %s WHERE id = ? AND scope_id = ?`,
		strings.Join(setParts, ", "),
		s.dialect.now(),
	)
	args = append(args, id, scopeID)
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return fmt.Errorf("update recording session capture status: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqlStore) UpdateRecordingSessionSummaryStatus(ctx context.Context, id int64, status RecordingSessionSummaryStatus, message string, at time.Time) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	status = normalizeRecordingSessionSummaryStatus(status)
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE recording_sessions
		 SET summary_status = ?, summary_error = ?, summary_updated_at = ?, updated_at = %s
		 WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		string(status),
		strings.TrimSpace(message),
		at,
		id,
		scopeID,
	)
	if err != nil {
		return fmt.Errorf("update recording session summary status: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqlStore) FinishRecordingSession(ctx context.Context, id int64, summary string, endedAt time.Time) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	// An empty summary must not erase one that already exists: the finish
	// hook always passes "" for meetings (notes arrive later via the
	// enhancement job), and blindly overwriting would wipe a summary the
	// user already generated. Without a new summary the summary columns are
	// left untouched.
	summary = strings.TrimSpace(summary)
	var (
		result sql.Result
		err2   error
	)
	if summary == "" {
		result, err2 = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
			`UPDATE recording_sessions
			 SET status = ?, capture_status = ?, capture_stopped_at = COALESCE(capture_stopped_at, ?),
			     ended_at = ?, updated_at = %s
			 WHERE id = ? AND scope_id = ?`, s.dialect.now())),
			string(RecordingSessionStatusFinished),
			string(RecordingSessionCaptureStopped),
			endedAt.UTC(),
			endedAt.UTC(),
			id,
			scopeID,
		)
	} else {
		result, err2 = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
			`UPDATE recording_sessions
			 SET status = ?, capture_status = ?, capture_stopped_at = COALESCE(capture_stopped_at, ?),
			     summary = ?, summary_status = ?, summary_error = '', summary_updated_at = ?,
			     ended_at = ?, updated_at = %s
			 WHERE id = ? AND scope_id = ?`, s.dialect.now())),
			string(RecordingSessionStatusFinished),
			string(RecordingSessionCaptureStopped),
			endedAt.UTC(),
			summary,
			string(RecordingSessionSummaryReady),
			endedAt.UTC(),
			endedAt.UTC(),
			id,
			scopeID,
		)
	}
	if err2 != nil {
		return fmt.Errorf("finish recording session: %w", err2)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqlStore) DeleteRecordingSession(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	snapshotFiles := s.snapshotFilesForSessionDelete(ctx, `rs.id = ? AND rs.scope_id = ?`, id, scopeID)
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(`DELETE FROM recording_sessions WHERE id = ? AND scope_id = ?`), id, scopeID)
	if err != nil {
		return fmt.Errorf("delete recording session: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	removeSnapshotFiles(snapshotFiles)
	return nil
}

// enforceMeetingRetention discards finished meetings past the retention window.
//
// Only meetings, and only finished ones: a dictation session is a different
// thing with its own lifetime, and a meeting still being recorded is not a
// candidate no matter how long it has run. Pinned meetings are always kept —
// retention exists so a machine does not accumulate transcripts of every call,
// not to delete the one meeting someone needed.
//
// The children (segments, notes, write-ups) go with the session through the
// foreign keys, which is why nothing here deletes them one by one. Snapshot
// image files live outside the database, so their paths are collected before
// the rows cascade away and the files are removed afterwards.
func (s *sqlStore) enforceMeetingRetention() {
	if s.meetingRetentionDays <= 0 {
		return
	}
	cutoff := s.dialect.timeArg(time.Now().Add(-time.Duration(s.meetingRetentionDays) * 24 * time.Hour))
	snapshotFiles := s.snapshotFilesForSessionDelete(context.Background(), //nolint:contextcheck // background goroutine should not be bound to a request context
		`rs.kind = ? AND rs.status = ? AND rs.retention_pinned = ? AND rs.ended_at IS NOT NULL AND rs.ended_at < ?`,
		string(RecordingSessionKindMeeting),
		string(RecordingSessionStatusFinished),
		false,
		cutoff,
	)
	result, err := s.db.ExecContext(context.Background(), s.dialect.rebind( //nolint:contextcheck // background goroutine should not be bound to a request context
		`DELETE FROM recording_sessions
		 WHERE kind = ? AND status = ? AND retention_pinned = ? AND ended_at IS NOT NULL AND ended_at < ?`),
		string(RecordingSessionKindMeeting),
		string(RecordingSessionStatusFinished),
		false,
		cutoff,
	)
	if err != nil {
		slog.Warn("store: meeting retention sweep", "err", err)
		return
	}
	if removed, _ := result.RowsAffected(); removed > 0 {
		removeSnapshotFiles(snapshotFiles)
		slog.Info("store: meetings discarded by retention", "count", removed, "days", s.meetingRetentionDays)
	}
}

// SetRecordingSessionPinned keeps one meeting out of the retention sweep.
func (s *sqlStore) SetRecordingSessionPinned(ctx context.Context, id int64, pinned bool) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE recording_sessions SET retention_pinned = ?, updated_at = %s WHERE id = ? AND scope_id = ?`,
		s.dialect.now())),
		pinned,
		id,
		scopeID,
	)
	if err != nil {
		return fmt.Errorf("pin recording session: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
