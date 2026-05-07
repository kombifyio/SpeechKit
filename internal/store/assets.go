package store

import (
	"context"
	"database/sql"
	"os"
	"time"
)

type execContexter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func sqliteTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
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
