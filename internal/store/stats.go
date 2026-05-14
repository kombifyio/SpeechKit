package store

import (
	"context"
	"database/sql"
)

func refreshSQLiteStoreStats(ctx context.Context, db execContexter, scopeID int64) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO store_stats (
			scope_id,
			transcriptions_count,
			quick_notes_count,
			total_words,
			total_audio_duration_ms,
			total_latency_ms,
			latency_count,
			updated_at
		)
		VALUES (
			?,
			(SELECT COUNT(*) FROM transcriptions WHERE scope_id = ?),
			(SELECT COUNT(*) FROM quick_notes WHERE scope_id = ?),
			(SELECT COALESCE(SUM(word_count), 0) FROM (
				SELECT word_count FROM transcriptions WHERE scope_id = ?
				UNION ALL
				SELECT word_count FROM quick_notes WHERE scope_id = ?
			)),
			(SELECT COALESCE(SUM(duration_ms), 0) FROM (
				SELECT duration_ms FROM transcriptions WHERE scope_id = ?
				UNION ALL
				SELECT duration_ms FROM quick_notes WHERE scope_id = ?
			)),
			(SELECT COALESCE(SUM(latency_ms), 0) FROM (
				SELECT latency_ms FROM transcriptions WHERE scope_id = ? AND latency_ms > 0
				UNION ALL
				SELECT latency_ms FROM quick_notes WHERE scope_id = ? AND latency_ms > 0
			)),
			(SELECT COUNT(*) FROM (
				SELECT latency_ms FROM transcriptions WHERE scope_id = ? AND latency_ms > 0
				UNION ALL
				SELECT latency_ms FROM quick_notes WHERE scope_id = ? AND latency_ms > 0
			)),
			CURRENT_TIMESTAMP
		)
		ON CONFLICT(scope_id) DO UPDATE SET
			transcriptions_count = excluded.transcriptions_count,
			quick_notes_count = excluded.quick_notes_count,
			total_words = excluded.total_words,
			total_audio_duration_ms = excluded.total_audio_duration_ms,
			total_latency_ms = excluded.total_latency_ms,
			latency_count = excluded.latency_count,
			updated_at = CURRENT_TIMESTAMP`,
		scopeID,
		scopeID,
		scopeID,
		scopeID,
		scopeID,
		scopeID,
		scopeID,
		scopeID,
		scopeID,
		scopeID,
		scopeID,
	)
	return err
}

func refreshAllSQLiteStoreStats(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id FROM storage_scopes ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable
	for rows.Next() {
		var scopeID int64
		if err := rows.Scan(&scopeID); err != nil {
			return err
		}
		if err := refreshSQLiteStoreStats(ctx, db, scopeID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func refreshPostgresStoreStats(ctx context.Context, db execContexter, scopeID int64) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO store_stats (
			scope_id,
			transcriptions_count,
			quick_notes_count,
			total_words,
			total_audio_duration_ms,
			total_latency_ms,
			latency_count,
			updated_at
		)
		VALUES (
			$1,
			(SELECT COUNT(*) FROM transcriptions WHERE scope_id = $1),
			(SELECT COUNT(*) FROM quick_notes WHERE scope_id = $1),
			(SELECT COALESCE(SUM(word_count), 0) FROM (
				SELECT word_count FROM transcriptions WHERE scope_id = $1
				UNION ALL
				SELECT word_count FROM quick_notes WHERE scope_id = $1
			) AS words),
			(SELECT COALESCE(SUM(duration_ms), 0) FROM (
				SELECT duration_ms FROM transcriptions WHERE scope_id = $1
				UNION ALL
				SELECT duration_ms FROM quick_notes WHERE scope_id = $1
			) AS durations),
			(SELECT COALESCE(SUM(latency_ms), 0) FROM (
				SELECT latency_ms FROM transcriptions WHERE scope_id = $1 AND latency_ms > 0
				UNION ALL
				SELECT latency_ms FROM quick_notes WHERE scope_id = $1 AND latency_ms > 0
			) AS latencies),
			(SELECT COUNT(*) FROM (
				SELECT latency_ms FROM transcriptions WHERE scope_id = $1 AND latency_ms > 0
				UNION ALL
				SELECT latency_ms FROM quick_notes WHERE scope_id = $1 AND latency_ms > 0
			) AS latency_rows),
			NOW()
		)
		ON CONFLICT(scope_id) DO UPDATE SET
			transcriptions_count = excluded.transcriptions_count,
			quick_notes_count = excluded.quick_notes_count,
			total_words = excluded.total_words,
			total_audio_duration_ms = excluded.total_audio_duration_ms,
			total_latency_ms = excluded.total_latency_ms,
			latency_count = excluded.latency_count,
			updated_at = NOW()`,
		scopeID,
	)
	return err
}

func refreshAllPostgresStoreStats(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id FROM storage_scopes ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable
	for rows.Next() {
		var scopeID int64
		if err := rows.Scan(&scopeID); err != nil {
			return err
		}
		if err := refreshPostgresStoreStats(ctx, db, scopeID); err != nil {
			return err
		}
	}
	return rows.Err()
}
