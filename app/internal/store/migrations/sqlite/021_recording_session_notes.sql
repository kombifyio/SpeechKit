-- The notes the user types themselves during a meeting. They are the anchors
-- the enhancement builds around, so they are stored verbatim and separately
-- from anything a model produces: blocks_json keeps each line with the moment
-- it was written, which is what ties a note to the part of the conversation it
-- was about.
CREATE TABLE IF NOT EXISTS recording_session_notes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  INTEGER NOT NULL UNIQUE REFERENCES recording_sessions(id) ON DELETE CASCADE,
    content_md  TEXT NOT NULL DEFAULT '',
    blocks_json TEXT NOT NULL DEFAULT '[]',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
