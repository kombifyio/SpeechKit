package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type execContexter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var _ AudioAssetStore = (*SQLiteStore)(nil)
var _ AudioAssetStore = (*PostgresStore)(nil)

func sqliteTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func (s *SQLiteStore) GetAudioAsset(ctx context.Context, ownerKind string, ownerID int64) (*AudioAsset, error) {
	return getPrimaryAudioAsset(ctx, s.db, "sqlite", ownerKind, ownerID)
}

func (s *PostgresStore) GetAudioAsset(ctx context.Context, ownerKind string, ownerID int64) (*AudioAsset, error) {
	return getPrimaryAudioAsset(ctx, s.db, "postgres", ownerKind, ownerID)
}

func recordAudioAsset(ctx context.Context, db execContexter, dialect, ownerKind string, ownerID int64, path string, durationMs int64, mimeType string) error {
	if path == "" || ownerID <= 0 {
		return nil
	}
	if mimeType == "" {
		mimeType = mimeTypeForAudioPath(path)
	}
	var size int64
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	if dialect == "postgres" {
		_, err := db.ExecContext(ctx, `INSERT INTO audio_assets
			(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT(owner_kind, owner_id, path) DO UPDATE SET
				storage_kind = excluded.storage_kind,
				mime_type = excluded.mime_type,
				size_bytes = excluded.size_bytes,
				duration_ms = excluded.duration_ms,
				updated_at = NOW()`,
			ownerKind, ownerID, string(AudioStorageLocalFile), path, mimeType, size, durationMs)
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO audio_assets
		(owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(owner_kind, owner_id, path) DO UPDATE SET
			storage_kind = excluded.storage_kind,
			mime_type = excluded.mime_type,
			size_bytes = excluded.size_bytes,
			duration_ms = excluded.duration_ms,
			updated_at = CURRENT_TIMESTAMP`,
		ownerKind, ownerID, string(AudioStorageLocalFile), path, mimeType, size, durationMs)
	return err
}

func deleteAudioAsset(ctx context.Context, db execContexter, dialect, ownerKind string, ownerID int64, path string) error {
	if path == "" || ownerID <= 0 {
		return nil
	}
	if dialect == "postgres" {
		_, err := db.ExecContext(ctx, `DELETE FROM audio_assets WHERE owner_kind = $1 AND owner_id = $2 AND path = $3`, ownerKind, ownerID, path)
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM audio_assets WHERE owner_kind = ? AND owner_id = ? AND path = ?`, ownerKind, ownerID, path)
	return err
}

func deleteAudioAssetsForOwner(ctx context.Context, db execContexter, dialect, ownerKind string, ownerID int64) error {
	if ownerID <= 0 {
		return nil
	}
	if dialect == "postgres" {
		_, err := db.ExecContext(ctx, `DELETE FROM audio_assets WHERE owner_kind = $1 AND owner_id = $2`, ownerKind, ownerID)
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM audio_assets WHERE owner_kind = ? AND owner_id = ?`, ownerKind, ownerID)
	return err
}

func quickNoteAudioPaths(ctx context.Context, db queryContexter, dialect string, ownerID int64, fallbackPath string) ([]string, error) {
	if ownerID <= 0 {
		return nil, nil
	}
	const ownerKind = "quick_note"
	query := `SELECT path FROM audio_assets
		WHERE owner_kind = ? AND owner_id = ? AND path != ''
		ORDER BY created_at DESC, id DESC`
	if dialect == "postgres" {
		query = `SELECT path FROM audio_assets
			WHERE owner_kind = $1 AND owner_id = $2 AND path <> ''
			ORDER BY created_at DESC, id DESC`
	}

	rows, err := db.QueryContext(ctx, query, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	seen := make(map[string]struct{})
	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	fallbackPath = strings.TrimSpace(fallbackPath)
	if fallbackPath != "" {
		if _, ok := seen[fallbackPath]; !ok {
			paths = append(paths, fallbackPath)
		}
	}
	return paths, nil
}

func getPrimaryAudioAsset(ctx context.Context, db queryRower, dialect, ownerKind string, ownerID int64) (*AudioAsset, error) {
	ownerKind = strings.TrimSpace(ownerKind)
	if ownerKind == "" || ownerID <= 0 {
		return nil, nil
	}

	query := `SELECT storage_kind, path, mime_type, size_bytes, duration_ms
		FROM audio_assets
		WHERE owner_kind = ? AND owner_id = ? AND path != ''
		ORDER BY created_at DESC, id DESC
		LIMIT 1`
	if dialect == "postgres" {
		query = `SELECT storage_kind, path, mime_type, size_bytes, duration_ms
			FROM audio_assets
			WHERE owner_kind = $1 AND owner_id = $2 AND path <> ''
			ORDER BY created_at DESC, id DESC
			LIMIT 1`
	}

	var (
		asset       AudioAsset
		storageKind string
	)
	err := db.QueryRowContext(ctx, query, ownerKind, ownerID).Scan(
		&storageKind,
		&asset.Path,
		&asset.MimeType,
		&asset.SizeBytes,
		&asset.DurationMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	asset.StorageKind = AudioStorageKind(storageKind)
	return normalizeAudioAsset(asset), nil
}

func getPrimaryAudioAssets(ctx context.Context, db queryContexter, dialect, ownerKind string, ownerIDs []int64) (map[int64]*AudioAsset, error) {
	assets := make(map[int64]*AudioAsset)
	ownerKind = strings.TrimSpace(ownerKind)
	ids := compactPositiveInt64s(ownerIDs)
	if ownerKind == "" || len(ids) == 0 {
		return assets, nil
	}

	placeholders, err := sqlInPlaceholders(dialect, 2, len(ids))
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`SELECT owner_id, storage_kind, path, mime_type, size_bytes, duration_ms
		FROM audio_assets
		WHERE owner_kind = ? AND owner_id IN (%s) AND path != ''
		ORDER BY owner_id ASC, created_at DESC, id DESC`, placeholders) // #nosec G201 -- placeholders are generated from count and fixed dialect.
	args := make([]any, 0, len(ids)+1)
	args = append(args, ownerKind)
	args = appendInt64Args(args, ids)
	if dialect == "postgres" {
		query = fmt.Sprintf(`SELECT owner_id, storage_kind, path, mime_type, size_bytes, duration_ms
			FROM audio_assets
			WHERE owner_kind = $1 AND owner_id IN (%s) AND path <> ''
			ORDER BY owner_id ASC, created_at DESC, id DESC`, placeholders) // #nosec G201 -- placeholders are generated from count and fixed dialect.
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	for rows.Next() {
		var (
			ownerID     int64
			asset       AudioAsset
			storageKind string
		)
		if err := rows.Scan(
			&ownerID,
			&storageKind,
			&asset.Path,
			&asset.MimeType,
			&asset.SizeBytes,
			&asset.DurationMs,
		); err != nil {
			return nil, err
		}
		if _, ok := assets[ownerID]; ok {
			continue
		}
		asset.StorageKind = AudioStorageKind(storageKind)
		if normalized := normalizeAudioAsset(asset); normalized != nil {
			assets[ownerID] = normalized
		}
	}
	return assets, rows.Err()
}

func hydrateTranscriptionAudioBatch(ctx context.Context, db queryContexter, dialect string, transcriptions []Transcription) error {
	ids := make([]int64, 0, len(transcriptions))
	for _, transcription := range transcriptions {
		ids = append(ids, transcription.ID)
	}
	assets, err := getPrimaryAudioAssets(ctx, db, dialect, "transcription", ids)
	if err != nil {
		return err
	}
	for i := range transcriptions {
		if asset := assets[transcriptions[i].ID]; asset != nil {
			transcriptions[i].Audio = asset
			continue
		}
		transcriptions[i].Audio = buildLocalAudioAsset(transcriptions[i].AudioPath, transcriptions[i].DurationMs)
	}
	return nil
}

func hydrateQuickNoteAudioBatch(ctx context.Context, db queryContexter, dialect string, notes []QuickNote) error {
	ids := make([]int64, 0, len(notes))
	for _, note := range notes {
		ids = append(ids, note.ID)
	}
	assets, err := getPrimaryAudioAssets(ctx, db, dialect, "quick_note", ids)
	if err != nil {
		return err
	}
	for i := range notes {
		if asset := assets[notes[i].ID]; asset != nil {
			notes[i].Audio = asset
			continue
		}
		notes[i].Audio = buildLocalAudioAsset(notes[i].AudioPath, notes[i].DurationMs)
	}
	return nil
}

func normalizeAudioAsset(asset AudioAsset) *AudioAsset {
	if strings.TrimSpace(asset.Path) == "" {
		return nil
	}
	if asset.StorageKind == "" {
		asset.StorageKind = AudioStorageLocalFile
	}
	if asset.MimeType == "" {
		asset.MimeType = "audio/wav"
	}
	if asset.StorageKind == AudioStorageLocalFile && asset.SizeBytes == 0 {
		if info, err := os.Stat(asset.Path); err == nil {
			asset.SizeBytes = info.Size()
		}
	}
	return &asset
}
