CREATE TABLE IF NOT EXISTS recording_sessions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id        INTEGER NOT NULL DEFAULT 1 REFERENCES storage_scopes(id) ON DELETE CASCADE,
    external_id     TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL DEFAULT 'dictation',
    status          TEXT NOT NULL DEFAULT 'active',
    title           TEXT NOT NULL DEFAULT '',
    language        TEXT NOT NULL DEFAULT '',
    language_base   TEXT NOT NULL DEFAULT '',
    provider        TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL DEFAULT '',
    input_source    TEXT NOT NULL DEFAULT '',
    processing_mode TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL DEFAULT '',
    started_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at        DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    owner_user_id   TEXT NOT NULL DEFAULT '',
    owner_org_id    TEXT NOT NULL DEFAULT '',
    owner_source    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_recording_sessions_scope_created
    ON recording_sessions(scope_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_recording_sessions_scope_status
    ON recording_sessions(scope_id, status, started_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS recording_session_segments (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id       INTEGER NOT NULL REFERENCES recording_sessions(id) ON DELETE CASCADE,
    segment_index    INTEGER NOT NULL,
    transcription_id INTEGER REFERENCES transcriptions(id) ON DELETE SET NULL,
    provider_item_id TEXT NOT NULL DEFAULT '',
    text             TEXT NOT NULL DEFAULT '',
    is_final         INTEGER NOT NULL DEFAULT 0,
    started_ms       INTEGER NOT NULL DEFAULT 0,
    ended_ms         INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(session_id, segment_index)
);

CREATE INDEX IF NOT EXISTS idx_recording_session_segments_session
    ON recording_session_segments(session_id, segment_index);

CREATE INDEX IF NOT EXISTS idx_recording_session_segments_transcription
    ON recording_session_segments(transcription_id);
