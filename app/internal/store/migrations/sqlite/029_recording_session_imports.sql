CREATE TABLE IF NOT EXISTS recording_session_imports (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id          INTEGER NOT NULL REFERENCES recording_sessions(id) ON DELETE CASCADE,
    kind                TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'waiting',
    attempts            INTEGER NOT NULL DEFAULT 0,
    next_attempt_at     DATETIME,
    deadline_at         DATETIME,
    external_meeting_id TEXT NOT NULL DEFAULT '',
    external_item_id    TEXT NOT NULL DEFAULT '',
    subject             TEXT NOT NULL DEFAULT '',
    content_json        TEXT NOT NULL DEFAULT '',
    error_kind          TEXT NOT NULL DEFAULT '',
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (session_id, kind)
);
CREATE INDEX IF NOT EXISTS idx_recording_session_imports_due
    ON recording_session_imports(status, next_attempt_at);
