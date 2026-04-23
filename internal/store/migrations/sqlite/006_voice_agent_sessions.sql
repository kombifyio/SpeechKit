CREATE TABLE IF NOT EXISTS voice_agent_sessions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    title               TEXT NOT NULL DEFAULT '',
    summary             TEXT NOT NULL,
    raw_summary         TEXT NOT NULL DEFAULT '',
    transcript          TEXT NOT NULL DEFAULT '',
    language            TEXT NOT NULL DEFAULT '',
    provider_profile_id TEXT NOT NULL DEFAULT '',
    runtime_kind        TEXT NOT NULL DEFAULT '',
    turns_json          TEXT NOT NULL DEFAULT '[]',
    ideas_json          TEXT NOT NULL DEFAULT '[]',
    decisions_json      TEXT NOT NULL DEFAULT '[]',
    open_questions_json TEXT NOT NULL DEFAULT '[]',
    next_steps_json     TEXT NOT NULL DEFAULT '[]',
    started_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_sessions_created_at
    ON voice_agent_sessions(created_at DESC, id DESC);
