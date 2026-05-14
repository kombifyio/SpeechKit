CREATE TABLE IF NOT EXISTS storage_scopes (
    id          BIGSERIAL PRIMARY KEY,
    scope_key   TEXT NOT NULL UNIQUE,
    install_id  TEXT NOT NULL DEFAULT '',
    device_id   TEXT NOT NULL DEFAULT '',
    user_id     TEXT NOT NULL DEFAULT '',
    tenant_id   TEXT NOT NULL DEFAULT '',
    labels_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO storage_scopes
    (id, scope_key, install_id, labels_json)
VALUES
    (1, 'install:local', 'local', '{}'::jsonb)
ON CONFLICT(scope_key) DO NOTHING;

ALTER TABLE transcriptions
    ADD COLUMN IF NOT EXISTS scope_id BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id);

ALTER TABLE quick_notes
    ADD COLUMN IF NOT EXISTS scope_id BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id);

ALTER TABLE user_dictionary_entries
    ADD COLUMN IF NOT EXISTS scope_id BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id);

ALTER TABLE voice_agent_sessions
    ADD COLUMN IF NOT EXISTS scope_id BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id);

ALTER TABLE audio_assets
    ADD COLUMN IF NOT EXISTS scope_id BIGINT NOT NULL DEFAULT 1 REFERENCES storage_scopes(id);

CREATE INDEX IF NOT EXISTS idx_transcriptions_scope_created_at
    ON transcriptions(scope_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_quick_notes_scope_created_at
    ON quick_notes(scope_id, pinned DESC, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_scope_created_at
    ON voice_agent_sessions(scope_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audio_assets_scope_created_at
    ON audio_assets(scope_id, created_at, id);

CREATE TABLE IF NOT EXISTS store_stats (
    scope_id                BIGINT PRIMARY KEY REFERENCES storage_scopes(id) ON DELETE CASCADE,
    transcriptions_count    BIGINT NOT NULL DEFAULT 0,
    quick_notes_count       BIGINT NOT NULL DEFAULT 0,
    total_words             BIGINT NOT NULL DEFAULT 0,
    total_audio_duration_ms BIGINT NOT NULL DEFAULT 0,
    total_latency_ms        BIGINT NOT NULL DEFAULT 0,
    latency_count           BIGINT NOT NULL DEFAULT 0,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO store_stats (scope_id)
VALUES (1)
ON CONFLICT(scope_id) DO NOTHING;

INSERT INTO store_stats (scope_id)
SELECT DISTINCT scope_id FROM transcriptions
ON CONFLICT(scope_id) DO NOTHING;

INSERT INTO store_stats (scope_id)
SELECT DISTINCT scope_id FROM quick_notes
ON CONFLICT(scope_id) DO NOTHING;
