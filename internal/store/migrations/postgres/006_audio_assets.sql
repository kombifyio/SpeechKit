CREATE TABLE IF NOT EXISTS audio_assets (
    id           BIGSERIAL PRIMARY KEY,
    owner_kind   TEXT NOT NULL,
    owner_id     BIGINT NOT NULL,
    storage_kind TEXT NOT NULL DEFAULT 'local-file',
    path         TEXT NOT NULL,
    mime_type    TEXT NOT NULL DEFAULT 'audio/wav',
    size_bytes   BIGINT NOT NULL DEFAULT 0,
    duration_ms  BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(owner_kind, owner_id, path)
);

CREATE INDEX IF NOT EXISTS idx_audio_assets_owner
    ON audio_assets(owner_kind, owner_id);

CREATE INDEX IF NOT EXISTS idx_audio_assets_created_at
    ON audio_assets(created_at, id);

