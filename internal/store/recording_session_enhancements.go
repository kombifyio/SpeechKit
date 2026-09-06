package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

var _ RecordingSessionEnhancementStore = (*SQLiteStore)(nil)
var _ RecordingSessionEnhancementStore = (*PostgresStore)(nil)

// CreateRecordingSessionEnhancement opens a write-up run. It starts as running
// because it is created by the job that is about to do the work.
func (s *sqlStore) CreateRecordingSessionEnhancement(ctx context.Context, sessionID int64, enhancement RecordingSessionEnhancement) (int64, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return 0, err
	}
	enhancement = normalizeRecordingSessionEnhancement(enhancement)
	id, err := s.dialect.insertReturningID(ctx, s.db,
		`INSERT INTO recording_session_enhancements (
			session_id, template_slug, template_snapshot, status, error, provider, model,
			stage, progress, attempt, input_fingerprint, error_kind, retryable, consent_version,
			structured, content_json, content_md
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		enhancement.TemplateSlug,
		enhancement.TemplateSnapshot,
		string(enhancement.Status),
		enhancement.Error,
		enhancement.Provider,
		enhancement.Model,
		enhancement.Stage,
		enhancement.Progress,
		enhancement.Attempt,
		enhancement.InputFingerprint,
		enhancement.ErrorKind,
		enhancement.Retryable,
		enhancement.ConsentVersion,
		enhancement.Structured,
		enhancement.ContentJSON,
		enhancement.ContentMD,
	)
	if err != nil {
		return 0, fmt.Errorf("create recording session enhancement: %w", err)
	}
	return id, nil
}

// UpdateRecordingSessionEnhancement records the outcome of a write-up run.
func (s *sqlStore) UpdateRecordingSessionEnhancement(ctx context.Context, id int64, enhancement RecordingSessionEnhancement) error {
	enhancement = normalizeRecordingSessionEnhancement(enhancement)
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(
		`UPDATE recording_session_enhancements
		 SET status = ?, error = ?, provider = ?, model = ?, stage = ?, progress = ?,
		     attempt = ?, input_fingerprint = ?, error_kind = ?, retryable = ?, consent_version = ?, structured = ?,
		     content_json = ?, content_md = ?, updated_at = ?
		 WHERE id = ?`),
		string(enhancement.Status),
		enhancement.Error,
		enhancement.Provider,
		enhancement.Model,
		enhancement.Stage,
		enhancement.Progress,
		enhancement.Attempt,
		enhancement.InputFingerprint,
		enhancement.ErrorKind,
		enhancement.Retryable,
		enhancement.ConsentVersion,
		enhancement.Structured,
		enhancement.ContentJSON,
		enhancement.ContentMD,
		time.Now().UTC(),
		id,
	)
	if err != nil {
		return fmt.Errorf("update recording session enhancement: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListRecordingSessionEnhancements returns the write-ups of one meeting,
// newest first, so a caller can show the current one and offer the earlier
// takes without a second query.
func (s *sqlStore) ListRecordingSessionEnhancements(ctx context.Context, sessionID int64) ([]RecordingSessionEnhancement, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return nil, err
	}
	return s.loadRecordingSessionEnhancements(ctx, sessionID)
}

// loadRecordingSessionEnhancements reads the write-ups of a session the caller
// has already established access to.
func (s *sqlStore) loadRecordingSessionEnhancements(ctx context.Context, sessionID int64) ([]RecordingSessionEnhancement, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT id, session_id, template_slug, template_snapshot, status, error, provider, model,
			stage, progress, attempt, input_fingerprint, error_kind, retryable, consent_version,
			structured, content_json, content_md, created_at, updated_at
		 FROM recording_session_enhancements
		 WHERE session_id = ?
		 ORDER BY created_at DESC, id DESC`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list recording session enhancements: %w", err)
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable.

	out := make([]RecordingSessionEnhancement, 0)
	for rows.Next() {
		var enhancement RecordingSessionEnhancement
		var status string
		var structured boolValue
		var retryable boolValue
		if err := rows.Scan(
			&enhancement.ID,
			&enhancement.SessionID,
			&enhancement.TemplateSlug,
			&enhancement.TemplateSnapshot,
			&status,
			&enhancement.Error,
			&enhancement.Provider,
			&enhancement.Model,
			&enhancement.Stage,
			&enhancement.Progress,
			&enhancement.Attempt,
			&enhancement.InputFingerprint,
			&enhancement.ErrorKind,
			&retryable,
			&enhancement.ConsentVersion,
			&structured,
			&enhancement.ContentJSON,
			&enhancement.ContentMD,
			&enhancement.CreatedAt,
			&enhancement.UpdatedAt,
		); err != nil {
			return nil, err
		}
		enhancement.Status = RecordingSessionEnhancementStatus(status)
		enhancement.Structured = bool(structured)
		enhancement.Retryable = bool(retryable)
		out = append(out, enhancement)
	}
	return out, rows.Err()
}

func normalizeRecordingSessionEnhancement(enhancement RecordingSessionEnhancement) RecordingSessionEnhancement {
	enhancement.TemplateSlug = strings.TrimSpace(enhancement.TemplateSlug)
	enhancement.Error = strings.TrimSpace(enhancement.Error)
	enhancement.Provider = strings.TrimSpace(enhancement.Provider)
	enhancement.Model = strings.TrimSpace(enhancement.Model)
	enhancement.Stage = strings.TrimSpace(enhancement.Stage)
	enhancement.InputFingerprint = strings.TrimSpace(enhancement.InputFingerprint)
	enhancement.ErrorKind = strings.TrimSpace(enhancement.ErrorKind)
	if enhancement.Progress < 0 {
		enhancement.Progress = 0
	} else if enhancement.Progress > 100 {
		enhancement.Progress = 100
	}
	if enhancement.Attempt <= 0 {
		enhancement.Attempt = 1
	}
	switch enhancement.Status {
	case RecordingSessionEnhancementPending, RecordingSessionEnhancementRunning, RecordingSessionEnhancementPartial,
		RecordingSessionEnhancementReady, RecordingSessionEnhancementFailed, RecordingSessionEnhancementCancelled:
	default:
		enhancement.Status = RecordingSessionEnhancementIdle
	}
	return enhancement
}
