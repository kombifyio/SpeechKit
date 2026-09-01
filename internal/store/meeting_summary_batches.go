package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

var _ MeetingSummaryBatchStore = (*SQLiteStore)(nil)
var _ MeetingSummaryBatchStore = (*PostgresStore)(nil)

func (s *sqlStore) UpsertMeetingSummaryBatch(ctx context.Context, batch MeetingSummaryBatch) (MeetingSummaryBatch, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return MeetingSummaryBatch{}, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, batch.SessionID, scopeID); err != nil {
		return MeetingSummaryBatch{}, err
	}
	batch.BatchKey = strings.TrimSpace(batch.BatchKey)
	if batch.BatchKey == "" || batch.StartSegmentID <= 0 || batch.EndSegmentID < batch.StartSegmentID {
		return MeetingSummaryBatch{}, fmt.Errorf("invalid meeting summary batch")
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(`
		INSERT INTO meeting_summary_batches
			(session_id, batch_key, level, start_segment_id, end_segment_id, source_fingerprint,
			 status, digest_json, provider, model, error_kind, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(batch_key) DO UPDATE SET
			status = excluded.status,
			digest_json = excluded.digest_json,
			provider = excluded.provider,
			model = excluded.model,
			error_kind = excluded.error_kind,
			updated_at = excluded.updated_at`),
		batch.SessionID, batch.BatchKey, batch.Level, batch.StartSegmentID, batch.EndSegmentID,
		batch.SourceFingerprint, string(batch.Status), batch.DigestJSON, batch.Provider,
		batch.Model, batch.ErrorKind, now)
	if err != nil {
		return MeetingSummaryBatch{}, fmt.Errorf("upsert meeting summary batch: %w", err)
	}
	rows, err := s.ListMeetingSummaryBatches(ctx, batch.SessionID)
	if err != nil {
		return MeetingSummaryBatch{}, err
	}
	for _, stored := range rows {
		if stored.BatchKey == batch.BatchKey {
			return stored, nil
		}
	}
	return MeetingSummaryBatch{}, fmt.Errorf("upserted meeting summary batch not found")
}

func (s *sqlStore) ListMeetingSummaryBatches(ctx context.Context, sessionID int64) ([]MeetingSummaryBatch, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(`
		SELECT id, session_id, batch_key, level, start_segment_id, end_segment_id,
		       source_fingerprint, status, digest_json, provider, model, error_kind,
		       created_at, updated_at
		FROM meeting_summary_batches
		WHERE session_id = ?
		ORDER BY level, start_segment_id, id`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list meeting summary batches: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	out := make([]MeetingSummaryBatch, 0)
	for rows.Next() {
		var batch MeetingSummaryBatch
		var status string
		if err := rows.Scan(
			&batch.ID, &batch.SessionID, &batch.BatchKey, &batch.Level,
			&batch.StartSegmentID, &batch.EndSegmentID, &batch.SourceFingerprint,
			&status, &batch.DigestJSON, &batch.Provider, &batch.Model,
			&batch.ErrorKind, &batch.CreatedAt, &batch.UpdatedAt,
		); err != nil {
			return nil, err
		}
		batch.Status = MeetingSummaryBatchStatus(status)
		out = append(out, batch)
	}
	return out, rows.Err()
}
