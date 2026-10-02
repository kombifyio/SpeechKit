CREATE TABLE IF NOT EXISTS voice_agent_session_turns (
    id         BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES voice_agent_sessions(id) ON DELETE CASCADE,
    turn_index BIGINT NOT NULL,
    role       TEXT NOT NULL,
    text       TEXT NOT NULL,
    created_at TIMESTAMPTZ,
    UNIQUE(session_id, turn_index)
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_session_turns_session
    ON voice_agent_session_turns(session_id, turn_index);

CREATE TABLE IF NOT EXISTS voice_agent_session_summary_items (
    id         BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES voice_agent_sessions(id) ON DELETE CASCADE,
    item_type  TEXT NOT NULL,
    item_index BIGINT NOT NULL,
    text       TEXT NOT NULL,
    UNIQUE(session_id, item_type, item_index)
);

CREATE INDEX IF NOT EXISTS idx_voice_agent_session_summary_items_session
    ON voice_agent_session_summary_items(session_id, item_type, item_index);

