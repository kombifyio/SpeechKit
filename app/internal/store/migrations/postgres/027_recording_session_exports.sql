CREATE TABLE IF NOT EXISTS recording_session_exports (
    id              BIGSERIAL PRIMARY KEY,
    session_id      BIGINT NOT NULL REFERENCES recording_sessions(id) ON DELETE CASCADE,
    target_id       TEXT NOT NULL,
    destination_id  TEXT NOT NULL DEFAULT '',
    external_id     TEXT NOT NULL DEFAULT '',
    url             TEXT NOT NULL DEFAULT '',
    fingerprint     TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
    last_error_kind TEXT NOT NULL DEFAULT '',
    pushed_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (session_id, target_id)
);
