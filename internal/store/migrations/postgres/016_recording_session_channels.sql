ALTER TABLE recording_session_segments
    ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS speaker TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_recording_session_segments_timeline
    ON recording_session_segments(session_id, started_ms, id);
