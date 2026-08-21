CREATE TABLE IF NOT EXISTS recording_session_enhancements (
    id                BIGSERIAL PRIMARY KEY,
    session_id        BIGINT NOT NULL REFERENCES recording_sessions(id) ON DELETE CASCADE,
    template_slug     TEXT NOT NULL DEFAULT 'default_meeting',
    template_snapshot TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'idle',
    error             TEXT NOT NULL DEFAULT '',
    provider          TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    structured        BOOLEAN NOT NULL DEFAULT FALSE,
    content_json      TEXT NOT NULL DEFAULT '',
    content_md        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_recording_session_enhancements_session
    ON recording_session_enhancements(session_id, created_at DESC, id DESC);
