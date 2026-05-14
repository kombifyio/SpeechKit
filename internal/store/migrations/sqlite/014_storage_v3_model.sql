CREATE TABLE IF NOT EXISTS transcription_audio_assets (
    transcription_id INTEGER NOT NULL REFERENCES transcriptions(id) ON DELETE CASCADE,
    audio_asset_id   INTEGER NOT NULL REFERENCES audio_assets(id) ON DELETE CASCADE,
    role             TEXT NOT NULL DEFAULT 'source',
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(transcription_id, audio_asset_id)
);

CREATE TABLE IF NOT EXISTS quick_note_audio_assets (
    quick_note_id   INTEGER NOT NULL REFERENCES quick_notes(id) ON DELETE CASCADE,
    audio_asset_id  INTEGER NOT NULL REFERENCES audio_assets(id) ON DELETE CASCADE,
    role            TEXT NOT NULL DEFAULT 'source',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(quick_note_id, audio_asset_id)
);

INSERT OR IGNORE INTO transcription_audio_assets (transcription_id, audio_asset_id, role)
SELECT owner_id, id, 'source'
FROM audio_assets
WHERE owner_kind = 'transcription' AND owner_id > 0;

INSERT OR IGNORE INTO quick_note_audio_assets (quick_note_id, audio_asset_id, role)
SELECT owner_id, id, 'source'
FROM audio_assets
WHERE owner_kind = 'quick_note' AND owner_id > 0;

UPDATE transcriptions
SET language_base = lower(CASE
    WHEN instr(language, '-') > 0 THEN substr(language, 1, instr(language, '-') - 1)
    WHEN instr(language, '_') > 0 THEN substr(language, 1, instr(language, '_') - 1)
    ELSE language
END)
WHERE language_base = '' AND COALESCE(language, '') != '';

UPDATE quick_notes
SET language_base = lower(CASE
    WHEN instr(language, '-') > 0 THEN substr(language, 1, instr(language, '-') - 1)
    WHEN instr(language, '_') > 0 THEN substr(language, 1, instr(language, '_') - 1)
    ELSE language
END)
WHERE language_base = '' AND COALESCE(language, '') != '';

UPDATE voice_agent_sessions
SET language_base = lower(CASE
    WHEN instr(language, '-') > 0 THEN substr(language, 1, instr(language, '-') - 1)
    WHEN instr(language, '_') > 0 THEN substr(language, 1, instr(language, '_') - 1)
    ELSE language
END)
WHERE language_base = '' AND COALESCE(language, '') != '';

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
