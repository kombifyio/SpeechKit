package store

import (
	"context"
	"database/sql"
	"strings"
)

func (s *sqlStore) ListRecordingSessions(ctx context.Context, opts ListOpts) ([]RecordingSession, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	return s.listRecordingSessionsByScopeID(ctx, scopeID, opts)
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
