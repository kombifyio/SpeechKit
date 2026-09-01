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

func (s *sqlStore) ListRecordingSessions(ctx context.Context, opts ListOpts) ([]RecordingSession, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	return s.listRecordingSessionsByScopeID(ctx, scopeID, opts)
}

func (s *sqlStore) AppendRecordingSessionSegment(ctx context.Context, sessionID int64, segment RecordingSessionSegment) (int64, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return 0, err
	}
	allocateIndex := segment.SegmentIndex < 0
	segment = normalizeRecordingSessionSegment(segment)
	var id int64
	if allocateIndex {
		id, err = s.appendRecordingSessionSegmentAtNextIndex(ctx, sessionID, segment)
	} else {
		id, err = s.upsertRecordingSessionSegment(ctx, sessionID, segment)
	}
	if err != nil {
		return 0, fmt.Errorf("append recording session segment: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE recording_sessions SET updated_at = %s WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		sessionID,
		scopeID,
	)
	return id, nil
}

func (s *sqlStore) upsertRecordingSessionSegment(ctx context.Context, sessionID int64, segment RecordingSessionSegment) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`INSERT INTO recording_session_segments (
			session_id, segment_index, transcription_id, provider_item_id, text, is_final,
			channel, speaker, started_ms, ended_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, segment_index) DO UPDATE SET
			transcription_id = excluded.transcription_id,
			provider_item_id = excluded.provider_item_id,
			text = excluded.text,
			is_final = excluded.is_final,
			channel = excluded.channel,
			speaker = excluded.speaker,
			started_ms = excluded.started_ms,
			ended_ms = excluded.ended_ms
		RETURNING id`),
		sessionID,
		segment.SegmentIndex,
		nullableInt64(segment.TranscriptionID),
		segment.ProviderItemID,
		segment.Text,
		segment.IsFinal,
		segment.Channel,
		segment.Speaker,
		segment.StartedMs,
		segment.EndedMs,
	).Scan(&id)
	return id, err
}

