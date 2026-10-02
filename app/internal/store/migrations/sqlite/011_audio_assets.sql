CREATE TABLE IF NOT EXISTS audio_assets (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_kind   TEXT NOT NULL,
    owner_id     INTEGER NOT NULL,
    storage_kind TEXT NOT NULL DEFAULT 'local-file',
    path         TEXT NOT NULL,
    mime_type    TEXT NOT NULL DEFAULT 'audio/wav',
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(owner_kind, owner_id, path)
);

CREATE INDEX IF NOT EXISTS idx_audio_assets_owner
    ON audio_assets(owner_kind, owner_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_audio_assets_owner_path
    ON audio_assets(owner_kind, owner_id, path);

CREATE INDEX IF NOT EXISTS idx_audio_assets_created_at
    ON audio_assets(created_at, id);

