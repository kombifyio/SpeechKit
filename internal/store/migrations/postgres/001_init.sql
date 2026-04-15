CREATE TABLE IF NOT EXISTS transcriptions (
    id          BIGSERIAL PRIMARY KEY,
    text        TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT 'de',
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    latency_ms  BIGINT NOT NULL DEFAULT 0,
    audio_path  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transcriptions_created_at
    ON transcriptions(created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS quick_notes (
    id          BIGSERIAL PRIMARY KEY,
    text        TEXT NOT NULL,
    language    TEXT NOT NULL DEFAULT 'de',
    provider    TEXT NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    latency_ms  BIGINT NOT NULL DEFAULT 0,
    audio_path  TEXT NOT NULL DEFAULT '',
    pinned      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_quick_notes_created_at
    ON quick_notes(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_quick_notes_pinned
    ON quick_notes(pinned DESC, created_at DESC, id DESC);
