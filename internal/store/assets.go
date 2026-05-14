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

type queryRowContexter interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type dbContexter interface {
	execContexter
	queryRowContexter
}

func sqliteTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func recordAudioAsset(ctx context.Context, db execContexter, dialect, ownerKind string, ownerID int64, path string, durationMs int64) error {
	dbq, ok := db.(dbContexter)
	if !ok {
		return fmt.Errorf("record audio asset: database handle does not support QueryRowContext")
	}
	return recordScopedAudioAsset(ctx, dbq, dialect, 1, ownerKind, ownerID, path, durationMs)
}

func recordScopedAudioAsset(ctx context.Context, db dbContexter, dialect string, scopeID int64, ownerKind string, ownerID int64, path string, durationMs int64) error {
	if path == "" || ownerID <= 0 {
		return nil
	}
	var size int64
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	var assetID int64
	if dialect == "postgres" {
		err := db.QueryRowContext(ctx, `INSERT INTO audio_assets
			(scope_id, owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT(owner_kind, owner_id, path) DO UPDATE SET
				scope_id = excluded.scope_id,
				storage_kind = excluded.storage_kind,
				mime_type = excluded.mime_type,
				size_bytes = excluded.size_bytes,
				duration_ms = excluded.duration_ms,
				updated_at = NOW()
			RETURNING id`,
			scopeID, ownerKind, ownerID, string(AudioStorageLocalFile), path, mimeTypeForAudioPath(path), size, durationMs).Scan(&assetID)
		if err != nil {
			return err
		}
		return linkAudioAsset(ctx, db, dialect, ownerKind, ownerID, assetID)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audio_assets
		(scope_id, owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(owner_kind, owner_id, path) DO UPDATE SET
			scope_id = excluded.scope_id,
			storage_kind = excluded.storage_kind,
			mime_type = excluded.mime_type,
			size_bytes = excluded.size_bytes,
			duration_ms = excluded.duration_ms,
			updated_at = CURRENT_TIMESTAMP`,
		scopeID, ownerKind, ownerID, string(AudioStorageLocalFile), path, mimeTypeForAudioPath(path), size, durationMs); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT id FROM audio_assets WHERE owner_kind = ? AND owner_id = ? AND path = ?`,
		ownerKind, ownerID, path).Scan(&assetID); err != nil {
		return err
	}
	return linkAudioAsset(ctx, db, dialect, ownerKind, ownerID, assetID)
}

func (s *SQLiteStore) GetAudioAsset(ctx context.Context, ownerKind string, ownerID int64) (*AudioAsset, error) {
	return getAudioAsset(ctx, s.db, "sqlite", ownerKind, ownerID)
}

func (s *PostgresStore) GetAudioAsset(ctx context.Context, ownerKind string, ownerID int64) (*AudioAsset, error) {
	return getAudioAsset(ctx, s.db, "postgres", ownerKind, ownerID)
}

func getAudioAsset(ctx context.Context, db queryRowContexter, dialect, ownerKind string, ownerID int64) (*AudioAsset, error) {
	if strings.TrimSpace(ownerKind) == "" || ownerID <= 0 {
		return nil, nil
	}
	query := `SELECT storage_kind, path, mime_type, size_bytes, duration_ms
		FROM audio_assets
		WHERE owner_kind = ? AND owner_id = ? AND path != ''
		ORDER BY created_at DESC, id DESC
		LIMIT 1`
	args := []any{ownerKind, ownerID}
	if dialect == "postgres" {
		query = `SELECT storage_kind, path, mime_type, size_bytes, duration_ms
			FROM audio_assets
			WHERE owner_kind = $1 AND owner_id = $2 AND path <> ''
			ORDER BY created_at DESC, id DESC
			LIMIT 1`
	}
	var storageKind, path, mimeType string
	var sizeBytes, durationMs int64
	err := db.QueryRowContext(ctx, query, args...).Scan(&storageKind, &path, &mimeType, &sizeBytes, &durationMs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return buildAudioAsset(storageKind, path, mimeType, sizeBytes, durationMs), nil
}

func linkAudioAsset(ctx context.Context, db execContexter, dialect, ownerKind string, ownerID, assetID int64) error {
	if ownerID <= 0 || assetID <= 0 {
		return nil
	}
	switch ownerKind {
	case "transcription":
		if dialect == "postgres" {
			if _, err := db.ExecContext(ctx, `DELETE FROM transcription_audio_assets WHERE transcription_id = $1 AND role = 'source'`, ownerID); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, `INSERT INTO transcription_audio_assets (transcription_id, audio_asset_id, role)
				VALUES ($1, $2, 'source')
				ON CONFLICT(transcription_id, audio_asset_id) DO NOTHING`, ownerID, assetID)
			return err
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM transcription_audio_assets WHERE transcription_id = ? AND role = 'source'`, ownerID); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO transcription_audio_assets (transcription_id, audio_asset_id, role)
			VALUES (?, ?, 'source')`, ownerID, assetID)
		return err
	case "quick_note":
		if dialect == "postgres" {
			if _, err := db.ExecContext(ctx, `DELETE FROM quick_note_audio_assets WHERE quick_note_id = $1 AND role = 'source'`, ownerID); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx, `INSERT INTO quick_note_audio_assets (quick_note_id, audio_asset_id, role)
				VALUES ($1, $2, 'source')
				ON CONFLICT(quick_note_id, audio_asset_id) DO NOTHING`, ownerID, assetID)
			return err
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM quick_note_audio_assets WHERE quick_note_id = ? AND role = 'source'`, ownerID); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO quick_note_audio_assets (quick_note_id, audio_asset_id, role)
			VALUES (?, ?, 'source')`, ownerID, assetID)
		return err
	default:
		return nil
	}
}

