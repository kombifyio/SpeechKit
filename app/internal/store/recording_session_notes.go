package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var _ RecordingSessionNotesStore = (*SQLiteStore)(nil)
var _ RecordingSessionNotesStore = (*PostgresStore)(nil)

// SaveRecordingSessionNotes replaces the notes of one meeting. The note pane is
// autosaved as the user types, so this is an upsert on the session rather than
// an append.
func (s *sqlStore) SaveRecordingSessionNotes(ctx context.Context, sessionID int64, notes RecordingSessionNotes) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return err
	}
	notes = normalizeRecordingSessionNotes(notes)
	blocks, err := json.Marshal(notes.Blocks)
	if err != nil {
		return fmt.Errorf("encode recording session note blocks: %w", err)
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, s.dialect.rebind(
		`INSERT INTO recording_session_notes (session_id, content_md, blocks_json, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
			content_md = excluded.content_md,
			blocks_json = excluded.blocks_json,
			updated_at = excluded.updated_at`),
		sessionID,
		notes.ContentMD,
		string(blocks),
		now,
	); err != nil {
		return fmt.Errorf("save recording session notes: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE recording_sessions SET updated_at = %s WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		sessionID,
		scopeID,
	)
	return nil
}

// GetRecordingSessionNotes returns the meeting's notes. A meeting nobody typed
// into has empty notes rather than none, so callers do not need to distinguish
// "not written yet" from "written and cleared".
func (s *sqlStore) GetRecordingSessionNotes(ctx context.Context, sessionID int64) (*RecordingSessionNotes, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return nil, err
	}
	return s.loadRecordingSessionNotes(ctx, sessionID)
}

// loadRecordingSessionNotes reads notes for a session the caller has already
// established access to. Subject exports resolve their own scope explicitly, so
// they cannot go through the context-scoped check.
func (s *sqlStore) loadRecordingSessionNotes(ctx context.Context, sessionID int64) (*RecordingSessionNotes, error) {
	notes := RecordingSessionNotes{SessionID: sessionID, Blocks: []RecordingSessionNoteBlock{}}
	var blocks string
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT content_md, blocks_json, created_at, updated_at
		 FROM recording_session_notes WHERE session_id = ?`), sessionID).
		Scan(&notes.ContentMD, &blocks, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return &notes, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load recording session notes: %w", err)
	}
	if strings.TrimSpace(blocks) != "" {
		if err := json.Unmarshal([]byte(blocks), &notes.Blocks); err != nil {
			return nil, fmt.Errorf("decode recording session note blocks: %w", err)
		}
	}
	if notes.Blocks == nil {
		notes.Blocks = []RecordingSessionNoteBlock{}
	}
	if createdAt.Valid {
		notes.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		notes.UpdatedAt = updatedAt.Time
	}
	return &notes, nil
}

func normalizeRecordingSessionNotes(notes RecordingSessionNotes) RecordingSessionNotes {
	blocks := make([]RecordingSessionNoteBlock, 0, len(notes.Blocks))
	for _, block := range notes.Blocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		if block.TsMs < 0 {
			block.TsMs = 0
		}
		block.Text = text
		block.ID = strings.TrimSpace(block.ID)
		blocks = append(blocks, block)
	}
	notes.Blocks = blocks
	return notes
}
