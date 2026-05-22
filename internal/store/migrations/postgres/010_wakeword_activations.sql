-- v0.37.5: wake-word activation training-data collection.
--
-- Postgres counterpart of internal/store/migrations/sqlite/015_wakeword_activations.sql.
-- Schema follows the same field set; types use Postgres-native
-- BIGSERIAL-less PK because the upload contract carries a client-
-- generated ULID/timestamp ID and the server preserves it.
--
-- Multi-tenant scoping via owner_user_id + owner_org_id mirrors the
-- existing scoped tables (voice_agent_sessions, audio_assets, ...).

CREATE TABLE IF NOT EXISTS wakeword_activations (
    id                 TEXT PRIMARY KEY,
    owner_user_id      TEXT NOT NULL,
    owner_org_id       TEXT NOT NULL,
    phrase_id          TEXT NOT NULL,
    phrase             TEXT NOT NULL DEFAULT '',
    backend            TEXT NOT NULL DEFAULT '',
    score              DOUBLE PRECISION NOT NULL DEFAULT 0,
    captured_at        TIMESTAMPTZ NOT NULL,
    uploaded_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    label              TEXT NOT NULL DEFAULT '',
    audio_path         TEXT NOT NULL,
    audio_bytes        BIGINT NOT NULL DEFAULT 0,
    sample_rate        INTEGER NOT NULL DEFAULT 16000,
    pre_roll_ms        INTEGER NOT NULL DEFAULT 0,
    post_roll_ms       INTEGER NOT NULL DEFAULT 0,
    metadata_json      JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_wakeword_activations_owner
    ON wakeword_activations(owner_org_id, owner_user_id, captured_at DESC);

CREATE INDEX IF NOT EXISTS idx_wakeword_activations_phrase
    ON wakeword_activations(phrase_id, captured_at DESC);

CREATE INDEX IF NOT EXISTS idx_wakeword_activations_label
    ON wakeword_activations(label, captured_at DESC);
