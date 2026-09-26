package store

import (
	"context"
	"database/sql"
)

func runPostgresAudioAssetsMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
ALTER TABLE transcriptions
	ADD COLUMN IF NOT EXISTS audio_path TEXT NOT NULL DEFAULT '';
ALTER TABLE quick_notes
	ADD COLUMN IF NOT EXISTS audio_path TEXT NOT NULL DEFAULT '';
ALTER TABLE audio_assets
	ADD COLUMN IF NOT EXISTS owner_kind TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_id BIGINT NOT NULL DEFAULT 0;
`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, postgresMigration006); err != nil {
		return err
	}
	return backfillPostgresAudioAssets(ctx, db)
}

func runPostgresVoiceAgentSessionsMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, postgresMigration002); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
ALTER TABLE voice_agent_sessions
	ADD COLUMN IF NOT EXISTS turns_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS ideas_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS decisions_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS open_questions_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS next_steps_json JSONB NOT NULL DEFAULT '[]'::jsonb;
`)
	return err
}

func runPostgresStorage3ModelMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
ALTER TABLE transcriptions
	ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_org_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_source TEXT NOT NULL DEFAULT '';
ALTER TABLE voice_agent_sessions
	ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_org_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS owner_source TEXT NOT NULL DEFAULT '';
`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, postgresMigration009); err != nil {
		return err
	}
	return refreshAllPostgresStoreStats(ctx, db)
}
