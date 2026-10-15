package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

var _ RecordingSessionExportStore = (*SQLiteStore)(nil)
var _ RecordingSessionExportStore = (*PostgresStore)(nil)
var _ PendingRecordingSessionExports = (*SQLiteStore)(nil)
var _ PendingRecordingSessionExports = (*PostgresStore)(nil)

// UpsertRecordingSessionExport writes the one export record of a session and
// target, creating it on first use.
func (s *sqlStore) UpsertRecordingSessionExport(ctx context.Context, item RecordingSessionExport) (RecordingSessionExport, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return RecordingSessionExport{}, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, item.SessionID, scopeID); err != nil {
		return RecordingSessionExport{}, err
	}
	item.TargetID = strings.TrimSpace(item.TargetID)
	if item.TargetID == "" {
		return RecordingSessionExport{}, fmt.Errorf("recording session export needs a target id")
	}
	if strings.TrimSpace(string(item.Status)) == "" {
		item.Status = RecordingSessionExportPending
	}
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(`
		INSERT INTO recording_session_exports
			(session_id, target_id, destination_id, external_id, url, fingerprint,
			 status, last_error_kind, pushed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, target_id) DO UPDATE SET
			destination_id = excluded.destination_id,
			external_id = excluded.external_id,
			url = excluded.url,
			fingerprint = excluded.fingerprint,
			status = excluded.status,
			last_error_kind = excluded.last_error_kind,
			pushed_at = excluded.pushed_at,
			updated_at = excluded.updated_at`),
		item.SessionID, item.TargetID, strings.TrimSpace(item.DestinationID),
		strings.TrimSpace(item.ExternalID), strings.TrimSpace(item.URL), strings.TrimSpace(item.Fingerprint),
		string(item.Status), strings.TrimSpace(item.LastErrorKind),
		s.nullableTimeArg(item.PushedAt), time.Now().UTC(),
	)
	if err != nil {
		return RecordingSessionExport{}, fmt.Errorf("upsert recording session export: %w", err)
	}
	rows, err := s.loadRecordingSessionExports(ctx, item.SessionID)
	if err != nil {
		return RecordingSessionExport{}, err
	}
	for _, stored := range rows {
		if stored.TargetID == item.TargetID {
			return stored, nil
		}
	}
	return RecordingSessionExport{}, fmt.Errorf("upserted recording session export not found")
}

// ListRecordingSessionExports returns the export records of one session in
// the caller's scope, ordered by target.
func (s *sqlStore) ListRecordingSessionExports(ctx context.Context, sessionID int64) ([]RecordingSessionExport, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return nil, err
	}
	return s.loadRecordingSessionExports(ctx, sessionID)
}

// ListPendingRecordingSessionExports returns the export records in the
// caller's scope that are still pending: an export the process did not finish
// (a crash or a shutdown during the push), which the host starts again.
func (s *sqlStore) ListPendingRecordingSessionExports(ctx context.Context) ([]RecordingSessionExport, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(`
		SELECT e.session_id
		FROM recording_session_exports e
		JOIN recording_sessions r ON r.id = e.session_id
		WHERE r.scope_id = ? AND e.status = ?
		GROUP BY e.session_id
		ORDER BY e.session_id`), scopeID, string(RecordingSessionExportPending))
	if err != nil {
		return nil, fmt.Errorf("list pending recording session exports: %w", err)
	}
	var sessions []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		sessions = append(sessions, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	out := make([]RecordingSessionExport, 0, len(sessions))
	for _, id := range sessions {
		items, err := s.loadRecordingSessionExports(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.Status == RecordingSessionExportPending {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

func (s *sqlStore) loadRecordingSessionExports(ctx context.Context, sessionID int64) ([]RecordingSessionExport, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(`
		SELECT id, session_id, target_id, destination_id, external_id, url, fingerprint,
		       status, last_error_kind, pushed_at, created_at, updated_at
		FROM recording_session_exports
		WHERE session_id = ?
		ORDER BY target_id, id`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list recording session exports: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only rows; the scan error is what matters
	out := make([]RecordingSessionExport, 0)
	for rows.Next() {
		var item RecordingSessionExport
		var status string
		var pushedAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.SessionID, &item.TargetID, &item.DestinationID, &item.ExternalID,
			&item.URL, &item.Fingerprint, &status, &item.LastErrorKind, &pushedAt,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.Status = RecordingSessionExportStatus(status)
		if pushedAt.Valid {
			item.PushedAt = pushedAt.Time.UTC()
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