// appendRecordingSessionSegmentAtNextIndex claims the next free index for the
// session inside the insert itself, so concurrent capture channels writing into
// the same meeting cannot collide on the (session_id, segment_index) key. A
// racing pair of inserts can still lose the unique check on Postgres, where
// writers run in parallel, so the loser simply retries against the new maximum.
func (s *sqlStore) appendRecordingSessionSegmentAtNextIndex(ctx context.Context, sessionID int64, segment RecordingSessionSegment) (int64, error) {
	const attempts = 3
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		var id int64
		err := s.db.QueryRowContext(ctx, s.dialect.rebind(
			`INSERT INTO recording_session_segments (
				session_id, segment_index, transcription_id, provider_item_id, text, is_final,
				channel, speaker, started_ms, ended_ms
			)
			SELECT ?, COALESCE(MAX(segment_index), -1) + 1, ?, ?, ?, ?, ?, ?, ?, ?
			FROM recording_session_segments WHERE session_id = ?
			RETURNING id`),
			sessionID,
			nullableInt64(segment.TranscriptionID),
			segment.ProviderItemID,
			segment.Text,
			segment.IsFinal,
			segment.Channel,
			segment.Speaker,
			segment.StartedMs,
			segment.EndedMs,
			sessionID,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if !isUniqueConstraintError(err) {
			return 0, err
		}
	}
	return 0, lastErr
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key")
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

func (s *sqlStore) GetRecordingSession(ctx context.Context, id int64) (*RecordingSession, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, s.dialect.rebind(recordingSessionSelectSQL+` WHERE id = ? AND scope_id = ?`), id, scopeID)
	session, err := scanRecordingSession(row)
	if err != nil {
		return nil, err
	}
	session.Segments, err = s.listRecordingSessionSegments(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	if session.Notes, err = s.GetRecordingSessionNotes(ctx, session.ID); err != nil {
		return nil, err
	}
	if session.Snapshots, err = s.loadRecordingSessionSnapshots(ctx, session.ID); err != nil {
		return nil, err
	}
	return session, nil
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

const recordingSessionSelectSQL = `SELECT id, external_id, kind, status, capture_status, summary_status, summary_error,
		title, language, provider, model, input_source, processing_mode, summary,
		started_at, ended_at, capture_started_at, capture_paused_at, capture_stopped_at,
		summary_updated_at, created_at, updated_at,
		COALESCE(owner_user_id, ''), COALESCE(owner_org_id, ''), COALESCE(owner_source, ''),
		retention_pinned
	FROM recording_sessions`

type recordingSessionRow interface {
	Scan(dest ...any) error
}

func scanRecordingSession(row recordingSessionRow) (*RecordingSession, error) {
	var session RecordingSession
	var endedAt, captureStartedAt, capturePausedAt, captureStoppedAt, summaryUpdatedAt sql.NullTime
	var kind, status, captureStatus, summaryStatus string
	var retentionPinned boolValue
	if err := row.Scan(
		&session.ID,
		&session.ExternalID,
		&kind,
		&status,
		&captureStatus,
		&summaryStatus,
		&session.SummaryError,
		&session.Title,
		&session.Language,
		&session.Provider,
		&session.Model,
		&session.InputSource,
		&session.ProcessingMode,
		&session.Summary,
		&session.StartedAt,
		&endedAt,
		&captureStartedAt,
		&capturePausedAt,
		&captureStoppedAt,
		&summaryUpdatedAt,
		&session.CreatedAt,
		&session.UpdatedAt,
		&session.OwnerUserID,
		&session.OwnerOrgID,
		&session.OwnerSource,
		&retentionPinned,
	); err != nil {
		return nil, err
	}
	if endedAt.Valid {
		session.EndedAt = endedAt.Time
	}
	if captureStartedAt.Valid {
		session.CaptureStartedAt = captureStartedAt.Time
	}
	if capturePausedAt.Valid {
		session.CapturePausedAt = capturePausedAt.Time
	}
	if captureStoppedAt.Valid {
		session.CaptureStoppedAt = captureStoppedAt.Time
	}
	if summaryUpdatedAt.Valid {
		session.SummaryUpdatedAt = summaryUpdatedAt.Time
	}
	session.RetentionPinned = bool(retentionPinned)
	session.Kind = RecordingSessionKind(kind)
	session.Status = RecordingSessionStatus(status)
	session.CaptureStatus = normalizeRecordingSessionCaptureStatus(RecordingSessionCaptureStatus(captureStatus))
	session.SummaryStatus = normalizeRecordingSessionSummaryStatus(RecordingSessionSummaryStatus(summaryStatus))
	return &session, nil
}

func (s *sqlStore) ensureRecordingSessionInScope(ctx context.Context, id, scopeID int64) error {
	var found int64
	err := s.db.QueryRowContext(ctx, s.dialect.rebind(`SELECT id FROM recording_sessions WHERE id = ? AND scope_id = ?`), id, scopeID).Scan(&found)
	if err != nil {
		return err
	}
	return nil
}

func (s *sqlStore) listRecordingSessionSegments(ctx context.Context, sessionID int64) ([]RecordingSessionSegment, error) {
	// Ordered by capture wall-clock so the microphone and system loopback
	// channels of a meeting interleave into one readable timeline; the index is
	// only the tie-breaker for sessions recorded before timestamps existed.
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT id, session_id, segment_index, COALESCE(transcription_id, 0), provider_item_id, text,
			is_final, channel, speaker, started_ms, ended_ms, created_at
		 FROM recording_session_segments
		 WHERE session_id = ?
		 ORDER BY started_ms ASC, segment_index ASC, id ASC`), sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable.

	out := make([]RecordingSessionSegment, 0)
	for rows.Next() {
		var segment RecordingSessionSegment
		var segmentIndex int64
		var isFinal boolValue
		if err := rows.Scan(
			&segment.ID,
			&segment.SessionID,
			&segmentIndex,
			&segment.TranscriptionID,
			&segment.ProviderItemID,
			&segment.Text,
			&isFinal,
			&segment.Channel,
			&segment.Speaker,
			&segment.StartedMs,
			&segment.EndedMs,
			&segment.CreatedAt,
		); err != nil {
			return nil, err
		}
		segment.SegmentIndex = int(segmentIndex)
		segment.IsFinal = bool(isFinal)
		out = append(out, segment)
	}
	return out, rows.Err()
}

func (s *sqlStore) listRecordingSessionsByScopeID(ctx context.Context, scopeID int64, opts ...ListOpts) ([]RecordingSession, error) {
	query := recordingSessionSelectSQL + ` WHERE scope_id = ?`
	args := []any{scopeID}
	if len(opts) > 0 {
		if kind := strings.ToLower(strings.TrimSpace(opts[0].Kind)); kind != "" {
			query += ` AND kind = ?`
			args = append(args, kind)
		}
	}
	query += ` ORDER BY created_at DESC, id DESC`
	if len(opts) > 0 {
		limit := opts[0].Limit
		if limit > 0 {
			query += ` LIMIT ?`
			args = append(args, limit)
		}
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable.

	var sessions []RecordingSession
	for rows.Next() {
		session, err := scanRecordingSession(rows)
		if err != nil {
			return nil, err
		}
		session.Segments, err = s.listRecordingSessionSegments(ctx, session.ID)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *session)
	}
	if sessions == nil {
		sessions = []RecordingSession{}
	}
	return sessions, rows.Err()
}

func normalizeRecordingSession(session RecordingSession) RecordingSession {
	now := time.Now().UTC()
	session.ExternalID = strings.TrimSpace(session.ExternalID)
	session.Title = strings.TrimSpace(session.Title)
	session.Language = strings.TrimSpace(session.Language)
	session.Provider = strings.TrimSpace(session.Provider)
	session.Model = strings.TrimSpace(session.Model)
	session.InputSource = strings.TrimSpace(session.InputSource)
	session.ProcessingMode = strings.TrimSpace(session.ProcessingMode)
	session.Summary = strings.TrimSpace(session.Summary)
	switch session.Kind {
	case RecordingSessionKindMeeting:
	default:
		session.Kind = RecordingSessionKindDictation
	}
	switch session.Status {
	case RecordingSessionStatusFinished, RecordingSessionStatusFailed:
	default:
		session.Status = RecordingSessionStatusActive
	}
	session.CaptureStatus = normalizeRecordingSessionCaptureStatus(session.CaptureStatus)
	session.SummaryStatus = normalizeRecordingSessionSummaryStatus(session.SummaryStatus)
	session.SummaryError = strings.TrimSpace(session.SummaryError)
	if session.StartedAt.IsZero() {
		session.StartedAt = now
	}
	return session
}

func normalizeRecordingSessionCaptureStatus(status RecordingSessionCaptureStatus) RecordingSessionCaptureStatus {
	switch status {
	case RecordingSessionCaptureRecording, RecordingSessionCapturePaused, RecordingSessionCaptureStopped:
		return status
	default:
		return RecordingSessionCaptureIdle
	}
}

func normalizeRecordingSessionSummaryStatus(status RecordingSessionSummaryStatus) RecordingSessionSummaryStatus {
	switch status {
	case RecordingSessionSummaryRunning, RecordingSessionSummaryReady, RecordingSessionSummaryFailed:
		return status
	default:
		return RecordingSessionSummaryIdle
	}
}

func normalizeRecordingSessionSegment(segment RecordingSessionSegment) RecordingSessionSegment {
	segment.ProviderItemID = strings.TrimSpace(segment.ProviderItemID)
	segment.Text = strings.TrimSpace(segment.Text)
	segment.Channel = strings.TrimSpace(segment.Channel)
	segment.Speaker = strings.TrimSpace(segment.Speaker)
	if segment.SegmentIndex < 0 {
		segment.SegmentIndex = 0
	}
	if segment.StartedMs < 0 {
		segment.StartedMs = 0
	}
	if segment.EndedMs < segment.StartedMs {
		segment.EndedMs = segment.StartedMs
	}
	return segment
}

func nullableRecordingTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
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
