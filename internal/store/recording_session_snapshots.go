package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var _ RecordingSessionSnapshotStore = (*SQLiteStore)(nil)
var _ RecordingSessionSnapshotStore = (*PostgresStore)(nil)

// SaveRecordingSessionSnapshot writes the image to the snapshot directory and
// records it against the session. Snapshots are captured locally and stay
// local: the file never leaves the machine through this package.
func (s *sqlStore) SaveRecordingSessionSnapshot(ctx context.Context, sessionID int64, input RecordingSessionSnapshotInput) (*RecordingSessionSnapshot, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return nil, err
	}
	if len(input.Data) == 0 {
		return nil, fmt.Errorf("save recording session snapshot: empty image")
	}
	mimeType := strings.TrimSpace(input.MimeType)
	if mimeType == "" {
		mimeType = "image/png"
	}
	if input.CapturedMs < 0 {
		input.CapturedMs = 0
	}
	if err := os.MkdirAll(s.snapshotDir, 0o700); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}
	filename := fmt.Sprintf("meeting-%d-%d%s", sessionID, time.Now().UnixNano(), extensionForImageMime(mimeType))
	path := filepath.Join(s.snapshotDir, filename)
	if err := os.WriteFile(path, input.Data, 0o600); err != nil {
		return nil, fmt.Errorf("save snapshot image: %w", err)
	}
	snapshot := RecordingSessionSnapshot{
		SessionID:  sessionID,
		CapturedMs: input.CapturedMs,
		Path:       path,
		MimeType:   mimeType,
		SizeBytes:  int64(len(input.Data)),
		Width:      input.Width,
		Height:     input.Height,
		Monitor:    strings.TrimSpace(input.Monitor),
		Note:       strings.TrimSpace(input.Note),
		CreatedAt:  time.Now().UTC(),
	}
	id, err := s.dialect.insertReturningID(ctx, s.db,
		`INSERT INTO recording_session_snapshots
			(session_id, captured_ms, path, mime_type, size_bytes, width, height, monitor, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.SessionID,
		snapshot.CapturedMs,
		snapshot.Path,
		snapshot.MimeType,
		snapshot.SizeBytes,
		snapshot.Width,
		snapshot.Height,
		snapshot.Monitor,
		snapshot.Note,
		s.dialect.timeArg(snapshot.CreatedAt),
	)
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("save recording session snapshot: %w", err)
	}
	snapshot.ID = id
	_, _ = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE recording_sessions SET updated_at = %s WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		sessionID,
		scopeID,
	)
	return &snapshot, nil
}

// GetRecordingSessionSnapshot returns one snapshot, path included, so callers
// can serve the image file. Missing snapshots return sql.ErrNoRows.
func (s *sqlStore) GetRecordingSessionSnapshot(ctx context.Context, id int64) (*RecordingSessionSnapshot, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, s.dialect.rebind(
		recordingSessionSnapshotSelectSQL+`
		 WHERE rss.id = ? AND rs.scope_id = ?`), id, scopeID)
	snapshot, err := scanRecordingSessionSnapshot(row)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// ListRecordingSessionSnapshots returns the session's snapshots in timeline
// order.
func (s *sqlStore) ListRecordingSessionSnapshots(ctx context.Context, sessionID int64) ([]RecordingSessionSnapshot, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecordingSessionInScope(ctx, sessionID, scopeID); err != nil {
		return nil, err
	}
	return s.loadRecordingSessionSnapshots(ctx, sessionID)
}

// loadRecordingSessionSnapshots reads snapshots for a session the caller has
// already established access to.
func (s *sqlStore) loadRecordingSessionSnapshots(ctx context.Context, sessionID int64) ([]RecordingSessionSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		recordingSessionSnapshotSelectSQL+`
		 WHERE rss.session_id = ?
		 ORDER BY rss.captured_ms ASC, rss.id ASC`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list recording session snapshots: %w", err)
	}
	defer rows.Close()
	snapshots := []RecordingSessionSnapshot{}
	for rows.Next() {
		snapshot, err := scanRecordingSessionSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, *snapshot)
	}
	return snapshots, rows.Err()
}

// DeleteRecordingSessionSnapshot removes the row and its image file.
func (s *sqlStore) DeleteRecordingSessionSnapshot(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	var path string
	err = s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT rss.path FROM recording_session_snapshots rss
		 JOIN recording_sessions rs ON rs.id = rss.session_id
		 WHERE rss.id = ? AND rs.scope_id = ?`), id, scopeID).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return fmt.Errorf("delete recording session snapshot: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, s.dialect.rebind(
		`DELETE FROM recording_session_snapshots WHERE id = ?`), id); err != nil {
		return fmt.Errorf("delete recording session snapshot: %w", err)
	}
	removeSnapshotFiles([]string{path})
	return nil
}

const recordingSessionSnapshotSelectSQL = `SELECT rss.id, rss.session_id, rss.captured_ms, rss.path,
		rss.mime_type, rss.size_bytes, rss.width, rss.height, rss.monitor, rss.note, rss.description, rss.created_at
	FROM recording_session_snapshots rss
	JOIN recording_sessions rs ON rs.id = rss.session_id`

type recordingSessionSnapshotRow interface {
	Scan(dest ...any) error
}

func scanRecordingSessionSnapshot(row recordingSessionSnapshotRow) (*RecordingSessionSnapshot, error) {
	var snapshot RecordingSessionSnapshot
	if err := row.Scan(
		&snapshot.ID,
		&snapshot.SessionID,
		&snapshot.CapturedMs,
		&snapshot.Path,
		&snapshot.MimeType,
		&snapshot.SizeBytes,
		&snapshot.Width,
		&snapshot.Height,
		&snapshot.Monitor,
		&snapshot.Note,
		&snapshot.Description,
		&snapshot.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// snapshotFilesForSessionDelete collects the image paths that a session delete
// (or the retention sweep) would orphan, so the caller can remove the files
// after the rows cascade away.
func (s *sqlStore) snapshotFilesForSessionDelete(ctx context.Context, where string, args ...any) []string {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT rss.path FROM recording_session_snapshots rss
		 JOIN recording_sessions rs ON rs.id = rss.session_id
		 WHERE `+where), args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return paths
		}
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// removeSnapshotFiles deletes image files best-effort: the database row is
// already gone, so a failed remove must not fail the operation.
func removeSnapshotFiles(paths []string) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
	}
}

func extensionForImageMime(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}
