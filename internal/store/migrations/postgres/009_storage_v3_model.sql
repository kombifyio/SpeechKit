ALTER TABLE transcriptions
    ADD COLUMN IF NOT EXISTS language_base TEXT NOT NULL DEFAULT '';

ALTER TABLE quick_notes
    ADD COLUMN IF NOT EXISTS language_base TEXT NOT NULL DEFAULT '';

ALTER TABLE voice_agent_sessions
    ADD COLUMN IF NOT EXISTS language_base TEXT NOT NULL DEFAULT '';

ALTER TABLE user_dictionary_entries
    DROP CONSTRAINT IF EXISTS user_dictionary_entries_spoken_canonical_language_source_key;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'user_dictionary_entries_scope_spoken_canonical_language_source_key'
    ) THEN
        ALTER TABLE user_dictionary_entries
            ADD CONSTRAINT user_dictionary_entries_scope_spoken_canonical_language_source_key
            UNIQUE(scope_id, spoken, canonical, language, source);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS transcription_audio_assets (
    transcription_id BIGINT NOT NULL REFERENCES transcriptions(id) ON DELETE CASCADE,
    audio_asset_id   BIGINT NOT NULL REFERENCES audio_assets(id) ON DELETE CASCADE,
    role             TEXT NOT NULL DEFAULT 'source',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(transcription_id, audio_asset_id)
);

CREATE TABLE IF NOT EXISTS quick_note_audio_assets (
    quick_note_id   BIGINT NOT NULL REFERENCES quick_notes(id) ON DELETE CASCADE,
    audio_asset_id  BIGINT NOT NULL REFERENCES audio_assets(id) ON DELETE CASCADE,
    role            TEXT NOT NULL DEFAULT 'source',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(quick_note_id, audio_asset_id)
);

INSERT INTO transcription_audio_assets (transcription_id, audio_asset_id, role)
SELECT owner_id, id, 'source'
FROM audio_assets
WHERE owner_kind = 'transcription' AND owner_id > 0
ON CONFLICT(transcription_id, audio_asset_id) DO NOTHING;

INSERT INTO quick_note_audio_assets (quick_note_id, audio_asset_id, role)
SELECT owner_id, id, 'source'
FROM audio_assets
WHERE owner_kind = 'quick_note' AND owner_id > 0
ON CONFLICT(quick_note_id, audio_asset_id) DO NOTHING;

UPDATE transcriptions
SET language_base = lower(split_part(replace(language, '_', '-'), '-', 1))
WHERE language_base = '' AND COALESCE(language, '') <> '';

UPDATE quick_notes
SET language_base = lower(split_part(replace(language, '_', '-'), '-', 1))
WHERE language_base = '' AND COALESCE(language, '') <> '';

UPDATE voice_agent_sessions
SET language_base = lower(split_part(replace(language, '_', '-'), '-', 1))
WHERE language_base = '' AND COALESCE(language, '') <> '';

CREATE INDEX IF NOT EXISTS idx_transcriptions_scope_language_created
    ON transcriptions(scope_id, language_base, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_quick_notes_scope_language_created
    ON quick_notes(scope_id, language_base, pinned DESC, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_scope_created
    ON voice_agent_sessions(scope_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audio_assets_scope_created
    ON audio_assets(scope_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_transcription_audio_assets_asset
    ON transcription_audio_assets(audio_asset_id);

CREATE INDEX IF NOT EXISTS idx_quick_note_audio_assets_asset
    ON quick_note_audio_assets(audio_asset_id);

CREATE INDEX IF NOT EXISTS idx_user_dictionary_language
    ON user_dictionary_entries(scope_id, language, enabled, id);

CREATE INDEX IF NOT EXISTS idx_user_dictionary_canonical_lookup
    ON user_dictionary_entries(scope_id, lower(canonical), language, enabled);
