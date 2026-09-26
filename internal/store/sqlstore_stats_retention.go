package store

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// ── Stats ────────────────────────────────────────────────────────────────────

func (s *sqlStore) Stats(ctx context.Context) (Stats, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return Stats{}, err
	}
	var stats Stats
	var totalLatency, latencyCount int64
	err = s.db.QueryRowContext(ctx, s.dialect.rebind(`SELECT transcriptions_count, quick_notes_count, total_words,
		total_audio_duration_ms, total_latency_ms, latency_count
		FROM store_stats WHERE scope_id = ?`), scopeID).
		Scan(&stats.Transcriptions, &stats.QuickNotes, &stats.TotalWords, &stats.TotalAudioDurationMs, &totalLatency, &latencyCount)
	if err != nil {
		return Stats{}, err
	}
	if stats.TotalAudioDurationMs > 0 {
		stats.AverageWordsPerMinute = float64(stats.TotalWords) / (float64(stats.TotalAudioDurationMs) / float64(time.Minute/time.Millisecond))
	}
	if latencyCount > 0 {
		stats.AverageLatencyMs = totalLatency / latencyCount
	}
	return stats, nil
}

// ── Maintenance ──────────────────────────────────────────────────────────────

func (s *sqlStore) enforceStorageLimit() {
	s.enforceAudioRetention()

	entries, err := os.ReadDir(s.audioDir)
	if err != nil {
		return
	}
	var totalSize int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		totalSize += info.Size()
	}
	limitBytes := int64(s.maxStorageMB) * 1024 * 1024
	if totalSize <= limitBytes {
		return
	}

	tx, err := s.db.BeginTx(context.Background(), nil) //nolint:contextcheck // background goroutine should not be bound to request context
	if err != nil {
		slog.Warn("store: begin cleanup tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, error not actionable

	rows, err := tx.QueryContext(context.Background(), //nolint:contextcheck // background goroutine should not be bound to request context
		`SELECT owner_kind AS kind, owner_id AS id, path
		 FROM audio_assets
		 WHERE path <> ''
		 ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	for rows.Next() && totalSize > limitBytes {
		var kind, path string
		var id int64
		if err := rows.Scan(&kind, &id, &path); err != nil {
			slog.Warn("store: scan cleanup row", "err", err)
			continue
		}
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			slog.Warn("store: remove audio", "path", path, "err", err)
			continue
		}
		totalSize -= info.Size()
		if err := deleteAudioAsset(context.Background(), tx, s.dialect.name, kind, id, path); err != nil { //nolint:contextcheck // background goroutine should not be bound to request context
			slog.Warn("store: delete audio asset", "kind", kind, "id", id, "err", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: cleanup rows error", "err", err)
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit cleanup tx", "err", err)
	}
}

func (s *sqlStore) enforceAudioRetention() {
	if s.audioRetentionDays <= 0 {
		return
	}
	cutoff := s.dialect.timeArg(time.Now().Add(-time.Duration(s.audioRetentionDays) * 24 * time.Hour))

	tx, err := s.db.BeginTx(context.Background(), nil) //nolint:contextcheck // background goroutine should not be bound to request context
	if err != nil {
		slog.Warn("store: begin retention tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, error not actionable

	rows, err := tx.QueryContext(context.Background(), //nolint:contextcheck // background goroutine should not be bound to request context
		s.dialect.rebind(`SELECT owner_kind AS kind, owner_id AS id, path
		 FROM audio_assets
		 WHERE path <> '' AND created_at < ?
		 ORDER BY created_at ASC, id ASC`),
		cutoff,
	)
	if err != nil {
		slog.Warn("store: query retention rows", "err", err)
		return
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	for rows.Next() {
		var kind, path string
		var id int64
		if err := rows.Scan(&kind, &id, &path); err != nil {
			slog.Warn("store: scan retention row", "err", err)
			continue
		}
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("store: remove retained audio", "path", path, "err", err)
			continue
		}
		if err := deleteAudioAsset(context.Background(), tx, s.dialect.name, kind, id, path); err != nil { //nolint:contextcheck // background goroutine should not be bound to request context
			slog.Warn("store: delete retained audio asset", "kind", kind, "id", id, "err", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: retention rows error", "err", err)
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit retention tx", "err", err)
	}
}
