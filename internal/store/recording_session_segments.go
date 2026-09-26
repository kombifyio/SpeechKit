package store

import (
	"context"
	"fmt"
	"strings"
)

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
