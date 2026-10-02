package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

var _ RecordingSessionImportStore = (*SQLiteStore)(nil)
var _ RecordingSessionImportStore = (*PostgresStore)(nil)

// UpsertRecordingSessionImport writes the one import row of a session and
// kind, creating it on first use.
func (s *sqlStore) UpsertRecordingSessionImport(ctx context.Context, item RecordingSessionImport) (RecordingSessionImport, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return RecordingSessionImport{}, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, item.SessionID, scopeID); err != nil {
		return RecordingSessionImport{}, err
	}
	item.Kind = RecordingSessionImportKind(strings.TrimSpace(string(item.Kind)))
	if item.Kind == "" {
		return RecordingSessionImport{}, fmt.Errorf("recording session import needs a kind")
	}
	if strings.TrimSpace(string(item.Status)) == "" {
		item.Status = RecordingSessionImportWaiting
	}
	if item.Attempts < 0 {
		item.Attempts = 0
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(`
		INSERT INTO recording_session_imports
			(session_id, kind, status, attempts, next_attempt_at, deadline_at,
			 external_meeting_id, external_item_id, subject, content_json, error_kind, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, kind) DO UPDATE SET
			status = excluded.status,
			attempts = excluded.attempts,
			next_attempt_at = excluded.next_attempt_at,
			deadline_at = excluded.deadline_at,
			external_meeting_id = excluded.external_meeting_id,
			external_item_id = excluded.external_item_id,
			subject = excluded.subject,
			content_json = excluded.content_json,
			error_kind = excluded.error_kind,
			updated_at = excluded.updated_at`),
		item.SessionID, string(item.Kind), string(item.Status), item.Attempts,
		// Written in the dialect's comparison format: SQLite compares these
		// as text against the due-time argument, and a driver-formatted
		// timestamp with an offset suffix sorts after the same second.
		s.nullableTimeArg(item.NextAttemptAt), s.nullableTimeArg(item.DeadlineAt),
		strings.TrimSpace(item.ExternalMeetingID), strings.TrimSpace(item.ExternalItemID),
		strings.TrimSpace(item.Subject), item.ContentJSON, strings.TrimSpace(item.ErrorKind), now,
	)
	if err != nil {
		return RecordingSessionImport{}, fmt.Errorf("upsert recording session import: %w", err)
	}
	rows, err := s.loadRecordingSessionImports(ctx, item.SessionID)
	if err != nil {
		return RecordingSessionImport{}, err
	}
	for _, stored := range rows {
		if stored.Kind == item.Kind {
			return stored, nil
		}
	}
	return RecordingSessionImport{}, fmt.Errorf("upserted recording session import not found")
}

func (s *sqlStore) ListRecordingSessionImports(ctx context.Context, sessionID int64) ([]RecordingSessionImport, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return nil, err
	}
	return s.loadRecordingSessionImports(ctx, sessionID)
}

func (s *sqlStore) ListDueRecordingSessionImports(ctx context.Context, now time.Time, limit int) ([]RecordingSessionImport, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	return s.queryRecordingSessionImports(ctx, `
		SELECT i.id, i.session_id, i.kind, i.status, i.attempts, i.next_attempt_at, i.deadline_at,
		       i.external_meeting_id, i.external_item_id, i.subject, i.content_json, i.error_kind,
		       i.created_at, i.updated_at
		FROM recording_session_imports i
		JOIN recording_sessions s ON s.id = i.session_id
		WHERE s.scope_id = ? AND i.status = ? AND (i.next_attempt_at IS NULL OR i.next_attempt_at <= ?)
		ORDER BY i.next_attempt_at, i.id
		LIMIT ?`, scopeID, string(RecordingSessionImportWaiting), s.dialect.timeArg(now), limit)
}

func (s *sqlStore) nullableTimeArg(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return s.dialect.timeArg(value)
}

// loadRecordingSessionImports reads the imports of a session the caller has
// already established access to.
func (s *sqlStore) loadRecordingSessionImports(ctx context.Context, sessionID int64) ([]RecordingSessionImport, error) {
	return s.queryRecordingSessionImports(ctx, `
		SELECT id, session_id, kind, status, attempts, next_attempt_at, deadline_at,
		       external_meeting_id, external_item_id, subject, content_json, error_kind,
		       created_at, updated_at
		FROM recording_session_imports
		WHERE session_id = ?
		ORDER BY kind, id`, sessionID)
}

func (s *sqlStore) queryRecordingSessionImports(ctx context.Context, query string, args ...any) ([]RecordingSessionImport, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list recording session imports: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only rows; the scan error is what matters
	out := make([]RecordingSessionImport, 0)
	for rows.Next() {
		var item RecordingSessionImport
		var kind, status string
		var nextAttempt, deadline sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.SessionID, &kind, &status, &item.Attempts, &nextAttempt, &deadline,
			&item.ExternalMeetingID, &item.ExternalItemID, &item.Subject, &item.ContentJSON,
			&item.ErrorKind, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.Kind = RecordingSessionImportKind(kind)
		item.Status = RecordingSessionImportStatus(status)
		if nextAttempt.Valid {
			item.NextAttemptAt = nextAttempt.Time.UTC()
		}
		if deadline.Valid {
			item.DeadlineAt = deadline.Time.UTC()
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
