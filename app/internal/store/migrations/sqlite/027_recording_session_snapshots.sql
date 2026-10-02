CREATE TABLE IF NOT EXISTS recording_session_snapshots (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  INTEGER NOT NULL REFERENCES recording_sessions(id) ON DELETE CASCADE,
    captured_ms INTEGER NOT NULL DEFAULT 0,
    path        TEXT NOT NULL,
    mime_type   TEXT NOT NULL DEFAULT 'image/png',
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    width       INTEGER NOT NULL DEFAULT 0,
    height      INTEGER NOT NULL DEFAULT 0,
    monitor     TEXT NOT NULL DEFAULT '',
    note        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_recording_session_snapshots_session
    ON recording_session_snapshots(session_id, captured_ms);
