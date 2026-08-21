-- The written-up notes for a meeting. Several rows per meeting on purpose:
-- re-running with a different template is a new write-up rather than an
-- overwrite, so the user can go back to the one they liked. The template used
-- is snapshotted with the result, which keeps an old write-up explicable after
-- the template's wording changes.
CREATE TABLE IF NOT EXISTS recording_session_enhancements (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id        INTEGER NOT NULL REFERENCES recording_sessions(id) ON DELETE CASCADE,
    template_slug     TEXT NOT NULL DEFAULT 'default_meeting',
    template_snapshot TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'idle',
    error             TEXT NOT NULL DEFAULT '',
    provider          TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    structured        INTEGER NOT NULL DEFAULT 0,
    content_json      TEXT NOT NULL DEFAULT '',
    content_md        TEXT NOT NULL DEFAULT '',
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_recording_session_enhancements_session
    ON recording_session_enhancements(session_id, created_at DESC, id DESC);
