CREATE TABLE IF NOT EXISTS recording_session_notes (
    id          BIGSERIAL PRIMARY KEY,
    session_id  BIGINT NOT NULL UNIQUE REFERENCES recording_sessions(id) ON DELETE CASCADE,
    content_md  TEXT NOT NULL DEFAULT '',
    blocks_json TEXT NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
