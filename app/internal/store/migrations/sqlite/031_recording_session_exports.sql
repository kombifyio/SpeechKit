CREATE TABLE IF NOT EXISTS recording_session_exports (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id      INTEGER NOT NULL REFERENCES recording_sessions(id) ON DELETE CASCADE,
    target_id       TEXT NOT NULL,
    destination_id  TEXT NOT NULL DEFAULT '',
    external_id     TEXT NOT NULL DEFAULT '',
    url             TEXT NOT NULL DEFAULT '',
    fingerprint     TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
    last_error_kind TEXT NOT NULL DEFAULT '',
    pushed_at       DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (session_id, target_id)
);
