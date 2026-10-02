package store

import (
	"context"
	"database/sql"
	"strings"
)

func ensureSQLiteLegacyColumnsForCurrentMigrations(ctx context.Context, db *sql.DB) error {
	specs := map[string]map[string]string{
		"transcriptions": {
			"language_base": "TEXT NOT NULL DEFAULT ''",
			"audio_path":    "TEXT NOT NULL DEFAULT ''",
			"owner_user_id": "TEXT NOT NULL DEFAULT ''",
			"owner_org_id":  "TEXT NOT NULL DEFAULT ''",
			"owner_source":  "TEXT NOT NULL DEFAULT ''",
			"speaker_json":  "TEXT NOT NULL DEFAULT ''",
			"pinned":        "INTEGER NOT NULL DEFAULT 0",
		},
		"quick_notes": {
			"language_base": "TEXT NOT NULL DEFAULT ''",
			"audio_path":    "TEXT NOT NULL DEFAULT ''",
		},
		"voice_agent_sessions": {
			"language_base":       "TEXT NOT NULL DEFAULT ''",
			"owner_user_id":       "TEXT NOT NULL DEFAULT ''",
			"owner_org_id":        "TEXT NOT NULL DEFAULT ''",
			"owner_source":        "TEXT NOT NULL DEFAULT ''",
			"raw_summary":         "TEXT NOT NULL DEFAULT ''",
			"transcript":          "TEXT NOT NULL DEFAULT ''",
			"runtime_kind":        "TEXT NOT NULL DEFAULT ''",
			"turns_json":          "TEXT NOT NULL DEFAULT '[]'",
			"ideas_json":          "TEXT NOT NULL DEFAULT '[]'",
			"decisions_json":      "TEXT NOT NULL DEFAULT '[]'",
			"open_questions_json": "TEXT NOT NULL DEFAULT '[]'",
			"next_steps_json":     "TEXT NOT NULL DEFAULT '[]'",
			"started_at":          "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"ended_at":            "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"created_at":          "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"provider_profile_id": "TEXT NOT NULL DEFAULT ''",
		},
		"voice_agent_personas": {
			"description":      "TEXT NOT NULL DEFAULT ''",
			"voice":            "TEXT NOT NULL DEFAULT ''",
			"locale":           "TEXT NOT NULL DEFAULT ''",
			"default_role":     "TEXT NOT NULL DEFAULT ''",
			"default_sequence": "TEXT NOT NULL DEFAULT ''",
			"tags_json":        "TEXT NOT NULL DEFAULT '[]'",
			"metadata_json":    "TEXT NOT NULL DEFAULT '{}'",
			"created_at":       "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"updated_at":       "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
		"voice_agent_roles": {
			"refinement_prompt":              "TEXT NOT NULL DEFAULT ''",
			"locale":                         "TEXT NOT NULL DEFAULT ''",
			"vocabulary_hint":                "TEXT NOT NULL DEFAULT ''",
			"tool_allowlist_json":            "TEXT NOT NULL DEFAULT '[]'",
			"temperature":                    "REAL NOT NULL DEFAULT 0",
			"thinking_enabled":               "INTEGER NOT NULL DEFAULT 0",
			"thinking_level":                 "TEXT NOT NULL DEFAULT ''",
			"include_thoughts":               "INTEGER NOT NULL DEFAULT 0",
			"thinking_budget":                "INTEGER NOT NULL DEFAULT 0",
			"automatic_activity_detection":   "INTEGER NOT NULL DEFAULT 0",
			"vad_start_sensitivity":          "TEXT NOT NULL DEFAULT ''",
			"vad_end_sensitivity":            "TEXT NOT NULL DEFAULT ''",
			"vad_prefix_padding_ms":          "INTEGER NOT NULL DEFAULT 0",
			"vad_silence_duration_ms":        "INTEGER NOT NULL DEFAULT 0",
			"activity_handling":              "TEXT NOT NULL DEFAULT ''",
			"turn_coverage":                  "TEXT NOT NULL DEFAULT ''",
			"context_compression_enabled":    "INTEGER NOT NULL DEFAULT 0",
			"context_compression_trigger_tk": "INTEGER NOT NULL DEFAULT 0",
			"context_compression_target_tk":  "INTEGER NOT NULL DEFAULT 0",
			"enable_affective_dialog":        "INTEGER NOT NULL DEFAULT 0",
			"created_at":                     "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"updated_at":                     "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
		"voice_agent_sequences": {
			"description": "TEXT NOT NULL DEFAULT ''",
			"completion":  "TEXT NOT NULL DEFAULT ''",
			"max_turns":   "INTEGER NOT NULL DEFAULT 0",
			"steps_json":  "TEXT NOT NULL DEFAULT '[]'",
			"created_at":  "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
			"updated_at":  "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
		"audio_assets": {
			"owner_kind": "TEXT NOT NULL DEFAULT ''",
			"owner_id":   "INTEGER NOT NULL DEFAULT 0",
		},
	}
	for table, columns := range specs {
		exists, err := tableExists(ctx, db, "sqlite", table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		for column, definition := range columns {
			if err := ensureSQLiteColumn(ctx, db, table, column, definition); err != nil {
				return err
			}
		}
	}
	return nil
}

func runSQLiteAudioAssetsMigration(ctx context.Context, db *sql.DB) error {
	if err := ensureSQLiteColumn(ctx, db, "audio_assets", "owner_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "audio_assets", "owner_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, sqliteMigration011); err != nil {
		return err
	}
	return backfillSQLiteAudioAssets(ctx, db)
}

func runSQLiteMeetingEnhancementJobsMigration(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"stage", "TEXT NOT NULL DEFAULT ''"},
		{"progress", "INTEGER NOT NULL DEFAULT 0"},
		{"attempt", "INTEGER NOT NULL DEFAULT 1"},
		{"input_fingerprint", "TEXT NOT NULL DEFAULT ''"},
		{"error_kind", "TEXT NOT NULL DEFAULT ''"},
		{"retryable", "INTEGER NOT NULL DEFAULT 0"},
		{"consent_version", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		if err := ensureSQLiteColumn(ctx, db, "recording_session_enhancements", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func runSQLiteStorageScopesMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, sqliteMigration013); err != nil {
		return err
	}
	if err := ensureSQLiteScopedStoreStats(ctx, db); err != nil {
		return err
	}
	for _, table := range []string{
		"transcriptions",
		"quick_notes",
		"user_dictionary_entries",
		"voice_agent_sessions",
		"audio_assets",
	} {
		if err := ensureSQLiteColumn(ctx, db, table, "scope_id", "INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id)"); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO store_stats (scope_id) VALUES (1)`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO store_stats (scope_id)
		SELECT DISTINCT scope_id FROM transcriptions
		UNION
		SELECT DISTINCT scope_id FROM quick_notes`)
	return err
}

func ensureSQLiteScopedStoreStats(ctx context.Context, db *sql.DB) error {
	hasScopeID, err := sqliteColumnExists(ctx, db, "store_stats", "scope_id")
	if err != nil {
		return err
	}
	if hasScopeID {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE store_stats_scoped (
			scope_id                INTEGER PRIMARY KEY REFERENCES storage_scopes(id) ON DELETE CASCADE,
			transcriptions_count    INTEGER NOT NULL DEFAULT 0,
			quick_notes_count       INTEGER NOT NULL DEFAULT 0,
			total_words             INTEGER NOT NULL DEFAULT 0,
			total_audio_duration_ms INTEGER NOT NULL DEFAULT 0,
			total_latency_ms        INTEGER NOT NULL DEFAULT 0,
			latency_count           INTEGER NOT NULL DEFAULT 0,
			updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT OR IGNORE INTO store_stats_scoped
			(scope_id, transcriptions_count, quick_notes_count, total_words, total_audio_duration_ms, total_latency_ms, latency_count, updated_at)
		SELECT 1, transcriptions_count, quick_notes_count, total_words, total_audio_duration_ms, total_latency_ms, latency_count, updated_at
		FROM store_stats;
		DROP TABLE store_stats;
		ALTER TABLE store_stats_scoped RENAME TO store_stats;
	`); err != nil {
		return err
	}
	return tx.Commit()
}

func runSQLiteStorage3ModelMigration(ctx context.Context, db *sql.DB) error {
	for _, table := range []string{"transcriptions", "voice_agent_sessions"} {
		if err := ensureSQLiteColumn(ctx, db, table, "owner_user_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		if err := ensureSQLiteColumn(ctx, db, table, "owner_org_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		if err := ensureSQLiteColumn(ctx, db, table, "owner_source", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	for _, table := range []string{"transcriptions", "quick_notes", "voice_agent_sessions"} {
		if err := ensureSQLiteColumn(ctx, db, table, "language_base", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	needsDictionaryRebuild, err := sqliteUserDictionaryNeedsScopedRebuild(ctx, db)
	if err != nil {
		return err
	}
	if needsDictionaryRebuild {
		if err := rebuildSQLiteUserDictionaryForScopes(ctx, db); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, sqliteMigration014); err != nil {
		return err
	}
	return refreshAllSQLiteStoreStats(ctx, db)
}

func runSQLiteRecordingSessionStateMigration(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{name: "capture_status", definition: "TEXT NOT NULL DEFAULT 'idle'"},
		{name: "capture_started_at", definition: "DATETIME"},
		{name: "capture_paused_at", definition: "DATETIME"},
		{name: "capture_stopped_at", definition: "DATETIME"},
		{name: "summary_status", definition: "TEXT NOT NULL DEFAULT 'idle'"},
		{name: "summary_error", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "summary_updated_at", definition: "DATETIME"},
	}
	for _, column := range columns {
		if err := ensureSQLiteColumn(ctx, db, "recording_sessions", column.name, column.definition); err != nil {
			return err
		}
	}
	_, err := db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_recording_sessions_scope_capture
    ON recording_sessions(scope_id, capture_status, updated_at DESC, id DESC);
`)
	return err
}

// runSQLiteRecordingSessionChannelsMigration adds the capture-channel and
// speaker columns meeting capture needs to keep the microphone and system
// loopback streams apart, plus the wall-clock timeline index the meeting views
// order by.
func runSQLiteRecordingSessionChannelsMigration(ctx context.Context, db *sql.DB) error {
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "channel", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "speaker", definition: "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureSQLiteColumn(ctx, db, "recording_session_segments", column.name, column.definition); err != nil {
			return err
		}
	}
	_, err := db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_recording_session_segments_timeline
    ON recording_session_segments(session_id, started_ms, id);
`)
	return err
}

func rebuildSQLiteUserDictionaryForScopes(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if _, err := tx.ExecContext(ctx, `
		DROP TABLE IF EXISTS user_dictionary_entries_v3;
		CREATE TABLE user_dictionary_entries_v3 (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			scope_id    INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id),
			spoken      TEXT NOT NULL,
			canonical   TEXT NOT NULL,
			language    TEXT NOT NULL DEFAULT '',
			source      TEXT NOT NULL DEFAULT 'settings',
			enabled     INTEGER NOT NULL DEFAULT 1,
			usage_count INTEGER NOT NULL DEFAULT 0,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(scope_id, spoken, canonical, language, source)
		);
		INSERT OR IGNORE INTO user_dictionary_entries_v3
			(id, scope_id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at)
		SELECT id, COALESCE(scope_id, 1), spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		FROM user_dictionary_entries;
		DROP TABLE user_dictionary_entries;
		ALTER TABLE user_dictionary_entries_v3 RENAME TO user_dictionary_entries;
		CREATE INDEX IF NOT EXISTS idx_user_dictionary_language
			ON user_dictionary_entries(scope_id, language, enabled, id);
		CREATE INDEX IF NOT EXISTS idx_user_dictionary_canonical_lookup
			ON user_dictionary_entries(scope_id, lower(canonical), language, enabled);
	`); err != nil {
		return err
	}
	return tx.Commit()
}

func sqliteUserDictionaryNeedsScopedRebuild(ctx context.Context, db *sql.DB) (bool, error) {
	var createSQL string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'table' AND name = 'user_dictionary_entries'`).Scan(&createSQL); err != nil {
		return false, err
	}
	normalized := strings.ToLower(createSQL)
	normalized = strings.NewReplacer("\r", "", "\n", "", "\t", "", " ", "").Replace(normalized)
	return !strings.Contains(normalized, "unique(scope_id,spoken,canonical,language,source)"), nil
}
