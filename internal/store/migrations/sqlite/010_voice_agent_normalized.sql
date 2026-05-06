CREATE TABLE IF NOT EXISTS voice_agent_session_turns (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    turn_index INTEGER NOT NULL,
    role       TEXT NOT NULL,
    text       TEXT NOT NULL,
    created_at DATETIME,
    FOREIGN KEY(session_id) REFERENCES voice_agent_sessions(id) ON DELETE CASCADE,
    UNIQUE(session_id, turn_index)
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_session_turns_session
    ON voice_agent_session_turns(session_id, turn_index);

CREATE TABLE IF NOT EXISTS voice_agent_session_summary_items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    item_type  TEXT NOT NULL,
    item_index INTEGER NOT NULL,
    text       TEXT NOT NULL,
    FOREIGN KEY(session_id) REFERENCES voice_agent_sessions(id) ON DELETE CASCADE,
    UNIQUE(session_id, item_type, item_index)
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_session_summary_items_session
    ON voice_agent_session_summary_items(session_id, item_type, item_index);