func deleteAudioAsset(ctx context.Context, db execContexter, dialect, ownerKind string, ownerID int64, path string) error {
	if path == "" || ownerID <= 0 {
		return nil
	}
	if dialect == "postgres" {
		if _, err := db.ExecContext(ctx, `DELETE FROM transcription_audio_assets
			WHERE audio_asset_id IN (SELECT id FROM audio_assets WHERE owner_kind = $1 AND owner_id = $2 AND path = $3)`,
			ownerKind, ownerID, path); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM quick_note_audio_assets
			WHERE audio_asset_id IN (SELECT id FROM audio_assets WHERE owner_kind = $1 AND owner_id = $2 AND path = $3)`,
			ownerKind, ownerID, path); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `DELETE FROM audio_assets WHERE owner_kind = $1 AND owner_id = $2 AND path = $3`, ownerKind, ownerID, path)
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM transcription_audio_assets
		WHERE audio_asset_id IN (SELECT id FROM audio_assets WHERE owner_kind = ? AND owner_id = ? AND path = ?)`,
		ownerKind, ownerID, path); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM quick_note_audio_assets
		WHERE audio_asset_id IN (SELECT id FROM audio_assets WHERE owner_kind = ? AND owner_id = ? AND path = ?)`,
		ownerKind, ownerID, path); err != nil {
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
		if _, err := db.ExecContext(ctx, `DELETE FROM transcription_audio_assets
			WHERE audio_asset_id IN (SELECT id FROM audio_assets WHERE owner_kind = $1 AND owner_id = $2)`,
			ownerKind, ownerID); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM quick_note_audio_assets
			WHERE audio_asset_id IN (SELECT id FROM audio_assets WHERE owner_kind = $1 AND owner_id = $2)`,
			ownerKind, ownerID); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `DELETE FROM audio_assets WHERE owner_kind = $1 AND owner_id = $2`, ownerKind, ownerID)
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM transcription_audio_assets
		WHERE audio_asset_id IN (SELECT id FROM audio_assets WHERE owner_kind = ? AND owner_id = ?)`,
		ownerKind, ownerID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM quick_note_audio_assets
		WHERE audio_asset_id IN (SELECT id FROM audio_assets WHERE owner_kind = ? AND owner_id = ?)`,
		ownerKind, ownerID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM audio_assets WHERE owner_kind = ? AND owner_id = ?`, ownerKind, ownerID)
	return err
}
