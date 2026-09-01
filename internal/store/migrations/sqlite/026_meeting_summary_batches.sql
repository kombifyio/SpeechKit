CREATE TABLE IF NOT EXISTS meeting_summary_batches (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id         INTEGER NOT NULL REFERENCES recording_sessions(id) ON DELETE CASCADE,
    batch_key          TEXT NOT NULL UNIQUE,
    level              INTEGER NOT NULL DEFAULT 0,
    start_segment_id   INTEGER NOT NULL,
    end_segment_id     INTEGER NOT NULL,
    source_fingerprint TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'sealed',
    digest_json        TEXT NOT NULL DEFAULT '',
    provider           TEXT NOT NULL DEFAULT '',
    model              TEXT NOT NULL DEFAULT '',
    error_kind         TEXT NOT NULL DEFAULT '',
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_meeting_summary_batches_session
    ON meeting_summary_batches(session_id, level, start_segment_id);
